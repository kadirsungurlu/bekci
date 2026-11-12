package eklentiajan

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// Item panelin jobs yanıtındaki `checks` öğesi (B.4). Bu paket yalnız
// source=plugin öğelerini işler; yerleşik denetimler (743) ayrıdır.
// Panel öğeyi göndererek eklentiyi açar, göndermeyerek kapatır; aralığı seçer.
type Item struct {
	Name     string          `json:"name"`
	Source   string          `json:"source"`
	Interval int             `json:"interval"` // sn
	Manifest json.RawMessage `json:"manifest"` // imzalı zarf (680 §5)
	Body     string          `json:"body"`     // base64 gövde
	// Yerleşik denetim (source=builtin, 743): tür ve parametreler.
	Kind   string          `json:"kind,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Red kodları: panele `error` alanında gider, arayüz metne çevirir.
const (
	RedImza     = "signature"     // imzasız ya da güvenilmeyen anahtar
	RedManifest = "manifest"      // katı ayrıştırma/sınır hatası (bilinmeyen alan dahil)
	RedAd       = "name"          // öğe adı manifest kimliğiyle uyuşmuyor
	RedGovde    = "body"          // gövde özeti manifestle uyuşmuyor ya da çok büyük
	RedIgneYok  = "not_pinned"    // yerel iğne yok
	RedIgne     = "pin_mismatch"  // yerel iğne başka içeriği gösteriyor
	RedSure     = "expired"       // not_after geçmiş
	RedErken    = "not_yet_valid" // not_before gelmemiş
	RedAralik   = "interval"      // panelin aralığı manifest sınırının dışında
	RedKapali   = "disabled"      // yerel yapılandırma yok ya da bozuk
	RedPlatform = "platform"      // systemd >= 254 olan Linux değil
)

// RedError kararın reddi.
type RedError struct {
	Code string
	Msg  string
}

func (e *RedError) Error() string { return e.Code + ": " + e.Msg }

func red(code, f string, a ...any) error { return &RedError{Code: code, Msg: fmt.Sprintf(f, a...)} }

// Plan çalıştırmaya hazır, doğrulanmış eklenti.
type Plan struct {
	M        *Manifest
	Body     []byte
	Interval time.Duration
}

// Decide öğeyi yerel güven köküne göre doğrular. Sıra: imza (önce, ayrıştırma
// sonra) -> katı manifest -> ad -> gövde özeti -> geçerlilik -> iğne -> aralık.
// Her çalıştırmadan önce yeniden çağrılır: iğne kaldırılınca ya da süre
// dolunca bir sonraki tur çalışmaz.
func Decide(cfg *Config, it Item, now time.Time) (*Plan, error) {
	if cfg == nil {
		return nil, red(RedKapali, "yerel yapılandırma yok")
	}
	payload, err := VerifyEnvelope(it.Manifest, cfg.Keys())
	if err != nil {
		return nil, red(RedImza, "%v", err)
	}
	m, err := ParseManifest(payload)
	if err != nil {
		return nil, red(RedManifest, "%v", err)
	}
	if it.Name != m.ID {
		return nil, red(RedAd, "öğe %q, manifest %q", it.Name, m.ID)
	}
	body, err := base64.StdEncoding.Strict().DecodeString(it.Body)
	if err != nil || len(body) == 0 || len(body) > MaxBody {
		return nil, red(RedGovde, "gövde geçersiz ya da sınır dışı")
	}
	if Digest(body) != m.SHA256 {
		return nil, red(RedGovde, "gövde özeti manifestle uyuşmuyor")
	}
	switch t := now.Unix(); {
	case t >= m.NotAfter:
		return nil, red(RedSure, "not_after geçti")
	case t < m.NotBefore:
		return nil, red(RedErken, "not_before gelmedi")
	}
	switch pin := cfg.Pin(m.ID); {
	case pin == "":
		return nil, red(RedIgneYok, "sunucuda 'uptime betik eklenti izin %s %s' gerekli", m.ID, m.SHA256)
	case pin != m.SHA256:
		return nil, red(RedIgne, "iğne %s, içerik %s", pin, m.SHA256)
	}
	if it.Interval < m.IntervalMin || it.Interval > m.IntervalMax {
		return nil, red(RedAralik, "%d sn, izinli %d-%d", it.Interval, m.IntervalMin, m.IntervalMax)
	}
	return &Plan{M: m, Body: body, Interval: time.Duration(it.Interval) * time.Second}, nil
}
