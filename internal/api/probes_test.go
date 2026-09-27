package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

type createdProbe struct {
	Probe         probeAdminView `json:"probe"`
	Token         string         `json:"token"`
	ServerURL     string         `json:"server_url"`
	DockerCommand string         `json:"docker_command"`
}

func (e *env) newProbe(name string) createdProbe {
	e.t.Helper()
	var cp createdProbe
	e.mustDo("POST", "/api/probes", map[string]any{"name": name}, &cp, 201)
	return cp
}

func (e *env) httpMonitor(name, url string) monitorView {
	e.t.Helper()
	var m monitorView
	e.mustDo("POST", "/api/monitors", map[string]any{
		"name": name, "type": "http", "interval": 5000, "timeout": 300,
		"config": map[string]any{"url": url, "basic_user": "u", "basic_pass": "cok-gizli-parola"},
	}, &m, 201)
	return m
}

func (e *env) setLocations(id int64, body map[string]any, want int) locationsView {
	e.t.Helper()
	var v locationsView
	e.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/locations", id), body, &v, want)
	return v
}

var probeTokenFormat = regexp.MustCompile(`^upr_[0-9A-Za-z]{43}$`)

func TestProbeRegistry(t *testing.T) {
	f := newFeatureEnv(t)
	admin := f.env
	f.s.ProbeImage = "ghcr.io/kadir/uptime:latest"

	cp := admin.newProbe("Frankfurt")
	if !probeTokenFormat.MatchString(cp.Token) || !strings.HasPrefix(cp.Token, cp.Probe.TokenPrefix) || len(cp.Probe.TokenPrefix) != 12 {
		t.Fatalf("token biçimi hatalı: %q prefix %q", cp.Token, cp.Probe.TokenPrefix)
	}
	wantCmd := "docker run -d --name uptime-probe --restart unless-stopped -e PROBE_SERVER=" + admin.srv.URL +
		" -e PROBE_TOKEN=" + cp.Token + " ghcr.io/kadir/uptime:latest probe"
	if cp.DockerCommand != wantCmd || cp.ServerURL != admin.srv.URL {
		t.Fatalf("kurulum komutu:\n%s\n%s bekleniyordu", cp.DockerCommand, wantCmd)
	}
	if !cp.Probe.Active || cp.Probe.Online || cp.Probe.LastSeenAt != 0 {
		t.Fatalf("yeni kontrol noktası: %+v", cp.Probe)
	}

	// Doğrulama ve çakışan ad.
	admin.mustDo("POST", "/api/probes", map[string]any{"name": " "}, nil, 400)
	admin.mustDo("POST", "/api/probes", map[string]any{"name": strings.Repeat("a", 101)}, nil, 400)
	admin.mustDo("POST", "/api/probes", map[string]any{"name": "frankfurt"}, nil, 409)
	other := admin.newProbe("İstanbul-2")
	admin.mustDo("PUT", fmt.Sprintf("/api/probes/%d", other.Probe.ID), map[string]any{"name": "FRANKFURT"}, nil, 409)

	// Liste: yönetici tüm bilgiyi görür, token ve özeti hiçbir zaman.
	var raw json.RawMessage
	admin.mustDo("GET", "/api/probes", nil, &raw, 200)
	if strings.Contains(string(raw), cp.Token) || strings.Contains(string(raw), hashToken(cp.Token)) ||
		!strings.Contains(string(raw), `"token_prefix"`) || !strings.Contains(string(raw), `"monitor_count"`) {
		t.Fatalf("yönetici listesi: %s", raw)
	}
	// Editör/izleyici yalnızca ad ve durum görür; yönetemez.
	editor, _ := admin.newUser("editor1", store.RoleEditor, nil)
	viewer, _ := admin.newUser("viewer1", store.RoleViewer, nil)
	for _, c := range []*env{editor, viewer} {
		var list []map[string]any
		c.mustDo("GET", "/api/probes", nil, &list, 200)
		if len(list) != 2 || list[0]["name"] != "Frankfurt" {
			t.Fatalf("liste: %v", list)
		}
		for k := range list[0] {
			if k != "id" && k != "name" && k != "active" && k != "online" && k != "last_seen_at" {
				t.Errorf("yönetici olmayana %q alanı gösterilmemeli", k)
			}
		}
		c.mustDo("POST", "/api/probes", map[string]any{"name": "x"}, nil, 403)
		c.mustDo("PUT", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), map[string]any{"name": "x"}, nil, 403)
		c.mustDo("DELETE", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), nil, nil, 403)
		c.mustDo("POST", fmt.Sprintf("/api/probes/%d/token", cp.Probe.ID), nil, nil, 403)
	}

	// Kontrol noktası isteği son görülme bilgisini yazar; çevrimiçi görünür.
	hdr := bearer(cp.Token)
	hdr["X-Probe-Version"] = "abc1234"
	if code, _, body := admin.rawReq("GET", "/api/probe/jobs", hdr, nil); code != 200 {
		t.Fatalf("iş listesi: %d %s", code, body)
	}
	var list []probeAdminView
	admin.mustDo("GET", "/api/probes", nil, &list, 200)
	if !list[0].Online || list[0].Version != "abc1234" || list[0].LastIP != "127.0.0.1" || list[0].LastSeenAt == 0 {
		t.Fatalf("son görülme: %+v", list[0])
	}
	f.clk.advance(2 * time.Minute)
	admin.mustDo("GET", "/api/probes", nil, &list, 200)
	if list[0].Online {
		t.Fatal("90 saniyeden uzun süredir görülmeyen kontrol noktası çevrimdışı olmalı")
	}

	// Token yenileme: eski token hemen geçersiz.
	var regen createdProbe
	admin.mustDo("POST", fmt.Sprintf("/api/probes/%d/token", cp.Probe.ID), nil, &regen, 200)
	if regen.Token == cp.Token || !probeTokenFormat.MatchString(regen.Token) || !strings.Contains(regen.DockerCommand, regen.Token) {
		t.Fatalf("yeni token: %+v", regen)
	}
	if code, _, _ := admin.rawReq("GET", "/api/probe/jobs", bearer(cp.Token), nil); code != 401 {
		t.Errorf("eski token: %d, 401 bekleniyordu", code)
	}
	if code, _, _ := admin.rawReq("GET", "/api/probe/jobs", bearer(regen.Token), nil); code != 200 {
		t.Errorf("yeni token: %d", code)
	}

	// Devre dışı bırakma: istekler hemen reddedilir; tekrar etkinleştirilebilir.
	var upd probeAdminView
	admin.mustDo("PUT", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), map[string]any{"name": "Frankfurt-1", "active": false}, &upd, 200)
	if upd.Active || upd.Name != "Frankfurt-1" {
		t.Fatalf("güncelleme: %+v", upd)
	}
	if code, _, _ := admin.rawReq("GET", "/api/probe/jobs", bearer(regen.Token), nil); code != 403 {
		t.Errorf("devre dışı kontrol noktası: %d, 403 bekleniyordu", code)
	}
	admin.mustDo("PUT", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), map[string]any{"name": "Frankfurt-1", "active": true}, nil, 200)
	if code, _, _ := admin.rawReq("GET", "/api/probe/jobs", bearer(regen.Token), nil); code != 200 {
		t.Errorf("tekrar etkin kontrol noktası: %d", code)
	}

	// Silme.
	admin.mustDo("DELETE", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), nil, nil, 200)
	admin.mustDo("DELETE", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), nil, nil, 404)
	if code, _, _ := admin.rawReq("GET", "/api/probe/jobs", bearer(regen.Token), nil); code != 401 {
		t.Errorf("silinen kontrol noktası: %d, 401 bekleniyordu", code)
	}

	// İşlem kaydı.
	var audit []store.AuditEntry
	admin.mustDo("GET", "/api/audit", nil, &audit, 200)
	seen := map[string]bool{}
	for _, a := range audit {
		seen[a.Action] = true
	}
	for _, a := range []string{"probe.create", "probe.update", "probe.token", "probe.delete"} {
		if !seen[a] {
			t.Errorf("işlem kaydında %s yok", a)
		}
	}
}

