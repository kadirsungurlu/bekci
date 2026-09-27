package notify

import (
	"context"
	"encoding/json"
	"strings"
)

func init() { Register("apprise", apprise{}) }

// Apprise API sunucusu (https://github.com/caronc/apprise-api) üzerinden;
// 100'den fazla servise (Signal, WeChat, Matrix, ...) tek noktadan gönderim.
type appriseConfig struct {
	Server string `json:"server"`
	URLs   string `json:"urls"` // virgülle ayrılmış apprise URL listesi
}

type apprise struct{}

func (apprise) Secrets() []string { return []string{"urls"} }

func (apprise) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c appriseConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Server = strings.TrimRight(strings.TrimSpace(c.Server), "/")
	if err := validURL("Apprise sunucu adresi", c.Server); err != nil {
		return nil, err
	}
	list, err := splitNonEmptyCSV("Apprise URL'leri", c.URLs)
	if err != nil {
		return nil, err
	}
	c.URLs = strings.Join(list, ",")
	return encode(c), nil
}

func appriseType(ev Event) string {
	switch ev.Kind {
	case KindDown, KindReminder:
		return "failure"
	case KindCert:
		return "warning"
	default:
		return "success"
	}
}

func (apprise) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c appriseConfig
	json.Unmarshal(raw, &c)
	payload := map[string]any{
		"urls":  c.URLs,
		"title": ev.Title(),
		"body":  ev.Text(),
		"type":  appriseType(ev),
	}
	return postJSON(ctx, c.Server+"/notify", payload, nil)
}
