package check

import (
	"context"
	"encoding/json"
)

// pushChecker pasif tiptir: hedef sistem /api/push/{token} adresine sinyal
// gönderir, motor belirtilen aralıkta sinyal gelmezse monitörü DOWN yapar.
type pushChecker struct{}

func init() { Register(TypePush, pushChecker{}) }

// PushConfig push monitörünün ayarı. GraceSec tolerans: sinyal beklenen
// aralığın üstüne bu kadar saniye daha beklenir, sonra "sinyal gelmedi"
// sayılır (ör. bazen 20, bazen 35 dakika süren yedek için aralık 30 dk +
// tolerans 15 dk). 0: tolerans yok (eski davranış).
type PushConfig struct {
	GraceSec int `json:"grace_sec,omitempty"`
}

// PushGraceMax toleransın üst sınırı (saniye; 24 saat).
const PushGraceMax = 86400

func (pushChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c PushConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	if c.GraceSec < 0 || c.GraceSec > PushGraceMax {
		return nil, invalid("Tolerans 0-86400 saniye arasında olmalı")
	}
	b, _ := json.Marshal(c)
	return b, nil
}

// PushGrace ayardaki tolerans (saniye; bozuk veya eski ayarda 0).
func PushGrace(raw json.RawMessage) int {
	var c PushConfig
	if len(raw) == 0 || json.Unmarshal(raw, &c) != nil || c.GraceSec < 0 {
		return 0
	}
	return c.GraceSec
}

func (pushChecker) Target(json.RawMessage) string { return "Push" }

func (pushChecker) Check(context.Context, json.RawMessage) Result {
	return down("Belirtilen sürede push sinyali gelmedi")
}
