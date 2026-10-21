package api

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

type subListView struct {
	Count, Pending int
	Items          []struct {
		ID          int64
		Email       string
		Confirmed   bool
		CreatedAt   int64 `json:"created_at"`
		ConfirmedAt int64 `json:"confirmed_at"`
	}
}

func (se *subEnv) subList(pageID int64) subListView {
	se.t.Helper()
	var l subListView
	se.mustDo("GET", fmt.Sprintf("/api/status-pages/%d/subscribers", pageID), nil, &l, 200)
	return l
}

// Yedek al -> temiz kuruluma geri yükle: onaylı ve bekleyen aboneler, tarihler
// taşınır; eski e-postadaki çıkış ve onay bağlantıları çalışır (kimlikler yeni
// kurulumda farklı olsa da), yeni e-postalardaki bağlantı da çalışır.
func TestSubscribersBackupRestore(t *testing.T) {
	src := setupSubs(t, 1000)
	src.enableMail()
	src.setSubscriptions(true, 200)

	// Kimlikler kaysın diye ilk abone silinir: a=2, b=3 olur; hedefte 1, 2.
	src.subscribe("sil@ornek.com", 202)
	src.waitMails("sil@ornek.com", 1)
	first := src.subList(src.pageID).Items[0]
	if code := src.do("DELETE", fmt.Sprintf("/api/status-pages/%d/subscribers/%d", src.pageID, first.ID), nil, nil); code != 204 {
		t.Fatalf("silme: %d", code)
	}

	src.subscribe("a@ornek.com", 202)
	src.anon().mustDo("POST", "/api/public/subscriptions/confirm",
		map[string]string{"token": src.confirmToken(src.waitMails("a@ornek.com", 1)[0])}, nil, 200)
	src.subscribe("b@ornek.com", 202)
	bConfirm := src.confirmToken(src.waitMails("b@ornek.com", 1)[0])

	src.event(notify.KindDown, 1)
	aUnsub := src.unsubToken(src.waitMails("a@ornek.com", 2)[1])
	before := src.subList(src.pageID)

	doc, _ := src.env.exportDoc()
	if len(doc.StatusPages) != 1 || len(doc.StatusPages[0].Subscribers) != 2 || !doc.StatusPages[0].Subscriptions {
		t.Fatalf("yedekte aboneler yok: %+v", doc.StatusPages)
	}

	dst := setupSubs(t, 1000)
	// Eski biçim (abonesiz) yedek sorunsuz yüklenir.
	var old map[string]any
	raw, _ := json.Marshal(doc)
	json.Unmarshal(raw, &old)
	for _, p := range old["status_pages"].([]any) {
		delete(p.(map[string]any), "subscribers")
		delete(p.(map[string]any), "subscriptions")
	}
	dst.env.importDoc("/api/import?mode=replace&confirm=yes", old, 200)
	var pages []store.StatusPage
	dst.mustDo("GET", "/api/status-pages", nil, &pages, 200)
	if len(pages) != 1 || pages[0].Subscriptions || dst.subList(pages[0].ID).Count+dst.subList(pages[0].ID).Pending != 0 {
		t.Fatalf("eski yedek: %+v", pages)
	}

	dst.env.importDoc("/api/import?mode=replace&confirm=yes", doc, 200)
	dst.mustDo("GET", "/api/status-pages", nil, &pages, 200)
	if len(pages) != 1 || !pages[0].Subscriptions {
		t.Fatalf("geri yüklenen sayfa: %+v", pages)
	}
	pid := pages[0].ID
	after := dst.subList(pid)
	if after.Count != 1 || after.Pending != 1 || len(after.Items) != 2 {
		t.Fatalf("geri yüklenen aboneler: %+v", after)
	}
	byMail := map[string]int{}
	for i, it := range before.Items {
		byMail[it.Email] = i
	}
	for _, it := range after.Items {
		b := before.Items[byMail[it.Email]]
		if b.Email != it.Email || b.Confirmed != it.Confirmed || b.CreatedAt != it.CreatedAt || b.ConfirmedAt != it.ConfirmedAt {
			t.Fatalf("abone farklı: önce %+v sonra %+v", b, it)
		}
		if it.Email == "a@ornek.com" && it.ID == b.ID {
			t.Fatalf("test kimlik kaymasını sınamıyor: %d", it.ID)
		}
	}
	if _, ok := dst.s.systemMailer(t.Context()); !ok {
		dst.enableMail()
	}

	// Eski onay bağlantısı (bekleyen b) ve eski çıkış bağlantısı (a) çalışır.
	dst.anon().mustDo("POST", "/api/public/subscriptions/confirm", map[string]string{"token": bConfirm}, nil, 200)
	dst.anon().mustDo("POST", "/api/public/subscriptions/unsubscribe", map[string]string{"token": aUnsub}, nil, 200)
	l := dst.subList(pid)
	if l.Count != 1 || l.Pending != 0 || l.Items[0].Email != "b@ornek.com" {
		t.Fatalf("eski bağlantılardan sonra: %+v", l)
	}

	// Yeni e-postadaki çıkış bağlantısı (yeni kimlikle) da çalışır; dil korunur.
	// Monitör geri yüklemede yeni kimlik aldı.
	dst.mon.ID = pages[0].Sections[0].Monitors[0].ID
	dst.event(notify.KindDown, 1)
	m := dst.waitMails("b@ornek.com", 1)[0]
	if m.headers["List-Unsubscribe"] == "" {
		t.Fatalf("çıkış başlığı yok: %+v", m)
	}
	dst.anon().mustDo("POST", "/api/public/subscriptions/unsubscribe", map[string]string{"token": dst.unsubToken(m)}, nil, 200)
	if l := dst.subList(pid); l.Count != 0 {
		t.Fatalf("yeni bağlantıyla çıkış olmadı: %+v", l)
	}
}
