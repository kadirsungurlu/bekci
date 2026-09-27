package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

func init() { Register("matrix", matrix{}) }

type matrixConfig struct {
	HomeserverURL string `json:"homeserver_url"`
	AccessToken   string `json:"access_token"`
	RoomID        string `json:"room_id"`
}

var matrixRoomID = regexp.MustCompile(`^![^:\s]+:\S+$`)

type matrix struct{}

func (matrix) Secrets() []string { return []string{"access_token"} }

func (matrix) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c matrixConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.HomeserverURL = strings.TrimRight(strings.TrimSpace(c.HomeserverURL), "/")
	if err := validURL("Sunucu adresi", c.HomeserverURL); err != nil {
		return nil, err
	}
	if err := required("Erişim jetonu (access token)", c.AccessToken); err != nil {
		return nil, err
	}
	c.RoomID = strings.TrimSpace(c.RoomID)
	if !matrixRoomID.MatchString(c.RoomID) {
		return nil, invalid("Oda ID'si \"!oda:sunucu\" biçiminde olmalı")
	}
	return encode(c), nil
}

var matrixTxnCounter uint64

func nextMatrixTxnID() string {
	n := atomic.AddUint64(&matrixTxnCounter, 1)
	return fmt.Sprintf("uptime-%d-%d", time.Now().UnixNano(), n)
}

func (matrix) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c matrixConfig
	json.Unmarshal(raw, &c)
	plain := ev.Text()
	formatted := strings.ReplaceAll(html.EscapeString(plain), "\n", "<br/>")
	body := map[string]any{
		"msgtype":        "m.text",
		"body":           plain,
		"format":         "org.matrix.custom.html",
		"formatted_body": formatted,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	u := c.HomeserverURL + "/_matrix/client/v3/rooms/" + url.PathEscape(c.RoomID) +
		"/send/m.room.message/" + url.PathEscape(nextMatrixTxnID())
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + c.AccessToken,
	}
	return doRequest(ctx, http.MethodPut, u, bytes.NewReader(b), headers)
}
