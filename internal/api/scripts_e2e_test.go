package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betik"
	"github.com/kadirsungurlu/bekci/internal/betikajan"
	"github.com/kadirsungurlu/bekci/internal/betikkayit"
	"github.com/kadirsungurlu/bekci/internal/probe"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Uçtan uca: gerçek panel (Timescale/PG) + gerçek probe istemcisi + gerçek ajan
// + gerçek systemd birimi. Panelle ajan arasındaki araya giren ele geçirilmiş
// paneli taklit eder: iş listesini ajana varmadan değiştirir. Çalışması için
// UPTIME_TEST_PG ve BEKCI_TEST_SYSTEMD=1 (root + systemd) gerekir.

const (
	e2eSelam  = `printf 'selam [%s] %s\n' "$BEKCI_P_AD" "$(id -un)"` + "\nexit 0\n"
	e2eCikis  = "echo patladi\nexit 3\n"
	e2eBekle  = "echo basladi\nsleep 60\necho bitti\n"
	e2eTemiz  = "echo temiz\n"
	e2eOnayli = "echo onayli\n"
	e2eOlmaz  = "echo olmaz\n"
)

type e2e struct {
	*sEnv
	paths betikajan.Paths
	agent *betikajan.Agent
	ts    *httptest.Server

	mu      sync.Mutex
	mangle  func(*betik.JobList)
	sawJobs []betik.RunJob
	polls   map[string]int
	// accepted olayını panelden saklar: holdAccepted kapanana dek bekletir
	// (gecikmiş olay), dropAccepted 503 döner (ajan bellekte tutar).
	holdAccepted chan struct{}
	heldAccepted chan struct{}
	dropAccepted bool
	stopProbe    func()
}

// gateEvent ajan olayını geciktirir ya da geri çevirir; false dönerse yanıt yazılmıştır.
func (x *e2e) gateEvent(w http.ResponseWriter, r *http.Request) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	var ev struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(body, &ev)
	if ev.Kind != "accepted" {
		return true
	}
	x.mu.Lock()
	hold, held, drop := x.holdAccepted, x.heldAccepted, x.dropAccepted
	x.mu.Unlock()
	if drop {
		w.WriteHeader(http.StatusServiceUnavailable)
		return false
	}
	if hold != nil {
		select {
		case held <- struct{}{}:
		default:
		}
		<-hold
	}
	return true
}

func (x *e2e) setMangle(f func(*betik.JobList)) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.mangle = f
}

// ServeHTTP paneli sarar: yalnız ajanın iş listesini değiştirebilir.
func (x *e2e) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/probe/jobs" {
		if r.URL.Path != "/api/probe/script-result" || x.gateEvent(w, r) {
			x.s.Handler().ServeHTTP(w, r)
		}
		return
	}
	x.mu.Lock()
	x.polls[r.Header.Get("Authorization")]++
	x.mu.Unlock()
	rec := httptest.NewRecorder()
	x.s.Handler().ServeHTTP(rec, r) // uzun yoklama: değiştirici yanıttan sonra okunur
	x.mu.Lock()
	f := x.mangle
	x.mu.Unlock()
	var doc map[string]json.RawMessage
	var l betik.JobList
	if rec.Code == 200 && json.Unmarshal(rec.Body.Bytes(), &doc) == nil && len(doc["script_jobs"]) > 0 {
		_ = json.Unmarshal(doc["script_jobs"], &l)
		x.mu.Lock()
		x.sawJobs = append(x.sawJobs, l.Runs...)
		x.mu.Unlock()
	}
	if f == nil || doc == nil {
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		w.Write(rec.Body.Bytes())
		return
	}
	f(&l)
	doc["script_jobs"], _ = json.Marshal(&l)
	b, _ := json.Marshal(doc)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(b)
}

