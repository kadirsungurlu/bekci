package denetim

import (
	"context"

	"github.com/kadirsungurlu/bekci/internal/eklenti"
)

func f64(v float64) *float64 { return &v }

// noop altyapı denetimi (743): hiçbir şeye bağlanmaz. Seçilen durumu döner;
// kimlik verilirse dosyayı okur (izin denetimi), hedef verilirse yalnız
// izin kuralını uygular. Ücretsiz; kurulumu ve uçtan uca akışı sınamak için.
func init() {
	Kaydet(Tur{
		Kind:     "noop",
		Interval: Aralik{Min: 60, Max: 86400, Default: 300},
		Label:    Metin{TR: "Boş denetim (sınama)", EN: "No-op check (test)"},
		Help: Metin{
			TR: "Hiçbir şeye bağlanmaz; seçilen durumu döner. Ajan, kimlik dosyası ve hedef kuralını sınamak için.",
			EN: "Connects to nothing and returns the chosen status. Use it to test the agent, credential file and target rule.",
		},
		Params: []Param{
			{Name: "status", Type: PSelect, Default: "ok", Label: Metin{TR: "Dönecek durum", EN: "Status to return"},
				Options: []Secenek{
					{Value: "ok", Label: Metin{TR: "Tamam", EN: "OK"}},
					{Value: "warning", Label: Metin{TR: "Uyarı", EN: "Warning"}},
					{Value: "critical", Label: Metin{TR: "Kritik", EN: "Critical"}},
					{Value: "unknown", Label: Metin{TR: "Bilinmiyor", EN: "Unknown"}},
				}},
			{Name: "message", Type: PString, MaxLen: 200, Label: Metin{TR: "İleti", EN: "Message"}},
			{Name: "value", Type: PInt, Default: 1, Min: f64(-1e9), Max: f64(1e9),
				Label: Metin{TR: "Metrik değeri", EN: "Metric value"},
				Help:  Metin{TR: "'value' metriği olarak kaydedilir.", EN: "Recorded as the 'value' metric."}},
			{Name: "target", Type: PTarget, Label: Metin{TR: "Hedef (bağlanılmaz)", EN: "Target (not contacted)"},
				Help: Metin{TR: "Yalnız loopback; başka ana bilgisayar kimlik dosyasının hosts listesinde olmalı.",
					EN: "Loopback only; other hosts must be listed in the credential file's hosts."}},
			{Name: "credential", Type: PCredential, Label: Metin{TR: "Kimlik dosyası adı", EN: "Credential file name"},
				Help: Metin{TR: "Ajanda kimlik.d/<ad>.json; içerik panele gönderilmez.",
					EN: "kimlik.d/<name>.json on the agent; the content is never sent to the panel."}},
		},
		Calistir: func(_ context.Context, g Girdi) eklenti.Result {
			msg := g.String("message")
			if msg == "" {
				msg = "noop"
			}
			if g.Kimlik != nil {
				msg += " (kimlik: " + g.String("credential") + ")"
			}
			return eklenti.Result{Status: eklenti.Status(g.String("status")), Message: msg,
				Metrics: []eklenti.Metric{{Name: "value", Value: float64(g.Int("value"))}}}
		},
	})
}
