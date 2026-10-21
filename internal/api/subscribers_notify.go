package api

// Durum sayfası abonelerine e-posta bildirimi.
//
// Kaynaklar: motorun monitör olayları (notify.Dispatcher gözlemcisi; yalnızca
// olay açan KindDown ve kapatan KindUp — eşik ve yeniden deneme mantığı
// motorda uygulanmış olur), elle açılan olay ve "çözüldü" güncellemesi, bakım
// ve duyuru oluşturma. Güncellemeler sayfa başına subNotifyWindow boyunca
// biriktirilir; aynı olayın başlangıcı ve çözümü aynı pencereye düştüyse
// ikisi de atılır (dalgalanma). Pencere sonunda her onaylı aboneye sayfa
// başına tek özet e-posta gider. Abone başına saatte en fazla
// subPerHour e-posta (bellek içi kayan pencere); aşan atlanır.
//
// E-postada monitörün sayfada görünen adı kullanılır; hedef adres ve hata
// mesajı gönderilmez.

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

const (
	subNotifyWindow = 60 * time.Second
	subPerHour      = 6
)

// subItem biriken bir güncelleme.
type subItem struct {
	key  string // aynı olayın başlangıç/çözüm eşleşmesi: "m:<olay>", "i:<olay>", "mt:<bakım>", "a:<duyuru>"
	kind string // down | up | incident | resolved | maintenance | announcement
	name string
	at   time.Time
}

// subState abonelik bildirimlerinin bellekteki durumu.
type subState struct {
	mu      sync.Mutex
	window  time.Duration         // birleştirme penceresi (testler kısaltır)
	batches map[int64][]subItem   // sayfa → bekleyen güncellemeler
	sent    map[int64][]time.Time // abone → son bir saatteki gönderimler
	ipRL    *ipLimiter            // abone olma isteği, IP başına
}

func newSubState() *subState {
	return &subState{window: subNotifyWindow, batches: map[int64][]subItem{}, sent: map[int64][]time.Time{},
		ipRL: newIPLimiterWindow(subPerIP, subIPWindow)}
}

// forget silinen abonenin sayaçlarını bırakır.
func (st *subState) forget(id int64) {
	st.mu.Lock()
	delete(st.sent, id)
	st.mu.Unlock()
}

// allowSend abone başına saatlik sınır; izin verirse gönderimi sayar.
func (st *subState) allowSend(id int64, now time.Time) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	cut := now.Add(-time.Hour)
	list := st.sent[id][:0]
	for _, t := range st.sent[id] {
		if t.After(cut) {
			list = append(list, t)
		}
	}
	if len(list) >= subPerHour {
		st.sent[id] = list
		return false
	}
	st.sent[id] = append(list, now)
	return true
}

// subEnqueue güncellemeyi sayfanın penceresine ekler; pencere ilk güncellemeyle başlar.
func (s *Server) subEnqueue(pageID int64, it subItem) {
	st := s.subs
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, open := st.batches[pageID]; !open {
		time.AfterFunc(st.window, func() { s.subFlush(pageID) })
	}
	st.batches[pageID] = append(st.batches[pageID], it)
}

// subObserve Dispatcher gözlemcisi: olay açan/kapatan monitör bildirimleri.
func (s *Server) subObserve(ev notify.Event) {
	if (ev.Kind != notify.KindDown && ev.Kind != notify.KindUp) || ev.MonitorID == 0 || ev.IncidentID == 0 || ev.ProbeID != 0 {
		return
	}
	kind := "down"
	if ev.Kind == notify.KindUp {
		kind = "up"
	}
	at := ev.Time
	if at.IsZero() {
		at = s.now()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pages, err := s.store.ListPages(ctx)
		if err != nil {
			s.log.Error("abone bildirimi: sayfalar okunamadı", "hata", err)
			return
		}
		for _, p := range pages {
			if !p.Published || !p.Subscriptions {
				continue
			}
			if name, ok := pageMonitorName(p, ev.MonitorID, ev.MonitorName); ok {
				s.subEnqueue(p.ID, subItem{key: "m:" + strconv.FormatInt(ev.IncidentID, 10), kind: kind, name: name, at: at})
			}
		}
	}()
}

// pageMonitorName monitör sayfadaysa sayfada görünen adı.
func pageMonitorName(p store.StatusPage, id int64, fallback string) (string, bool) {
	for _, sec := range p.Sections {
		for _, m := range sec.Monitors {
			if m.ID == id {
				if m.Name != "" {
					return m.Name, true
				}
				return fallback, true
			}
		}
	}
	return "", false
}

// subNotifyManual elle açılan olay (kind: incident | resolved).
func (s *Server) subNotifyManual(pageID, incidentID int64, kind, title string) {
	if pageID == 0 {
		return
	}
	p, err := s.store.GetPage(context.Background(), pageID)
	if err != nil || !p.Published || !p.Subscriptions {
		return
	}
	s.subEnqueue(pageID, subItem{key: "i:" + strconv.FormatInt(incidentID, 10), kind: kind, name: title, at: s.now()})
}

// subNotifyAnnouncement yeni duyuru.
func (s *Server) subNotifyAnnouncement(p store.StatusPage, a store.Announcement) {
	if !p.Published || !p.Subscriptions {
		return
	}
	s.subEnqueue(p.ID, subItem{key: "a:" + strconv.FormatInt(a.ID, 10), kind: "announcement", name: a.Title, at: s.now()})
}

