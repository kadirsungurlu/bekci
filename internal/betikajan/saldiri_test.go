package betikajan

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/agentupdate"
	"github.com/kadirsungurlu/bekci/internal/betik"
)

// Saldırı testleri (680 §14): her biri kodunu doğrular, hiçbirinde yürütücü
// çağrılmaz, kota tüketilmez.

const scriptBody = "echo merhaba\n"

type fakeExec struct {
	mu      sync.Mutex
	runs    []Spec
	stops   []string
	units   []string
	out     Outcome
	block   chan struct{}
	entered chan struct{}
}

func (f *fakeExec) Run(ctx context.Context, s Spec) Outcome {
	f.mu.Lock()
	f.runs = append(f.runs, s)
	block := f.block
	f.mu.Unlock()
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if block != nil {
		<-block
	}
	return f.out
}

func (f *fakeExec) Stop(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops = append(f.stops, id)
	return nil
}
func (f *fakeExec) Units() ([]string, error) { return f.units, nil }
func (f *fakeExec) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.runs)
}

type harness struct {
	t            *testing.T
	paths        Paths
	yayin, onay  ed25519.PrivateKey
	yayin2       ed25519.PrivateKey
	exec         *fakeExec
	agent        *Agent
	clock        time.Time
	host         string
	seq          byte
	params       string
	manifestBase betik.Manifest
}

func genKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func pubStr(k ed25519.PrivateKey) string {
	return betik.EncodePublicKey(k.Public().(ed25519.PublicKey))
}

