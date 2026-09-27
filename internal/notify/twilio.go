package notify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Test edilebilirlik için değiştirilebilir.
var twilioAPI = "https://api.twilio.com"

func init() { Register("twilio", twilio{}) }

type twilioConfig struct {
	AccountSID string `json:"account_sid"`
	AuthToken  string `json:"auth_token"`
	From       string `json:"from"`
	To         string `json:"to"` // virgülle ayrılmış
}

var twilioPhone = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

type twilio struct{}

func (twilio) Secrets() []string { return []string{"auth_token"} }

func (twilio) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c twilioConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.AccountSID = strings.TrimSpace(c.AccountSID)
	if err := required("Account SID", c.AccountSID); err != nil {
		return nil, err
	}
	if err := required("Auth token", c.AuthToken); err != nil {
		return nil, err
	}
	c.From = strings.TrimSpace(c.From)
	if !twilioPhone.MatchString(c.From) {
		return nil, invalid("Gönderen numarası ülke koduyla \"+15551234567\" biçiminde olmalı")
	}
	toList, err := splitNonEmptyCSV("Alıcı numaralar", c.To)
	if err != nil {
		return nil, err
	}
	for _, to := range toList {
		if !twilioPhone.MatchString(to) {
			return nil, invalid("Alıcı numarası \"%s\" geçersiz; ülke koduyla \"+905551112233\" biçiminde olmalı", to)
		}
	}
	c.To = strings.Join(toList, ",")
	return encode(c), nil
}

func (twilio) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c twilioConfig
	json.Unmarshal(raw, &c)
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(c.AccountSID+":"+c.AuthToken))
	u := twilioAPI + "/2010-04-01/Accounts/" + url.PathEscape(c.AccountSID) + "/Messages.json"
	text := ev.Text()

	var errs []string
	for _, to := range strings.Split(c.To, ",") {
		form := url.Values{"From": {c.From}, "To": {to}, "Body": {text}}
		err := doRequest(ctx, http.MethodPost, u, strings.NewReader(form.Encode()), map[string]string{
			"Content-Type":  "application/x-www-form-urlencoded",
			"Authorization": auth,
		})
		if err != nil {
			errs = append(errs, to+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("twilio gönderim hatası: %s", strings.Join(errs, "; "))
	}
	return nil
}
