package notify

import (
	"context"
	"encoding/json"
	"strings"
)

func init() { Register("bark", bark{}) }

type barkConfig struct {
	Server    string `json:"server"`
	DeviceKey string `json:"device_key"`
	Sound     string `json:"sound"`
	Group     string `json:"group"`
}

type bark struct{}

func (bark) Secrets() []string { return []string{"device_key"} }

func (bark) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c barkConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Server = strings.TrimRight(strings.TrimSpace(c.Server), "/")
	if c.Server == "" {
		c.Server = "https://api.day.app"
	}
	if err := validURL("Sunucu adresi", c.Server); err != nil {
		return nil, err
	}
	if err := required("Cihaz anahtarı (device key)", c.DeviceKey); err != nil {
		return nil, err
	}
	c.Sound = strings.TrimSpace(c.Sound)
	c.Group = strings.TrimSpace(c.Group)
	if c.Group == "" {
		c.Group = "Uptime"
	}
	return encode(c), nil
}

func (bark) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c barkConfig
	json.Unmarshal(raw, &c)
	payload := map[string]any{
		"title":      ev.Title(),
		"body":       ev.Text(),
		"device_key": c.DeviceKey,
		"group":      c.Group,
	}
	if c.Sound != "" {
		payload["sound"] = c.Sound
	}
	if ev.IsProblem() {
		payload["level"] = "critical"
	}
	if u := ev.DetailURL(); u != "" {
		payload["url"] = u
	}
	return postJSON(ctx, c.Server+"/push", payload, nil)
}