func newHarness(t *testing.T, allow ...Allow) *harness {
	t.Helper()
	d := t.TempDir()
	h := &harness{
		t: t, yayin: genKey(t), onay: genKey(t), yayin2: genKey(t), exec: &fakeExec{out: Outcome{ExitCode: 0, OutputSHA256: betik.Digest([]byte("o")), Output: []byte("o")}},
		clock:  time.Unix(1_800_000_000, 0),
		paths:  Paths{Config: filepath.Join(d, "etc", "betik.json"), StateDir: filepath.Join(d, "lib"), LogDir: filepath.Join(d, "log")},
		params: "{}",
	}
	if len(allow) == 0 {
		allow = []Allow{{ID: "temizle", MinApproval: "none", SHA256: betik.Digest([]byte(scriptBody))}}
	}
	key, err := Enable(h.paths, EnableOptions{
		Yayin: []string{pubStr(h.yayin)}, Onay: []string{pubStr(h.onay)}, Groups: []string{"web"}, Allow: allow,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.host = key
	h.manifestBase = betik.Manifest{
		Schema: 1, ID: "temizle", Version: 1, SHA256: betik.Digest([]byte(scriptBody)), Interpreter: "sh", RunAs: "root",
		Platforms: []string{"linux"}, Approval: "none", Groups: []string{"web"}, TimeoutSec: 60, OutputMax: 4096,
		MaxPerDay: 100, NotBefore: h.clock.Unix() - 100, NotAfter: h.clock.Unix() + 1000,
	}
	plat := Platform{OK: true}
	h.agent = New(Options{Paths: h.paths, Exec: h.exec, Platform: &plat, Now: func() time.Time { return h.clock },
		Jitter: func(time.Duration) time.Duration { return 0 }})
	h.agent.capTTL = 0
	h.agent.SetPanelSecure(true)
	return h
}

func (h *harness) runID() string {
	h.seq++
	return strings.Repeat("a", 30) + string("0123456789abcdef"[h.seq>>4]) + string("0123456789abcdef"[h.seq&15])
}

func (h *harness) signManifest(m *betik.Manifest) json.RawMessage {
	raw, err := betik.Sign(h.yayin, betik.KindManifest, mustJSON(m))
	if err != nil {
		h.t.Fatal(err)
	}
	return raw
}

// job varsayılan geçerli iş.
func (h *harness) job(f func(*betik.Manifest)) betik.RunJob {
	m := h.manifestBase
	if f != nil {
		f(&m)
	}
	return betik.RunJob{RunID: h.runID(), Manifest: h.signManifest(&m), Body: []byte(scriptBody), Params: []byte(h.params)}
}

func (h *harness) ticket(j *betik.RunJob, f func(*betik.Ticket)) json.RawMessage {
	env, err := betik.ParseEnvelope(j.Manifest)
	if err != nil {
		h.t.Fatal(err)
	}
	m, _ := betik.ParseManifest(env.Payload)
	tk := betik.Ticket{
		Schema: 1, RunID: j.RunID, HostKey: h.host, ScriptID: m.ID, Version: m.Version,
		ManifestSHA256: env.PayloadDigest(), ParamsSHA256: betik.Digest(j.Params),
		IssuedAt: h.clock.Unix(), ExpiresAt: h.clock.Unix() + 600, Approver: "kadir",
	}
	if f != nil {
		f(&tk)
	}
	raw, err := betik.Sign(h.onay, betik.KindTicket, mustJSON(&tk))
	if err != nil {
		h.t.Fatal(err)
	}
	return raw
}

func (h *harness) handle(j ...betik.RunJob) {
	h.agent.Handle(&betik.JobList{Runs: j})
	h.agent.wg.Wait()
}

func (h *harness) lastEvent() betik.Event {
	h.t.Helper()
	h.agent.mu.Lock()
	defer h.agent.mu.Unlock()
	if len(h.agent.events) == 0 {
		h.t.Fatal("olay yok")
	}
	return h.agent.events[len(h.agent.events)-1]
}

// wantReject: kod tutmalı, betik çalışmamalı, kota tüketilmemeli.
func (h *harness) wantReject(code string) {
	h.t.Helper()
	ev := h.lastEvent()
	if ev.Kind != betik.EventRejected || ev.Code != code {
		h.t.Fatalf("olay %+v, beklenen red %s", ev, code)
	}
	if n := h.exec.count(); n != 0 {
		h.t.Fatalf("betik çalıştı (%d)", n)
	}
	if st, err := NewStore(h.paths.StateDir).Read(); err == nil && (st.Hour.Normal != 0 || st.Hour.Signed != 0 || len(st.Runs) != 0) {
		h.t.Fatalf("red kota tüketti: %+v", st.Hour)
	}
}

func TestValidRunAndReceipt(t *testing.T) {
	h := newHarness(t)
	h.handle(h.job(nil))
	if h.exec.count() != 1 {
		t.Fatal("çalışmadı")
	}
	key, err := h.agent.store.LoadKey()
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	var sent []betik.Event
	h.agent.flush(context.Background(), func(_ context.Context, ev *betik.Event) (int, error) {
		sent = append(sent, *ev)
		return 200, nil
	})
	if len(sent) != 2 || sent[0].Kind != betik.EventAccepted || sent[1].Kind != betik.EventReceipt {
		t.Fatalf("%+v", sent)
	}
	env, err := betik.ParseEnvelope(sent[1].Receipt)
	if err != nil || !env.Verify(betik.KindReceipt, pub) {
		t.Fatal("makbuz imzası", err)
	}
	r, err := betik.ParseReceipt(env.Payload)
	if err != nil || r.Status != betik.StatusOK || r.Seq != 1 || r.Prev != "" {
		t.Fatalf("%+v %v", r, err)
	}
	// İkinci çalıştırma zinciri sürdürür, sürüm tabanı yükselmiş olur.
	h.handle(h.job(func(m *betik.Manifest) { m.Version = 2 }))
	st, _ := NewStore(h.paths.StateDir).Read()
	if st.Seq != 2 || st.Scripts["temizle"].Floor != 2 || st.Prev == "" || len(st.Active) != 0 {
		t.Fatalf("%+v", st)
	}
	data, _ := os.ReadFile(filepath.Join(h.paths.LogDir, logFile))
	if strings.Count(string(data), `"kind":"receipt"`) != 2 {
		t.Fatalf("kayıt: %s", data)
	}
}

func TestManifestAttacks(t *testing.T) {
	for name, c := range map[string]struct {
		code string
		mut  func(h *harness, j *betik.RunJob)
	}{
		"imzasız": {betik.CodeEnvelopeInvalid, func(h *harness, j *betik.RunJob) {
			j.Manifest = json.RawMessage(`{"key":"x","payload":"e30=","sig":""}`)
		}},
		"sahte imza": {betik.CodeSigInvalid, func(h *harness, j *betik.RunJob) {
			e, _ := betik.ParseEnvelope(j.Manifest)
			e.Sig[0] ^= 1
			j.Manifest = rewrap(e)
		}},
		"yanlış anahtar": {betik.CodeSigInvalid, func(h *harness, j *betik.RunJob) {
			j.Manifest, _ = betik.Sign(h.yayin2, betik.KindManifest, mustJSON(&h.manifestBase))
		}},
		"onay anahtarı bildirim imzalayamaz": {betik.CodeSigInvalid, func(h *harness, j *betik.RunJob) {
			j.Manifest, _ = betik.Sign(h.onay, betik.KindManifest, mustJSON(&h.manifestBase))
		}},
		"başka tür": {betik.CodeSigInvalid, func(h *harness, j *betik.RunJob) {
			j.Manifest, _ = betik.Sign(h.yayin, betik.KindRevoke, mustJSON(&h.manifestBase))
		}},
		"ajan güncelleme imzası": {betik.CodeSigInvalid, func(h *harness, j *betik.RunJob) {
			p := mustJSON(&h.manifestBase)
			e, _ := betik.ParseEnvelope(j.Manifest)
			e.Payload = p
			e.Sig = ed25519.Sign(h.yayin, append([]byte("bekci-agent-manifest-v1\n"), p...))
			j.Manifest = rewrap(e)
			_ = agentupdate.PublicKeyBase64
		}},
		"değiştirilmiş alan": {betik.CodeSigInvalid, func(h *harness, j *betik.RunJob) {
			e, _ := betik.ParseEnvelope(j.Manifest)
			e.Payload = []byte(strings.Replace(string(e.Payload), `"approval":"none"`, `"approval":"panel"`, 1))
			j.Manifest = rewrap(e)
		}},
		"değiştirilmiş gövde": {betik.CodeBodyMismatch, func(h *harness, j *betik.RunJob) { j.Body = []byte("rm -rf /") }},
		"büyük gövde":         {betik.CodeBodyTooLarge, func(h *harness, j *betik.RunJob) { j.Body = make([]byte, betik.MaxBody+1) }},
		"süresi geçmiş": {betik.CodeExpired, func(h *harness, j *betik.RunJob) {
			m := h.manifestBase
			m.NotAfter = h.clock.Unix() - 1
			m.NotBefore = m.NotAfter - 10
			*j = betik.RunJob{RunID: j.RunID, Manifest: h.signManifest(&m), Body: j.Body, Params: j.Params}
		}},
		"henüz başlamamış": {betik.CodeNotYet, func(h *harness, j *betik.RunJob) {
			m := h.manifestBase
			m.NotBefore = h.clock.Unix() + 10
			j.Manifest = h.signManifest(&m)
		}},
		"izinsiz grup": {betik.CodeGroupDenied, func(h *harness, j *betik.RunJob) {
			m := h.manifestBase
			m.Groups = []string{"db"}
			j.Manifest = h.signManifest(&m)
		}},
		"izin dışı": {betik.CodeNotAllowed, func(h *harness, j *betik.RunJob) {
			m := h.manifestBase
			m.ID = "baska"
			j.Manifest = h.signManifest(&m)
		}},
		"K_yayın iğneli id yeni gövde": {betik.CodePinMismatch, func(h *harness, j *betik.RunJob) {
			m := h.manifestBase
			j.Body = []byte("curl kotu | sh")
			m.SHA256 = betik.Digest(j.Body)
			j.Manifest = h.signManifest(&m)
		}},
		"bilinmeyen schema": {betik.CodeSchemaUnknown, func(h *harness, j *betik.RunJob) {
			m := h.manifestBase
			m.Schema = 2
			j.Manifest = h.signManifest(&m)
		}},
		"bilinmeyen alan": {betik.CodePayloadInvalid, func(h *harness, j *betik.RunJob) {
			p := strings.Replace(string(mustJSON(&h.manifestBase)), `"schema":1`, `"schema":1,"extra":true`, 1)
			j.Manifest, _ = betik.Sign(h.yayin, betik.KindManifest, []byte(p))
		}},
		"yinelenen anahtar": {betik.CodePayloadInvalid, func(h *harness, j *betik.RunJob) {
			p := strings.Replace(string(mustJSON(&h.manifestBase)), `"schema":1`, `"schema":1,"approval":"signed"`, 1)
			j.Manifest, _ = betik.Sign(h.yayin, betik.KindManifest, []byte(p))
		}},
		"büyük zarf": {betik.CodeEnvelopeInvalid, func(h *harness, j *betik.RunJob) {
			j.Manifest, _ = betik.Sign(h.yayin, betik.KindManifest, make([]byte, betik.MaxEnvelope))
		}},
		"dev version": {betik.CodeVersionJump, func(h *harness, j *betik.RunJob) {
			m := h.manifestBase
			m.Version = 1 << 40
			j.Manifest = h.signManifest(&m)
		}},
		"iki yüz seksen gün ömür": {betik.CodePayloadInvalid, func(h *harness, j *betik.RunJob) {
			m := h.manifestBase
			m.NotAfter = m.NotBefore + betik.MaxManifestLife + 1
			j.Manifest = h.signManifest(&m)
		}},
		"şema dışı parametre": {betik.CodeParamsInvalid, func(h *harness, j *betik.RunJob) { j.Params = []byte(`{"x":"$(id)"}`) }},
		"parametre boş":       {betik.CodeParamsInvalid, func(h *harness, j *betik.RunJob) { j.Params = nil }},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			j := h.job(nil)
			c.mut(h, &j)
			h.handle(j)
			h.wantReject(c.code)
		})
	}
}

