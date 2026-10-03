package api

// Web Push (E-14): panel kullanıcılarının tarayıcılarına doğrudan bildirim.
//
//	GET    /api/webpush/vapid                 uygulama açık anahtarı (applicationServerKey)
//	GET    /api/webpush/subscriptions         kullanıcının cihazları
//	POST   /api/webpush/subscriptions         bu tarayıcının aboneliğini kaydet (PushSubscription JSON)
//	DELETE /api/webpush/subscriptions/{id}    cihazı kaldır
//	POST   /api/webpush/test                  kullanıcının tüm cihazlarına deneme bildirimi
//
// Gönderim "webpush" bildirim kanalı türüyle yapılır (notify/webpush.go):
// kanal monitörlere bağlanır, kural hattından geçer ve ayarındaki
// kullanıcıların (boş = herkes) aboneliklerine gider. Kısıtlı (müşteri)
// kullanıcı yalnızca görebildiği monitör/sunucunun bildirimini alır. VAPID
// anahtar çifti ilk ihtiyaçta üretilir ve settings tablosunda saklanır.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
	"github.com/kadirsungurlu/bekci/internal/webpush"
)

const (
	maxPushPerUser   = 10
	maxPushEndpoint  = 2048
	pushSendTimeout  = 20 * time.Second
	pushTTLProblem   = 6 * 3600
	pushTTLOther     = 3600
	maxPushUserAgent = 200
)

// vapidState anahtar çifti önbelleği.
type vapidState struct {
	mu   sync.Mutex
	keys webpush.Keys
	ok   bool
}

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/webpush/vapid", s.auth(s.webpushVAPID))
		mux.Handle("GET /api/webpush/subscriptions", s.auth(s.listPushSubscriptions))
		mux.Handle("POST /api/webpush/subscriptions", s.auth(s.createPushSubscription))
		mux.Handle("DELETE /api/webpush/subscriptions/{id}", s.auth(s.deletePushSubscription))
		mux.Handle("POST /api/webpush/test", s.auth(s.testPush))
	})
}

// vapid kayıtlı anahtar çiftini döner; yoksa üretip saklar.
func (s *Server) vapid(ctx context.Context) (webpush.Keys, error) {
	s.vapidKeys.mu.Lock()
	defer s.vapidKeys.mu.Unlock()
	if s.vapidKeys.ok {
		return s.vapidKeys.keys, nil
	}
	raw, ok, err := s.store.VAPIDKeys(ctx)
	if err != nil {
		return webpush.Keys{}, err
	}
	var keys webpush.Keys
	if !ok || json.Unmarshal([]byte(raw), &keys) != nil || keys.Public == "" {
		keys, err = webpush.GenerateKeys()
		if err != nil {
			return keys, err
		}
		b, _ := json.Marshal(keys)
		if raw, err = s.store.SaveVAPIDKeys(ctx, string(b)); err != nil {
			return keys, err
		}
		json.Unmarshal([]byte(raw), &keys) // başka örnek önce yazdıysa onunki
		s.log.Info("Web Push VAPID anahtarı hazır")
	}
	s.vapidKeys.keys, s.vapidKeys.ok = keys, true
	return keys, nil
}

func (s *Server) webpushVAPID(w http.ResponseWriter, r *http.Request) {
	keys, err := s.vapid(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"public_key": keys.Public})
}

