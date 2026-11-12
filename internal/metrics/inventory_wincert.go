package metrics

// Windows sertifika deposu (730 §D, iş 751): LocalMachine My + WebHosting
// depolarındaki sertifikalar envanterin certs bölümüne "store" kaynağıyla
// girer; böylece mevcut inv_certs + sertifika uyarıları Windows'ta da çalışır.
// Bu dosya platformdan bağımsız saf kısımdır (sınanabilir); depo ve bağlantı
// okuması inventory_wincert_windows.go'da.

import (
	"crypto/sha1"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"net/netip"
	"slices"
	"strconv"
	"strings"
)

// winStoreCert depodan okunan ham sertifika.
type winStoreCert struct {
	Store string // My | WebHosting
	DER   []byte
}

// winBinding sertifikanın kullanıldığı yer: Thumb SHA-1 parmak izi, Use
// görünen kullanım ("IIS 0.0.0.0:443", "IIS ornek.com:443", "MSSQL SQLEXPRESS").
type winBinding struct {
	Thumb string
	Use   string
}

// normThumb parmak izini karşılaştırılabilir biçime getirir: onaltılık
// olmayan karakterler (boşluk, kayıt defterindeki görünmez işaretler) atılır.
func normThumb(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// serverCert sunucu sertifikası olabilir mi: CA değil; EKU varsa sunucu
// kimlik doğrulaması ya da "her amaç" içermeli (istemci/MDM sertifikaları elenir).
func serverCert(c *x509.Certificate) bool {
	if c.IsCA {
		return false
	}
	if len(c.ExtKeyUsage) == 0 {
		return true
	}
	return slices.Contains(c.ExtKeyUsage, x509.ExtKeyUsageServerAuth) || slices.Contains(c.ExtKeyUsage, x509.ExtKeyUsageAny)
}

// winStoreInv depo sertifikalarını envanter kaydına çevirir. Bir bağlantıda
// (IIS, MSSQL) kullanılan sertifika her durumda listelenir; kullanılmayanlardan
// yalnız sunucu sertifikaları. Konum "LocalMachine\<depo>\<PARMAK İZİ>" ve
// varsa kullanım yerleri; aynı sertifika iki depoda varsa ilk depo kalır.
func winStoreInv(certs []winStoreCert, binds []winBinding) []InvCert {
	uses := map[string][]string{}
	for _, b := range binds {
		t := normThumb(b.Thumb)
		if t != "" && b.Use != "" && !slices.Contains(uses[t], b.Use) {
			uses[t] = append(uses[t], b.Use)
		}
	}
	out := []InvCert{}
	seen := map[string]bool{}
	for _, sc := range certs {
		if len(out) >= MaxInventoryCerts {
			break
		}
		cert, err := x509.ParseCertificate(sc.DER)
		if err != nil {
			continue
		}
		sum := sha1.Sum(sc.DER)
		thumb := hex.EncodeToString(sum[:])
		u := uses[thumb]
		if len(u) == 0 && !serverCert(cert) {
			continue
		}
		ic := invCert(cert)
		if seen[ic.Fingerprint] {
			continue
		}
		seen[ic.Fingerprint] = true
		ic.Source = "store"
		ic.Location = `LocalMachine\` + sc.Store + `\` + strings.ToUpper(thumb)
		if len(u) > 0 {
			slices.Sort(u)
			ic.Location += " (" + strings.Join(u, ", ") + ")"
		}
		out = append(out, ic)
	}
	return out
}

// sockaddrString Windows SOCKADDR (AF_INET 2 / AF_INET6 23) baytlarını
// "adres:port" yapar; tanınmayan aile boş döner.
func sockaddrString(b []byte) string {
	if len(b) < 8 {
		return ""
	}
	port := strconv.Itoa(int(binary.BigEndian.Uint16(b[2:4])))
	switch binary.LittleEndian.Uint16(b[0:2]) {
	case 2:
		return netip.AddrFrom4([4]byte(b[4:8])).String() + ":" + port
	case 23:
		if len(b) < 24 {
			return ""
		}
		return "[" + netip.AddrFrom16([16]byte(b[8:24])).String() + "]:" + port
	}
	return ""
}
