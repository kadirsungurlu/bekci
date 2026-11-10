package lisans

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// VarsayilanAdres lisans sunucusunun varsayılan adresidir.
const VarsayilanAdres = "https://lisans.bekci.app"

const (
	istemciZamanAsimi = 20 * time.Second
	govdeSiniri       = 64 << 10
)

// ErrUlasilamiyor ağ hatası ya da 5xx yanıtında döner (sarmalanmış).
var ErrUlasilamiyor = errors.New("lisans sunucusuna ulaşılamıyor")

// RetHatasi lisans sunucusunun 4xx kesin reddidir.
type RetHatasi struct {
	Kod   string
	Mesaj string
	HTTP  int
}

func (e *RetHatasi) Error() string {
	if e.Mesaj != "" {
		return fmt.Sprintf("lisans sunucusu reddetti (%d %s): %s", e.HTTP, e.Kod, e.Mesaj)
	}
	return fmt.Sprintf("lisans sunucusu reddetti (%d %s)", e.HTTP, e.Kod)
}

// KesinRet yenilemede kirayı hemen sildiren retler mi (iptal|bilinmeyen|cihaz).
func KesinRet(err error) bool {
	var r *RetHatasi
	if !errors.As(err, &r) {
		return false
	}
	switch r.Kod {
	case "iptal", "bilinmeyen", "cihaz":
		return true
	}
	return false
}

// Istek POST /v1/kira gövdesidir; izleme verisi asla gönderilmez.
type Istek struct {
	Lisans       string `json:"lisans"`
	Panel        string `json:"panel"`
	Cihaz        string `json:"cihaz,omitempty"`
	Surum        string `json:"surum"`
	SunucuSayisi int    `json:"sunucu_sayisi"`
}

// Istemci lisans sunucusu istemcisidir.
type Istemci struct {
	Adres string
	HTTP  *http.Client
}

// Adres yenileme adresini seçer: anahtardaki yenileme_adresi > LISANS_URL > varsayılan.
func Adres(a *Anahtar) string {
	if a != nil && strings.TrimSpace(a.YenilemeAdresi) != "" {
		return strings.TrimRight(strings.TrimSpace(a.YenilemeAdresi), "/")
	}
	if u := strings.TrimSpace(os.Getenv("LISANS_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	return VarsayilanAdres
}

// KiraIste kira ister; 200'de kira metnini döndürür (doğrulama çağıranın işidir).
func (c *Istemci) KiraIste(ctx context.Context, ist Istek) (string, error) {
	adres := strings.TrimRight(c.Adres, "/")
	if adres == "" {
		adres = VarsayilanAdres
	}
	govde, err := json.Marshal(ist)
	if err != nil {
		return "", err
	}
	ctx, iptal := context.WithTimeout(ctx, istemciZamanAsimi)
	defer iptal()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, adres+"/v1/kira", bytes.NewReader(govde))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: istemciZamanAsimi}
	}
	res, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUlasilamiyor, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, govdeSiniri+1))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUlasilamiyor, err)
	}
	if len(b) > govdeSiniri {
		return "", fmt.Errorf("%w: yanıt gövdesi çok büyük", ErrUlasilamiyor)
	}
	switch {
	case res.StatusCode == http.StatusOK:
		var y struct {
			Kira string `json:"kira"`
		}
		if err := json.Unmarshal(b, &y); err != nil || strings.TrimSpace(y.Kira) == "" {
			return "", fmt.Errorf("%w: geçersiz yanıt", ErrUlasilamiyor)
		}
		return strings.TrimSpace(y.Kira), nil
	case res.StatusCode >= 400 && res.StatusCode < 500:
		var y struct {
			Kod   string `json:"kod"`
			Mesaj string `json:"mesaj"`
		}
		_ = json.Unmarshal(b, &y)
		return "", &RetHatasi{Kod: y.Kod, Mesaj: y.Mesaj, HTTP: res.StatusCode}
	default:
		return "", fmt.Errorf("%w: HTTP %d", ErrUlasilamiyor, res.StatusCode)
	}
}
