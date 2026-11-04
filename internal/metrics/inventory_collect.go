package metrics

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// inventorySource işletim sistemine özgü bölümler (inventory_linux.go).
type inventorySource interface {
	services(ctx context.Context) ([]InvService, string) // ikinci değer: Note kodu
	ports(ctx context.Context) ([]InvPort, string)
}

// Inventory envanteri toplar. Bir bölüm toplanamazsa nedeni Notes'a yazılır;
// diğerleri etkilenmez. Metrik toplayıcıdan bağımsızdır, Collect ile yarışmaz
// (Docker istemcisi ve kaynak eşzamanlı kullanıma uygundur).
func (c *Collector) Inventory(ctx context.Context) Inventory {
	inv := Inventory{Time: c.opts.Now().UnixMilli(), Notes: map[string]string{},
		Services: []InvService{}, Containers: []InvContainer{}, Ports: []InvPort{}, Certs: []InvCert{}, Disks: []InvDisk{}}
	note := func(section, code string) {
		if code != "" {
			inv.Notes[section] = code
		}
	}
	src, ok := c.src.(inventorySource)
	switch {
	case c.reason != "" && c.src == nil:
		note(InvServices, NoteNoHost)
		note(InvPorts, NoteNoHost)
	case ok:
		var code string
		inv.Services, code = src.services(ctx)
		note(InvServices, code)
		inv.Ports, code = src.ports(ctx)
		note(InvPorts, code)
	default:
		note(InvServices, NoteUnsupported)
		note(InvPorts, NoteUnsupported)
	}
	var code string
	inv.Containers, code = c.inventoryContainers(ctx)
	note(InvContainers, code)

	if c.src != nil {
		if ms, err := c.src.mounts(ctx); err == nil {
			for _, d := range selectDisks(ms, c.src.usage) {
				inv.Disks = append(inv.Disks, InvDisk{Mount: d.Mount, Device: d.Device, FS: d.FS, Total: d.Total})
			}
		} else {
			note(InvDisks, NoteFailed)
		}
	} else {
		note(InvDisks, NoteNoHost)
	}

	if c.src == nil {
		note(InvCerts, NoteNoHost) // konteynerin kendi dosyaları host'unki değil
	} else {
		inv.Certs = append(c.localCerts(ctx), c.portCerts(ctx, inv.Ports)...)
	}

	inv.Sanitize()
	if len(inv.Notes) == 0 {
		inv.Notes = nil
	}
	return inv
}

// Konteynerler -----------------------------------------------------------------------------

// dockerInvItem liste yanıtının gereken alanları. Docker'ın Env alanı bilerek
// tanımlı değildir: çözülmez.
type dockerInvItem struct {
	ID      string   `json:"Id"`
	Names   []string `json:"Names"`
	Image   string   `json:"Image"`
	State   string   `json:"State"`
	Command string   `json:"Command"`
}