func TestProbeAuthAndCSRF(t *testing.T) {
	f := newFeatureEnv(t)
	admin := f.env
	cp := admin.newProbe("P")

	for _, h := range []map[string]string{
		nil,
		{"Authorization": "Bearer upr_yanlis"},
		{"Authorization": "Basic " + cp.Token},
		{"Authorization": "Bearer upr_" + strings.Repeat("a", 200)},
	} {
		if code, _, _ := admin.rawReq("GET", "/api/probe/jobs", h, nil); code != 401 {
			t.Errorf("%v: %d, 401 bekleniyordu", h, code)
		}
	}
	// Oturum çereziyle kontrol noktası uç noktası kullanılamaz.
	admin.mustDo("GET", "/api/probe/jobs", nil, nil, 401)
	// Probe token'ı kullanıcı API'sinde geçmez.
	if code, _, _ := admin.rawReq("GET", "/api/monitors", bearer(cp.Token), nil); code != 401 {
		t.Errorf("probe token'ıyla kullanıcı API'si: %d, 401 bekleniyordu", code)
	}
	if code, _, _ := admin.rawReq("POST", "/api/monitors", bearer(cp.Token), map[string]any{}); code != 403 {
		t.Errorf("probe token'ıyla X-Uptime'sız kullanıcı isteği CSRF'e takılmalı: %d", code)
	}
	// Sonuç gönderimi X-Uptime başlığı olmadan geçer (CSRF muafiyeti).
	code, _, body := admin.rawReq("POST", "/api/probe/results", bearer(cp.Token), map[string]any{"results": []any{}})
	if code != 200 {
		t.Fatalf("sonuç gönderimi: %d %s", code, body)
	}
	if code, _, _ := admin.rawReq("POST", "/api/probe/results", bearer("upr_yanlis"), map[string]any{}); code != 401 {
		t.Errorf("geçersiz token ile sonuç: %d", code)
	}

	// İstek sınırı.
	limited := false
	for range 250 {
		if code, hdr, _ := admin.rawReq("GET", "/api/probe/jobs", bearer(cp.Token), nil); code == 429 {
			limited = hdr.Get("Retry-After") != ""
			break
		}
	}
	if !limited {
		t.Error("istek sınırı uygulanmadı")
	}
}

