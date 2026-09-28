package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/brand"
	"github.com/kadirsungurlu/bekci/internal/i18n"
)

// Test edilebilirlik için değiştirilebilir; gerçek adresler bölgeye göre seçilir.
var (
	opsgenieAPIUS = "https://api.opsgenie.com"
	opsgenieAPIEU = "https://api.eu.opsgenie.com"
)

func init() { Register("opsgenie", opsgenie{}) }

type opsgenieConfig struct {
	APIKey   string `json:"api_key"`
	Region   string `json:"region"`   // us | eu
	Priority string `json:"priority"` // P1-P5
}

var opsgeniePriority = regexp.MustCompile(`^P[1-5]$`)

type opsgenie struct{}

func (opsgenie) Secrets() []string { return []string{"api_key"} }

func (opsgenie) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c opsgenieConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.APIKey = strings.TrimSpace(c.APIKey)
	if err := required("API anahtarı", c.APIKey); err != nil {
		return nil, err
	}
	c.Region = strings.ToLower(strings.TrimSpace(c.Region))
	if c.Region == "" {
		c.Region = "us"
	}
	if c.Region != "us" && c.Region != "eu" {
		return nil, invalid("Bölge us veya eu olmalı")
	}
	c.Priority = strings.ToUpper(strings.TrimSpace(c.Priority))
	if c.Priority == "" {
		c.Priority = "P3"
	}
	if !opsgeniePriority.MatchString(c.Priority) {
		return nil, invalid("Öncelik P1-P5 arasında olmalı")
	}
	return encode(c), nil
}

func opsgenieBase(region string) string {
	if region == "eu" {
		return opsgenieAPIEU
	}
	return opsgenieAPIUS
}

func opsgenieAlias(ev Event) string { return ev.AlertKey() }

func (opsgenie) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c opsgenieConfig
	json.Unmarshal(raw, &c)
	base := opsgenieBase(c.Region)
	headers := map[string]string{"Authorization": "GenieKey " + c.APIKey}
	alias := opsgenieAlias(ev)

	if ev.IsRecovery() {
		u := base + "/v2/alerts/" + url.PathEscape(alias) + "/close?identifierType=alias"
		return doRequest(ctx, http.MethodPost, u, bytes.NewReader(encode(map[string]string{"note": i18n.T(ev.Lang, "notify.opsgenie.close_note", brand.Name)})), mergeHeaders(headers, map[string]string{"Content-Type": "application/json"}))
	}
	priority := c.Priority
	if ev.Kind == KindCert {
		priority = "P3"
	}
	payload := map[string]any{
		"message":     ev.Title(),
		"alias":       alias,
		"description": ev.Text(),
		"priority":    priority,
	}
	return postJSON(ctx, base+"/v2/alerts", payload, headers)
}

func mergeHeaders(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
