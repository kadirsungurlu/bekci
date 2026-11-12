package denetim

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/eklenti"
)

func noop(t *testing.T) Tur {
	t.Helper()
	tur, ok := Bul("noop")
	if !ok {
		t.Fatal("noop kayıtlı değil")
	}
	return tur
}

func kod(err error) string {
	var h *Hata
	if errors.As(err, &h) {
		return h.Kod
	}
	return ""
}

func TestDogrulaNoop(t *testing.T) {
	tur := noop(t)
	p, err := tur.Dogrula(nil)
	if err != nil || p["status"] != "ok" || p["value"] != int64(1) {
		t.Fatalf("varsayılanlar: %v %v", p, err)
	}
	cases := []struct {
		in, code string
	}{
		{`{"status":"bad"}`, HataParams},
		{`{"nope":1}`, HataParams},
		{`{"value":1.5}`, HataParams},
		{`{"value":"3"}`, HataParams},
		{`{"value":1e12}`, HataParams},
		{`{"credential":"../etc/shadow"}`, HataParams},
		{`{"message":"a\nb"}`, HataParams},
		{`{"target":"10.0.0.5:3306"}`, HataTarget},
		{`{"target":"db.example.com:3306"}`, HataTarget},
		{`{"target":"0.0.0.0:80"}`, HataTarget},
		{`{"target":"127.0.0.1"}`, HataParams},
		{`{"target":"127.0.0.1:0"}`, HataParams},
		{`[1]`, HataParams},
	}
	for _, c := range cases {
		if _, err := tur.Dogrula(json.RawMessage(c.in)); kod(err) != c.code {
			t.Errorf("%s: kod %q, beklenen %q (%v)", c.in, kod(err), c.code, err)
		}
	}
	for _, ok := range []string{`{"target":"127.0.0.1:3306"}`, `{"target":"localhost:80"}`, `{"target":"[::1]:5432"}`,
		`{"target":"10.0.0.5:3306","credential":"db"}`, `{"value":-5,"status":"critical"}`} {
		if _, err := tur.Dogrula(json.RawMessage(ok)); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
}

func TestHedefURLSoket(t *testing.T) {
	for in, want := range map[string]string{"http://127.0.0.1/server-status?auto": "127.0.0.1",
		"https://localhost:8443/x": "localhost", "http://[::1]/": "::1"} {
		h, err := hedefHost(PURL, in)
		if err != nil || h != want || !Loopback(h) {
			t.Errorf("%s: %q %v", in, h, err)
		}
	}
	for _, bad := range []string{"ftp://127.0.0.1/", "http://u:p@127.0.0.1/", "127.0.0.1/x", "http://127.0.0.1:99999/"} {
		if _, err := hedefHost(PURL, bad); err == nil {
			t.Errorf("%s kabul edildi", bad)
		}
	}
	if Loopback("127.example.com") || Loopback("example.com") || !Loopback("127.8.0.1") || !Loopback("LOCALHOST.") {
		t.Error("Loopback hatalı")
	}
	for _, ok := range []string{"/var/run/mysqld/mysqld.sock", `\\.\pipe\MSSQL$SQLEXPRESS\sql\query`} {
		if soketDogrula(ok) != nil {
			t.Errorf("%s reddedildi", ok)
		}
	}
	for _, bad := range []string{"mysqld.sock", "/var/../etc/x", "tcp://127.0.0.1"} {
		if soketDogrula(bad) == nil {
			t.Errorf("%s kabul edildi", bad)
		}
	}
}

// kimlikKur test klasörü; sahip denetimi çalışan kullanıcıya göre yapılır.
func kimlikKur(t *testing.T, mode os.FileMode, body string) string {
	t.Helper()
	old := beklenenSahip
	beklenenSahip = uint32(os.Geteuid())
	t.Cleanup(func() { beklenenSahip = old })
	dir := filepath.Join(t.TempDir(), "kimlik.d")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "db.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestKimlikIzin(t *testing.T) {
	body := `{"user":"izle","password":"gizli","hosts":["10.0.0.5"]}`
	dir := kimlikKur(t, 0o600, body)
	k, err := KimlikOku(dir, "db")
	if err != nil || k.User != "izle" || !k.HostIzinli("10.0.0.5") || k.HostIzinli("10.0.0.6") {
		t.Fatalf("600: %+v %v", k, err)
	}
	for _, m := range []os.FileMode{0o644, 0o640, 0o604, 0o660} {
		dir := kimlikKur(t, m, body)
		_, err := KimlikOku(dir, "db")
		if kod(err) != HataCredential || !strings.Contains(err.Error(), "600 olmalı") {
			t.Errorf("%04o: %v", m, err)
		}
	}
	// Klasör herkese yazılabilir.
	dir = kimlikKur(t, 0o600, body)
	os.Chmod(dir, 0o777)
	if _, err := KimlikOku(dir, "db"); kod(err) != HataCredential {
		t.Errorf("777 klasör: %v", err)
	}
	// Sembolik bağ.
	dir = kimlikKur(t, 0o600, body)
	os.Symlink(filepath.Join(dir, "db.json"), filepath.Join(dir, "bag.json"))
	if _, err := KimlikOku(dir, "bag"); kod(err) != HataCredential || !strings.Contains(err.Error(), "bağ") {
		t.Errorf("bağ: %v", err)
	}
	// Yok, bozuk, bilinmeyen alan.
	if _, err := KimlikOku(dir, "yok"); kod(err) != HataCredential || !strings.Contains(err.Error(), "yok") {
		t.Errorf("yok: %v", err)
	}
	dir = kimlikKur(t, 0o600, `{"user":"a","pasword":"x"}`)
	if _, err := KimlikOku(dir, "db"); kod(err) != HataCredential {
		t.Errorf("bilinmeyen alan: %v", err)
	}
	// Sahibi root değil (yalnız root ile denenebilir).
	if os.Geteuid() == 0 {
		dir = kimlikKur(t, 0o600, body)
		os.Chown(filepath.Join(dir, "db.json"), 65534, 65534)
		if _, err := KimlikOku(dir, "db"); kod(err) != HataCredential || !strings.Contains(err.Error(), "sahibi root değil") {
			t.Errorf("sahip: %v", err)
		}
	}
}

func TestYurutNoop(t *testing.T) {
	tur := noop(t)
	ctx := context.Background()
	r, code := tur.Yurut(ctx, json.RawMessage(`{"status":"warning","value":7}`), t.TempDir(), "linux")
	if code != "" || r.Status != eklenti.Warning || len(r.Metrics) != 1 || r.Metrics[0].Value != 7 {
		t.Fatalf("noop: %+v %q", r, code)
	}
	// Kimlik izni yanlış -> açık hata.
	dir := kimlikKur(t, 0o644, `{"user":"a"}`)
	r, code = tur.Yurut(ctx, json.RawMessage(`{"credential":"db"}`), dir, "linux")
	if code != HataCredential || r.Status != eklenti.Unknown || !strings.Contains(r.Message, "chmod 600") {
		t.Fatalf("kimlik izni: %+v %q", r, code)
	}
	// Loopback dışı hedef: kimlik listesinde yoksa red, varsa geçer.
	dir = kimlikKur(t, 0o600, `{"hosts":["10.0.0.5:3306"]}`)
	if _, code = tur.Yurut(ctx, json.RawMessage(`{"target":"10.0.0.6:3306","credential":"db"}`), dir, "linux"); code != HataTarget {
		t.Fatalf("liste dışı hedef: %q", code)
	}
	if r, code = tur.Yurut(ctx, json.RawMessage(`{"target":"10.0.0.5:3306","credential":"db"}`), dir, "linux"); code != "" {
		t.Fatalf("listedeki hedef: %+v %q", r, code)
	}
	if _, code = tur.Yurut(ctx, json.RawMessage(`{"target":"10.0.0.5:3306"}`), dir, "linux"); code != HataTarget {
		t.Fatalf("kimliksiz hedef: %q", code)
	}
}

func TestYurutPanikPlatform(t *testing.T) {
	tur := Tur{Kind: "x", Platforms: []string{"windows"}, Calistir: func(context.Context, Girdi) eklenti.Result { panic("boom") }}
	if _, code := tur.Yurut(context.Background(), nil, "", "linux"); code != HataPlatform {
		t.Fatalf("platform: %q", code)
	}
	tur.Platforms = nil
	if r, code := tur.Yurut(context.Background(), nil, "", "linux"); code != HataBuiltin || !strings.Contains(r.Message, "boom") {
		t.Fatalf("panik: %+v %q", r, code)
	}
}