func rewrap(e *betik.Envelope) json.RawMessage {
	return mustJSON(map[string]any{"key": e.Key, "payload": e.Payload, "sig": e.Sig})
}

func TestRunIDFormat(t *testing.T) {
	h := newHarness(t)
	j := h.job(nil)
	for _, id := range []string{"", "x", "../../etc", strings.Repeat("A", 32), strings.Repeat("a", 33), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa;"} {
		j.RunID = id
		h.handle(j)
		h.wantReject(betik.CodeRunIDFormat)
		if ev := h.lastEvent(); ev.RunID != "" {
			t.Fatal("bozuk run_id panele geri yazıldı")
		}
	}
}

func signedAllow() Allow { return Allow{ID: "temizle", MinApproval: "signed"} }

func TestTicketAttacks(t *testing.T) {
	for name, c := range map[string]struct {
		code string
		mut  func(h *harness, j *betik.RunJob)
	}{
		"bilet yok":         {betik.CodeTicketMissing, func(h *harness, j *betik.RunJob) { j.Ticket = nil }},
		"bilet zarfı bozuk": {betik.CodeTicketInvalid, func(h *harness, j *betik.RunJob) { j.Ticket = json.RawMessage(`{"key":1}`) }},
		"bilet şeması bilinmeyen": {betik.CodeSchemaUnknown, func(h *harness, j *betik.RunJob) {
			j.Ticket = h.ticket(j, func(t *betik.Ticket) { t.Schema = 2 })
		}},
		"başka ajana": {betik.CodeTicketHost, func(h *harness, j *betik.RunJob) {
			j.Ticket = h.ticket(j, func(t *betik.Ticket) { t.HostKey = betik.Digest([]byte("baska")) })
		}},
		"başka parametre": {betik.CodeTicketMismatch, func(h *harness, j *betik.RunJob) {
			j.Ticket = h.ticket(j, func(t *betik.Ticket) { t.ParamsSHA256 = betik.Digest([]byte(`{"a":1}`)) })
		}},
		"başka run_id": {betik.CodeTicketMismatch, func(h *harness, j *betik.RunJob) {
			j.Ticket = h.ticket(j, func(t *betik.Ticket) { t.RunID = strings.Repeat("b", 32) })
		}},
		"başka bildirim": {betik.CodeTicketMismatch, func(h *harness, j *betik.RunJob) {
			j.Ticket = h.ticket(j, func(t *betik.Ticket) { t.ManifestSHA256 = betik.Digest([]byte("x")) })
		}},
		"süresi geçmiş": {betik.CodeTicketExpired, func(h *harness, j *betik.RunJob) {
			j.Ticket = h.ticket(j, func(t *betik.Ticket) { t.IssuedAt = h.clock.Unix() - 800; t.ExpiresAt = h.clock.Unix() - 1 })
		}},
		"gelecekten": {betik.CodeTicketExpired, func(h *harness, j *betik.RunJob) {
			j.Ticket = h.ticket(j, func(t *betik.Ticket) { t.IssuedAt = h.clock.Unix() + 300; t.ExpiresAt = h.clock.Unix() + 600 })
		}},
		"ömür uzun": {betik.CodeTicketInvalid, func(h *harness, j *betik.RunJob) {
			j.Ticket = h.ticket(j, func(t *betik.Ticket) { t.ExpiresAt = t.IssuedAt + betik.MaxTicketLife + 1 })
		}},
		"yayın anahtarıyla imzalı bilet": {betik.CodeTicketSig, func(h *harness, j *betik.RunJob) {
			e, _ := betik.ParseEnvelope(h.ticket(j, nil))
			j.Ticket, _ = betik.Sign(h.yayin, betik.KindTicket, e.Payload)
		}},
		"bildirim imzası bilet yerine": {betik.CodeTicketSig, func(h *harness, j *betik.RunJob) {
			e, _ := betik.ParseEnvelope(h.ticket(j, nil))
			j.Ticket, _ = betik.Sign(h.onay, betik.KindManifest, e.Payload)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, signedAllow())
			j := h.job(nil)
			j.Ticket = h.ticket(&j, nil)
			c.mut(h, &j)
			h.handle(j)
			h.wantReject(c.code)
		})
	}
}

