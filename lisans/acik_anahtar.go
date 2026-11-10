// Package lisans, Bekci Pro lisans anahtarı ve kirasının doğrulanmasını, cihaz kimliğini
// ve saf değerlendirmeyi içerir (sözleşme: docs/LISANS-TASARIM.md). Yalnız açık anahtar gömülüdür;
// gizli anahtar bu pakete girmez.
package lisans

import (
	"crypto/ed25519"
	"encoding/base64"
	"runtime/debug"
	"strings"
	"time"
)

// AcikAnahtarlar imza doğrulamada kabul edilen açık anahtarlardır (anahtar döndürmede birden çok olabilir).
var AcikAnahtarlar = []ed25519.PublicKey{
	mustAcik("UOPp3Z4oULx5oM6q16BDukyPK3WXWObnoMx4OjQIaSo="),
}

// DerlemeZamani Pro imajında ldflags ile verilir:
// -X github.com/kadirsungurlu/bekci/lisans.DerlemeZamani=<RFC3339>
var DerlemeZamani string

// ProDerleme, Pro modülü kendini işaretleyince (kanca.ProDerleme) true olur.
var ProDerleme bool

func mustAcik(b64 string) ed25519.PublicKey {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("lisans: gömülü açık anahtar bozuk")
	}
	return ed25519.PublicKey(b)
}

// Derleme derleme zamanını döndürür: önce DerlemeZamani, yoksa VCS zamanı; ikisi de yoksa sıfır (bilinmiyor).
func Derleme() time.Time {
	if s := strings.TrimSpace(DerlemeZamani); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC()
		}
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, st := range bi.Settings {
			if st.Key == "vcs.time" {
				if t, err := time.Parse(time.RFC3339, st.Value); err == nil {
					return t.UTC()
				}
			}
		}
	}
	return time.Time{}
}
