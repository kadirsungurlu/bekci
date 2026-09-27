package notify

import (
	"context"
	"encoding/json"
	"strings"
)

// Test edilebilirlik için değiştirilebilir.
var pushbulletAPI = "https://api.pushbullet.com/v2/pushes"

func init() { Register("pushbullet", pushbullet{}) }

type pushbulletConfig struct {
	AccessToken string `json:"access_token"`
	ChannelTag  string `json:"channel_tag"`
	DeviceIden  string `json:"device_iden"`
}

type pushbullet struct{}

func (pushbullet) Secrets() []string { return []string{"access_token"} }

func (pushbullet) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c pushbulletConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	if err := required("Erişim jetonu (access token)", c.AccessToken); err != nil {
		return nil, err
	}
	c.ChannelTag = strings.TrimSpace(c.ChannelTag)
	c.DeviceIden = strings.TrimSpace(c.DeviceIden)
	if c.ChannelTag != "" && c.DeviceIden != "" {
		return nil, invalid("Kanal etiketi ve cihaz ID'sinden yalnızca biri belirtilebilir")
	}
	return encode(c), nil
}

func (pushbullet) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c pushbulletConfig
	json.Unmarshal(raw, &c)
	payload := map[string]any{"type": "note", "title": ev.Title(), "body": ev.Text()}
	if c.ChannelTag != "" {
		payload["channel_tag"] = c.ChannelTag
	}
	if c.DeviceIden != "" {
		payload["device_iden"] = c.DeviceIden
	}
	return postJSON(ctx, pushbulletAPI, payload, map[string]string{"Access-Token": c.AccessToken})
}