func TestSignedRunAndTicketReplay(t *testing.T) {
	h := newHarness(t, signedAllow())
	j := h.job(nil)
	j.Ticket = h.ticket(&j, nil)
	h.handle(j)
	if h.exec.count() != 1 {
		t.Fatalf("bilet kabul edilmedi: %+v", h.lastEvent())
	}
	h.handle(j)
	if ev := h.lastEvent(); ev.Code != betik.CodeRunIDReplay {
		t.Fatalf("%+v", ev)
	}
	if h.exec.count() != 1 {
		t.Fatal("tekrar çalıştı")
	}
}

// S1: K_yayın sızdı, iğnesiz id altında yeni gövde: bilet olmadan çalışmaz.
func TestUnpinnedAlwaysNeedsTicket(t *testing.T) {
	h := newHarness(t, Allow{ID: "temizle", MinApproval: "none"})
	h.handle(h.job(nil))
	h.wantReject(betik.CodeTicketMissing)
}

// İmzalı approval yerel tabanın altındaysa yerel kazanır.
func TestLocalFloorWins(t *testing.T) {
	h := newHarness(t, Allow{ID: "temizle", MinApproval: "signed", SHA256: betik.Digest([]byte(scriptBody))})
	h.handle(h.job(nil))
	h.wantReject(betik.CodeTicketMissing)
}

