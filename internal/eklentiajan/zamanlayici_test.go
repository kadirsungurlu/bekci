package eklentiajan

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/denetim"
	"github.com/kadirsungurlu/bekci/internal/eklenti"
)

func readOlaylar(t *testing.T, dir string) []Olay {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "kayit.jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Olay
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var o Olay
		json.Unmarshal(sc.Bytes(), &o)
		out = append(out, o)
	}
	return out
}

func turler(os []Olay) string {
	var s []string
	for _, o := range os {
		s = append(s, o.Tur)
	}
	return strings.Join(s, ",")
}

func TestZamanlayiciAkis(t *testing.T) {
	f := newFixture(t)
	f.config(Digest(f.body))
	now := testNow
	ex := &fakeExec{out: Outcome{Stdout: []byte("OK | a=1\n")}}
	s := f.sched(ex)
	s.Now = func() time.Time { return now }
	it := f.item(f.manifest(nil), f.priv, 300)
	ctx := context.Background()

	s.Handle(nil) // eski panel: alan yok
	s.RunDue(ctx)
	if ex.calls.Load() != 0 {
		t.Fatal("alan yokken çalıştı")
	}
	builtin := Item{Name: "noop", Source: "builtin", Kind: "noop", Interval: 60}
	s.Handle(mustJSON([]Item{it, builtin}))
	now = now.Add(firstSpread)
	s.RunDue(ctx)
	if ex.calls.Load() != 1 || len(s.Queue()) != 2 { // yerleşik Exec kullanmaz
		t.Fatalf("ilk tur: çağrı %d kuyruk %d", ex.calls.Load(), len(s.Queue()))
	}
	now = now.Add(299 * time.Second)
	s.RunDue(ctx)
	if ex.calls.Load() != 1 {
		t.Fatal("aralık dolmadan çalıştı")
	}
	now = now.Add(time.Second)
	s.RunDue(ctx)
	if ex.calls.Load() != 2 {
		t.Fatal("aralık dolunca çalışmadı")
	}
	// Sürüm değişimi kayda girer.
	f.body = []byte(testBody + "# v4\n")
	f.config(Digest(f.body))
	it4 := f.item(f.manifest(func(m map[string]any) { m["version"] = 4 }), f.priv, 300)
	s.Handle(mustJSON([]Item{it4}))
	now = now.Add(300 * time.Second)
	s.RunDue(ctx)
	// Panel kapatır.
	s.Handle(mustJSON([]Item{}))
	now = now.Add(time.Hour)
	s.RunDue(ctx)
	if ex.calls.Load() != 3 {
		t.Fatalf("çağrı %d", ex.calls.Load())
	}
	// Günlük özet.
	s.ozet()
	now = now.Add(25 * time.Hour)
	s.ozet()
	time.Sleep(50 * time.Millisecond) // Handle olayları ayrı goroutine'de yazılır
	got := turler(readOlaylar(t, f.p.LogDir))
	for _, want := range []string{"ac", "surum", "kapat", "ozet"} {
		if !strings.Contains(got, want) {
			t.Fatalf("olaylar %q, %q yok", got, want)
		}
	}
	if strings.Count(got, "surum") != 2 { // 0->3, 3->4; her çalıştırma değil
		t.Fatalf("olaylar %q", got)
	}
	ok, err := (&DosyaKayit{Dir: f.p.LogDir}).Saglam()
	if !ok || err != nil {
		t.Fatalf("zincir %v %v", ok, err)
	}
}

