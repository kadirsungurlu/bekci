package notify

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

// Test edilebilirlik için değiştirilebilir. LINE Notify kapatıldığından
// Messaging API'nin push mesaj uç noktası kullanılır.
var lineAPI = "https://api.line.me/v2/bot/message/push"

func init() { Register("line", line{}) }

type lineConfig struct {
	ChannelAccessToken string `json:"channel_access_token"`
	To                 string `json:"to"` // kullanıcı veya grup ID'si (U.../C.../R...)
}

var lineTarget = regexp.MustCompile(`^[UCR][0-9a-f]{32}$`)

type line struct{}

func (line) Secrets() []string { return []string{"channel_access_token"} }

func (line) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c lineConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	if err := required("Kanal erişim jetonu", c.ChannelAccessToken); err != nil {
		return nil, err
	}
	c.To = strings.TrimSpace(c.To)
	if !lineTarget.MatchString(c.To) {
		return nil, invalid("Alıcı ID'si U/C/R ile başlayan 33 karakterlik LINE kimliği olmalı")
	}
	return encode(c), nil
}

func (line) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c lineConfig
	json.Unmarshal(raw, &c)
	text := ev.Text()
	if r := []rune(text); len(r) > 4900 {
		text = string(r[:4900]) + "…"
	}
	payload := map[string]any{
		"to":       c.To,
		"messages": []map[string]string{{"type": "text", "text": text}},
	}
	return postJSON(ctx, lineAPI, payload, map[string]string{"Authorization": "Bearer " + c.ChannelAccessToken})
}