// D3: panel sınıfına bilet gelirse doğrulanır, bozuksa reddedilir.
func TestTicketVerifiedOnPanelClass(t *testing.T) {
	h := newHarness(t)
	j := h.job(func(m *betik.Manifest) { m.Approval = "panel" })
	j.Ticket = h.ticket(&j, func(t *betik.Ticket) { t.HostKey = betik.Digest([]byte("x")) })
	h.handle(j)
	h.wantReject(betik.CodeTicketHost)
}

func TestVersionRollbackAndRevoke(t *testing.T) {
	h := newHarness(t)
	h.handle(h.job(func(m *betik.Manifest) { m.Version = 5 }))
	if h.exec.count() != 1 {
		t.Fatal("ilk çalışma olmadı")
	}
	h.handle(h.job(func(m *betik.Manifest) { m.Version = 4 }))
	if ev := h.lastEvent(); ev.Code != betik.CodeVersionRollback {
		t.Fatalf("%+v", ev)
	}
	// İptal: panel iletti, kalıcı.
	rev, _ := betik.Sign(h.yayin, betik.KindRevoke, mustJSON(&betik.Revoke{Schema: 1, ID: "temizle", BelowVersion: 7}))
	h.agent.Handle(&betik.JobList{Revokes: []json.RawMessage{rev}})
	h.handle(h.job(func(m *betik.Manifest) { m.Version = 6 }))
	if ev := h.lastEvent(); ev.Code != betik.CodeRevoked {
		t.Fatalf("%+v", ev)
	}
	// Sahte iptal ve dev iptal işlenmez.
	bad, _ := betik.Sign(h.yayin2, betik.KindRevoke, mustJSON(&betik.Revoke{Schema: 1, ID: "x", BelowVersion: 2}))
	dev, _ := betik.Sign(h.yayin, betik.KindRevoke, mustJSON(&betik.Revoke{Schema: 1, ID: "temizle", BelowVersion: 1 << 40}))
	h.agent.Handle(&betik.JobList{Revokes: []json.RawMessage{bad, dev}})
	st, _ := NewStore(h.paths.StateDir).Read()
	if st.Revokes["temizle"] != 7 || st.Revokes["x"] != 0 {
		t.Fatalf("%+v", st.Revokes)
	}
}

// Taban başarısız çalışmada yükselmez.
func TestFloorOnlyOnSuccess(t *testing.T) {
	h := newHarness(t)
	h.exec.out.ExitCode = 3
	h.handle(h.job(func(m *betik.Manifest) { m.Version = 5 }))
	st, _ := NewStore(h.paths.StateDir).Read()
	if st.Scripts["temizle"].Floor != 0 {
		t.Fatal("başarısız çalışma tabanı yükseltti")
	}
}

func TestRateLimits(t *testing.T) {
	h := newHarness(t)
	h.manifestBase.MinIntervalSec = 600
	h.handle(h.job(nil))
	h.handle(h.job(nil))
	if ev := h.lastEvent(); ev.Code != betik.CodeRateInterval {
		t.Fatalf("%+v", ev)
	}
	h = newHarness(t)
	h.manifestBase.MaxPerDay = 2
	h.handle(h.job(nil))
	h.handle(h.job(nil))
	h.handle(h.job(nil))
	if ev := h.lastEvent(); ev.Code != betik.CodeRateDay {
		t.Fatalf("%+v", ev)
	}
	h = newHarness(t)
	for range DefaultMaxRunsPerHour + 1 {
		h.handle(h.job(nil))
	}
	if ev := h.lastEvent(); ev.Code != betik.CodeRateHour {
		t.Fatalf("%+v", ev)
	}
	if h.exec.count() != DefaultMaxRunsPerHour {
		t.Fatalf("%d çalıştı", h.exec.count())
	}
}

