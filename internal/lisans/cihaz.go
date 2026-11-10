package lisans

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// cihazKaynak bir product_uuid + machine-id dosya çiftidir.
type cihazKaynak struct{ uuid, mid string }

// CihazKimligi makineden cihaz kimliğini türetir (DB'de tutulmaz). Kaynak sırası:
// $BEKCI_CIHAZ_DIZIN, /run/bekci-host, /sys/class/dmi/id/product_uuid + /etc/machine-id.
func CihazKimligi() (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("cihaz kimliği bu işletim sisteminde (%s) desteklenmiyor", runtime.GOOS)
	}
	var k []cihazKaynak
	if d := strings.TrimSpace(os.Getenv("BEKCI_CIHAZ_DIZIN")); d != "" {
		k = append(k, dizinKaynak(d))
	}
	k = append(k, dizinKaynak("/run/bekci-host"),
		cihazKaynak{uuid: "/sys/class/dmi/id/product_uuid", mid: "/etc/machine-id"})
	return cihazKimligiKaynaklarla(k)
}

func dizinKaynak(d string) cihazKaynak {
	return cihazKaynak{uuid: filepath.Join(d, "product_uuid"), mid: filepath.Join(d, "machine-id")}
}

// cihazKimligiDizinlerle her dizinde product_uuid + machine-id arar (test için).
func cihazKimligiDizinlerle(dizinler ...string) (string, error) {
	k := make([]cihazKaynak, 0, len(dizinler))
	for _, d := range dizinler {
		k = append(k, dizinKaynak(d))
	}
	return cihazKimligiKaynaklarla(k)
}

func oku(yol string) (string, error) {
	b, err := os.ReadFile(yol)
	if err != nil {
		return "", err
	}
	s := strings.ToLower(strings.TrimSpace(string(b)))
	if s == "" {
		return "", errors.New("dosya boş")
	}
	return s, nil
}

func cihazKimligiKaynaklarla(kaynaklar []cihazKaynak) (string, error) {
	var eksik []string
	for _, k := range kaynaklar {
		u, err1 := oku(k.uuid)
		m, err2 := oku(k.mid)
		if err1 == nil && err2 == nil {
			return cihazFormul(u, m), nil
		}
		if err1 != nil {
			eksik = append(eksik, fmt.Sprintf("%s (%v)", k.uuid, err1))
		}
		if err2 != nil {
			eksik = append(eksik, fmt.Sprintf("%s (%v)", k.mid, err2))
		}
	}
	return "", fmt.Errorf("cihaz kimliği okunamadı; okunamayan dosyalar: %s. Docker'da ana makinenin "+
		"/sys/class/dmi/id/product_uuid ve /etc/machine-id dosyalarını /run/bekci-host/ altına salt okunur bağlayın",
		strings.Join(eksik, ", "))
}

// cihazFormul: hex(sha256("bekci-cihaz-v1\n" + product_uuid + "\n" + machine_id))[:32].
func cihazFormul(uuid, mid string) string {
	h := sha256.Sum256([]byte("bekci-cihaz-v1\n" + uuid + "\n" + mid))
	return hex.EncodeToString(h[:])[:32]
}