func randHex(t *testing.T) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func setupE2E(t *testing.T) *e2e {
	t.Helper()
	if os.Getenv("BEKCI_TEST_SYSTEMD") != "1" {
		t.Skip("BEKCI_TEST_SYSTEMD=1 yok")
	}
	plat := betikajan.DetectPlatform()
	if !plat.OK { // değişken açıkça verildiyse sessiz atlama CI'da testi körleştirir
		t.Fatal(plat.Detail)
	}
	sx := setupScripts(t)
	sx.addKeys()
	d := "/run/bekci-hat1-e2e-" + randHex(t)
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	sx.s.probeRL.limit = 1 << 30 // yoklama 20 ms'de bir: 429 ajanı üstel beklemeye sokar
	x := &e2e{sEnv: sx, polls: map[string]int{}, paths: betikajan.Paths{Config: filepath.Join(d, "etc", "betik.json"), StateDir: filepath.Join(d, "state"), LogDir: filepath.Join(d, "log")}}
	_, err := betikajan.Enable(x.paths, betikajan.EnableOptions{
		Yayin: []string{betik.EncodePublicKey(pubOf(x.yayin))}, Onay: []string{betik.EncodePublicKey(pubOf(x.onay))},
		Groups: []string{"bakim"}, MaxRunsPerHour: 100,
		// İğneli "none" betikler ajanda biletsiz çalışır; iğnesiz olanlar bilet ister.
		Allow: []betikajan.Allow{
			{ID: "selam", MinApproval: "none", SHA256: betik.Digest([]byte(e2eSelam))},
			{ID: "cikis", MinApproval: "none", SHA256: betik.Digest([]byte(e2eCikis))},
			{ID: "bekle", MinApproval: "none", SHA256: betik.Digest([]byte(e2eBekle))},
			{ID: "kurcala", MinApproval: "none", SHA256: betik.Digest([]byte(e2eTemiz))},
			{ID: "panelli", MinApproval: "panel", SHA256: betik.Digest([]byte(e2eOnayli))},
			{ID: "biletli", MinApproval: "signed"},
			{ID: "grupsuz", MinApproval: "none", SHA256: betik.Digest([]byte(e2eOlmaz))},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	x.agent = betikajan.New(betikajan.Options{Paths: x.paths, Platform: &plat, Now: x.clk.now})
	x.ts = httptest.NewTLSServer(x)
	t.Cleanup(x.ts.Close)
	var cp struct {
		Probe struct {
			ID int64 `json:"id"`
		} `json:"probe"`
		Token string `json:"token"`
	}
	x.mustDo("POST", "/api/probes", map[string]any{"name": "e2e"}, &cp, 201)
	x.probe = createdProbeT{cp.Probe.ID, cp.Token}
	x.stopProbe = x.runProbe(t, x.agent, cp.Token)
	var pending string
	x.until("anahtar bekleyen görünmedi", func() bool {
		var hosts []scriptHostView
		x.mustDo("GET", "/api/scripts/hosts", nil, &hosts, 200)
		for _, h := range hosts {
			if h.ProbeID == x.probe.ID {
				pending = h.PendingKey
			}
		}
		return pending != ""
	})
	x.mustDo("POST", fmt.Sprintf("/api/scripts/hosts/%d/approve", x.probe.ID), map[string]any{"host_key": pending, "code": x.step()}, nil, 200)
	return x
}

// runProbe ajanı başlatır; dönen işlev süreci kapatır (yeniden başlatma denemesi).
func (x *e2e) runProbe(t *testing.T, a *betikajan.Agent, token string) (stop func()) {
	t.Helper()
	cfg := probe.Config{Server: x.ts.URL, Token: token, Version: "1.7.0", Unit: 20 * time.Millisecond,
		NoMetrics: true, NoInventory: true, HTTPClient: x.ts.Client()}
	if a != nil {
		cfg.Scripts = a
	}
	c, err := probe.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	stop = sync.OnceFunc(func() { cancel(); <-done })
	t.Cleanup(stop)
	return stop
}

func (x *e2e) until(what string, f func() bool) {
	x.t.Helper()
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if f() {
			return
		}
	}
	x.t.Fatal(what)
}

func (x *e2e) manifest(id string, ver int64, approval, body string, f func(*betik.Manifest)) (json.RawMessage, betik.Manifest) {
	x.t.Helper()
	now := x.clk.now().Unix()
	m := betik.Manifest{Schema: 1, ID: id, Version: ver, SHA256: betik.Digest([]byte(body)), Interpreter: "sh", RunAs: "nobody",
		Platforms: []string{"linux"}, Approval: approval, Groups: []string{"bakim"}, Params: []betik.Param{}, TimeoutSec: 60, OutputMax: 4096,
		MaxPerDay: 50, NotBefore: now - 10, NotAfter: now + 30*86400}
	if f != nil {
		f(&m)
	}
	p, err := json.Marshal(m)
	if err != nil {
		x.t.Fatal(err)
	}
	env, err := betik.Sign(x.yayin, betik.KindManifest, p)
	if err != nil {
		x.t.Fatal(err)
	}
	return env, m
}

func (x *e2e) put(id string, ver int64, approval, body string, f func(*betik.Manifest)) {
	x.t.Helper()
	env, _ := x.manifest(id, ver, approval, body, f)
	x.mustDo("POST", "/api/scripts", map[string]any{"manifest": env, "body": []byte(body), "code": x.step()}, nil, 201)
}

func (x *e2e) begin(id string, ver int64, extra map[string]any) runView {
	x.t.Helper()
	code, v, raw := x.start(x.env, id, ver, x.secret, extra)
	if code != 201 {
		x.t.Fatalf("başlatma %d: %s", code, raw)
	}
	return v
}

// final çalıştırma bitene dek bekler.
func (x *e2e) final(run string) (string, string, string) {
	x.t.Helper()
	var d struct {
		Run    runView `json:"run"`
		Output *struct {
			Text string `json:"text"`
		} `json:"output"`
	}
	x.until("çalıştırma bitmedi: "+run, func() bool {
		d.Output = nil
		x.mustDo("GET", "/api/scripts/runs/"+run, nil, &d, 200)
		switch d.Run.State {
		case runOK, runFailed, runTimeout, runCancelled, runLost, runAgentRejected, runExpired, runRejected:
			return true
		}
		return false
	})
	text := ""
	if d.Output != nil {
		text = d.Output.Text
	}
	return d.Run.State, d.Run.Code, text
}

func (x *e2e) wantRejected(run, code string) {
	x.t.Helper()
	st, c, out := x.final(run)
	if st != runAgentRejected || c != code || out != "" {
		x.t.Errorf("beklenen %s, gelen %s/%s çıktı %q", code, st, c, out)
	}
}

// Bütün senaryolar tek ajanla, sırayla: sürüm ve kota durumu birikir.
func TestScriptEndToEnd(t *testing.T) {
	x := setupE2E(t)

	// İmzalı betik, parametre düz veri olarak gider, nobody çalışır, çıktı ve makbuz panele gelir.
	ad := func(m *betik.Manifest) { m.Params = []betik.Param{{Name: "ad", Type: "name"}} }
	x.put("selam", 1, betik.ApprovalNone, e2eSelam, ad)
	if code, _, raw := x.start(x.env, "selam", 1, x.secret, map[string]any{"params": map[string]any{"ad": "$(touch /tmp/x);`id`"}}); code != 422 {
		t.Fatalf("kabuk karakterli parametre: %d %s", code, raw)
	}
	v := x.begin("selam", 1, map[string]any{"params": map[string]any{"ad": "web-1"}})
	st, _, out := x.final(v.RunID)
	if st != runOK || out != "selam [web-1] nobody\n" {
		t.Fatalf("mutlu yol: %s %q", st, out)
	}

	// Başarısız çıkış kodu makbuzda.
	x.put("cikis", 1, betik.ApprovalNone, e2eCikis, nil)
	v = x.begin("cikis", 1, nil)
	if st, _, out := x.final(v.RunID); st != runFailed || !strings.Contains(out, "patladi") {
		t.Fatalf("exit 3: %s %q", st, out)
	}

	// Geri sürüm: 2 görülmüşken 1 ajanda reddedilir.
	x.put("selam", 2, betik.ApprovalNone, e2eSelam, ad)
	v = x.begin("selam", 2, map[string]any{"params": map[string]any{"ad": "web-2"}})
	if st, _, _ := x.final(v.RunID); st != runOK {
		t.Fatalf("v2: %s", st)
	}
	v = x.begin("selam", 1, map[string]any{"params": map[string]any{"ad": "a"}})
	x.wantRejected(v.RunID, betik.CodeVersionRollback)

	// Bilet akışı: bilet gelene dek ajana hiçbir şey gitmez.
	x.put("biletli", 1, betik.ApprovalSigned, "echo biletle\n", nil)
	v = x.begin("biletli", 1, nil)
	if v.State != runAwaitTicket {
		t.Fatal(v.State)
	}
	time.Sleep(200 * time.Millisecond)
	if s := x.state(v.RunID); s != runAwaitTicket {
		t.Fatalf("biletsiz ilerledi: %s", s)
	}
	var d struct {
		Req map[string]any `json:"ticket_request"`
	}
	x.mustDo("GET", "/api/scripts/runs/"+v.RunID, nil, &d, 200)
	x.mustDo("POST", "/api/scripts/runs/"+v.RunID+"/ticket", map[string]any{"ticket": x.ticket(v, d.Req, nil), "code": x.step()}, nil, 200)
	if st, _, out := x.final(v.RunID); st != runOK || out != "biletle\n" {
		t.Fatalf("bilet: %s %q", st, out)
	}

	// Panel onayı: ikinci yönetici onaylamadan çalışmaz.
	ayse, ayseSec := x.secondAdmin()
	x.put("panelli", 1, betik.ApprovalPanel, e2eOnayli, nil)
	v = x.begin("panelli", 1, nil)
	if v.State != runAwaitApproval {
		t.Fatal(v.State)
	}
	x.clk.advance(31 * time.Second)
	ayse.mustDo("POST", "/api/scripts/runs/"+v.RunID+"/approve", map[string]any{"code": x.code(ayseSec)}, nil, 200)
	if st, _, _ := x.final(v.RunID); st != runOK {
		t.Fatalf("panel onayı: %s", st)
	}

	// İzinsiz sunucu: yerel izin listesinde yok / grup uymuyor.
	x.put("listedisi", 1, betik.ApprovalNone, e2eOlmaz, nil)
	x.wantRejected(x.begin("listedisi", 1, nil).RunID, betik.CodeNotAllowed)
	x.put("grupsuz", 1, betik.ApprovalNone, e2eOlmaz, func(m *betik.Manifest) { m.Groups = []string{"baska"} })
	x.wantRejected(x.begin("grupsuz", 1, nil).RunID, betik.CodeGroupDenied)

	// Ele geçirilmiş panel: geçerli iş ajana varmadan değiştirilir.
	x.put("kurcala", 1, betik.ApprovalNone, e2eTemiz, nil)
	body := func(j *betik.RunJob) { j.Body = []byte("echo KOTU\n") }
	foreign := genKey(t)
	attacks := []struct {
		name, code string
		f          func(*betik.RunJob)
	}{
		{"değiştirilmiş içerik", betik.CodeBodyMismatch, body},
		{"imzasız", betik.CodeEnvelopeInvalid, func(j *betik.RunJob) { j.Manifest = json.RawMessage(`{}`) }},
		{"yabancı anahtar", betik.CodeSigInvalid, func(j *betik.RunJob) {
			_, m := x.manifest("kurcala", 1, betik.ApprovalNone, "echo KOTU\n", nil)
			p, _ := json.Marshal(m)
			j.Manifest, _ = betik.Sign(foreign, betik.KindManifest, p)
			j.Body = []byte("echo KOTU\n")
		}},
		{"süresi geçmiş imza", betik.CodeExpired, func(j *betik.RunJob) {
			j.Manifest, _ = x.manifest("kurcala", 1, betik.ApprovalNone, e2eTemiz, func(m *betik.Manifest) {
				m.NotBefore, m.NotAfter = m.NotBefore-1000, x.clk.now().Unix()-1
			})
		}},
		{"gövde kaldırıldı", betik.CodeBodyMismatch, func(j *betik.RunJob) { j.Body = nil }},
	}
	for _, a := range attacks {
		x.setMangle(func(l *betik.JobList) {
			for i := range l.Runs {
				a.f(&l.Runs[i])
			}
		})
		v = x.begin("kurcala", 1, nil)
		st, code, out := x.final(v.RunID)
		x.setMangle(nil)
		if st != runAgentRejected || code != a.code || strings.Contains(out, "KOTU") {
			t.Errorf("%s: %s/%s %q (beklenen %s)", a.name, st, code, out, a.code)
		}
	}

	// Bilet atlatma: panel biletsiz gönderir, parametreyi değiştirir.
	x.put("biletli", 2, betik.ApprovalSigned, "echo \"[$BEKCI_P_AD]\"\n", func(m *betik.Manifest) {
		m.Params = []betik.Param{{Name: "ad", Type: "name"}}
	})
	ticketed := func(params string) runView {
		v := x.begin("biletli", 2, map[string]any{"params": json.RawMessage(params)})
		var d struct {
			Req map[string]any `json:"ticket_request"`
		}
		x.mustDo("GET", "/api/scripts/runs/"+v.RunID, nil, &d, 200)
		x.mustDo("POST", "/api/scripts/runs/"+v.RunID+"/ticket", map[string]any{"ticket": x.ticket(v, d.Req, nil), "code": x.step()}, nil, 200)
		return v
	}
	x.setMangle(func(l *betik.JobList) {
		for i := range l.Runs {
			l.Runs[i].Ticket = nil
		}
	})
	x.wantRejected(ticketed(`{"ad":"a"}`).RunID, betik.CodeTicketMissing)
	x.setMangle(func(l *betik.JobList) {
		for i := range l.Runs {
			l.Runs[i].Params = []byte(`{"ad":"KOTU"}`)
		}
	})
	x.wantRejected(ticketed(`{"ad":"b"}`).RunID, betik.CodeTicketMismatch)

	// Bitmiş işi yeniden yollama: ikinci kez çalışmaz, panel durumu değişmez.
	x.setMangle(nil)
	v = x.begin("kurcala", 1, nil)
	if st, _, _ := x.final(v.RunID); st != runOK {
		t.Fatalf("temiz kurcala: %s", st)
	}
	x.mu.Lock()
	var old betik.RunJob
	for _, j := range x.sawJobs {
		if j.RunID == v.RunID {
			old = j
		}
	}
	x.mu.Unlock()
	if old.RunID == "" {
		t.Fatal("yeniden yollanacak iş yok")
	}
	x.setMangle(func(l *betik.JobList) { l.Runs = append(l.Runs, old) })
	x.until("yeniden oynatma ajan kaydına düşmedi", func() bool {
		logs, _ := filepath.Glob(filepath.Join(x.paths.LogDir, "*"))
		for _, f := range logs {
			b, _ := os.ReadFile(f)
			if bytes.Contains(b, []byte(betik.CodeRunIDReplay)) {
				return true
			}
		}
		return false
	})
	x.setMangle(nil)
	if st, _, out := x.final(v.RunID); st != runOK || out != e2eTemiz[5:] {
		t.Fatalf("yeniden oynatma durumu değiştirdi: %s %q", st, out)
	}

	// İptal: uzun iş gerçekten durur.
	x.put("bekle", 1, betik.ApprovalNone, e2eBekle, nil)
	v = x.begin("bekle", 1, nil)
	for end := time.Now().Add(15 * time.Second); x.state(v.RunID) != runAccepted; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(end) {
			st, c, _ := x.final(v.RunID)
			t.Fatalf("kabul gelmedi: %s %s", st, c)
		}
	}
	time.Sleep(time.Second) // kabuk açılsın
	x.mustDo("POST", "/api/scripts/runs/"+v.RunID+"/cancel", map[string]any{}, nil, 200)
	if st, _, out := x.final(v.RunID); st != runCancelled || strings.Contains(out, "bitti") {
		t.Fatalf("iptal: %s %q", st, out)
	}

	// Teslim edilmiş, accepted'i yolda kalmış iş iptal edilirse ajanda da durur.
	x.mu.Lock()
	x.holdAccepted, x.heldAccepted = make(chan struct{}), make(chan struct{}, 1)
	gate, gateHit := x.holdAccepted, x.heldAccepted
	x.mu.Unlock()
	v = x.begin("bekle", 1, nil)
	select {
	case <-gateHit:
	case <-time.After(15 * time.Second):
		t.Fatal("accepted gelmedi")
	}
	x.mustDo("POST", "/api/scripts/runs/"+v.RunID+"/cancel", map[string]any{}, nil, 200)
	x.mu.Lock()
	x.holdAccepted = nil
	x.mu.Unlock()
	close(gate)
	x.until("geç accepted sonrası iş durmadı", func() bool { return !x.agent.Busy() }) // sleep 60'tan kısa
	x.until("iptal makbuzu gelmedi", func() bool {
		var d struct {
			Events []betikkayit.Event `json:"events"`
		}
		x.mustDo("GET", "/api/scripts/runs/"+v.RunID, nil, &d, 200)
		return d.Events[len(d.Events)-1].Kind == "receipt"
	})
	if st, _, out := x.final(v.RunID); st != runCancelled || strings.Contains(out, "bitti") {
		t.Fatalf("geç iptal: %s %q", st, out)
	}

	// Ajan yeniden başlar: accepted bellekte kalmış, makbuz diskte. Panel işi
	// yeniden yollar, ajan run_id_replay der; iş yine çalışmış görünür, çıktı gelir.
	x.mu.Lock()
	x.dropAccepted = true
	x.mu.Unlock()
	v = x.begin("kurcala", 1, nil)
	x.until("makbuz diske yazılmadı", func() bool {
		f, _ := filepath.Glob(filepath.Join(x.paths.StateDir, "*", "*.json"))
		return len(f) > 0
	})
	x.stopProbe()
	x.mu.Lock()
	x.dropAccepted = false
	x.mu.Unlock()
	plat := betikajan.DetectPlatform()
	x.agent = betikajan.New(betikajan.Options{Paths: x.paths, Platform: &plat, Now: x.clk.now})
	x.stopProbe = x.runProbe(t, x.agent, x.probe.Token)
	if st, c, out := x.final(v.RunID); st != runOK || out != e2eTemiz[5:] {
		t.Fatalf("yeniden başlatma: %s %s %q", st, c, out)
	}

	// Acil durdurma: yeni çalıştırma panelde kesilir, sürdürünce açılır.
	x.mustDo("POST", "/api/scripts/control/stop", map[string]any{}, nil, 200)
	if code, _, raw := x.start(x.env, "kurcala", 1, x.secret, nil); code != 409 {
		t.Fatalf("acil durdurmada: %d %s", code, raw)
	}
	x.mustDo("POST", "/api/scripts/control/resume", map[string]any{"code": x.step()}, nil, 200)

	// Yetkisiz kullanıcılar: hiçbiri iş kuyruğa sokamaz.
	ed, _ := x.newUser("edit", store.RoleEditor, nil)
	vw, _ := x.newUser("izle", store.RoleViewer, nil)
	noTF, _ := x.newUser("mehmet", store.RoleAdmin, nil)
	key := x.newKey("otomasyon", store.RoleAdmin, 0)
	req := map[string]any{"script_id": "kurcala", "version": 1, "probe_id": x.probe.ID, "code": "123456"}
	for name, c := range map[string]*env{"editör": ed, "izleyici": vw, "2FA'sız yönetici": noTF} {
		if code := c.do("POST", "/api/scripts/runs", req, nil); code != 401 && code != 403 {
			t.Errorf("%s: %d", name, code)
		}
	}
	b, _ := json.Marshal(req)
	hr, _ := http.NewRequest("POST", x.srv.URL+"/api/scripts/runs", bytes.NewReader(b))
	hr.Header.Set("Authorization", "Bearer "+key.Secret)
	if resp, err := http.DefaultClient.Do(hr); err != nil || (resp.StatusCode != 401 && resp.StatusCode != 403) {
		t.Errorf("API anahtarı: %v %v", resp, err)
	}
	if code, _, _ := x.start(x.env, "kurcala", 1, "AAAAAAAAAAAAAAAA", nil); code == 201 {
		t.Error("yanlış 2FA kabul edildi")
	}

	// Süresi geçmiş imza panelde de kesilir.
	x.put("surum", 1, betik.ApprovalNone, "echo gec\n", func(m *betik.Manifest) { m.NotAfter = x.clk.now().Unix() + 100 })
	x.clk.advance(200 * time.Second)
	if code, _, raw := x.start(x.env, "surum", 1, x.secret, nil); code != 409 || !strings.Contains(raw, betik.CodeExpired) {
		t.Fatalf("süresi geçmiş: %d %s", code, raw)
	}

	// Kapalı ajan: kuyruktaki iş bile reddedilir, panel yeni istek almaz.
	hold := func(l *betik.JobList) { l.Runs = nil }
	x.setMangle(hold)
	v = x.begin("kurcala", 1, nil)
	if err := betikajan.Disable(x.paths); err != nil {
		t.Fatal(err)
	}
	x.setMangle(nil)
	x.wantRejected(v.RunID, betik.CodeDisabled)
	x.until("kapalı durum panele gelmedi", func() bool {
		code, _, _ := x.start(x.env, "kurcala", 1, x.secret, nil)
		return code == 409
	})

	// Kayıt zinciri bütün bu olaylardan sonra doğrulanır.
	x.chain(t)
}

// Eski ajan: yetenek başlığı yok; panel betik isteğini baştan reddeder, ajana bir şey gitmez.
func TestScriptOldAgentAgainstNewPanel(t *testing.T) {
	x := setupE2E(t)
	var cp struct {
		Probe struct {
			ID int64 `json:"id"`
		} `json:"probe"`
		Token string `json:"token"`
	}
	x.mustDo("POST", "/api/probes", map[string]any{"name": "eski"}, &cp, 201)
	x.runProbe(t, nil, cp.Token)
	x.put("selam", 1, betik.ApprovalNone, "echo x\n", nil)
	x.until("eski ajan yoklamadı", func() bool {
		x.mu.Lock()
		defer x.mu.Unlock()
		return x.polls["Bearer "+cp.Token] > 1
	})
	code, _, raw := x.start(x.env, "selam", 1, x.secret, map[string]any{"probe_id": cp.Probe.ID})
	if code != 409 || !strings.Contains(raw, "host_unsupported") {
		t.Fatalf("%d %s", code, raw)
	}
}