// Düşük sınıf kotası ve yuvası signed betiği kilitleyemez.
func TestSignedQuotaAndSlotSeparate(t *testing.T) {
	pin := betik.Digest([]byte(scriptBody))
	h := newHarness(t, Allow{ID: "temizle", MinApproval: "none", SHA256: pin}, signedAllow2())
	for range DefaultMaxRunsPerHour {
		h.handle(h.job(nil))
	}
	h.exec.block, h.exec.entered = make(chan struct{}), make(chan struct{}, 4)
	sign := func() betik.RunJob {
		j := h.job(func(m *betik.Manifest) { m.ID = "acil"; m.Approval = "signed" })
		j.Ticket = h.ticket(&j, nil)
		return j
	}
	// Normal yuvayı tutmak için kota penceresini geçir.
	h.clock = h.clock.Add(time.Hour)
	h.manifestBase.NotAfter = h.clock.Unix() + 1000
	h.agent.Handle(&betik.JobList{Runs: []betik.RunJob{h.job(nil)}})
	<-h.exec.entered
	h.agent.Handle(&betik.JobList{Runs: []betik.RunJob{h.job(nil)}})
	if ev := h.lastEvent(); ev.Code != betik.CodeBusy {
		t.Fatalf("%+v", ev)
	}
	h.agent.Handle(&betik.JobList{Runs: []betik.RunJob{sign()}})
	<-h.exec.entered
	if ev := h.lastEvent(); ev.Kind != betik.EventAccepted {
		t.Fatalf("signed yuva: %+v", ev)
	}
	close(h.exec.block)
	h.agent.wg.Wait()
}

func signedAllow2() Allow { return Allow{ID: "acil", MinApproval: "signed"} }

func TestDisabledAndBrokenFiles(t *testing.T) {
	cases := map[string]struct {
		code string
		mut  func(h *harness)
	}{
		"kapalı":      {betik.CodeDisabled, func(h *harness) { Disable(h.paths) }},
		"dosya yok":   {betik.CodeDisabled, func(h *harness) { os.Remove(h.paths.Config) }},
		"dosya bozuk": {betik.CodeConfigInvalid, func(h *harness) { os.WriteFile(h.paths.Config, []byte("{"), 0o640) }},
		"gevşek izin": {betik.CodeConfigInvalid, func(h *harness) { os.Chmod(h.paths.Config, 0o666) }},
		"sembolik bağ": {betik.CodeConfigInvalid, func(h *harness) {
			os.Rename(h.paths.Config, h.paths.Config+".x")
			os.Symlink(h.paths.Config+".x", h.paths.Config)
		}},
		"durum silindi": {betik.CodeStateInvalid, func(h *harness) { os.Remove(filepath.Join(h.paths.StateDir, stateFile)) }},
		"durum bozuk": {betik.CodeStateInvalid, func(h *harness) {
			p := filepath.Join(h.paths.StateDir, stateFile)
			b, _ := os.ReadFile(p)
			b[len(b)/2] ^= 1
			os.WriteFile(p, b, 0o600)
		}},
		"anahtar yok":  {betik.CodeStateInvalid, func(h *harness) { os.Remove(filepath.Join(h.paths.StateDir, keyFile)) }},
		"http panel":   {betik.CodeInsecurePanel, func(h *harness) { h.agent.SetPanelSecure(false) }},
		"docker ajanı": {betik.CodePlatform, func(h *harness) { h.agent.platform = Platform{Detail: "konteyner"} }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			j := h.job(nil)
			c.mut(h)
			h.handle(j)
			h.wantReject(c.code)
			if v, key := h.agent.Capability(); !strings.Contains(v, "state=off") || key != "" {
				t.Fatalf("yetenek: %s %s", v, key)
			}
		})
	}
}

func TestDiskFull(t *testing.T) {
	h := newHarness(t)
	calls := 0
	h.agent.store.write = func(string, []byte, os.FileMode) error { calls++; return syscall.ENOSPC }
	h.handle(h.job(nil))
	if ev := h.lastEvent(); ev.Code != betik.CodeStateWrite {
		t.Fatalf("%+v", ev)
	}
	if h.exec.count() != 0 || calls != 2 {
		t.Fatalf("çalıştı=%d deneme=%d", h.exec.count(), calls)
	}
	if h.agent.Busy() {
		t.Fatal("yuva bırakılmadı")
	}
}

// Disk dolunca ayrılan alan bırakılır, ikinci yazım geçer.
func TestReserveReleased(t *testing.T) {
	h := newHarness(t)
	calls := 0
	h.agent.store.write = func(p string, d []byte, m os.FileMode) error {
		if calls++; calls == 1 {
			return syscall.ENOSPC
		}
		return atomicWrite(p, d, m)
	}
	h.handle(h.job(nil))
	if h.exec.count() != 1 {
		t.Fatalf("%+v", h.lastEvent())
	}
	if _, err := os.Stat(filepath.Join(h.paths.StateDir, reserveFile)); !os.IsNotExist(err) {
		t.Fatal("rezerv silinmedi")
	}
}

