package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betik"
	"github.com/kadirsungurlu/bekci/internal/betikkayit"
	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
	"github.com/kadirsungurlu/bekci/internal/store/storetest"
)

// Betik panel saldırı testleri: PostgreSQL ister (D8), UPTIME_TEST_PG yoksa atlanır.

type sEnv struct {
	*fenv
	yayin, onay, host ed25519.PrivateKey
	secret            string
	probe             createdProbeT
	hostKey           string
	receiptSeq        int64
	receiptPrev       string
}

type createdProbeT struct {
	ID    int64
	Token string
}

func genKey(t *testing.T) ed25519.PrivateKey {
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func pubOf(k ed25519.PrivateKey) ed25519.PublicKey { return k.Public().(ed25519.PublicKey) }

func setupScripts(t *testing.T) *sEnv {
	t.Helper()
	return setupScriptsStatic(t, fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}, 0)
}

// setupScriptsStatic: off saati baştan kaydırır (2FA adımı da o saate göre kurulur).
func setupScriptsStatic(t *testing.T, static fs.FS, off time.Duration) *sEnv {
	t.Helper()
	if os.Getenv("UPTIME_TEST_PG") == "" {
		t.Skip("UPTIME_TEST_PG yok")
	}
	st := storetest.Open(t, time.UTC)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := engine.NewHub()
	disp := notify.NewDispatcher(st, log)
	eng := engine.New(st, disp, hub, log, engine.Config{Unit: time.Millisecond, BaseURL: "https://uptime.test"})
	ctx, cancel := context.WithCancel(context.Background())
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(st, eng, hub, disp, log, static, "test")
	clk := &testClock{}
	clk.advance(off)
	s.now = clk.now
	s.probeHold = 10 * time.Millisecond
	srv := httptest.NewServer(s.Handler())
	jar, _ := cookiejar.New(nil)
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
		cancel()
		eng.Wait()
		disp.Wait(time.Second)
	})
	e := &env{t: t, srv: srv, client: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	f := &fenv{env: e, s: s, st: st, clk: clk}
	x := &sEnv{fenv: f, yayin: genKey(t), onay: genKey(t), host: genKey(t)}
	x.secret, _, _ = f.enable2FA(e, "cok-gizli-sifre")
	return x
}

// code her çağrıda yeni TOTP adımı üretir: kod tekrar kullanılamaz.
func (x *sEnv) step() string {
	x.clk.advance(31 * time.Second)
	return x.code(x.secret)
}

func (x *sEnv) addKeys() {
	x.t.Helper()
	x.mustDo("POST", "/api/scripts/keys", map[string]any{"slot": "yayin", "public_key": betik.EncodePublicKey(pubOf(x.yayin)), "name": "y", "code": x.step()}, nil, 201)
	x.mustDo("POST", "/api/scripts/keys", map[string]any{"slot": "onay", "public_key": betik.EncodePublicKey(pubOf(x.onay)), "name": "o", "code": x.step()}, nil, 201)
}

func (x *sEnv) signed(id string, ver int64, approval string) (json.RawMessage, []byte) {
	x.t.Helper()
	body := []byte("#!/bin/sh\necho merhaba\n")
	now := x.clk.now().Unix()
	m := betik.Manifest{Schema: 1, ID: id, Version: ver, SHA256: betik.Digest(body), Interpreter: "sh", RunAs: "nobody",
		Platforms: []string{"linux"}, Approval: approval, Groups: []string{"bakim"}, Params: []betik.Param{}, TimeoutSec: 60, OutputMax: 4096,
		MinIntervalSec: 0, MaxPerDay: 10, NotBefore: now - 10, NotAfter: now + 30*86400}
	p, err := json.Marshal(m)
	if err != nil {
		x.t.Fatal(err)
	}
	env, err := betik.Sign(x.yayin, betik.KindManifest, p)
	if err != nil {
		x.t.Fatal(err)
	}
	return env, body
}

func (x *sEnv) upload(id string, ver int64, approval string) {
	x.t.Helper()
	env, body := x.signed(id, ver, approval)
	x.mustDo("POST", "/api/scripts", map[string]any{"manifest": env, "body": body, "code": x.step()}, nil, 201)
}

