package notify

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

func init() { Register("homeassistant", homeassistant{}) }

type homeassistantConfig struct {
	URL     string `json:"url"`
	Token   string `json:"token"`   // uzun ömürlü erişim jetonu
	Service string `json:"service"` // ör. mobile_app_kadir_iphone
}

var haServiceName = regexp.MustCompile(`^[a-z0-9_]+$`)

type homeassistant struct{}

func (homeassistant) Secrets() []string { return []string{"token"} }

func (homeassistant) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c homeassistantConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.URL = strings.TrimRight(strings.TrimSpace(c.URL), "/")
	if err := validURL("Home Assistant adresi", c.URL); err != nil {
		return nil, err
	}
	if err := required("Erişim jetonu", c.Token); err != nil {
		return nil, err
	}
	c.Service = strings.TrimSpace(c.Service)
	if !haServiceName.MatchString(c.Service) {
		return nil, invalid("Bildirim servisi küçük harf, rakam ve alt çizgiden oluşmalı (ör. mobile_app_telefon)")
	}
	return encode(c), nil
}

func (homeassistant) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c homeassistantConfig
	json.Unmarshal(raw, &c)
	payload := map[string]any{"title": ev.Title(), "message": ev.Text()}
	return postJSON(ctx, c.URL+"/api/services/notify/"+c.Service, payload,
		map[string]string{"Authorization": "Bearer " + c.Token})
}