func (s *Server) listPushSubscriptions(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.PushSubscriptionsForUser(r.Context(), userFrom(r).ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createPushSubscription(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.APIKeyName != "" {
		writeError(w, http.StatusForbidden, "Bu işlem API anahtarıyla yapılamaz")
		return
	}
	var in webpush.Subscription
	if !readJSON(w, r, &in) {
		return
	}
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	ep, err := url.Parse(in.Endpoint)
	if err != nil || ep.Scheme != "https" && !(ep.Scheme == "http" && isLocalHost(ep.Hostname())) || ep.Host == "" || len(in.Endpoint) > maxPushEndpoint {
		writeError(w, http.StatusBadRequest, "Push bitiş noktası geçersiz")
		return
	}
	// Anahtarlar şifreleme denemesiyle doğrulanır (biçim ve uzunluk).
	if _, err := webpush.Encrypt(in, []byte("{}")); err != nil {
		writeError(w, http.StatusBadRequest, "Abonelik anahtarları geçersiz")
		return
	}
	existing, err := s.store.PushSubscriptionsForUser(r.Context(), u.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	known := false
	for _, p := range existing {
		if p.Endpoint == in.Endpoint {
			known = true
		}
	}
	if !known && len(existing) >= maxPushPerUser {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("En fazla %d cihaz kaydedilebilir; eskilerini kaldırın", maxPushPerUser))
		return
	}
	ua := r.UserAgent()
	if len(ua) > maxPushUserAgent {
		ua = ua[:maxPushUserAgent]
	}
	id, err := s.store.UpsertPushSubscription(r.Context(), store.PushSubscription{UserID: u.ID, Endpoint: in.Endpoint,
		P256dh: in.Keys.P256dh, Auth: in.Keys.Auth, UserAgent: ua, CreatedAt: s.now().Unix()})
	if err != nil {
		s.dbError(w, err)
		return
	}
	if !known {
		s.audit(r, u, "user.push_subscribe", "user", u.ID, u.Username, "")
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func isLocalHost(h string) bool { return h == "localhost" || h == "127.0.0.1" || h == "::1" }

func (s *Server) deletePushSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := userFrom(r)
	if err := s.store.DeletePushSubscription(r.Context(), u.ID, id); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, u, "user.push_unsubscribe", "user", u.ID, u.Username, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// testPush kullanıcının cihazlarına deneme bildirimi (kanal ve kurallardan bağımsız).
func (s *Server) testPush(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if !s.notifyRL.allowTest(u.ID, s.now()) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "Çok fazla test isteği; bir dakika sonra tekrar deneyin")
		return
	}
	subs, err := s.store.PushSubscriptionsForUser(r.Context(), u.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if len(subs) == 0 {
		writeError(w, http.StatusBadRequest, "Bu hesapta kayıtlı cihaz yok")
		return
	}
	ev := notify.Event{Kind: notify.KindTest, MonitorName: "Test", Time: s.now(), Lang: userLang(r)}
	sent, failed := s.pushTo(r.Context(), subs, ev)
	if sent == 0 {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("Gönderilemedi: %d cihazın hiçbirine ulaşılamadı", failed))
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"sent": sent, "failed": failed})
}

// pushPayload servis çalışanının okuduğu JSON.
type pushPayload struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	URL        string `json:"url"`
	Tag        string `json:"tag,omitempty"`
	IncidentID int64  `json:"incident_id,omitempty"`
	Kind       string `json:"kind"`
}

// pushBody bildirim gövdesi: başlık dışındaki satırlar (en çok 3; bağlantı satırı hariç).
func pushBody(ev notify.Event) string {
	var lines []string
	for _, r := range ev.Rows() {
		if r.Key == "link" || r.Key == "time" {
			continue
		}
		if len(r.Locations) > 0 {
			var names []string
			for _, l := range r.Locations {
				names = append(names, l.Name)
			}
			lines = append(lines, r.Label+": "+strings.Join(names, ", "))
		} else {
			lines = append(lines, r.Label+": "+r.Value)
		}
		if len(lines) == 3 {
			break
		}
	}
	return strings.Join(lines, "\n")
}

// pushURL tıklanınca açılacak yol: olay sayfası, yoksa monitör/sunucu, yoksa ana sayfa.
func pushURL(ev notify.Event) string {
	switch {
	case ev.IncidentID != 0:
		return fmt.Sprintf("/#/incidents/%d", ev.IncidentID)
	case ev.MonitorID != 0:
		return fmt.Sprintf("/#/monitors/%d", ev.MonitorID)
	case ev.ProbeID != 0 && ev.MonitorType == "server":
		return fmt.Sprintf("/#/servers/%d", ev.ProbeID)
	case ev.ProbeID != 0:
		return "/#/settings/probes"
	}
	return "/#/"
}

