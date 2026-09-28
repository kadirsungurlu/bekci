package notify

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/brand"
)

func init() { Register("rocketchat", rocketchat{}) }

type rocketchatConfig struct {
	WebhookURL string `json:"webhook_url"`
	Channel    string `json:"channel"`
	Alias      string `json:"alias"` // görünen gönderen adı
	Avatar     string `json:"avatar"`
}

type rocketchat struct{}

func (rocketchat) Secrets() []string { return []string{"webhook_url"} }

func (rocketchat) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c rocketchatConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.WebhookURL = strings.TrimSpace(c.WebhookURL)
	if err := validURL("Webhook adresi", c.WebhookURL); err != nil {
		return nil, err
	}
	c.Channel = strings.TrimSpace(c.Channel)
	c.Alias = strings.TrimSpace(c.Alias)
	if c.Alias == "" {
		c.Alias = brand.Name
	}
	c.Avatar = strings.TrimSpace(c.Avatar)
	if c.Avatar != "" {
		if err := validURL("Avatar adresi", c.Avatar); err != nil {
			return nil, err
		}
	}
	return encode(c), nil
}

func (rocketchat) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c rocketchatConfig
	json.Unmarshal(raw, &c)
	p := map[string]any{"text": ev.Text(), "alias": c.Alias}
	if c.Channel != "" {
		p["channel"] = c.Channel
	}
	if c.Avatar != "" {
		p["avatar"] = c.Avatar
	}
	return postJSON(ctx, c.WebhookURL, p, nil)
}