// connect ajan ekler, yetenek başlığıyla yoklatır, anahtarı onaylar.
func (x *sEnv) connect() {
	x.t.Helper()
	var cp struct {
		Probe struct {
			ID int64 `json:"id"`
		} `json:"probe"`
		Token string `json:"token"`
	}
	x.mustDo("POST", "/api/probes", map[string]any{"name": "s1"}, &cp, 201)
	x.probe = createdProbeT{cp.Probe.ID, cp.Token}
	x.poll()
	var hosts []scriptHostView
	x.mustDo("GET", "/api/scripts/hosts", nil, &hosts, 200)
	x.hostKey = hosts[0].PendingKey
	if x.hostKey == "" || hosts[0].HostKey != "" {
		x.t.Fatalf("yeni anahtar bekleyen olmalı: %+v", hosts[0])
	}
	x.mustDo("POST", fmt.Sprintf("/api/scripts/hosts/%d/approve", x.probe.ID), map[string]any{"host_key": x.hostKey, "code": x.step()}, nil, 200)
}

func (x *sEnv) poll() betik.JobList {
	x.t.Helper()
	hdr := bearer(x.probe.Token)
	hdr["X-Probe-Scripts"] = "schemas=1;state=on"
	hdr["X-Probe-Script-Key"] = betik.EncodePublicKey(pubOf(x.host))
	x.clk.advance(301 * time.Second)
	code, _, body := x.rawReq("GET", "/api/probe/jobs", hdr, nil)
	if code != 200 {
		x.t.Fatalf("jobs: %d %s", code, body)
	}
	var out struct {
		ScriptJobs betik.JobList `json:"script_jobs"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		x.t.Fatal(err)
	}
	return out.ScriptJobs
}

func (x *sEnv) probeEvent(tok string, ev map[string]any) int {
	x.t.Helper()
	code, _, _ := x.rawReq("POST", "/api/probe/script-result", bearer(tok), ev)
	return code
}

func (x *sEnv) start(c *env, id string, ver int64, secret string, extra map[string]any) (int, runView, string) {
	x.t.Helper()
	x.clk.advance(31 * time.Second)
	b := map[string]any{"script_id": id, "version": ver, "probe_id": x.probe.ID, "params": map[string]any{}, "code": x.code(secret)}
	for k, v := range extra {
		b[k] = v
	}
	var raw json.RawMessage
	code := c.do("POST", "/api/scripts/runs", b, &raw)
	var v runView
	_ = json.Unmarshal(raw, &v)
	return code, v, string(raw)
}

func (x *sEnv) receipt(v runView, ov func(*betik.Receipt)) json.RawMessage {
	x.t.Helper()
	var d struct {
		Run    runView            `json:"run"`
		Events []betikkayit.Event `json:"events"`
	}
	x.mustDo("GET", "/api/scripts/runs/"+v.RunID, nil, &d, 200)
	var req runRequest
	_ = json.Unmarshal([]byte(d.Events[0].Data), &req)
	now := x.clk.now().Unix()
	r := betik.Receipt{Schema: 1, Seq: x.receiptSeq + 1, Prev: x.receiptPrev, RunID: v.RunID, ScriptID: req.ScriptID, Version: req.Version,
		ManifestSHA256: req.ManifestSHA256, ParamsSHA256: req.ParamsSHA256, StartedAt: now - 1, EndedAt: now,
		Status: "ok", OutputSHA256: betik.Digest([]byte("çıktı"))}
	if ov != nil {
		ov(&r)
	}
	p, err := json.Marshal(r)
	if err != nil {
		x.t.Fatal(err)
	}
	env, err := betik.Sign(x.host, betik.KindReceipt, p)
	if err != nil {
		x.t.Fatal(err)
	}
	return env
}

// secondAdmin ikinci 2FA'lı yönetici.
func (x *sEnv) secondAdmin() (*env, string) {
	x.t.Helper()
	c, _ := x.newUser("ayse", store.RoleAdmin, nil)
	sec, _, _ := x.enable2FA(c, "kalici-sifre-1")
	return c, sec
}

func (x *sEnv) state(run string) string {
	x.t.Helper()
	var d struct {
		Run runView `json:"run"`
	}
	x.mustDo("GET", "/api/scripts/runs/"+run, nil, &d, 200)
	return d.Run.State
}

func TestScriptAccessControl(t *testing.T) {
	x := setupScripts(t)
	x.addKeys()
	x.connect()
	x.upload("disk-temizle", 1, betik.ApprovalNone)

	// API anahtarı: yönetici rolü bile olsa hiçbir betik ucuna giremez (K1).
	ck := x.newKey("otomasyon", store.RoleAdmin, 0)
	for _, ep := range [][2]string{{"GET", "/api/scripts"}, {"GET", "/api/scripts/runs"}, {"GET", "/api/scripts/export"},
		{"POST", "/api/scripts/runs"}, {"POST", "/api/scripts/control/stop"}, {"POST", "/api/scripts/keys"}} {
		if code, _, _ := x.rawReq(ep[0], ep[1], bearer(ck.Secret), map[string]any{}); code < 400 {
			t.Errorf("API anahtarı %s %s: %d", ep[0], ep[1], code)
		}
	}
	// Çerez + Bearer birlikte: oturum yetkisi anahtarla karışmaz.
	req, _ := http.NewRequest("POST", x.srv.URL+"/api/scripts/runs", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+ck.Secret)
	req.Header.Set("X-Uptime", "1")
	resp, err := x.client.Do(req) // çerez kavanozlu istemci
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("çerez+Bearer: %d", resp.StatusCode)
	}
	// Yönetici olmayanlar.
	ed, _ := x.newUser("edit", store.RoleEditor, nil)
	vw, _ := x.newUser("izle", store.RoleViewer, nil)
	for _, c := range []*env{ed, vw, x.newClient()} {
		if code := c.do("GET", "/api/scripts", nil, nil); code != 401 && code != 403 {
			t.Errorf("yetkisiz okuma: %d", code)
		}
		if code := c.do("POST", "/api/scripts/runs", map[string]any{"script_id": "disk-temizle", "version": 1, "probe_id": x.probe.ID}, nil); code != 401 && code != 403 {
			t.Errorf("yetkisiz başlatma: %d", code)
		}
		if code := c.do("POST", "/api/scripts/control/stop", map[string]any{}, nil); code != 401 && code != 403 {
			t.Errorf("yetkisiz durdurma: %d", code)
		}
	}
	// 2FA'sız yönetici başlatamaz ama acil durdurabilir.
	noTF, _ := x.newUser("mehmet", store.RoleAdmin, nil)
	if code := noTF.do("POST", "/api/scripts/runs", map[string]any{"script_id": "disk-temizle", "version": 1, "probe_id": x.probe.ID}, nil); code != 403 {
		t.Errorf("2FA'sız başlatma: %d", code)
	}
	// Step-up: kod yok, yanlış, tekrar kullanılmış; kurtarma kodu yok.
	b := map[string]any{"script_id": "disk-temizle", "version": 1, "probe_id": x.probe.ID}
	x.mustDo("POST", "/api/scripts/runs", b, nil, 400)
	b["code"] = "000000"
	x.mustDo("POST", "/api/scripts/runs", b, nil, 400)
	code, v, raw := x.start(x.env, "disk-temizle", 1, x.secret, nil)
	if code != 201 || v.State != runQueued {
		t.Fatalf("başlatma: %d %s", code, raw)
	}
	b["code"] = x.code(x.secret)
	x.mustDo("POST", "/api/scripts/runs", b, nil, 400)
	// Sınır: başlatma sonrası kuyruğu geçen başka sunucu yok; bilinmeyen betik.
	if c, _, _ := x.start(x.env, "yok", 1, x.secret, nil); c != 404 {
		t.Errorf("bilinmeyen betik: %d", c)
	}
	// Acil durdurma 2FA'sız yöneticiyle de çalışır, kuyruk boşalır, yeni başlatma durur.
	noTF.mustDo("POST", "/api/scripts/control/stop", map[string]any{}, nil, 200)
	if got := x.state(v.RunID); got != runCancelled {
		t.Errorf("durdurma sonrası: %s", got)
	}
	if c, _, _ := x.start(x.env, "disk-temizle", 1, x.secret, nil); c != 409 {
		t.Errorf("durdurmada başlatma: %d", c)
	}
	// Devam etmek step-up ister ve 2FA'sız yapılamaz.
	noTF.mustDo("POST", "/api/scripts/control/resume", map[string]any{"code": "123456"}, nil, 403)
	x.mustDo("POST", "/api/scripts/control/resume", map[string]any{"code": x.step()}, nil, 200)
}

func TestScriptUpload(t *testing.T) {
	x := setupScripts(t)
	env, body := x.signed("a-b", 1, betik.ApprovalNone)
	// Anahtar yokken imza doğrulanamaz.
	x.mustDo("POST", "/api/scripts", map[string]any{"manifest": env, "body": body, "code": x.step()}, nil, 422)
	x.addKeys()
	// Onay anahtarını yayın yuvasına aynı anda koymak reddedilir.
	x.mustDo("POST", "/api/scripts/keys", map[string]any{"slot": "yayin", "public_key": betik.EncodePublicKey(pubOf(x.onay)), "name": "o2", "code": x.step()}, nil, 409)
	// Değiştirilmiş gövde.
	x.mustDo("POST", "/api/scripts", map[string]any{"manifest": env, "body": []byte("rm -rf /"), "code": x.step()}, nil, 422)
	// Yanlış anahtarla imzalı (onay anahtarı K_yayın yerine).
	x.yayin, x.onay = x.onay, x.yayin
	bad, b2 := x.signed("a-b", 2, betik.ApprovalNone)
	x.yayin, x.onay = x.onay, x.yayin
	x.mustDo("POST", "/api/scripts", map[string]any{"manifest": bad, "body": b2, "code": x.step()}, nil, 422)
	x.mustDo("POST", "/api/scripts", map[string]any{"manifest": env, "body": body, "code": x.step()}, nil, 201)
	x.mustDo("POST", "/api/scripts", map[string]any{"manifest": env, "body": body, "code": x.step()}, nil, 409)
	// İptal edilen sürüm başlatılamaz.
	x.connect()
	p, _ := json.Marshal(betik.Revoke{Schema: 1, ID: "a-b", BelowVersion: 2})
	rv, err := betik.Sign(x.yayin, betik.KindRevoke, p)
	if err != nil {
		t.Fatal(err)
	}
	x.mustDo("POST", "/api/scripts/revokes", map[string]any{"revoke": json.RawMessage(rv), "code": x.step()}, nil, 201)
	if c, _, _ := x.start(x.env, "a-b", 1, x.secret, nil); c != 409 {
		t.Errorf("iptal edilmiş sürüm: %d", c)
	}
	if l := x.poll(); len(l.Revokes) != 1 {
		t.Errorf("iptal zarfı ajana gitmeli: %+v", l)
	}
}

func TestScriptApprovalFlow(t *testing.T) {
	x := setupScripts(t)
	x.addKeys()
	x.connect()
	x.upload("servis", 1, betik.ApprovalPanel)

	// D3: tek yöneticide panel onayı bilete düşer, tek başına kuyruğa giremez.
	_, v, raw := x.start(x.env, "servis", 1, x.secret, nil)
	if v.State != runAwaitTicket || v.Needs != betik.ApprovalSigned {
		t.Fatalf("tek yönetici: %s", raw)
	}
	x.mustDo("POST", "/api/scripts/runs/"+v.RunID+"/reject", map[string]any{}, nil, 200)

	ayse, ayseSec := x.secondAdmin()
	_, v, _ = x.start(x.env, "servis", 1, x.secret, nil)
	if v.State != runAwaitApproval {
		t.Fatalf("iki yönetici: %s", v.State)
	}
	if l := x.poll(); len(l.Runs) != 0 {
		t.Fatal("onaysız iş ajana gitmemeli")
	}
	// Kendi isteğini onaylama.
	x.mustDo("POST", "/api/scripts/runs/"+v.RunID+"/approve", map[string]any{"code": x.step()}, nil, 403)
	// İkinci yönetici onaylar; ikinci kez onay reddedilir.
	x.clk.advance(31 * time.Second)
	ayse.mustDo("POST", "/api/scripts/runs/"+v.RunID+"/approve", map[string]any{"code": x.code(ayseSec)}, nil, 200)
	x.clk.advance(31 * time.Second)
	ayse.mustDo("POST", "/api/scripts/runs/"+v.RunID+"/approve", map[string]any{"code": x.code(ayseSec)}, nil, 409)
	l := x.poll()
	if len(l.Runs) != 1 || l.Runs[0].RunID != v.RunID {
		t.Fatalf("onaylı iş gitmeli: %+v", l)
	}
	// Kabul görülene dek yeniden gönderilir, sonra gönderilmez.
	if l := x.poll(); len(l.Runs) != 1 {
		t.Fatal("kabulsüz iş yeniden gönderilmeli")
	}
	x.mustDo("POST", "/api/scripts/runs/"+v.RunID+"/approve", map[string]any{"code": x.step()}, nil, 403)
	if c := x.probeEvent(x.probe.Token, map[string]any{"kind": "accepted", "run_id": v.RunID, "time": 1}); c != 200 {
		t.Fatalf("accepted: %d", c)
	}
	if l := x.poll(); len(l.Runs) != 0 {
		t.Fatal("kabul edilen iş bir daha gitmemeli")
	}
	// run_id_replay kabul sonrası kapatmaz.
	x.probeEvent(x.probe.Token, map[string]any{"kind": "rejected", "run_id": v.RunID, "code": betik.CodeRunIDReplay, "time": 1})
	if s := x.state(v.RunID); s != runAccepted {
		t.Fatalf("replay durumu değiştirdi: %s", s)
	}
	// İptal komutu ajana gider.
	x.mustDo("POST", "/api/scripts/runs/"+v.RunID+"/cancel", map[string]any{}, nil, 200)
	if l := x.poll(); len(l.Cancel) != 1 {
		t.Fatalf("iptal gitmeli: %+v", l)
	}
}

func (x *sEnv) ticket(v runView, tr map[string]any, ov func(*betik.Ticket)) json.RawMessage {
	x.t.Helper()
	now := x.clk.now().Unix()
	tk := betik.Ticket{Schema: 1, RunID: v.RunID, HostKey: tr["host_key"].(string), ScriptID: tr["script_id"].(string),
		Version: int64(tr["version"].(float64)), ManifestSHA256: tr["manifest_sha256"].(string), ParamsSHA256: tr["params_sha256"].(string),
		IssuedAt: now, ExpiresAt: now + 600, Approver: "kadir"}
	if ov != nil {
		ov(&tk)
	}
	p, _ := json.Marshal(tk)
	k := x.onay
	e, err := betik.Sign(k, betik.KindTicket, p)
	if err != nil {
		x.t.Fatal(err)
	}
	return e
}

func TestScriptTicket(t *testing.T) {
	x := setupScripts(t)
	x.addKeys()
	x.connect()
	x.upload("yenile", 1, betik.ApprovalSigned)
	_, v, _ := x.start(x.env, "yenile", 1, x.secret, nil)
	var d struct {
		Req map[string]any `json:"ticket_request"`
	}
	x.mustDo("GET", "/api/scripts/runs/"+v.RunID, nil, &d, 200)
	if d.Req == nil {
		t.Fatal("ticket_request yok")
	}
	path := "/api/scripts/runs/" + v.RunID + "/ticket"
	// Başka parametre, başka sunucu, süresi dolmuş, yanlış anahtar.
	x.mustDo("POST", path, map[string]any{"ticket": x.ticket(v, d.Req, func(k *betik.Ticket) { k.ParamsSHA256 = betik.Digest([]byte("x")) }), "code": x.step()}, nil, 409)
	x.mustDo("POST", path, map[string]any{"ticket": x.ticket(v, d.Req, func(k *betik.Ticket) { k.HostKey = betik.Digest([]byte("y")) }), "code": x.step()}, nil, 409)
	tk := x.ticket(v, d.Req, func(k *betik.Ticket) { k.ExpiresAt = x.clk.now().Unix() + 5 })
	x.mustDo("POST", path, map[string]any{"ticket": tk, "code": x.step()}, nil, 409)
	x.yayin, x.onay = x.onay, x.yayin
	wrong := x.ticket(v, d.Req, nil)
	x.yayin, x.onay = x.onay, x.yayin
	_ = wrong
	sign := genKey(t)
	x.onay, sign = sign, x.onay
	wrong = x.ticket(v, d.Req, nil)
	x.onay, sign = sign, x.onay
	x.mustDo("POST", path, map[string]any{"ticket": wrong, "code": x.step()}, nil, 422)
	if x.state(v.RunID) != runAwaitTicket {
		t.Fatal("geçersiz biletler durumu değiştirmemeli")
	}
	x.mustDo("POST", path, map[string]any{"ticket": x.ticket(v, d.Req, nil), "code": x.step()}, nil, 200)
	l := x.poll()
	if len(l.Runs) != 1 || len(l.Runs[0].Ticket) == 0 {
		t.Fatalf("bilet ajana gitmeli: %+v", l)
	}
}

func TestScriptProbeEvents(t *testing.T) {
	x := setupScripts(t)
	x.addKeys()
	x.connect()
	x.upload("disk", 1, betik.ApprovalNone)
	var other struct {
		Token string `json:"token"`
	}
	x.mustDo("POST", "/api/probes", map[string]any{"name": "s2"}, &other, 201)
	_, v, _ := x.start(x.env, "disk", 1, x.secret, nil)
	// Başka ajan bu run_id'ye olay yazamaz.
	if c := x.probeEvent(other.Token, map[string]any{"kind": "accepted", "run_id": v.RunID, "time": 1}); c != 409 {
		t.Errorf("başka ajan: %d", c)
	}
	if c := x.probeEvent(x.probe.Token, map[string]any{"kind": "accepted", "run_id": strings.Repeat("a", 32), "time": 1}); c != 409 {
		t.Errorf("uydurma run: %d", c)
	}
	x.probeEvent(x.probe.Token, map[string]any{"kind": "accepted", "run_id": v.RunID, "time": 1})
	// Sahte makbuz: ajan anahtarı dışında imza.
	real := x.host
	x.host = genKey(t)
	bad := x.receipt(v, nil)
	x.host = real
	if c := x.probeEvent(x.probe.Token, map[string]any{"kind": "receipt", "run_id": v.RunID, "receipt": bad}); c != 422 {
		t.Errorf("sahte makbuz: %d", c)
	}
	// Başka betiğin özeti.
	if c := x.probeEvent(x.probe.Token, map[string]any{"kind": "receipt", "run_id": v.RunID,
		"receipt": x.receipt(v, func(r *betik.Receipt) { r.ManifestSHA256 = betik.Digest([]byte("z")) })}); c != 422 {
		t.Errorf("özet tutmayan makbuz: %d", c)
	}
	out := []byte("\x1b[31mkirmizi\x1b[0m\x07 <script>\n")
	good := x.receipt(v, nil)
	if c := x.probeEvent(x.probe.Token, map[string]any{"kind": "receipt", "run_id": v.RunID, "receipt": good, "output": out}); c != 200 {
		t.Fatalf("makbuz: %d", c)
	}
	if c := x.probeEvent(x.probe.Token, map[string]any{"kind": "receipt", "run_id": v.RunID, "receipt": good, "output": out}); c != 200 {
		t.Fatalf("yinelenen makbuz 2xx olmalı: %d", c)
	}
	var d struct {
		Run    runView            `json:"run"`
		Events []betikkayit.Event `json:"events"`
		Output struct {
			Text string `json:"text"`
		} `json:"output"`
	}
	x.mustDo("GET", "/api/scripts/runs/"+v.RunID, nil, &d, 200)
	if d.Run.State != runOK || d.Output.Text != "kirmizi <script>\n" {
		t.Fatalf("sonuç: %+v %q", d.Run, d.Output.Text)
	}
	if n := len(d.Events); d.Events[n-1].Kind != "receipt" || strings.Count(fmt.Sprint(d.Events), "receipt") < 1 {
		t.Fatal("makbuz bir kez yazılmalı")
	}
	// Eski ajan: başlık yoksa script_jobs yok.
	_, v2, _ := x.start(x.env, "disk", 1, x.secret, nil)
	code, _, body := x.rawReq("GET", "/api/probe/jobs", bearer(x.probe.Token), nil)
	if code != 200 || strings.Contains(string(body), "script_jobs") {
		t.Fatalf("eski ajana alan gitmemeli: %s", body)
	}
	// Süre dolumu.
	x.clk.advance(2 * time.Hour)
	x.s.scriptTick(context.Background(), t.TempDir())
	if s := x.state(v2.RunID); s != runExpired {
		t.Errorf("süre dolumu: %s", s)
	}
	// Zincir dışa aktarılır ve çevrimdışı doğrulanır.
	x.mustDo("POST", "/api/scripts/control/stop", map[string]any{}, nil, 200)
	x.chain(t)
}

func (x *sEnv) chain(t *testing.T) {
	t.Helper()
	var body json.RawMessage
	x.mustDo("GET", "/api/scripts/export", nil, &body, 200)
	ex, err := betikkayit.ParseExport(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := betikkayit.Verify(ex); err != nil {
		t.Fatalf("zincir: %v", err)
	}
}
