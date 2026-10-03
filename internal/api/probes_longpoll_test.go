package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/probe"
)

// Uzun yoklama (long-poll) testleri: iş listesi isteği liste güncelse
// bekletilir, değişiklikte hemen yanıtlanır (probes.go, probeJobs).

type jobsReply struct {
	PollAfter int        `json:"poll_after"`
	Version   *int64     `json:"version"`
	Jobs      []probeJob `json:"jobs"`
}

type polled struct {
	code int
	resp jobsReply
	took time.Duration
	err  error
}

// pollJobsAsync iş listesini ister (goroutine'den de çağrılabilir: t.Fatal yok).
func (e *env) pollJobsAsync(ctx context.Context, token, query string) polled {
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/probe/jobs"+query, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return polled{err: err, took: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	p := polled{code: resp.StatusCode, took: time.Since(start)}
	if resp.StatusCode == 200 {
		p.err = json.Unmarshal(body, &p.resp)
	}
	return p
}

func (e *env) pollJobs(token, query string) polled {
	e.t.Helper()
	p := e.pollJobsAsync(context.Background(), token, query)
	if p.err != nil {
		e.t.Fatal(p.err)
	}
	return p
}

// goPoll isteği arka planda başlatır; sonuç kanaldan gelir.
func (e *env) goPoll(token, query string) <-chan polled {
	ch := make(chan polled, 1)
	go func() { ch <- e.pollJobsAsync(context.Background(), token, query) }()
	return ch
}

// notYet isteğin d süresince yanıtlanmadığını (bekletildiğini) doğrular.
func notYet(t *testing.T, ch <-chan polled, d time.Duration) {
	t.Helper()
	select {
	case p := <-ch:
		t.Fatalf("istek bekletilmeliydi, %v sonra yanıtlandı (durum %d)", p.took, p.code)
	case <-time.After(d):
	}
}

func recv(t *testing.T, ch <-chan polled, within time.Duration) polled {
	t.Helper()
	select {
	case p := <-ch:
		if p.err != nil {
			t.Fatal(p.err)
		}
		return p
	case <-time.After(within):
		t.Fatalf("istek %v içinde yanıtlanmadı", within)
	}
	return polled{}
}

func TestProbeLongPollHoldAndTimeout(t *testing.T) {
	f := newFeatureEnv(t)
	f.s.probeHold = 1500 * time.Millisecond
	cp := f.newProbe("P")

	// İlk istek hemen yanıtlanır (sunucu ajanın listesini bilmiyor).
	first := f.pollJobs(cp.Token, "")
	if first.code != 200 || first.took > time.Second || first.resp.Version == nil || first.resp.PollAfter != 1 {
		t.Fatalf("ilk istek: %+v", first)
	}
	// Değişiklik yok: süre dolana kadar bekletilir, aynı sürümle yanıtlanır.
	second := f.pollJobs(cp.Token, "")
	if second.code != 200 || second.took < 1400*time.Millisecond || second.took > 5*time.Second {
		t.Fatalf("ikinci istek bekletilmeliydi: %+v", second)
	}
	if *second.resp.Version != *first.resp.Version {
		t.Fatalf("sürüm değişmemeli: %d → %d", *first.resp.Version, *second.resp.Version)
	}
}

