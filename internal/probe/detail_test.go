package probe

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kadirsa1105/uptime-kadir-app/internal/check"
)

// Başarısız HTTP kontrolünün isteği/yanıtı (olay sayfası) sonuçla gider;
// başarılı sonuçta ayrıntı yoktur.
func TestFailedResultCarriesDetail(t *testing.T) {
	fs := newFakeServer(t)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "hatalı istek")
	}))
	t.Cleanup(bad.Close)
	good, _ := site(t)
	fs.set(func(f *fakeServer) { f.jobs = []Job{httpJob(1, bad.URL, 20), httpJob(2, good.URL, 20)} })
	start(t, Config{Server: fs.srv.URL})
	var failed, ok *Result
	waitFor(t, "iki sonuç", func() bool {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		for _, b := range fs.batches {
			for i := range b {
				if b[i].MonitorID == 1 && failed == nil {
					failed = &b[i]
				}
				if b[i].MonitorID == 2 && ok == nil {
					ok = &b[i]
				}
			}
		}
		return failed != nil && ok != nil
	})
	if failed.Up || failed.Detail == nil || failed.Detail.Status != 400 || failed.Detail.Body != "hatalı istek" {
		t.Fatalf("başarısız sonucun ayrıntısı: %+v", failed.Detail)
	}
	if !ok.Up || ok.Detail != nil {
		t.Fatalf("başarılı sonuçta ayrıntı olmamalı: %+v", ok)
	}
}

// Tek partideki ayrıntıların toplamı sınırlıdır; sınırı aşanlar ayrıntısız gider,
// tampondaki asıl sonuçlar değişmez.
func TestLimitDetails(t *testing.T) {
	body := strings.Repeat("a", check.DetailBodyMax)
	var batch []Result
	for i := range 40 { // ~40 × 16 KB > 256 KB
		batch = append(batch, Result{MonitorID: int64(i), Detail: &check.Detail{Method: "GET", Body: body}})
	}
	batch = append(batch, Result{MonitorID: 99, Up: true})
	out := limitDetails(batch)
	total, with := 0, 0
	for _, r := range out {
		if r.Detail != nil {
			b, _ := json.Marshal(r.Detail)
			total += len(b)
			with++
		}
	}
	if total > maxBatchDetail || with == 0 || with == 40 || len(out) != len(batch) {
		t.Fatalf("sınır uygulanmadı: toplam %d, ayrıntılı %d", total, with)
	}
	for _, r := range batch {
		if r.MonitorID != 99 && r.Detail == nil {
			t.Fatal("tampondaki sonuç değiştirildi")
		}
	}
	small := []Result{{MonitorID: 1}}
	if got := limitDetails(small); &got[0] != &small[0] {
		t.Fatal("ayrıntısız parti kopyalanmamalı")
	}
}
