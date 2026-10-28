package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// getPage GET isteği; gövde ve X-Total-Count.
func (e *env) getPage(t *testing.T, path string, out any) (int, int) {
	t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == 200 {
		json.NewDecoder(resp.Body).Decode(out)
	}
	total, _ := strconv.Atoi(resp.Header.Get("X-Total-Count"))
	return resp.StatusCode, total
}

// Sayfa numaralı olay listesi: sayfa boyu, ofset, toplam, tür ve arama süzgeci
// birlikte; üst sınırlar; CSV ofseti yok sayar (tüm süzülmüş sonuç).
func TestIncidentsPaged(t *testing.T) {
	admin, srv := setupAdminSrv(t)
	a := admin.push("Alfa")
	b := admin.push("Beta")
	ctx := t.Context()
	now := time.Now().Unix() - 100000
	// Alfa: 30 kapanmış olay, Beta: 7 kapanmış olay.
	for i := 0; i < 30; i++ {
		ts := now + int64(i)*100
		srv.store.OpenIncident(ctx, a.ID, ts, "HTTP 503")
		srv.store.ResolveIncident(ctx, a.ID, ts+10)
	}
	for i := 0; i < 7; i++ {
		ts := now + 5000 + int64(i)*100
		srv.store.OpenIncident(ctx, b.ID, ts, "Zaman aşımı")
		srv.store.ResolveIncident(ctx, b.ID, ts+10)
	}
	var list []store.Incident
	code, total := admin.getPage(t, "/api/incidents?offset=0&limit=25", &list)
	if code != 200 || total != 37 || len(list) != 25 {
		t.Fatalf("1. sayfa: %d toplam %d satır %d", code, total, len(list))
	}
	first := list[0].ID
	code, total = admin.getPage(t, "/api/incidents?offset=25&limit=25", &list)
	if code != 200 || total != 37 || len(list) != 12 || list[0].ID >= first {
		t.Fatalf("2. sayfa: %d toplam %d satır %d", code, total, len(list))
	}
	// Arama + tür süzgeci toplamı daraltır.
	_, total = admin.getPage(t, "/api/incidents?offset=0&limit=50&q=beta&kind=monitor", &list)
	if total != 7 || len(list) != 7 {
		t.Fatalf("süzülmüş: toplam %d satır %d", total, len(list))
	}
	_, total = admin.getPage(t, "/api/incidents?offset=0&limit=50&kind=server", &list)
	if total != 0 || len(list) != 0 {
		t.Fatalf("sunucu sekmesi: %d", total)
	}
	// Üst sınırlar: sayfa boyu 100'ü geçemez, ofset sınırlı, negatif yok.
	_, total = admin.getPage(t, "/api/incidents?offset=0&limit=100000", &list)
	if len(list) != 37 || total != 37 {
		t.Fatalf("büyük limit: %d", len(list))
	}
	if code, _ := admin.getPage(t, "/api/incidents?offset=-1", nil); code != 400 {
		t.Fatalf("negatif ofset: %d", code)
	}
	if code, _ := admin.getPage(t, fmt.Sprintf("/api/incidents?offset=%d", store.MaxIncidentOffset+1), nil); code != 400 {
		t.Fatalf("çok büyük ofset: %d", code)
	}
	// Eski imleçli kullanım (ofsetsiz) değişmedi: toplam başlığı yok.
	if _, total := admin.getPage(t, "/api/incidents?limit=10", &list); total != 0 || len(list) != 10 {
		t.Fatalf("imleçli: %d %d", total, len(list))
	}
	// CSV ofseti ve sayfa boyunu yok sayar: süzülmüş sonucun tamamı.
	rows, _ := admin.csv(t, "/api/incidents?format=csv&offset=25&limit=25&q=alfa")
	if len(rows) != 31 { // başlık + 30
		t.Fatalf("CSV satırı %d", len(rows))
	}
}

func TestAuditPaged(t *testing.T) {
	admin, srv := setupAdminSrv(t)
	for i := 0; i < 40; i++ {
		srv.audit(httptest.NewRequest("GET", "/", nil), store.User{ID: 1, Username: "kadir"}, "monitor.create", "monitor", int64(i), fmt.Sprintf("m%d", i), "")
	}
	var list []store.AuditEntry
	code, total := admin.getPage(t, "/api/audit?offset=25&limit=25&action=monitor.create", &list)
	if code != 200 || total != 40 || len(list) != 15 {
		t.Fatalf("işlem kaydı 2. sayfa: %d toplam %d satır %d", code, total, len(list))
	}
}
