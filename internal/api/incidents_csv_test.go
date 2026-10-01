package api

import (
	"encoding/csv"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// TestIncidentsCSV: GET /api/incidents?format=csv süzgece uyan olayları CSV
// olarak indirir; sütunlar sabittir, süren olayın süresi şu ana kadardır,
// müşteri kısıtlı izleyici yalnızca izinli monitörün satırlarını görür.
func TestIncidentsCSV(t *testing.T) {
	admin, srv := setupAdminSrv(t)
	a := admin.push("Alfa")
	b := admin.push("Beta")
	ctx := t.Context()
	now := time.Now().Unix()
	if err := srv.store.OpenIncident(ctx, a.ID, now-3600, "HTTP 503"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.ResolveIncident(ctx, a.ID, now-3000); err != nil {
		t.Fatal(err)
	}
	if err := srv.store.OpenIncident(ctx, b.ID, now-600, "Zaman aşımı, \"tırnak\""); err != nil {
		t.Fatal(err)
	}

	rows, hdr := admin.csv(t, "/api/incidents?format=csv")
	if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cd := hdr.Get("Content-Disposition"); !strings.Contains(cd, `filename="olaylar-`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if len(rows) != 3 || strings.Join(rows[0], ",") != "id,kind,source,started_at,resolved_at,duration_seconds,cause" {
		t.Fatalf("satırlar: %v", rows)
	}
	// En yeni önce: Beta (süren), sonra Alfa (çözülmüş, 600 sn).
	if rows[1][2] != "Beta" || rows[1][4] != "" || rows[1][6] != "Zaman aşımı, \"tırnak\"" || rows[1][1] != store.IncidentMonitor {
		t.Errorf("süren olay satırı yanlış: %v", rows[1])
	}
	if rows[2][2] != "Alfa" || rows[2][5] != "600" || rows[2][4] == "" {
		t.Errorf("çözülmüş olay satırı yanlış: %v", rows[2])
	}
	if _, err := time.Parse(time.RFC3339, rows[2][3]); err != nil {
		t.Errorf("başlangıç RFC 3339 olmalı: %q", rows[2][3])
	}

	// Süzgeç: yalnızca sunucu olayları → boş liste (yalnızca başlık).
	if rows, _ := admin.csv(t, "/api/incidents?format=csv&kind=server"); len(rows) != 1 {
		t.Errorf("sunucu süzgeci boş olmalı: %v", rows)
	}
	// Müşteri kısıtlı izleyici yalnızca Alfa'yı görür; dil İngilizce ise dosya adı da.
	viewer, _ := admin.newUser("musteri", store.RoleViewer, []int64{a.ID})
	viewer.mustDo("PUT", "/api/auth/preferences", map[string]any{"lang": "en"}, nil, 200)
	rows, hdr = viewer.csv(t, "/api/incidents?format=csv")
	if len(rows) != 2 || rows[1][2] != "Alfa" {
		t.Errorf("kısıtlı izleyici yalnızca izinli monitörü görmeli: %v", rows)
	}
	if cd := hdr.Get("Content-Disposition"); !strings.Contains(cd, `filename="incidents-`) {
		t.Errorf("İngilizce dosya adı bekleniyordu: %q", cd)
	}
}

// csv CSV yanıtını okur (UTF-8 BOM atılır) ve satırlarla başlıkları döner.
func (e *env) csv(t *testing.T, path string) ([][]string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("%s: durum %d: %s", path, resp.StatusCode, data)
	}
	if !strings.HasPrefix(string(data), "\xEF\xBB\xBF") {
		t.Errorf("UTF-8 BOM bekleniyordu")
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\xEF\xBB\xBF"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows, resp.Header
}