func TestClockBack(t *testing.T) {
	h := newHarness(t)
	h.handle(h.job(nil))
	h.clock = h.clock.Add(-time.Hour)
	h.manifestBase.NotBefore -= 7200
	h.handle(h.job(nil))
	if ev := h.lastEvent(); ev.Code != betik.CodeClockBack {
		t.Fatalf("%+v", ev)
	}
	if h.exec.count() != 1 {
		t.Fatal("saat geri iken çalıştı")
	}
	// Kısa NTP düzeltmesi kilitlemez.
	h.clock = h.clock.Add(time.Hour - 60*time.Second)
	h.handle(h.job(nil))
	if h.exec.count() != 2 {
		t.Fatalf("%+v", h.lastEvent())
	}
}

// Kilitlenme: run_id kaydı atıldı, makbuz yazılmadı. Açılışta lost; aynı run_id reddedilir.
func TestCrashRecovery(t *testing.T) {
	h := newHarness(t)
	h.exec.block, h.exec.entered = make(chan struct{}), make(chan struct{}, 1)
	j := h.job(nil)
	h.agent.Handle(&betik.JobList{Runs: []betik.RunJob{j}})
	<-h.exec.entered

	// Yeni süreç: aynı dosyalar, boş bellek.
	exec2 := &fakeExec{units: []string{j.RunID, strings.Repeat("c", 32)}}
	plat := Platform{OK: true}
	a2 := New(Options{Paths: h.paths, Exec: exec2, Platform: &plat, Now: func() time.Time { return h.clock }})
	a2.SetPanelSecure(true)
	a2.reconcile()
	if len(exec2.stops) != 2 {
		t.Fatalf("durdurulan: %v", exec2.stops)
	}
	st, _ := a2.store.Read()
	if len(st.Active) != 0 || st.Seq != 1 {
		t.Fatalf("%+v", st)
	}
	names := a2.queue.names()
	if len(names) != 1 {
		t.Fatalf("kuyruk: %v", names)
	}
	ev, _ := a2.queue.get(names[0])
	env, _ := betik.ParseEnvelope(ev.Receipt)
	r, _ := betik.ParseReceipt(env.Payload)
	if r.Status != betik.StatusLost || r.RunID != j.RunID {
		t.Fatalf("%+v", r)
	}
	a2.Handle(&betik.JobList{Runs: []betik.RunJob{j}})
	a2.wg.Wait()
	if len(exec2.runs) != 0 {
		t.Fatal("kilitlenme sonrası aynı run_id çalıştı")
	}
	if a2.events[len(a2.events)-1].Code != betik.CodeRunIDReplay {
		t.Fatalf("%+v", a2.events)
	}
	close(h.exec.block)
	h.agent.wg.Wait()
}

func TestCancelAndShutdown(t *testing.T) {
	h := newHarness(t)
	h.exec.block, h.exec.entered = make(chan struct{}), make(chan struct{}, 2)
	j := h.job(nil)
	h.agent.Handle(&betik.JobList{Runs: []betik.RunJob{j}})
	<-h.exec.entered
	if !h.agent.Busy() {
		t.Fatal("Busy değil")
	}
	h.agent.Handle(&betik.JobList{Cancel: []string{j.RunID, strings.Repeat("d", 32)}})
	close(h.exec.block)
	h.agent.wg.Wait()
	if h.agent.Busy() {
		t.Fatal("Busy kaldı")
	}
	status := func() string {
		n := h.agent.queue.names()
		ev, _ := h.agent.queue.get(n[len(n)-1])
		env, _ := betik.ParseEnvelope(ev.Receipt)
		r, _ := betik.ParseReceipt(env.Payload)
		return r.Status
	}
	if s := status(); s != betik.StatusCancelled {
		t.Fatal(s)
	}
	h.exec.block = make(chan struct{})
	h.agent.Handle(&betik.JobList{Runs: []betik.RunJob{h.job(nil)}})
	<-h.exec.entered
	go func() { time.Sleep(20 * time.Millisecond); close(h.exec.block) }()
	h.agent.shutdown()
	if s := status(); s != betik.StatusLost {
		t.Fatal(s)
	}
}

