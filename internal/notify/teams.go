package notify

import (
	"context"
	"encoding/json"
	"strings"
)

func init() { Register("teams", teams{}) }

// Microsoft Teams: eski O365 Connector bağlayıcısı Microsoft tarafından
// kapatıldığından burada Power Automate (Workflows) webhook adresine
// Adaptive Card gönderilir.
type teams struct{}

func (teams) Secrets() []string { return []string{"webhook_url"} }

func (teams) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	return normalizeWebhookURL(raw)
}

func (teams) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c webhookURLConfig
	json.Unmarshal(raw, &c)
	color := "good"
	if ev.IsProblem() {
		color = "attention"
	}
	title := strings.TrimSpace(strings.TrimLeft(ev.Title(), "🔴🟢🟡⚠️✅ "))
	card := map[string]any{
		"type": "message",
		"attachments": []map[string]any{{
			"contentType": "application/vnd.microsoft.card.adaptive",
			"content": map[string]any{
				"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
				"type":    "AdaptiveCard",
				"version": "1.4",
				"body": []map[string]any{
					{"type": "TextBlock", "text": title, "weight": "Bolder", "size": "Medium", "wrap": true, "color": color},
					{"type": "TextBlock", "text": ev.Body(), "wrap": true, "isSubtle": true},
				},
			},
		}},
	}
	return postJSON(ctx, c.WebhookURL, card, nil)
}
