package notify

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

func init() { Register("signal", signal{}) }

// signal-cli-rest-api (https://github.com/bbernhard/signal-cli-rest-api) üzerinden gönderim.
type signalConfig struct {
	URL        string `json:"url"`
	Number     string `json:"number"`     // gönderen (kayıtlı) numara, ör. +905551112233
	Recipients string `json:"recipients"` // virgülle ayrılmış numara/grup listesi
}

var signalNumber = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

type signal struct{}

// signal-cli-rest-api genelde ağ seviyesinde korunur; API'nin kendisinde gizli bir alan yoktur.
func (signal) Secrets() []string { return nil }

func (signal) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c signalConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.URL = strings.TrimRight(strings.TrimSpace(c.URL), "/")
	if err := validURL("signal-cli-rest-api adresi", c.URL); err != nil {
		return nil, err
	}
	c.Number = strings.TrimSpace(c.Number)
	if !signalNumber.MatchString(c.Number) {
		return nil, invalid("Gönderen numarası ülke koduyla \"+905551112233\" biçiminde olmalı")
	}
	list, err := splitNonEmptyCSV("Alıcılar", c.Recipients)
	if err != nil {
		return nil, err
	}
	c.Recipients = strings.Join(list, ",")
	return encode(c), nil
}

func (signal) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c signalConfig
	json.Unmarshal(raw, &c)
	var recipients []string
	for _, r := range strings.Split(c.Recipients, ",") {
		if r = strings.TrimSpace(r); r != "" {
			recipients = append(recipients, r)
		}
	}
	payload := map[string]any{
		"message":    ev.Text(),
		"number":     c.Number,
		"recipients": recipients,
	}
	return postJSON(ctx, c.URL+"/v2/send", payload, nil)
}

// splitNonEmptyCSV virgülle ayrılmış listeyi böler, boşları atar; en az bir
// öğe olmasını zorunlu kılar.
func splitNonEmptyCSV(field, s string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return nil, invalid("%s en az bir değer içermeli", field)
	}
	return out, nil
}
