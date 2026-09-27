package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Test edilebilirlik için değiştirilebilir. Gerçek adres Netgsm'in klasik HTTP GET
// SMS gönderim uç noktasıdır (sms/rest/v2 gibi daha yeni REST uç noktaları
// hesap tipine göre değişebildiğinden, en geriye dönük uyumlu olan bu klasik
// GET arayüzü kullanılmıştır: usercode, password, gsmno, message, msgheader
// parametreleri ve "00 <jobid>" / hata kodu biçiminde düz metin yanıt).
var netgsmAPI = "https://api.netgsm.com.tr/sms/send/get"

func init() { Register("netgsm", netgsm{}) }

type netgsmConfig struct {
	UserCode     string `json:"usercode"`
	Password     string `json:"password"`
	MsgHeader    string `json:"msgheader"`
	GSM          string `json:"gsm"` // virgülle ayrılmış numaralar
	TurkishChars bool   `json:"turkish_chars"`
}

type netgsm struct{}

func (netgsm) Secrets() []string { return []string{"password"} }

func (netgsm) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c netgsmConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.UserCode = strings.TrimSpace(c.UserCode)
	if err := required("Kullanıcı kodu", c.UserCode); err != nil {
		return nil, err
	}
	if err := required("Şifre", c.Password); err != nil {
		return nil, err
	}
	c.MsgHeader = strings.TrimSpace(c.MsgHeader)
	if err := required("Gönderici başlığı (msgheader)", c.MsgHeader); err != nil {
		return nil, err
	}
	numbers, err := splitNonEmptyCSV("Telefon numaraları", c.GSM)
	if err != nil {
		return nil, err
	}
	normalized := make([]string, 0, len(numbers))
	for _, n := range numbers {
		norm, ok := normalizeTRPhone(n)
		if !ok {
			return nil, invalid("Telefon numarası \"%s\" geçersiz; 10-12 hane olmalı (ör. 5551112233 veya 905551112233)", n)
		}
		normalized = append(normalized, norm)
	}
	c.GSM = strings.Join(normalized, ",")
	return encode(c), nil
}

// normalizeTRPhone Türkiye numaralarını 10 hane (5xxxxxxxxx) veya ülke koduyla
// 12 hane (90 5xxxxxxxxx) biçimine indirger; 0/90/+90 önekleri temizlenir.
func normalizeTRPhone(s string) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	switch {
	case strings.HasPrefix(d, "0090") && len(d) == 14:
		d = d[4:]
	case strings.HasPrefix(d, "090") && len(d) == 13:
		d = d[1:]
	case strings.HasPrefix(d, "0") && len(d) == 11:
		d = d[1:]
	}
	switch len(d) {
	case 10:
		return d, true
	case 12:
		if strings.HasPrefix(d, "90") {
			return d, true
		}
	}
	return d, false
}

func netgsmText(ev Event) string {
	title := strings.TrimSpace(strings.TrimLeft(ev.Title(), "🔴🟢⚠️✅ "))
	msg := title
	if ev.Kind == KindDown || ev.Kind == KindReminder {
		if ev.Message != "" {
			msg += ": " + ev.Message
		}
	}
	if r := []rune(msg); len(r) > 300 {
		msg = string(r[:299]) + "…"
	}
	return msg
}

func (netgsm) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c netgsmConfig
	json.Unmarshal(raw, &c)
	q := url.Values{
		"usercode":  {c.UserCode},
		"password":  {c.Password},
		"gsmno":     {c.GSM},
		"message":   {netgsmText(ev)},
		"msgheader": {c.MsgHeader},
	}
	if c.TurkishChars {
		q.Set("dil", "TR")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, netgsmAPI+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return redactURLError(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return netgsmParseResponse(string(body))
}

// netgsmParseResponse Netgsm'in düz metin yanıtını yorumlar: "00 <işno>" başarı,
// diğer kodlar hata anlamına gelir (bkz. Netgsm API dokümantasyonu).
func netgsmParseResponse(body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("Netgsm'den boş yanıt alındı")
	}
	code := body
	if i := strings.IndexAny(body, " \t\r\n"); i >= 0 {
		code = body[:i]
	}
	if code == "00" {
		return nil
	}
	messages := map[string]string{
		"20":  "mesaj metninde sorun var veya standart maksimum karakter sayısı aşıldı",
		"30":  "kullanıcı kodu/şifre hatalı veya API erişim izniniz yok",
		"40":  "gönderici başlığı (msgheader) sistemde tanımlı değil",
		"41":  "gönderici başlığı (msgheader) sistemde tanımlı değil",
		"50":  "IYS kontrollü gönderim bu abonelik türüyle yapılamaz",
		"51":  "IYS marka bilginiz bulunamadı",
		"60":  "bakiyeniz yetersiz",
		"70":  "hatalı veya eksik parametre gönderildi",
		"80":  "bu istek izinli IP listesinde olmayan bir adresten yapıldı",
		"85":  "mükerrer gönderim sınırı aşıldı",
		"100": "Netgsm sistem hatası, lütfen daha sonra tekrar deneyin",
		"101": "Netgsm sistem hatası, lütfen daha sonra tekrar deneyin",
	}
	if m, ok := messages[code]; ok {
		return fmt.Errorf("Netgsm hata %s: %s", code, m)
	}
	return fmt.Errorf("Netgsm hata kodu: %s", code)
}
