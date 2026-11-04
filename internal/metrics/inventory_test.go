package metrics

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMaskSecrets(t *testing.T) {
	cases := []struct{ in, gizli, kalan string }{
		{"app --password=abc123 --port 80", "abc123", "--port 80"},
		{"app --password hunter2 -v", "hunter2", "-v"},
		{"redis-server --requirepass s3cr3t", "s3cr3t", "redis-server"},
		{"env DB_PASSWORD=xyz API_KEY=k1 PATH=/bin", "xyz", "PATH=/bin"},
		{"env DB_PASSWORD=xyz API_KEY=k1", "k1", "env"},
		{"mysql -uroot -pGizli1 db", "Gizli1", "-uroot"},
		{"mysqldump -p1234 db", "1234", "mysqldump"},
		{"pg_dump postgres://u:pw99@db:5432/x", "pw99", "@db:5432/x"},
		{"curl -H 'Authorization: Bearer tok.en.1' http://x", "tok.en.1", "http://x"},
		{"svc --client-secret 'iki kelime' --ok", "iki kelime", "--ok"},
		{"app --token=\"a b\"", "a b", "--token="},
	}
	for _, c := range cases {
		got := MaskSecrets(c.in)
		if strings.Contains(got, c.gizli) || !strings.Contains(got, c.kalan) {
			t.Errorf("%q -> %q", c.in, got)
		}
	}
	// Gizli olmayan komutlara dokunulmaz.
	for _, s := range []string{"nginx -g daemon off;", "docker run -p 8080:80 x", "node server.js --port 3000", "/usr/bin/caddy run"} {
		if got := MaskSecrets(s); got != s {
			t.Errorf("%q bozuldu: %q", s, got)
		}
	}
}