func TestFlushKeepsUntilDelivered(t *testing.T) {
	h := newHarness(t)
	h.handle(h.job(nil))
	// Eski panel: 404, hiçbir şey silinmez.
	if h.agent.flush(context.Background(), func(context.Context, *betik.Event) (int, error) { return 404, nil }) {
		t.Fatal("404 teslim sayıldı")
	}
	if len(h.agent.events) != 1 || len(h.agent.queue.names()) != 1 {
		t.Fatal("olay kayboldu")
	}
	// 409 kalıcı red: kuyruktan çıkar, kayıtta kalır.
	if !h.agent.flush(context.Background(), func(context.Context, *betik.Event) (int, error) { return 409, nil }) {
		t.Fatal("409")
	}
	if len(h.agent.queue.names()) != 0 {
		t.Fatal("kuyruk boşalmadı")
	}
}

func TestQueueBounded(t *testing.T) {
	q := &diskQueue{dir: t.TempDir()}
	dropped := 0
	for i := int64(1); i <= maxQueueFiles+3; i++ {
		n, err := q.put(i, &betik.Event{Kind: betik.EventReceipt})
		if err != nil {
			t.Fatal(err)
		}
		dropped += n
	}
	if dropped != 3 || len(q.names()) != maxQueueFiles || q.names()[0] != seqName(4) {
		t.Fatalf("%d %d %s", dropped, len(q.names()), q.names()[0])
	}
}

func TestRejectLogBounded(t *testing.T) {
	h := newHarness(t)
	for range maxRejectHour + 50 {
		h.agent.journal.Reject(h.clock, "", "", betik.CodeBusy)
	}
	b, _ := os.ReadFile(filepath.Join(h.paths.LogDir, logFile))
	if n := strings.Count(string(b), "\n"); n != maxRejectHour {
		t.Fatalf("%d satır", n)
	}
}

func TestSameKeyTwoSlots(t *testing.T) {
	d := t.TempDir()
	k := pubStr(genKey(t))
	_, err := Enable(Paths{Config: d + "/c.json", StateDir: d + "/s", LogDir: d + "/l"},
		EnableOptions{Yayin: []string{k}, Onay: []string{k}, Groups: []string{"web"}})
	if err == nil || !strings.Contains(err.Error(), "iki yuvada") {
		t.Fatal(err)
	}
	if _, err := os.Stat(d + "/c.json"); err == nil {
		t.Fatal("dosya yazıldı")
	}
}

func TestParseAllowNoWildcard(t *testing.T) {
	for _, s := range []string{"*", "a*", "x=zz", "x=" + strings.Repeat("a", 64) + ":hic"} {
		if _, err := ParseAllow(s); err == nil {
			t.Errorf("%q kabul edildi", s)
		}
	}
	a, err := ParseAllow("x")
	if err != nil || a.MinApproval != "signed" {
		t.Fatal(a, err)
	}
	a, err = ParseAllow("x=" + strings.Repeat("a", 64))
	if err != nil || a.MinApproval != "none" {
		t.Fatal(a, err)
	}
}

func TestRepair(t *testing.T) {
	h := newHarness(t)
	h.handle(h.job(nil))
	// Yanlış ileri saatle kilitlenme: onar last_seen'i alır, taban korunur.
	h.clock = h.clock.Add(-time.Hour)
	rebuilt, err := Repair(h.paths, h.clock)
	if err != nil || rebuilt {
		t.Fatal(rebuilt, err)
	}
	// Bozuk durum: onar yeniden kurar, zincir kayit.log'dan sürer.
	st, _ := NewStore(h.paths.StateDir).Read()
	seq, prev := st.Seq, st.Prev
	os.WriteFile(filepath.Join(h.paths.StateDir, stateFile), []byte("bozuk"), 0o600)
	rebuilt, err = Repair(h.paths, h.clock)
	if err != nil || !rebuilt {
		t.Fatal(rebuilt, err)
	}
	st, _ = NewStore(h.paths.StateDir).Read()
	if st.Seq != seq || st.Prev != prev {
		t.Fatalf("zincir koptu: %d %s", st.Seq, st.Prev)
	}
}

func TestCapabilityOn(t *testing.T) {
	h := newHarness(t)
	v, key := h.agent.Capability()
	if v != "schemas=1;state=on" {
		t.Fatal(v)
	}
	k, err := betik.ParsePublicKey(key)
	if err != nil || betik.KeyID(k) != h.host {
		t.Fatal(err)
	}
}

func TestTickForwardJump(t *testing.T) {
	h := newHarness(t)
	h.agent.tick()
	st, _ := NewStore(h.paths.StateDir).Read()
	if st.LastSeen != h.clock.Unix() {
		t.Fatal("last_seen ilerlemedi")
	}
	h.clock = h.clock.Add(72 * time.Hour)
	h.agent.tick()
	if ev := h.lastEvent(); ev.Kind != betik.EventNotice || ev.Code != betik.NoticeClockForward {
		t.Fatalf("%+v", ev)
	}
}