func TestFlush(t *testing.T) {
	f := newFixture(t)
	now := testNow
	s := f.sched(&fakeExec{})
	s.Now = func() time.Time { return now }
	s.Handle(nil)
	for i := 0; i < maxQueue+5; i++ {
		s.push(Result{Name: "yuk", T: int64(i)})
	}
	if q := s.Queue(); len(q) != maxQueue || q[0].T != 5 {
		t.Fatalf("kuyruk %d ilk %d", len(q), q[0].T)
	}
	var sizes []int
	code := http.StatusNotFound
	post := func(_ context.Context, path string, body any) (int, error) {
		if path != ChecksPath {
			t.Fatal(path)
		}
		sizes = append(sizes, len(body.(map[string]any)["results"].([]Result)))
		return code, nil
	}
	ctx := context.Background()
	s.Flush(ctx, post) // eski panel: 404, kuyruk korunur, 10 dk beklenir
	s.Flush(ctx, post)
	if len(sizes) != 1 || len(s.Queue()) != maxQueue {
		t.Fatalf("404: gönderim %v kuyruk %d", sizes, len(s.Queue()))
	}
	now = now.Add(oldPanelWait)
	code = http.StatusOK
	s.Flush(ctx, post)
	if len(s.Queue()) != 0 || len(sizes) != 1+maxQueue/maxBatch || sizes[1] != maxBatch {
		t.Fatalf("200: gönderim %v kuyruk %d", sizes, len(s.Queue()))
	}
}

func TestKomut(t *testing.T) {
	f := newFixture(t)
	var out bytes.Buffer
	sha := Digest(f.body)
	if err := komut(f.p, []string{"izin", "yuk", sha}, &out, false); err == nil {
		t.Fatal("root olmadan izin verildi")
	}
	if err := komut(f.p, []string{"izin", "Yuk!", sha}, &out, true); err == nil {
		t.Fatal("geçersiz ad kabul edildi")
	}
	if err := komut(f.p, []string{"izin", "yuk", "abc"}, &out, true); err == nil {
		t.Fatal("geçersiz özet kabul edildi")
	}
	if err := komut(f.p, []string{"izin", "yuk", sha}, &out, true); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(f.p.Config)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("izin %v", st.Mode().Perm())
	}
	c, err := LoadConfig(f.p.Config)
	if err != nil || c.Pin("yuk") != sha {
		t.Fatalf("iğne %v %v", c, err)
	}
	if err := komut(f.p, []string{"kapat", "yuk"}, &out, true); err != nil {
		t.Fatal(err)
	}
	if err := komut(f.p, []string{"kapat", "yuk"}, &out, true); err == nil {
		t.Fatal("olmayan iğne kapatıldı")
	}
	out.Reset()
	if err := komut(f.p, []string{"liste"}, &out, false); err != nil || !strings.Contains(out.String(), "zincir sağlam") {
		t.Fatalf("liste %q %v", out.String(), err)
	}
	if got := turler(readOlaylar(t, f.p.LogDir)); got != "izin,igne_k" {
		t.Fatalf("olaylar %q", got)
	}
	// Zincir kurcalanırsa liste bildirir.
	p := filepath.Join(f.p.LogDir, "kayit.jsonl")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, bytes.Replace(b, []byte(`"izin"`), []byte(`"ac"`), 1), 0o600)
	out.Reset()
	komut(f.p, []string{"liste"}, &out, false)
	if !strings.Contains(out.String(), "KIRIK") {
		t.Fatalf("kırık zincir görülmedi: %q", out.String())
	}
}

func TestSystemdArgs(t *testing.T) {
	a := strings.Join((&Systemd{PrivatePIDs: true}).Args(Spec{Unit: "u.service", RunAs: "nobody", Timeout: 10 * time.Second}), " ")
	for _, want := range []string{"RuntimeMaxSec=75", "timeout -k 5 10 /bin/sh", "MemoryMax=128M", "CPUQuota=20%", "TasksMax=32", "ProtectSystem=strict",
		"PrivateNetwork=yes", "PrivatePIDs=yes", "User=nobody", "NoNewPrivileges=yes", "InaccessiblePaths=-/etc/uptime-agent.env",
		"InaccessiblePaths=-/etc/bekci", "--expand-environment=no"} {
		if !strings.Contains(a, want) {
			t.Fatalf("%q yok: %s", want, a)
		}
	}
	if strings.Contains(a, "ReadWritePaths") {
		t.Fatal("ReadWritePaths olmamalı")
	}
	a = strings.Join((&Systemd{}).Args(Spec{Unit: "u.service", RunAs: "root", Network: true, Timeout: time.Second}), " ")
	if strings.Contains(a, "PrivateNetwork") || strings.Contains(a, "User=") || strings.Contains(a, "PrivatePIDs") {
		t.Fatalf("root+ağ: %s", a)
	}
}