// pushTo aboneliklere gönderir; (başarılı, başarısız) sayılarını döner.
// 404/410 alan abonelik silinir.
func (s *Server) pushTo(ctx context.Context, subs []store.PushSubscription, ev notify.Event) (sent, failed int) {
	keys, err := s.vapid(ctx)
	if err != nil {
		s.log.Error("VAPID anahtarı alınamadı", "hata", err)
		return 0, len(subs)
	}
	if ev.Lang == "" {
		ev.Lang = i18n.Default
	}
	payload, _ := json.Marshal(pushPayload{Title: ev.Title(), Body: pushBody(ev), URL: pushURL(ev), Kind: ev.Kind,
		IncidentID: ev.IncidentID, Tag: pushTag(ev)})
	opt := webpush.Options{TTL: pushTTLOther, Urgency: "normal", Topic: pushTag(ev)}
	if notify.IsProblemStart(ev.Kind) || ev.Kind == notify.KindReminder {
		opt.TTL, opt.Urgency = pushTTLProblem, "high"
	}
	if strings.HasPrefix(s.BaseURL, "https://") {
		opt.Subject = s.BaseURL
	}
	now := s.now().Unix()
	for _, sub := range subs {
		var ws webpush.Subscription
		ws.Endpoint, ws.Keys.P256dh, ws.Keys.Auth = sub.Endpoint, sub.P256dh, sub.Auth
		sctx, cancel := context.WithTimeout(ctx, pushSendTimeout)
		err := webpush.Send(sctx, s.pushClient, keys, ws, payload, opt)
		cancel()
		switch {
		case err == nil:
			sent++
			s.store.TouchPush(ctx, sub.ID, now, false)
		case errors.Is(err, webpush.ErrGone):
			failed++
			s.store.DeletePushByEndpoint(ctx, sub.Endpoint)
			s.log.Info("Web Push aboneliği kaldırıldı (push servisi: yok)", "kullanıcı", sub.UserID)
		default:
			failed++
			s.store.TouchPush(ctx, sub.ID, now, true)
			s.log.Warn("Web Push gönderilemedi", "kullanıcı", sub.UserID, "hata", err)
		}
	}
	return sent, failed
}

// pushTag aynı olayın bildirimleri aynı etiketle (cihazda üst üste yazılır).
func pushTag(ev notify.Event) string {
	switch {
	case ev.IncidentID != 0:
		return fmt.Sprintf("inc-%d", ev.IncidentID)
	case ev.MonitorID != 0:
		return fmt.Sprintf("mon-%d-%s", ev.MonitorID, ev.Kind)
	case ev.ProbeID != 0:
		return fmt.Sprintf("srv-%d-%s", ev.ProbeID, ev.Kind)
	}
	return ""
}

// pushSender notify.WebPushSender: kanalın gerçek göndericisi.
type pushSender struct{ s *Server }

// SendWebPush kullanıcıların (boş = herkes) aboneliklerine gönderir; kısıtlı
// kullanıcı yalnızca görebildiği monitör/sunucu için alır. Hiçbir cihaza
// ulaşılamazsa hata döner (kanal "gönderilemedi" sayılır; teslimat kaydı yine yazılır).
func (p *pushSender) SendWebPush(ctx context.Context, users []string, ev notify.Event) error {
	subs, err := p.s.store.PushSubscriptionsFor(ctx, users)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return errors.New("Web Push aboneliği olan kullanıcı yok")
	}
	allowed := map[int64]bool{}
	var targets []store.PushSubscription
	for _, sub := range subs {
		ok, seen := allowed[sub.UserID]
		if !seen {
			u, err := p.s.store.UserByID(ctx, sub.UserID)
			ok = err == nil && pushVisible(u, ev)
			allowed[sub.UserID] = ok
		}
		if ok {
			targets = append(targets, sub)
		}
	}
	if len(targets) == 0 {
		return nil // kimse görmüyor: hata değil
	}
	sent, failed := p.s.pushTo(ctx, targets, ev)
	if sent == 0 && failed > 0 {
		return fmt.Errorf("%d cihazın hiçbirine ulaşılamadı", failed)
	}
	return nil
}

// pushVisible kullanıcı bu olayı görebilir mi (müşteri kısıtı).
func pushVisible(u store.User, ev notify.Event) bool {
	vis := visibleTo(u)
	switch {
	case vis.all:
		return true
	case ev.MonitorID != 0:
		return vis.can(ev.MonitorID)
	case ev.ProbeID != 0:
		return vis.canServer(ev.ProbeID)
	}
	return ev.Kind == notify.KindTest
}