func TestParseProcNet(t *testing.T) {
	tcp := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 111 1 0 100 0 0 10 0
   1: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 222 1 0 100 0 0 10 0
   2: 0100007F:C350 0100007F:0050 01 00000000:00000000 00:00000000 00000000     0        0 333 1 0 100 0 0 10 0
`
	got := parseProcNet(tcp, "tcp")
	if len(got) != 2 || got[0].addr != "127.0.0.1" || got[0].port != 8080 || got[1].addr != "0.0.0.0" || got[1].port != 80 || got[1].inode != "222" {
		t.Fatalf("tcp: %+v", got)
	}
	tcp6 := "   0: 00000000000000000000000000000000:01BB 00000000000000000000000000000000:0000 0A 0:0 0:0 0 0 0 444 1 0\n"
	if g := parseProcNet(tcp6, "tcp"); len(g) != 1 || g[0].addr != "::" || g[0].port != 443 {
		t.Fatalf("tcp6: %+v", g)
	}
	udp := "   0: 00000000:0035 00000000:0000 07 0:0 0:0 0 0 0 555 2 0\n   1: 00000000:D431 00000000:0000 07 0:0 0:0 0 0 0 556 2 0\n"
	if g := parseProcNet(udp, "udp"); len(g) != 1 || g[0].port != 53 {
		t.Fatalf("udp (geçici port atlanmalı): %+v", g)
	}
}

func makeCert(t *testing.T, cn string, isCA bool, until time.Time) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
		DNSNames: []string{cn}, NotBefore: time.Now().Add(-time.Hour), NotAfter: until, IsCA: isCA, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
}

func TestLocalCerts(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "etc/letsencrypt/live/a.example")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(10 * 24 * time.Hour).Truncate(time.Second)
	leaf, key := makeCert(t, "a.example", false, until)
	ca, _ := makeCert(t, "Bir CA", true, until)
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("cert.pem", leaf)
	write("fullchain.pem", append(append([]byte{}, leaf...), ca...))
	write("chain.pem", ca)
	write("privkey.pem", key)
	write("birlesik.pem", append(append([]byte{}, key...), leaf...)) // anahtar + sertifika aynı dosyada
	c := NewTestInventoryCollector(root)
	got := c.localCerts(context.Background())
	if len(got) != 1 || got[0].Subject != "a.example" || got[0].Location != "/etc/letsencrypt/live/a.example/birlesik.pem" && got[0].Location != "/etc/letsencrypt/live/a.example/cert.pem" {
		t.Fatalf("sertifikalar: %+v", got)
	}
	if got[0].NotAfter != until.Unix() || got[0].Source != "file" || len(got[0].Fingerprint) != 64 {
		t.Fatalf("alanlar: %+v", got[0])
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "PRIVATE") {
		t.Fatal("özel anahtar sızdı")
	}
}

func TestPortCerts(t *testing.T) {
	until := time.Now().Add(5 * 24 * time.Hour).Truncate(time.Second)
	cp, kp := makeCert(t, "tls.example", false, until)
	pair, err := tls.X509KeyPair(cp, kp)
	if err != nil {
		t.Fatal(err)
	}
	l, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepts := make(chan struct{}, 8)
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			accepts <- struct{}{}
			go func() { conn.(*tls.Conn).Handshake(); conn.Close() }()
		}
	}()
	plain, _ := net.Listen("tcp", "127.0.0.1:0") // TLS konuşmayan port
	defer plain.Close()
	go func() {
		for {
			conn, err := plain.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	port := func(l net.Listener) int { return l.Addr().(*net.TCPAddr).Port }
	c := NewTestInventoryCollector(t.TempDir())
	ports := []InvPort{{Proto: "tcp", Addr: "127.0.0.1", Port: port(l)}, {Proto: "tcp", Addr: "127.0.0.1", Port: port(plain)},
		{Proto: "udp", Addr: "127.0.0.1", Port: 443}, {Proto: "tcp", Addr: "0.0.0.0", Port: 22}}
	got := c.portCerts(context.Background(), ports)
	if len(got) != 1 || got[0].Subject != "tls.example" || got[0].NotAfter != until.Unix() || got[0].Source != "port" {
		t.Fatalf("port sertifikası: %+v", got)
	}
	// İkinci tur önbellekten: servise yeniden bağlanılmaz, sonuç aynı.
	<-accepts
	if again := c.portCerts(context.Background(), ports); len(again) != 1 {
		t.Fatalf("önbellek: %+v", again)
	}
	select {
	case <-accepts:
		t.Fatal("önbellek süresi dolmadan yeniden bağlanıldı")
	case <-time.After(50 * time.Millisecond):
	}
	off := NewTestInventoryCollector(t.TempDir())
	off.opts.Getenv = func(k string) string {
		if k == "PROBE_TLS_SCAN" {
			return "off"
		}
		return ""
	}
	if g := off.portCerts(context.Background(), ports); len(g) != 0 {
		t.Fatalf("kapalıyken tarandı: %+v", g)
	}
}

// Docker yanıtında ortam değişkeni olsa da envantere girmez, komut maskelenir,
// yeniden başlama sayısı inspect'ten gelir.
func TestInventoryContainers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/containers/json" {
			w.Write([]byte(`[{"Id":"abcdef0123456789","Names":["/web"],"Image":"nginx:1","State":"running","Command":"app --password=gizli1","Env":["SECRET=env-sizinti"]}]`))
			return
		}
		w.Write([]byte(`{"RestartCount":7,"State":{"StartedAt":"2026-11-04T00:00:00Z","Health":{"Status":"healthy"}},"Config":{"Env":["SECRET=env-sizinti"]}}`))
	}))
	defer srv.Close()
	c := NewTestInventoryCollector(t.TempDir())
	c.docker = newDockerClient(srv.URL)
	got, note := c.inventoryContainers(context.Background())
	if note != "" || len(got) != 1 || got[0].Name != "web" || got[0].RestartCount != 7 || got[0].Health != "healthy" {
		t.Fatalf("konteyner: %q %+v", note, got)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "gizli1") || strings.Contains(string(b), "env-sizinti") {
		t.Fatalf("gizli bilgi: %s", b)
	}
	c.docker = nil
	if _, note := c.inventoryContainers(context.Background()); note != NoteNoDocker {
		t.Fatalf("not: %q", note)
	}
}

func TestInventoryHashAndSanitize(t *testing.T) {
	a := Inventory{Time: 1, Ports: []InvPort{{Proto: "tcp", Addr: "0.0.0.0", Port: 443}, {Proto: "tcp", Addr: "0.0.0.0", Port: 80, Cmd: "x --password=q"}}}
	b := Inventory{Time: 2, Ports: []InvPort{{Proto: "tcp", Addr: "0.0.0.0", Port: 80, Cmd: "x --password=q"}, {Proto: "tcp", Addr: "0.0.0.0", Port: 443}}}
	a.Sanitize()
	b.Sanitize()
	if a.Hash() != b.Hash() {
		t.Fatal("sıra ve zaman özeti değiştirmemeli")
	}
	if a.Ports[0].Port != 80 || strings.Contains(a.Ports[0].Cmd, "=q") {
		t.Fatalf("temizlik: %+v", a.Ports[0])
	}
	b.Ports[1].Process = "nginx"
	if a.Hash() == b.Hash() {
		t.Fatal("değişiklik özeti değiştirmeli")
	}
	big := Inventory{}
	for i := range MaxInventoryItems + 5 {
		big.Services = append(big.Services, InvService{Name: strconv.Itoa(i), Description: strings.Repeat("ş", 1000)})
	}
	big.Sanitize()
	if len(big.Services) != MaxInventoryItems || len(big.Services[0].Description) > maxInvText {
		t.Fatalf("sınır: %d %d", len(big.Services), len(big.Services[0].Description))
	}
}

// NewTestInventoryCollector HOST_ROOT'u verilen dizin olan, kaynaksız toplayıcı.
func NewTestInventoryCollector(root string) *Collector {
	o := Options{Getenv: func(k string) string {
		if k == "HOST_ROOT" {
			return root
		}
		return ""
	}, Now: time.Now}
	return &Collector{opts: o}
}