// Yerleşik denetim (743): kafes/Exec gerekmez; bilinmeyen tür, kimlik izni
// ve loopback dışı hedef kodlu hata olarak panele gider.
func TestYerlesik(t *testing.T) {
	kd := filepath.Join(t.TempDir(), "kimlik.d")
	if err := os.Mkdir(kd, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kd, "acik.json"), []byte(`{"user":"a"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Scheduler{Paths: Paths{Kimlik: kd}, GOOS: "linux"} // Exec nil: eklentiler RedPlatform
	ctx := context.Background()
	cases := []struct {
		it     Item
		status eklenti.Status
		code   string
		msg    string
	}{
		{Item{Name: "a", Source: "builtin", Kind: "noop", Params: json.RawMessage(`{"status":"critical","value":3}`)}, eklenti.Critical, "", "noop"},
		{Item{Name: "b", Source: "builtin", Kind: "yok"}, eklenti.Unknown, "kind", "tanımıyor"},
		{Item{Name: "c", Source: "builtin", Kind: "noop", Params: json.RawMessage(`{"credential":"acik"}`)}, eklenti.Unknown, "credential", "kimlik"},
		{Item{Name: "d", Source: "builtin", Kind: "noop", Params: json.RawMessage(`{"target":"192.0.2.10:3306"}`)}, eklenti.Unknown, "target", "loopback"},
		{Item{Name: "e", Source: "builtin", Kind: "noop", Params: json.RawMessage(`{"bilinmeyen":1}`)}, eklenti.Unknown, "params", "bilinmeyen"},
	}
	for _, c := range cases {
		r := s.RunOne(ctx, c.it)
		if r.Source != "builtin" || r.Kind != c.it.Kind || r.Status != c.status || r.Error != c.code || !strings.Contains(r.Message, c.msg) || r.T == 0 {
			t.Errorf("%s: %+v", c.it.Name, r)
		}
	}
}

// Docker ajanında sunucu dosyası/programı isteyen tür açık iletiyle reddedilir
// (VM kabulü: yedek denetimi kapsayıcının dosya sisteminde "folder not found"
// diyordu).
func TestYerlesikKapsayici(t *testing.T) {
	denetim.Kaydet(denetim.Tur{Kind: "test_sunucu", YalnizSunucu: true,
		Interval: denetim.Aralik{Min: 60, Max: 600, Default: 60},
		Calistir: func(context.Context, denetim.Girdi) eklenti.Result {
			return eklenti.Result{Status: eklenti.OK, Metrics: []eklenti.Metric{}}
		}})
	ctx := context.Background()
	it := Item{Name: "y", Source: "builtin", Kind: "test_sunucu"}
	s := &Scheduler{GOOS: "linux", Container: true}
	if r := s.RunOne(ctx, it); r.Status != eklenti.Unknown || r.Error != denetim.HataPlatform || !strings.Contains(r.Message, "systemd") {
		t.Fatalf("kapsayıcı: %+v", r)
	}
	if r := s.RunOne(ctx, Item{Name: "n", Source: "builtin", Kind: "noop", Params: json.RawMessage(`{}`)}); r.Status != eklenti.OK {
		t.Fatalf("noop kapsayıcıda çalışmalı: %+v", r)
	}
	s.Container = false
	if r := s.RunOne(ctx, it); r.Status != eklenti.OK {
		t.Fatalf("sunucuda: %+v", r)
	}
}
