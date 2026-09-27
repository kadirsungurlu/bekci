package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

func TestTags(t *testing.T) {
	admin := setupAdmin(t)
	a, b := admin.push("A"), admin.push("B")

	var env, team tagView
	admin.mustDo("POST", "/api/tags", map[string]any{"name": " Ortam ", "color": "#2563EB"}, &env, 201)
	if env.Name != "Ortam" || env.Color != "#2563eb" {
		t.Errorf("etiket normalize edilmedi: %+v", env)
	}
	admin.mustDo("POST", "/api/tags", map[string]any{"name": "Ekip"}, &team, 201)
	if team.Color != defaultTagColor {
		t.Errorf("varsayılan renk: %s", team.Color)
	}
	for _, c := range []struct {
		body any
		want int
	}{
		{map[string]any{"name": "ORTAM"}, 409}, // büyük/küçük harf duyarsız tekil
		{map[string]any{"name": ""}, 400},
		{map[string]any{"name": strings.Repeat("x", 51)}, 400},
		{map[string]any{"name": "renk", "color": "mavi"}, 400},
		{map[string]any{"name": "renk", "color": "#12345"}, 400},
		{map[string]any{"name": "fazla", "extra": 1}, 400},
	} {
		if got := admin.do("POST", "/api/tags", c.body, nil); got != c.want {
			t.Errorf("POST %v: %d, %d bekleniyordu", c.body, got, c.want)
		}
	}
	// Ad değişikliği başka etiketle çakışamaz.
	if got := admin.do("PUT", fmt.Sprintf("/api/tags/%d", team.ID), map[string]any{"name": "ortam"}, nil); got != 409 {
		t.Errorf("çakışan yeniden adlandırma: %d", got)
	}
	admin.mustDo("PUT", fmt.Sprintf("/api/tags/%d", team.ID), map[string]any{"name": "Takım", "color": "#059669"}, &team, 200)

	// Monitör etiketleri.
	var got []store.MonitorTag
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/tags", a.ID), []map[string]any{
		{"tag_id": env.ID, "value": " canlı "}, {"tag_id": env.ID, "value": "canlı"}, {"tag_id": team.ID},
		{"tag_id": env.ID, "value": "test"},
	}, &got, 200)
	if len(got) != 3 || got[0].Name != "Ortam" || got[0].Value != "canlı" || got[2].Name != "Takım" {
		t.Errorf("monitör etiketleri: %+v", got)
	}
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/tags", b.ID), []map[string]any{{"tag_id": team.ID, "value": "altyapı"}}, nil, 200)
	for _, bad := range []any{
		[]map[string]any{{"tag_id": 9999}},
		[]map[string]any{{"tag_id": env.ID, "value": strings.Repeat("d", 101)}},
		map[string]any{"tag_id": env.ID},
	} {
		if code := admin.do("PUT", fmt.Sprintf("/api/monitors/%d/tags", a.ID), bad, nil); code != 400 {
			t.Errorf("geçersiz monitör etiketi %v: %d", bad, code)
		}
	}
	if code := admin.do("PUT", "/api/monitors/9999/tags", []any{}, nil); code != 404 {
		t.Errorf("olmayan monitör: %d", code)
	}

	var list []monitorView
	admin.mustDo("GET", "/api/monitors", nil, &list, 200)
	for _, m := range list {
		if m.ID == a.ID && len(m.Tags) != 3 || m.ID == b.ID && (len(m.Tags) != 1 || m.Tags[0].Value != "altyapı") {
			t.Errorf("listede etiketler: %s %+v", m.Name, m.Tags)
		}
	}
	admin.mustDo("GET", fmt.Sprintf("/api/monitors?tag=%d", env.ID), nil, &list, 200)
	if len(list) != 1 || list[0].ID != a.ID {
		t.Errorf("etiket filtresi: %+v", list)
	}
	if code := admin.do("GET", "/api/monitors?tag=abc", nil, nil); code != 400 {
		t.Errorf("geçersiz filtre: %d", code)
	}
	var detail struct{ Monitor monitorView }
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d", b.ID), nil, &detail, 200)
	if len(detail.Monitor.Tags) != 1 {
		t.Errorf("detayda etiketler: %+v", detail.Monitor.Tags)
	}

	// Yetkiler ve müşteri kısıtı.
	viewer, _ := admin.newUser("izleyici", store.RoleViewer, []int64{b.ID})
	var tags []tagView
	viewer.mustDo("GET", "/api/tags", nil, &tags, 200)
	for _, tv := range tags {
		want := map[int64]int{env.ID: 0, team.ID: 1}[tv.ID]
		if tv.MonitorCount != want {
			t.Errorf("kısıtlı izleyici sayısı %s: %d, %d bekleniyordu", tv.Name, tv.MonitorCount, want)
		}
	}
	viewer.mustDo("GET", fmt.Sprintf("/api/monitors?tag=%d", env.ID), nil, &list, 200)
	if len(list) != 0 {
		t.Errorf("kısıtlı izleyici görmediği monitörü filtrede gördü: %+v", list)
	}
	viewer.mustDo("GET", "/api/monitors", nil, &list, 200)
	if len(list) != 1 || len(list[0].Tags) != 1 {
		t.Errorf("izleyici listesi: %+v", list)
	}
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/tags"}, {"PUT", fmt.Sprintf("/api/tags/%d", env.ID)}, {"DELETE", fmt.Sprintf("/api/tags/%d", env.ID)},
		{"PUT", fmt.Sprintf("/api/monitors/%d/tags", b.ID)},
	} {
		if code := viewer.do(c.method, c.path, map[string]any{"name": "x"}, nil); code != 403 {
			t.Errorf("izleyici %s %s: %d", c.method, c.path, code)
		}
	}
	editor, _ := admin.newUser("editor", store.RoleEditor, nil)
	editor.mustDo("POST", "/api/tags", map[string]any{"name": "Editörün"}, nil, 201)

	// Silinen etiket monitörlerden de kalkar; işlem kaydı tutulur.
	admin.mustDo("DELETE", fmt.Sprintf("/api/tags/%d", env.ID), nil, nil, 200)
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d", a.ID), nil, &detail, 200)
	if len(detail.Monitor.Tags) != 1 || detail.Monitor.Tags[0].ID != team.ID {
		t.Errorf("silinen etiket kaldı: %+v", detail.Monitor.Tags)
	}
	if code := admin.do("DELETE", fmt.Sprintf("/api/tags/%d", env.ID), nil, nil); code != 404 {
		t.Errorf("iki kez silme: %d", code)
	}
	var audit []store.AuditEntry
	admin.mustDo("GET", "/api/audit", nil, &audit, 200)
	actions := map[string]bool{}
	for _, e := range audit {
		actions[e.Action] = true
	}
	for _, a := range []string{"tag.create", "tag.update", "tag.delete", "monitor.tags"} {
		if !actions[a] {
			t.Errorf("işlem kaydında %s yok", a)
		}
	}
}

func TestHTTPExtraSecretsMasked(t *testing.T) {
	admin := setupAdmin(t)
	cfg := map[string]any{
		"url": "http://127.0.0.1:1/", "proxy_url": "http://kadir:proxy-sifre@127.0.0.1:3128",
		"oauth_token_url": "http://127.0.0.1:1/token", "oauth_client_id": "id", "oauth_client_secret": "oauth-sir",
	}
	var m monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{"name": "gizli", "type": "http", "interval": 60000, "config": cfg}, &m, 201)
	body := string(m.Config)
	if strings.Contains(body, "proxy-sifre") || strings.Contains(body, "oauth-sir") ||
		!strings.Contains(body, `"proxy_pass":"`+notify.Mask) || !strings.Contains(body, `"proxy_user":"kadir"`) {
		t.Fatalf("gizli alanlar maskelenmedi: %s", body)
	}
}