// dockerInspect yalnız gereken alanlar; Config.Env istenmez.
type dockerInspect struct {
	RestartCount int `json:"RestartCount"`
	State        struct {
		StartedAt string `json:"StartedAt"`
		Health    *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
}

func (c *Collector) inventoryContainers(ctx context.Context) ([]InvContainer, string) {
	d := c.docker
	out := []InvContainer{}
	if d == nil || (d.sock != "" && !fileExists(d.sock)) {
		return out, NoteNoDocker
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var list []dockerInvItem
	if err := d.get(ctx, "/containers/json?all=1", &list); err != nil {
		return out, NoteFailed
	}
	for i, it := range list {
		if i >= MaxInventoryItems {
			break
		}
		ic := InvContainer{ID: shortID(it.ID), Image: it.Image, State: it.State, Command: MaskSecrets(it.Command)}
		if len(it.Names) > 0 {
			ic.Name = strings.TrimPrefix(it.Names[0], "/")
		}
		var ins dockerInspect
		if d.get(ctx, "/containers/"+url.PathEscape(it.ID)+"/json", &ins) == nil {
			ic.RestartCount, ic.StartedAt = ins.RestartCount, ins.State.StartedAt
			if ins.State.Health != nil {
				ic.Health = ins.State.Health.Status
			}
		}
		out = append(out, ic)
	}
	return out, ""
}

// Sertifikalar: yerel dosyalar -------------------------------------------------------------

// defaultCertDirs sertifikaların aranacağı yerler (host kökünden). CA
// paketleri (/etc/ssl/certs) bilerek yok: taranmaz.
var defaultCertDirs = []string{
	"/etc/letsencrypt/live", "/etc/ssl/private", "/etc/nginx", "/etc/apache2", "/etc/httpd",
	"/etc/haproxy", "/etc/caddy", "/etc/traefik", "/var/lib/caddy",
}

// dockerCertGlob Caddy'nin Docker birimindeki sertifikaları.
const dockerCertGlob = "/var/lib/docker/volumes/*/_data/caddy/certificates"

const (
	maxCertFileSize = 256 << 10
	maxCertVisits   = 5000
)

func (c *Collector) certDirs(root string) []string {
	dirs := slices.Clone(defaultCertDirs)
	if m, err := filepath.Glob(root + dockerCertGlob); err == nil {
		for _, p := range m {
			dirs = append(dirs, strings.TrimPrefix(p, root))
		}
	}
	for _, d := range strings.FieldsFunc(c.opts.Getenv("PROBE_CERT_DIRS"), func(r rune) bool { return r == ':' || r == ',' }) {
		dirs = append(dirs, d)
	}
	return dirs
}

// localCerts yerel sertifika dosyalarını tarar. Yalnız CERTIFICATE blokları
// çözülür; özel anahtar bloklarının içeriği hiçbir yere kopyalanmaz, .key
// uzantılı dosyalar açılmaz. CA sertifikaları (zincir dosyaları) ve aynı
// sertifikanın ikinci kopyası (cert.pem + fullchain.pem) atlanır.
func (c *Collector) localCerts(ctx context.Context) []InvCert {
	root := strings.TrimRight(c.opts.Getenv("HOST_ROOT"), "/")
	out := []InvCert{}
	seen := map[string]bool{}
	visits := 0
	for _, dir := range c.certDirs(root) {
		_ = filepath.WalkDir(root+dir, func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				if e != nil && e.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			visits++
			if visits > maxCertVisits || len(out) >= MaxInventoryCerts || ctx.Err() != nil {
				return filepath.SkipAll
			}
			if e.IsDir() || !certFileName(e.Name()) {
				return nil
			}
			info, err := os.Stat(path) // bağlantıyı izler (letsencrypt/live)
			if err != nil || !info.Mode().IsRegular() || info.Size() > maxCertFileSize {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			cert := parseLeaf(data)
			if cert == nil {
				return nil
			}
			ic := invCert(cert)
			if seen[ic.Fingerprint] {
				return nil
			}
			seen[ic.Fingerprint] = true
			ic.Source, ic.Location = "file", strings.TrimPrefix(path, root)
			out = append(out, ic)
			return nil
		})
	}
	return out
}

func certFileName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pem", ".crt", ".cer":
		n := strings.ToLower(name)
		return !strings.Contains(n, "key") && !strings.Contains(n, "chain.") || strings.Contains(n, "fullchain")
	}
	return false
}

// parseLeaf PEM verisindeki ilk CA olmayan sertifika (yoksa nil).
func parseLeaf(data []byte) *x509.Certificate {
	var only *x509.Certificate // dosyadaki tek sertifika CA ise
	count := 0
	for len(data) > 0 {
		var b *pem.Block
		b, data = pem.Decode(data)
		if b == nil {
			break
		}
		if b.Type != "CERTIFICATE" {
			continue // özel anahtar vb.: içeriğe bakılmaz
		}
		count++
		cert, err := x509.ParseCertificate(b.Bytes)
		if err == nil && !cert.IsCA {
			return cert
		}
		if err == nil {
			only = cert
		}
	}
	// "openssl req -x509" varsayılanı CA:TRUE yazar: dosyada tek başına duran
	// kendinden imzalı sertifika sunucu sertifikasıdır, zincir dosyası değil.
	// Ara CA (Let's Encrypt chain.pem) kendinden imzalı olmadığı için atlanır.
	if count == 1 && only != nil && only.CheckSignatureFrom(only) == nil {
		return only
	}
	return nil
}

func invCert(cert *x509.Certificate) InvCert {
	sum := sha256.Sum256(cert.Raw)
	subject := cert.Subject.CommonName
	if subject == "" && len(cert.DNSNames) > 0 {
		subject = cert.DNSNames[0]
	}
	issuer := cert.Issuer.CommonName
	if issuer == "" && len(cert.Issuer.Organization) > 0 {
		issuer = cert.Issuer.Organization[0]
	}
	return InvCert{Subject: subject, SANs: cert.DNSNames, Issuer: issuer, NotBefore: cert.NotBefore.Unix(),
		NotAfter: cert.NotAfter.Unix(), Fingerprint: hex.EncodeToString(sum[:])}
}