func TestProbeScopingAndResults(t *testing.T) {
	f := newFeatureEnv(t)
	admin := f.env
	ctx := context.Background()
	a, b := admin.newProbe("A"), admin.newProbe("B")
	m1 := admin.httpMonitor("m1", "https://a.example")
	m2 := admin.httpMonitor("m2", "https://b.example")
	m3 := admin.httpMonitor("m3", "https://c.example") // yalnızca ana sunucu
	// m1 yalnızca A'dan kontrol edilir (ana sunucu testte dış adrese çıkamaz).
	admin.setLocations(m1.ID, map[string]any{"include_local": false, "probe_ids": []int64{a.Probe.ID}, "down_when": "any"}, 200)
	admin.setLocations(m2.ID, map[string]any{"include_local": false, "probe_ids": []int64{b.Probe.ID, b.Probe.ID}}, 200)
	push := admin.push("push")

	jobs := func(token string) (ids []int64, raw string) {
		code, _, body := admin.rawReq("GET", "/api/probe/jobs", bearer(token), nil)
		if code != 200 {
			t.Fatalf("iş listesi: %d %s", code, body)
		}
		var resp struct {
			PollAfter int        `json:"poll_after"`
			Jobs      []probeJob `json:"jobs"`
		}
		json.Unmarshal(body, &resp)
		if resp.PollAfter != 30 {
			t.Errorf("poll_after %d", resp.PollAfter)
		}
		for _, j := range resp.Jobs {
			ids = append(ids, j.ID)
		}
		return ids, string(body)
	}
	ids, raw := jobs(a.Token)
	if len(ids) != 1 || ids[0] != m1.ID {
		t.Fatalf("A yalnızca m1'i görmeli: %v", ids)
	}
	// Kontrol noktası şifreler dahil tam ayarı alır.
	if !strings.Contains(raw, "cok-gizli-parola") || strings.Contains(raw, "https://b.example") {
		t.Fatalf("A'nın iş listesi: %s", raw)
	}
	if ids, _ := jobs(b.Token); len(ids) != 1 || ids[0] != m2.ID {
		t.Fatalf("B yalnızca m2'yi görmeli: %v", ids)
	}

	// A, B'nin ve atanmamış monitörün sonucunu gönderemez.
	now := time.Now().UnixMilli()
	post := func(token string, body any) (int, map[string]any) {
		code, _, data := admin.rawReq("POST", "/api/probe/results", bearer(token), body)
		var out map[string]any
		json.Unmarshal(data, &out)
		return code, out
	}
	code, out := post(a.Token, map[string]any{"sent_at": now, "results": []map[string]any{
		{"monitor_id": m1.ID, "time": now - 500, "up": false, "ping_ms": -1, "message": "HTTP 503\x00 Service Unavailable"},
		{"monitor_id": m2.ID, "time": now, "up": true, "ping_ms": 10},
		{"monitor_id": m3.ID, "time": now, "up": true, "ping_ms": 10},
		{"monitor_id": 9999, "time": now, "up": true},
		{"monitor_id": m1.ID, "time": now - 2*3600*1000, "up": true},
		{"monitor_id": m1.ID, "time": 0, "up": true},
	}})
	if code != 200 || out["accepted"] != float64(1) || len(out["rejected"].([]any)) != 5 {
		t.Fatalf("sonuç: %d %v", code, out)
	}
	rej := out["rejected"].([]any)[0].(map[string]any)
	if rej["index"] != float64(1) || rej["error"] != "Monitör bu kontrol noktasına atanmamış" {
		t.Errorf("ret: %v", rej)
	}
	// A'nın 503 sonucu m1'i (any kuralı, 0 tekrar) DOWN yapar; mesajda konum adı var.
	waitFor(t, "m1 DOWN", func() bool {
		m, _ := f.st.GetMonitor(ctx, m1.ID)
		return m.Status == store.StatusDown && m.LastMessage == "A: HTTP 503  Service Unavailable"
	})
	var lv locationsView
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d/locations", m1.ID), nil, &lv, 200)
	if lv.IncludeLocal || len(lv.ProbeIDs) != 1 || lv.DownWhen != "any" || !lv.Supported || len(lv.Locations) != 1 ||
		lv.Locations[0].Name != "A" || lv.Locations[0].Status != "down" || lv.Locations[0].ProbeID != a.Probe.ID {
		t.Fatalf("konumlar: %+v", lv)
	}
	// Monitör listesinde konum özeti.
	var ms []monitorView
	admin.mustDo("GET", "/api/monitors", nil, &ms, 200)
	for _, m := range ms {
		switch m.ID {
		case m2.ID:
			if m.Locations.IncludeLocal || len(m.Locations.ProbeIDs) != 1 || m.Locations.ProbeIDs[0] != b.Probe.ID {
				t.Errorf("m2 konumları: %+v", m.Locations)
			}
		case m3.ID:
			if !m.Locations.IncludeLocal || len(m.Locations.ProbeIDs) != 0 || m.Locations.DownWhen != "any" {
				t.Errorf("m3 konumları: %+v", m.Locations)
			}
		}
	}

	// Parti sınırı ve bozuk gövde.
	big := make([]map[string]any, 501)
	for i := range big {
		big[i] = map[string]any{"monitor_id": m1.ID, "time": now, "up": true}
	}
	if code, _ := post(a.Token, map[string]any{"sent_at": now, "results": big}); code != 413 {
		t.Errorf("501 sonuç: %d, 413 bekleniyordu", code)
	}
	if code, _, _ := admin.rawReq("POST", "/api/probe/results", bearer(a.Token), "bozuk"); code != 400 {
		t.Errorf("bozuk gövde: %d", code)
	}

	// Durdurulan monitör iş listesinden çıkar, sonucu reddedilir.
	admin.mustDo("POST", fmt.Sprintf("/api/monitors/%d/pause", m1.ID), nil, nil, 200)
	if ids, _ := jobs(a.Token); len(ids) != 0 {
		t.Fatalf("durdurulan monitör iş listesinde: %v", ids)
	}
	if _, out := post(a.Token, map[string]any{"results": []map[string]any{{"monitor_id": m1.ID, "time": time.Now().UnixMilli(), "up": true}}}); out["accepted"] != float64(0) {
		t.Errorf("durdurulan monitörün sonucu: %v", out)
	}

	// Konum doğrulaması.
	admin.setLocations(m3.ID, map[string]any{"include_local": false, "probe_ids": []int64{}}, 400)
	admin.setLocations(m3.ID, map[string]any{"probe_ids": []int64{a.Probe.ID}, "down_when": "most"}, 400)
	admin.setLocations(m3.ID, map[string]any{"probe_ids": []int64{12345}}, 400)
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/locations", m3.ID), map[string]any{"bilinmeyen": 1}, nil, 400)
	admin.mustDo("PUT", "/api/monitors/99999/locations", map[string]any{"probe_ids": []int64{a.Probe.ID}}, nil, 404)
	admin.setLocations(push.ID, map[string]any{"probe_ids": []int64{a.Probe.ID}}, 400)
	// Push için yalnızca ana sunucu ayarı sorun değil.
	pv := admin.setLocations(push.ID, map[string]any{"include_local": true, "probe_ids": []int64{}}, 200)
	if pv.Supported {
		t.Error("push monitörü kontrol noktasını desteklemez")
	}

	// Yetkiler ve müşteri kısıtı.
	viewer, _ := admin.newUser("viewer1", store.RoleViewer, nil)
	restricted, _ := admin.newUser("musteri", store.RoleViewer, []int64{m3.ID})
	viewer.mustDo("GET", fmt.Sprintf("/api/monitors/%d/locations", m2.ID), nil, nil, 200)
	viewer.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/locations", m2.ID), map[string]any{"include_local": true}, nil, 403)
	restricted.mustDo("GET", fmt.Sprintf("/api/monitors/%d/locations", m2.ID), nil, nil, 404)
	restricted.mustDo("GET", fmt.Sprintf("/api/monitors/%d/locations", m3.ID), nil, nil, 200)
	editor, _ := admin.newUser("editor1", store.RoleEditor, nil)
	editor.setLocations(m3.ID, map[string]any{"include_local": true, "probe_ids": []int64{b.Probe.ID}, "down_when": "majority"}, 200)

	// Kontrol noktası silinince yalnızca ona bağlı monitör ana sunucuya döner.
	admin.mustDo("DELETE", fmt.Sprintf("/api/probes/%d", b.Probe.ID), nil, nil, 200)
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d/locations", m2.ID), nil, &lv, 200)
	if !lv.IncludeLocal || len(lv.ProbeIDs) != 0 {
		t.Fatalf("m2 ana sunucuya dönmeliydi: %+v", lv)
	}
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d/locations", m3.ID), nil, &lv, 200)
	if !lv.IncludeLocal || len(lv.ProbeIDs) != 0 {
		t.Fatalf("m3: %+v", lv)
	}

	var audit []store.AuditEntry
	admin.mustDo("GET", "/api/audit", nil, &audit, 200)
	found := false
	for _, a := range audit {
		if a.Action == "monitor.locations" && a.TargetID == m1.ID && a.Detail == "konumlar: A; kural: any" {
			found = true
		}
	}
	if !found {
		t.Error("konum değişikliği işlem kaydında yok")
	}
}

// Kontrol noktası isteklerinin tarayıcı çerezleriyle ilgisi yok: çerezli
// istemcinin probe token'ıyla yaptığı istek de probe olarak doğrulanır.
func TestProbeRequestIgnoresCookie(t *testing.T) {
	f := newFeatureEnv(t)
	cp := f.newProbe("P")
	req, _ := http.NewRequest("POST", f.srv.URL+"/api/probe/results", strings.NewReader(`{"results":[]}`))
	req.Header.Set("Authorization", "Bearer "+cp.Token)
	resp, err := f.client.Do(req) // oturum çerezi de gider
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("durum %d", resp.StatusCode)
	}
}