// subNotifyMaintenance yeni bakım: etkilediği monitörleri içeren sayfalara.
func (s *Server) subNotifyMaintenance(ctx context.Context, m store.Maintenance) {
	if !m.AllMonitors && len(m.MonitorIDs) == 0 {
		return
	}
	pages, err := s.store.ListPages(ctx)
	if err != nil {
		s.log.Error("abone bildirimi: sayfalar okunamadı", "hata", err)
		return
	}
	for _, p := range pages {
		if !p.Published || !p.Subscriptions {
			continue
		}
		hit := false
		for _, id := range p.MonitorIDs() {
			if m.AllMonitors || containsID(m.MonitorIDs, id) {
				hit = true
				break
			}
		}
		if hit {
			s.subEnqueue(p.ID, subItem{key: "mt:" + strconv.FormatInt(m.ID, 10), kind: "maintenance", name: m.Title, at: s.now()})
		}
	}
}

func containsID(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// subEnds kind bir başlangıcı kapatıyor mu (dalgalanma eşleşmesi).
func subEnds(kind string) bool { return kind == "up" || kind == "resolved" }

// collapseFlaps aynı anahtarlı başlangıç + çözüm çiftlerini atar.
func collapseFlaps(items []subItem) []subItem {
	start, end := map[string]bool{}, map[string]bool{}
	for _, it := range items {
		if subEnds(it.kind) {
			end[it.key] = true
		} else if it.kind == "down" || it.kind == "incident" {
			start[it.key] = true
		}
	}
	out := items[:0:0]
	for _, it := range items {
		if start[it.key] && end[it.key] && (subEnds(it.kind) || it.kind == "down" || it.kind == "incident") {
			continue
		}
		out = append(out, it)
	}
	return out
}

// subFlush pencere sonunda sayfanın biriken güncellemelerini abonelere gönderir.
func (s *Server) subFlush(pageID int64) {
	st := s.subs
	st.mu.Lock()
	items := st.batches[pageID]
	delete(st.batches, pageID)
	st.mu.Unlock()
	items = collapseFlaps(items)
	if len(items) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	p, err := s.store.GetPage(ctx, pageID)
	if err != nil {
		return // sayfa silinmiş
	}
	mailer, ok := s.subscribable(ctx, p)
	if !ok {
		return
	}
	subs, err := s.store.ConfirmedPageSubscribers(ctx, pageID)
	if err != nil {
		s.log.Error("abone bildirimi: aboneler okunamadı", "sayfa", p.Slug, "hata", err)
		return
	}
	for _, sub := range subs {
		now := s.now()
		if !st.allowSend(sub.ID, now) {
			s.log.Warn("abone e-posta sınırı aşıldı; bildirim atlandı", "sayfa", p.Slug, "abone", sub.ID)
			continue
		}
		subject, text, html, headers := subUpdateMail(p, sub, items, now)
		sctx, scancel := context.WithTimeout(ctx, 30*time.Second)
		err := s.subMailer(sctx, mailer.Config, sub.Email, subject, text, html, headers)
		scancel()
		if err != nil {
			s.log.Error("abone bildirimi gönderilemedi", "sayfa", p.Slug, "abone", sub.ID, "hata", notify.SanitizeSendError("email", mailer.Config, err))
		}
	}
}

// subUpdateMail özet e-postası ve List-Unsubscribe başlıkları.
func subUpdateMail(p store.StatusPage, sub store.PageSubscriber, items []subItem, now time.Time) (subject, text, html string, headers map[string]string) {
	lang := i18n.Or(sub.Lang)
	base, pageURL := subLinks(p, sub)
	tok := url.QueryEscape(unsubToken(sub, now.Add(subUnsubLifetime).Unix()))
	unsubPage := pageURL + "?abonelik=cik&t=" + tok
	headers = map[string]string{
		"List-Unsubscribe":      "<" + base + subUnsubscribePath + "?t=" + tok + ">",
		"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
	}
	line := func(it subItem) string { return i18n.T(lang, "mail.sub.kind."+it.kind, it.name) }
	if len(items) == 1 {
		subject = "[" + p.Title + "] " + line(items[0])
	} else {
		subject = "[" + p.Title + "] " + i18n.T(lang, "mail.sub.many", len(items))
	}
	tone := "info"
	allUp, anyDown, anyMaint := true, false, false
	rows := make([]notify.Row, 0, len(items))
	var lines []string
	for _, it := range items {
		switch it.kind {
		case "down", "incident":
			anyDown = true
		case "maintenance":
			anyMaint = true
		}
		if !subEnds(it.kind) {
			allUp = false
		}
		when := it.at.UTC().Format("2006-01-02 15:04 UTC")
		rows = append(rows, notify.Row{Label: when, Value: line(it)})
		lines = append(lines, when+"  "+line(it))
	}
	switch {
	case anyDown:
		tone = "down"
	case allUp:
		tone = "up"
	case anyMaint:
		tone = "warn"
	}
	intro := i18n.T(lang, "mail.sub.intro", p.Title)
	unsubText := i18n.T(lang, "mail.sub.unsub")
	text = subject + "\n\n" + intro + "\n\n" + strings.Join(lines, "\n") + "\n\n" + pageURL + "\n\n" +
		fmt.Sprintf("%s: %s\n", unsubText, unsubPage)
	html = notify.RenderSubscriberMail(lang, tone, i18n.T(lang, "mail.sub.status"), subject, []string{intro}, rows,
		pageURL, i18n.T(lang, "mail.sub.open"), unsubPage, unsubText)
	return subject, text, html, headers
}
