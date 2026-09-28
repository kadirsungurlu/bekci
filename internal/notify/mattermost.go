package notify

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/brand"
)

func init() { Register("mattermost", mattermost{}) }

type mattermostConfig struct {
	WebhookURL string `json:"webhook_url"`
	Channel    string `json:"channel"`
	Username   string `json:"username"`
	IconURL    string `json:"icon_url"`
}

type mattermost struct{}

func (mattermost) Secrets() []string { return []string{"webhook_url"} }

func (mattermost) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c mattermostConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.WebhookURL = strings.TrimSpace(c.WebhookURL)
	if err := validURL("Webhook adresi", c.WebhookURL); err != nil {
		return nil, err
	}
	c.Channel = strings.TrimSpace(c.Channel)
	c.Username = strings.TrimSpace(c.Username)
	if c.Username == "" {
		c.Username = brand.Name
	}
	c.IconURL = strings.TrimSpace(c.IconURL)
	if c.IconURL != "" {
		if err := validURL("Simge adresi", c.IconURL); err != nil {
			return nil, err
		}
	}
	return encode(c), nil
}

func (mattermost) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c mattermostConfig
	json.Unmarshal(raw, &c)
	p := map[string]any{"text": ev.Text(), "username": c.Username}
	if c.Channel != "" {
		p["channel"] = c.Channel
	}
	if c.IconURL != "" {
		p["icon_url"] = c.IconURL
	}
	return postJSON(ctx, c.WebhookURL, p, nil)
}
