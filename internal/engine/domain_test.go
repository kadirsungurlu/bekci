package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/rdap"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// fakeLookup alan adına göre sabit sonuç verir; sorgu sayısını sayar.
type fakeLookup struct {
	mu      sync.Mutex
	results map[string]rdap.Result
	errs    map[string]error
	calls   []string
}

func (f *fakeLookup) Lookup(_ context.Context, host string) (rdap.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := rdap.Domain(host)
	f.calls = append(f.calls, d)
	if err, ok := f.errs[d]; ok {
		return rdap.Result{Domain: d}, err
	}
	if r, ok := f.results[d]; ok {
		return r, nil
	}
	return rdap.Result{Domain: d, Status: rdap.StatusUnsupported}, nil
}

func TestDomainScan(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := f.clock
	lk := &fakeLookup{
		results: map[string]rdap.Result{
			"ornek.com":  {Domain: "ornek.com", Status: rdap.StatusOK, Expires: now.AddDate(0, 0, 10), Registrar: "Kayıt A.Ş."},
			"uzak.net":   {Domain: "uzak.net", Status: rdap.StatusOK, Expires: now.AddDate(0, 0, 200)},
			"bitmis.org": {Domain: "bitmis.org", Status: rdap.StatusOK, Expires: now.AddDate(0, 0, -2)},
		},
		errs: map[string]error{"hata.com": rdap.ErrUnavailable},
	}
	f.e.SetDomainLookup(lk)
	s := store.DefaultSettings()
	f.e.SetSettings(s)

	mk := func(name, typ string, cfg string, mut func(*store.Monitor)) store.Monitor {
		return f.monitor(t, func(m *store.Monitor) {
			m.Name, m.Type, m.Config, m.DomainExpiry = name, typ, json.RawMessage(cfg), true
			if mut != nil {
				mut(m)
			}
		})
	}
	a := mk("Site", "http", `{"url":"https://www.ornek.com/x"}`, nil)
	a2 := mk("Site API", "http", `{"url":"https://api.ornek.com"}`, nil) // aynı alan adı: tek sorgu
	dns := mk("DNS", "dns", `{"host":"mail.uzak.net","record_type":"A","server":"1.1.1.1","port":53}`, nil)
	tls := mk("TLS", "tlscert", `{"host":"bitmis.org","port":443}`, nil)
	bad := mk("Bozuk", "http", `{"url":"https://hata.com"}`, nil)
	de := mk("Almanya", "http", `{"url":"https://ornek.de"}`, nil)
	ip := mk("IP", "http", `{"url":"http://10.0.0.1:8080"}`, nil)
	off := mk("Kapalı", "http", `{"url":"https://kapali.com"}`, func(m *store.Monitor) { m.DomainExpiry = false })
	push := mk("Push", "push", `{}`, nil)

	f.e.DomainScan(ctx)

	// Aynı alan adı bir kez sorgulandı; kapalı ve alan adsız monitörler sorgulanmadı.
	count := map[string]int{}
	for _, c := range lk.calls {
		count[c]++
	}
	if count["ornek.com"] != 1 || count["uzak.net"] != 1 || count["bitmis.org"] != 1 || count["hata.com"] != 1 || count["ornek.de"] != 1 || count["kapali.com"] != 0 || len(lk.calls) != 5 {
		t.Fatalf("sorgular: %v", lk.calls)
	}
	get := func(id int64) store.Monitor {
		m, err := f.st.GetMonitor(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	if m := get(a.ID); m.DomainStatus != store.DomainOK || m.DomainName != "ornek.com" || m.DomainRegistrar != "Kayıt A.Ş." || m.DomainExpiresAt != now.AddDate(0, 0, 10).Unix() || m.DomainCheckedAt == 0 {
		t.Fatalf("site: %+v", m)
	}
	if m := get(a2.ID); m.DomainStatus != store.DomainOK || m.DomainName != "ornek.com" {
		t.Fatalf("site api: %+v", m)
	}
	if m := get(dns.ID); m.DomainStatus != store.DomainOK || m.DomainName != "uzak.net" {
		t.Fatalf("dns: %+v", m)
	}
	if m := get(bad.ID); m.DomainStatus != store.DomainError || m.DomainExpiresAt != 0 || m.DomainCheckedAt == 0 {
		t.Fatalf("hata: %+v", m)
	}
	if m := get(de.ID); m.DomainStatus != store.DomainUnsupported {
		t.Fatalf("de: %+v", m)
	}
	if m := get(ip.ID); m.DomainStatus != store.DomainUnsupported || m.DomainCheckedAt == 0 {
		t.Fatalf("ip: %+v", m)
	}
	if m := get(push.ID); m.DomainStatus != store.DomainUnsupported {
		t.Fatalf("push: %+v", m)
	}
	if m := get(off.ID); m.DomainStatus != "" || m.DomainCheckedAt != 0 {
		t.Fatalf("kapalı monitör sorgulanmamalı: %+v", m)
	}

	// Bildirimler: ornek.com 10 gün (eşik 14 → 🟡) iki monitör için ayrı ayrı;
	// bitmis.org süresi dolmuş (🔴); uzak.net 200 gün eşik dışı; hata yok.
	var dom []notify.Event
	for _, ev := range f.n.events {
		if ev.Kind == notify.KindDomain {
			dom = append(dom, ev)
		}
	}
	if len(dom) != 3 {
		t.Fatalf("alan adı bildirimleri: %d %+v", len(dom), f.n.kinds())
	}
	byMon := map[int64]notify.Event{}
	for _, ev := range dom {
		byMon[ev.MonitorID] = ev
	}
	ev := byMon[a.ID]
	if ev.Domain != "ornek.com" || ev.DomainDays != 10 || ev.DomainRegistrar != "Kayıt A.Ş." || ev.DomainIsCritical() || ev.URL == "" {
		t.Fatalf("site bildirimi: %+v", ev)
	}
	if ev.Title() != "🟡 Site: ornek.com alan adı 10 gün içinde bitiyor" {
		t.Fatalf("başlık: %q", ev.Title())
	}
	if ev := byMon[tls.ID]; ev.DomainDays != -2 || !ev.DomainIsCritical() || ev.Title() != "🔴 TLS: bitmis.org alan adının süresi doldu" {
		t.Fatalf("bitmiş bildirimi: %+v %q", ev, ev.Title())
	}
	if _, ok := byMon[dns.ID]; ok {
		t.Fatal("200 gün kala bildirim gitmemeli")
	}

	// İkinci tarama aynı gün: hiç sorgu yok (günde bir).
	lk.calls = nil
	f.e.DomainScan(ctx)
	if len(lk.calls) != 0 {
		t.Fatalf("aynı gün yeniden sorgulanmamalı: %v", lk.calls)
	}
	// Ertesi gün: tekrar sorgulanır ama aynı eşik için bildirim tekrar gitmez;
	// hata veren alan adı düzelince bilgi yazılır.
	f.clock = now.Add(25 * time.Hour)
	lk.mu.Lock()
	delete(lk.errs, "hata.com")
	lk.results["hata.com"] = rdap.Result{Domain: "hata.com", Status: rdap.StatusOK, Expires: now.AddDate(0, 0, 60)}
	lk.mu.Unlock()
	before := len(f.n.events)
	f.e.DomainScan(ctx)
	if len(lk.calls) != 5 {
		t.Fatalf("ertesi gün sorgular: %v", lk.calls)
	}
	if len(f.n.events) != before {
		t.Fatalf("aynı eşik için ikinci bildirim gitmemeli: %+v", f.n.events[before:])
	}
	if m := get(bad.ID); m.DomainStatus != store.DomainOK || m.DomainExpiresAt != now.AddDate(0, 0, 60).Unix() {
		t.Fatalf("düzelen sorgu: %+v", m)
	}
	// Daha dar eşiğe girince (7) yeni bildirim 🔴.
	f.clock = now.Add(4*24*time.Hour + 25*time.Hour)
	f.e.DomainScan(ctx)
	last := f.n.events[len(f.n.events)-1]
	if last.Kind != notify.KindDomain || last.DomainDays != 4 || !last.DomainIsCritical() {
		t.Fatalf("7 gün eşiği: %+v", last)
	}
	// Ağ hatası eski bilgiyi silmez ve bildirim üretmez.
	lk.mu.Lock()
	lk.errs["ornek.com"] = errors.New("timeout")
	lk.mu.Unlock()
	f.clock = f.clock.Add(25 * time.Hour)
	before = len(f.n.events)
	f.e.DomainScan(ctx)
	if m := get(a.ID); m.DomainStatus != store.DomainError || m.DomainExpiresAt != now.AddDate(0, 0, 10).Unix() || m.DomainName != "ornek.com" {
		t.Fatalf("hata sonrası eski bilgi korunmalı: %+v", m)
	}
	for _, ev := range f.n.events[before:] {
		if ev.MonitorID == a.ID {
			t.Fatalf("hata bildirim üretmemeli: %+v", ev)
		}
	}
	// Eşik listesi boşsa bildirim gitmez.
	s.DomainDays = []int{}
	f.e.SetSettings(s)
	f.clock = f.clock.Add(25 * time.Hour)
	before = len(f.n.events)
	f.e.DomainScan(ctx)
	if len(f.n.events) != before {
		t.Fatalf("eşiksiz bildirim: %+v", f.n.events[before:])
	}
}

func TestDomainHost(t *testing.T) {
	cases := []struct {
		typ, cfg, want string
	}{
		{"http", `{"url":"https://www.ornek.com:8443/a"}`, "www.ornek.com"},
		{"dns", `{"host":"ornek.com"}`, "ornek.com"},
		{"tlscert", `{"host":"mail.ornek.com","port":993}`, "mail.ornek.com"},
		{"ping", `{"host":"ornek.com"}`, ""},
		{"push", `{}`, ""},
	}
	for _, c := range cases {
		if got := DomainHost(store.Monitor{Type: c.typ, Config: json.RawMessage(c.cfg)}); got != c.want {
			t.Errorf("%s: %q, %q bekleniyordu", c.typ, got, c.want)
		}
	}
}