func TestProbeLongPollReleaseOnChange(t *testing.T) {
	f := newFeatureEnv(t)
	f.s.probeHold = 10 * time.Second
	cp := f.newProbe("P")
	mon := f.httpMonitor("site", "https://a.example")
	f.pollJobs(cp.Token, "") // ilk liste

	// Konum ataması: bekleyen istek hemen, yeni işle yanıtlanır.
	ch := f.goPoll(cp.Token, "")
	notYet(t, ch, 300*time.Millisecond)
	start := time.Now()
	f.setLocations(mon.ID, map[string]any{"include_local": false, "probe_ids": []int64{cp.Probe.ID}}, 200)
	p := recv(t, ch, 2*time.Second)
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("değişiklikten %v sonra yanıtlandı", took)
	}
	if p.code != 200 || len(p.resp.Jobs) != 1 || p.resp.Jobs[0].ID != mon.ID {
		t.Fatalf("yeni iş gelmeliydi: %+v", p)
	}

	// Monitör ekleme de bekleyen isteği uyandırır.
	since := fmt.Sprintf("?since=%d", *p.resp.Version)
	ch = f.goPoll(cp.Token, since)
	notYet(t, ch, 300*time.Millisecond)
	start = time.Now()
	f.httpMonitor("yeni", "https://b.example")
	recv(t, ch, 2*time.Second)
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("monitör eklendikten %v sonra yanıtlandı", took)
	}

	// Durdurma: iş listeden hemen çıkar (durdurma Remove'dan sonra yazılır).
	cur := f.pollJobs(cp.Token, "?since=0")
	ch = f.goPoll(cp.Token, fmt.Sprintf("?since=%d", *cur.resp.Version))
	notYet(t, ch, 200*time.Millisecond)
	f.mustDo("POST", fmt.Sprintf("/api/monitors/%d/pause", mon.ID), nil, nil, 200)
	p = recv(t, ch, 2*time.Second)
	if len(p.resp.Jobs) != 0 {
		// Uyanma Remove'da gelmiş olabilir (durdurma henüz yazılmamış): sürüm
		// yine ilerlemiştir, sonraki istek hemen güncel listeyi alır.
		p = f.pollJobs(cp.Token, fmt.Sprintf("?since=%d", *p.resp.Version))
		if p.took > time.Second || len(p.resp.Jobs) != 0 {
			t.Fatalf("durdurulan iş listeden çıkmalı: %+v", p)
		}
	}
}

func TestProbeLongPollSince(t *testing.T) {
	f := newFeatureEnv(t)
	f.s.probeHold = 1500 * time.Millisecond
	cp := f.newProbe("P")
	cur := f.pollJobs(cp.Token, "?since=0")
	if cur.took > time.Second || cur.resp.Version == nil {
		t.Fatalf("since=0 hemen yanıtlanmalı: %+v", cur)
	}
	v := *cur.resp.Version
	if p := f.pollJobs(cp.Token, fmt.Sprintf("?since=%d", v)); p.took < 1400*time.Millisecond {
		t.Fatalf("güncel sürüm bekletilmeli: %v", p.took)
	}
	// Eski veya başka süreçten (yeniden başlatma öncesi) sürüm: hemen.
	for _, q := range []string{fmt.Sprintf("?since=%d", v-1), fmt.Sprintf("?since=%d", v+1000), "?since=bozuk"} {
		if p := f.pollJobs(cp.Token, q); p.took > time.Second || p.code != 200 {
			t.Fatalf("%s hemen yanıtlanmalı: %+v", q, p)
		}
	}
}

// Eski ajanlar since göndermez: sunucu son teslim ettiği sürümü hatırlar.
func TestProbeLongPollOldAgentHeuristic(t *testing.T) {
	f := newFeatureEnv(t)
	f.s.probeHold = 1500 * time.Millisecond
	cp := f.newProbe("P")
	if p := f.pollJobs(cp.Token, ""); p.took > time.Second {
		t.Fatalf("ilk istek hemen: %v", p.took)
	}
	if p := f.pollJobs(cp.Token, ""); p.took < 1400*time.Millisecond {
		t.Fatalf("liste güncel: bekletilmeli: %v", p.took)
	}
	// Uzun süre istek gelmedi (ajan yeniden başlamış olabilir): hemen yanıtla.
	f.clk.advance(probeFreshFor + time.Second)
	if p := f.pollJobs(cp.Token, ""); p.took > time.Second {
		t.Fatalf("uzun aradan sonra hemen yanıtlanmalı: %v", p.took)
	}
	// Başka bir kontrol noktasının teslimi bu ajanı etkilemez.
	other := f.newProbe("Q")
	if p := f.pollJobs(other.Token, ""); p.took > time.Second {
		t.Fatalf("diğer ajanın ilk isteği hemen: %v", p.took)
	}
}

