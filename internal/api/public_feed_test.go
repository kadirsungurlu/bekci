package api

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
)

// TestPublicFeed: durum sayfasının RSS akışı olayları ve duyuruları sayfanın
// dilinde verir; olay nedeni ve sayfada olmayan monitör yazılmaz; ileri
// tarihli duyuru yoktur; şifreli sayfanın akışı çerezsiz verilmez; yayında
// olmayan sayfa 404'tür.
func TestPublicFeed(t *testing.T) {
	pe := setupPages(t)
	ctx := context.Background()
	st := pe.s.store
	a := pe.seedMonitor("Dahili API", "https://10.0.0.5/saglik")
	other := pe.seedMonitor("Sayfada değil", "https://gizli.example.com")
	now := pe.s.now().Unix()
	st.OpenIncident(ctx, a.ID, now-7200, "10.0.0.5 bağlantı reddedildi")
	st.ResolveIncident(ctx, a.ID, now-3600)
	st.OpenIncident(ctx, a.ID, now-600, "yeni neden")
	st.OpenIncident(ctx, other.ID, now-60, "başka monitörün nedeni")

	var p pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{
		"slug": "genel", "title": "Genel", "description": "Servislerimiz", "lang": "en",
		"sections": []map[string]any{{"title": "Servisler", "monitors": []map[string]any{{"id": a.ID, "name": "API"}}}},
	}, &p, 201)
	pe.mustDo("POST", fmt.Sprintf("/api/status-pages/%d/announcements", p.ID), map[string]any{"title": "Bakım", "body": "Gece <b>bakım</b>", "severity": "warning", "starts_at": now - 100}, nil, 201)
	pe.mustDo("POST", fmt.Sprintf("/api/status-pages/%d/announcements", p.ID), map[string]any{"title": "Gelecek", "starts_at": now + 3600}, nil, 201)

	anon := pe.anon()
	for _, path := range []string{"/api/public/pages/genel/feed.xml", "/durum/genel/feed.xml"} {
		resp, body := anon.raw("GET", path, "", "", nil)
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/rss+xml") {
			t.Fatalf("%s: %d %s %s", path, resp.StatusCode, resp.Header.Get("Content-Type"), body)
		}
		text := string(body)
		for _, secret := range []string{"10.0.0.5", "bağlantı reddedildi", "yeni neden", "Sayfada değil", "başka monitörün", "Dahili API", "Gelecek"} {
			if strings.Contains(text, secret) {
				t.Errorf("akışta %q olmamalı:\n%s", secret, text)
			}
		}
		var doc rss
		if err := xml.Unmarshal(body, &doc); err != nil {
			t.Fatalf("XML çözülemedi: %v\n%s", err, text)
		}
		ch := doc.Channel
		// atom:link ile link aynı yerel ada çözüldüğü için bağlantılar ham metinde aranır.
		if ch.Title != "Genel" || ch.Description != "Servislerimiz" || ch.Language != "en" ||
			!strings.Contains(text, "<link>https://uptime.test/durum/genel</link>") ||
			!strings.Contains(text, `<atom:link href="https://uptime.test/api/public/pages/genel/feed.xml" rel="self" type="application/rss+xml">`) {
			t.Errorf("kanal yanlış: %+v\n%s", ch, text)
		}
		if len(ch.Items) != 3 {
			t.Fatalf("3 kayıt bekleniyordu: %+v", ch.Items)
		}
		// En yeni önce: duyuru (now-100), süren olay (now-600), çözülen (now-3600).
		if ch.Items[0].Title != "Bakım" || !strings.Contains(ch.Items[0].Description, "Gece <b>bakım</b>") || ch.Items[0].Category != "Announcement" {
			t.Errorf("duyuru kaydı yanlış: %+v", ch.Items[0])
		}
		if ch.Items[1].Title != "🔴 API: outage ongoing" || ch.Items[1].Category != "Incident" || !strings.HasPrefix(ch.Items[1].Description, "Started: ") {
			t.Errorf("süren olay kaydı yanlış: %+v", ch.Items[1])
		}
		if ch.Items[2].Title != "🟢 API: outage resolved" || !strings.Contains(ch.Items[2].Description, "Duration: 1h") {
			t.Errorf("çözülen olay kaydı yanlış: %+v", ch.Items[2])
		}
		if ch.Items[1].GUID.Value == ch.Items[2].GUID.Value || ch.Items[1].GUID.IsPermaLink {
			t.Errorf("kayıt kimlikleri ayrı ve kalıcı bağlantı olmamalı: %+v", ch.Items)
		}
	}

	// Olaylar kapalıysa yalnızca duyurular; Türkçe sayfada Türkçe metin.
	base := map[string]any{"slug": "genel", "title": "Genel", "description": "Servislerimiz",
		"sections": []map[string]any{{"title": "Servisler", "monitors": []map[string]any{{"id": a.ID, "name": "API"}}}}}
	upd := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), upd(map[string]any{"show_incidents": false, "lang": "tr"}), nil, 200)
	_, body := anon.raw("GET", "/api/public/pages/genel/feed.xml", "", "", nil)
	var doc rss
	xml.Unmarshal(body, &doc)
	if len(doc.Channel.Items) != 1 || doc.Channel.Items[0].Category != "Duyuru" || doc.Channel.Language != "tr" {
		t.Errorf("olaylar kapalı: yalnızca Türkçe duyuru bekleniyordu: %+v", doc.Channel)
	}

	// Yayından kaldırılan sayfa 404; şifreli sayfa çerezsiz 401.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), upd(map[string]any{"published": false}), nil, 200)
	if resp, _ := anon.raw("GET", "/durum/genel/feed.xml", "", "", nil); resp.StatusCode != 404 {
		t.Errorf("yayında olmayan sayfanın akışı 404 olmalı: %d", resp.StatusCode)
	}
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), upd(map[string]any{"published": true, "password": "gizli-sifre"}), nil, 200)
	if resp, _ := anon.raw("GET", "/api/public/pages/genel/feed.xml", "", "", nil); resp.StatusCode != 401 {
		t.Errorf("şifreli sayfanın akışı 401 olmalı: %d", resp.StatusCode)
	}
	resp, _ := anon.raw("POST", "/api/public/pages/genel/unlock", "", "application/json", []byte(`{"password":"gizli-sifre"}`))
	if resp.StatusCode != 200 {
		t.Fatalf("şifre açılmadı: %d", resp.StatusCode)
	}
	if resp, _ := anon.raw("GET", "/api/public/pages/genel/feed.xml", "", "", nil); resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "private, no-store" {
		t.Errorf("şifreyi açan tarayıcı akışı görmeli (önbelleksiz): %d %q", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
}
