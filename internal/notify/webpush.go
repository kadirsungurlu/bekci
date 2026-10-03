package notify

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

func init() { Register("webpush", webpushProvider{}) }

// Web Push kanalı: panele giriş yapmış kullanıcıların tarayıcılarına (ana
// ekrana eklenmiş PWA dahil) doğrudan bildirim. Gönderim mantığı (abonelikler,
// VAPID anahtarları, kullanıcı görünürlüğü) veritabanı gerektirir ve api
// paketinde yaşar; buraya SetWebPushSender ile bağlanır. Kural hattı (olay
// süzgeci, sessiz saatler, gecikme, eskalasyon) diğer kanallarla aynıdır.
type webpushConfig struct {
	// Users virgülle ayrılmış kullanıcı adları; boş = aboneliği olan herkes.
	// Kısıtlı (müşteri) kullanıcılar yalnızca görebildikleri monitörlerin
	// bildirimini alır.
	Users string `json:"users"`
}

// WebPushSender kanalın gerçek göndericisi (api paketi kurar).
type WebPushSender interface {
	SendWebPush(ctx context.Context, users []string, ev Event) error
}

var webpushSender WebPushSender

// SetWebPushSender Web Push göndericisini bağlar (uygulama açılışında).
func SetWebPushSender(s WebPushSender) { webpushSender = s }

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{3,32}$`)

type webpushProvider struct{}

func (webpushProvider) Secrets() []string { return nil }

func (webpushProvider) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c webpushConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	users := WebPushUsers(c.Users)
	for _, u := range users {
		if !usernameRe.MatchString(u) {
			return nil, invalid("Kullanıcı adı geçersiz: %s", u)
		}
	}
	c.Users = strings.Join(users, ", ")
	return encode(c), nil
}

// WebPushUsers ayarın kullanıcı listesini çözer (kırpılmış, tekil, küçük harf).
func WebPushUsers(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' || r == '\n' }) {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func (webpushProvider) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	if webpushSender == nil {
		return errors.New("Web Push göndericisi hazır değil")
	}
	var c webpushConfig
	json.Unmarshal(raw, &c)
	return webpushSender.SendWebPush(ctx, WebPushUsers(c.Users), ev)
}