// Bekleme sırasında ajan devre dışı bırakılır veya token'ı yenilenirse bekleyen
// istek hemen reddedilir.
func TestProbeLongPollRevalidate(t *testing.T) {
	f := newFeatureEnv(t)
	f.s.probeHold = 10 * time.Second
	cp := f.newProbe("P")
	f.pollJobs(cp.Token, "")

	ch := f.goPoll(cp.Token, "")
	notYet(t, ch, 200*time.Millisecond)
	f.mustDo("PUT", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), map[string]any{"name": "P", "active": false}, nil, 200)
	if p := recv(t, ch, 2*time.Second); p.code != 403 {
		t.Fatalf("devre dışı: %d, 403 bekleniyordu", p.code)
	}
	f.mustDo("PUT", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), map[string]any{"name": "P", "active": true}, nil, 200)
	f.pollJobs(cp.Token, "")

	ch = f.goPoll(cp.Token, "")
	notYet(t, ch, 200*time.Millisecond)
	f.mustDo("POST", fmt.Sprintf("/api/probes/%d/token", cp.Probe.ID), nil, nil, 200)
	if p := recv(t, ch, 2*time.Second); p.code != 401 {
		t.Fatalf("token yenilendi: %d, 401 bekleniyordu", p.code)
	}
}

// İstek iptal edilince (ajan bağlantıyı kapattı / sunucu kapanıyor) bekleme
// hemen biter; kapanışta ajan geçerli bir liste alır.
func TestProbeLongPollCancelAndShutdown(t *testing.T) {
	f := newFeatureEnv(t)
	f.s.probeHold = 10 * time.Second
	cp := f.newProbe("P")
	f.pollJobs(cp.Token, "") // ilk liste; IP 127.0.0.1'e kilitlenir

	// İptal: işleyici hemen döner.
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/api/probe/jobs", nil).WithContext(ctx)
	req.RemoteAddr = "127.0.0.1:40000"
	req.Header.Set("Authorization", "Bearer "+cp.Token)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { f.s.Handler().ServeHTTP(rec, req); close(done) }()
	time.Sleep(200 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("istek bekletilmeliydi")
	default:
	}
	start := time.Now()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("iptal edilen istek dönmedi")
	}
	if took := time.Since(start); took > time.Second || rec.Code != 200 {
		t.Fatalf("iptal: %v sonra, durum %d", took, rec.Code)
	}

	// Kapanış: BaseContext iptal edilince (cmd/uptime) bekleyen istek hemen
	// geçerli bir listeyle yanıtlanır.
	base, stop := context.WithCancel(context.Background())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: f.s.Handler(), BaseContext: func(net.Listener) context.Context { return base }}
	go hs.Serve(ln)
	defer hs.Close()
	senv := &env{t: t, srv: &httptest.Server{URL: "http://" + ln.Addr().String()}}
	ch := senv.goPoll(cp.Token, "")
	notYet(t, ch, 200*time.Millisecond)
	start = time.Now()
	stop()
	p := recv(t, ch, 2*time.Second)
	if took := time.Since(start); took > time.Second || p.code != 200 || p.resp.Version == nil {
		t.Fatalf("kapanış: %v sonra %+v", took, p)
	}
}

