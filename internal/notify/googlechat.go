package notify

import (
	"context"
	"encoding/json"
)

func init() { Register("googlechat", googlechat{}) }

// Google Chat: alan (space) için oluşturulan gelen webhook adresi.
type googlechat struct{}

func (googlechat) Secrets() []string { return []string{"webhook_url"} }

func (googlechat) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	return normalizeWebhookURL(raw)
}

func (googlechat) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c webhookURLConfig
	json.Unmarshal(raw, &c)
	return postJSON(ctx, c.WebhookURL, map[string]any{"text": ev.Text()}, nil)
}