// Sertifikalar: dinleyen TLS portları ------------------------------------------------------

// noTLSPorts TLS el sıkışması denenmeyen, TLS konuşmadığı bilinen portlar.
var noTLSPorts = map[int]bool{21: true, 22: true, 23: true, 25: true, 53: true, 80: true, 110: true, 111: true, 139: true,
	143: true, 445: true, 2049: true, 3306: true, 5432: true, 6379: true, 11211: true, 27017: true}

const (
	maxTLSPorts  = 64
	tlsDeadline  = 2 * time.Second
	tlsScanLimit = 30 * time.Second
	tlsHitTTL    = time.Hour     // TLS konuşan port: sertifika saatte bir yeniden okunur
	tlsMissTTL   = 6 * time.Hour // TLS konuşmayan port: nadiren denenir
)

type tlsCached struct {
	at   time.Time
	cert *InvCert
}

// portCerts dinleyen TCP portlarında TLS el sıkışması dener ve sertifikayı
// okur (yalnız yerel adrese; doğrulama yapılmaz, amaç bitiş tarihini görmek).
// Sonuçlar önbelleğe alınır: her turda her servise bağlanılmaz.
// PROBE_TLS_SCAN=off kapatır; PROBE_TLS_SNI virgüllü ad listesi (ada göre
// sertifika veren vekiller için) her TLS portunda ayrıca denenir.
func (c *Collector) portCerts(ctx context.Context, ports []InvPort) []InvCert {
	out := []InvCert{}
	if strings.EqualFold(c.opts.Getenv("PROBE_TLS_SCAN"), "off") {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, tlsScanLimit)
	defer cancel()
	var snis []string
	for _, n := range strings.Split(c.opts.Getenv("PROBE_TLS_SNI"), ",") {
		if n = strings.TrimSpace(n); n != "" {
			snis = append(snis, n)
		}
	}
	now := c.opts.Now()
	c.tlsMu.Lock()
	defer c.tlsMu.Unlock()
	if c.tlsCache == nil {
		c.tlsCache = map[string]tlsCached{}
	}
	tried := 0
	done := map[string]bool{}
	for _, p := range ports {
		if p.Proto != "tcp" || noTLSPorts[p.Port] || tried >= maxTLSPorts {
			continue
		}
		host := dialHost(p.Addr)
		addr := net.JoinHostPort(host, strconv.Itoa(p.Port))
		if done[addr] {
			continue
		}
		done[addr] = true
		tried++
		for _, sni := range append([]string{""}, snis...) {
			key := addr + "|" + sni
			hit, ok := c.tlsCache[key]
			if !ok || now.Sub(hit.at) > ttlOf(hit) {
				if ctx.Err() != nil {
					break
				}
				hit = tlsCached{at: now, cert: c.tlsDial(ctx, addr, sni)}
				c.tlsCache[key] = hit
			}
			if hit.cert == nil {
				continue
			}
			ic := *hit.cert
			ic.Source, ic.Location = "port", addr
			if sni != "" {
				ic.Location = addr + " (" + sni + ")"
			}
			out = append(out, ic)
		}
	}
	return out
}

func ttlOf(h tlsCached) time.Duration {
	if h.cert != nil {
		return tlsHitTTL
	}
	return tlsMissTTL
}

// dialHost dinleme adresinin bağlanılacak hali: joker adres yerel döngüye.
func dialHost(addr string) string {
	switch addr {
	case "", "0.0.0.0", "*":
		return "127.0.0.1"
	case "::":
		return "::1"
	}
	return addr
}

func (c *Collector) tlsDial(ctx context.Context, addr, sni string) *InvCert {
	dctx, cancel := context.WithTimeout(ctx, tlsDeadline)
	defer cancel()
	d := tls.Dialer{NetDialer: &net.Dialer{Timeout: tlsDeadline},
		Config: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS10, ServerName: sni}} //nolint:gosec // yalnız bitiş tarihi okunur
	conn, err := d.DialContext(dctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	tc, ok := conn.(*tls.Conn)
	if !ok {
		return nil
	}
	peers := tc.ConnectionState().PeerCertificates
	if len(peers) == 0 {
		return nil
	}
	ic := invCert(peers[0])
	return &ic
}