// Uçtan uca: gerçek kontrol noktası istemcisi uzun yoklamada bekler; yeni
// monitör atanınca işi ~1 sn içinde alır, konum önce "ilk sonuç bekleniyor",
// sonra "çalışıyor" olur.
func TestProbeLongPollEndToEnd(t *testing.T) {
	f := newFeatureEnv(t)
	f.s.probeHold = 10 * time.Second // uzun yoklama olmasaydı iş en erken 10 sn sonra gelirdi
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "merhaba") }))
	defer site.Close()
	cp := f.newProbe("CP Server İstanbul")

	c, err := probe.New(probe.Config{
		Server: f.srv.URL, Token: cp.Token, Version: "e2e", Unit: time.Millisecond, AllowInsecure: true,
		FlushEvery: 20 * time.Millisecond, MaxBackoff: 50 * time.Millisecond,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	pctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(pctx); close(done) }()
	defer func() { cancel(); <-done }()

	// Ajan ilk listeyi (boş) aldı ve şimdi bekletiliyor.
	waitFor(t, "ilk iş listesi", func() bool { _, ok := f.s.probePolls.get(cp.Probe.ID); return ok })
	time.Sleep(300 * time.Millisecond)

	start := time.Now()
	var mon monitorView
	f.mustDo("POST", "/api/monitors", map[string]any{
		"name": "Site", "type": "http", "interval": 5000, "retry_interval": 5000, "timeout": 300,
		"config": map[string]any{"url": site.URL},
	}, &mon, 201)
	v := f.setLocations(mon.ID, map[string]any{"include_local": false, "probe_ids": []int64{cp.Probe.ID}}, 200)
	if len(v.Locations) != 1 || v.Locations[0].Status != "waiting" {
		t.Fatalf("yeni konum ilk sonucu beklemeli: %+v", v.Locations)
	}
	var gotJob, gotUp time.Duration
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && gotUp == 0 {
		if gotJob == 0 {
			for _, id := range c.Jobs() {
				if id == mon.ID {
					gotJob = time.Since(start)
				}
			}
		}
		var lv locationsView
		f.mustDo("GET", fmt.Sprintf("/api/monitors/%d/locations", mon.ID), nil, &lv, 200)
		if len(lv.Locations) == 1 {
			switch lv.Locations[0].Status {
			case "up":
				gotUp = time.Since(start)
			case "waiting":
			default:
				t.Fatalf("beklenmeyen konum durumu: %+v", lv.Locations[0])
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if gotJob == 0 && gotUp > 0 {
		// Ajanın sonucu geldiyse işi almıştır: hızlı makinede iş, döngü
		// c.Jobs()'u okumadan başlayıp sonuç göndermiş olabilir.
		gotJob = gotUp
	}
	if gotJob == 0 || gotJob > 2*time.Second || gotUp == 0 || gotUp > 3*time.Second {
		t.Fatalf("iş %v, ilk sonuç %v sonra (iş en fazla 2 sn, sonuç 3 sn bekleniyordu)", gotJob, gotUp)
	}
	t.Logf("iş %v, konumun ilk sonucu %v sonra", gotJob.Round(time.Millisecond), gotUp.Round(time.Millisecond))
}

// Ajan bekleyen iş listesi isteğini kopardığında (durduruldu/silindi/çöktü)
// kopukluk hemen kaydedilir; normal yanıttan sonra yeniden bağlanmazsa
// probeReconnect sonra kaydedilir; yeniden bağlanınca silinir.
func TestProbeDisconnectDetected(t *testing.T) {
	f := newFeatureEnv(t)
	f.s.probeHold = 10 * time.Second
	f.s.probeReconnect = 300 * time.Millisecond
	cp := f.newProbe("P")
	id := cp.Probe.ID
	first := f.pollJobs(cp.Token, "?since=0")
	since := fmt.Sprintf("?since=%d", *first.resp.Version)

	waitGone := func(want bool, within time.Duration) time.Duration {
		t.Helper()
		start := time.Now()
		for f.s.engine.ProbeDisconnected(id) != want {
			if time.Since(start) > within {
				t.Fatalf("kopukluk %v olmadı (%v)", want, within)
			}
			time.Sleep(5 * time.Millisecond)
		}
		return time.Since(start)
	}

	// Bekleyen istek ajan tarafından koparılır: hemen.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan polled, 1)
	go func() { done <- f.pollJobsAsync(ctx, cp.Token, since) }()
	time.Sleep(200 * time.Millisecond)
	if f.s.engine.ProbeDisconnected(id) {
		t.Fatal("bağlıyken kopuk sayıldı")
	}
	cancel()
	<-done
	if took := waitGone(true, time.Second); took > 250*time.Millisecond {
		t.Fatalf("koparılan bağlantı %v sonra fark edildi", took)
	}

	// Yeniden bağlanınca silinir.
	ch := f.goPoll(cp.Token, since)
	waitGone(false, time.Second)

	// Sunucu normal yanıt verdi (iş listesi değişti), ajan geri gelmedi.
	f.httpMonitor("site", "https://a.example")
	recv(t, ch, 2*time.Second)
	time.Sleep(100 * time.Millisecond)
	if f.s.engine.ProbeDisconnected(id) {
		t.Fatal("normal yanıttan hemen sonra kopuk sayılmamalı (ajan yeniden bağlanıyor)")
	}
	waitGone(true, 2*time.Second)
}
