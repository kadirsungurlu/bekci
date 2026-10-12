package agentupdate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// useTestKey doğrulamayı test anahtarına yönlendirir; test bitince kaldırır.
func useTestKey(t *testing.T, pub ed25519.PublicKey) {
	t.Helper()
	TestPublicKey = pub
	t.Cleanup(func() { TestPublicKey = nil })
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestEmbeddedPublicKeyParses(t *testing.T) {
	pub, err := ParsePublicKey(PublicKeyBase64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		t.Fatalf("gömülü açık anahtar: %v", err)
	}
	if _, err := ParsePublicKey("abc"); err == nil {
		t.Error("kısa anahtar kabul edildi")
	}
}

func TestManifestCanonical(t *testing.T) {
	m := Manifest{Version: "1.3.1", OS: "linux", Arch: "amd64", SHA256: strings.Repeat("ab", 32)}
	want := "bekci-agent-manifest-v1\n1.3.1\nlinux\namd64\n" + strings.Repeat("ab", 32) + "\n"
	if got := string(m.Canonical()); got != want {
		t.Fatalf("kanonik: %q", got)
	}
	// Alan sırası ve biçimi sabittir; aynı bildirim aynı baytları verir.
	if !bytes.Equal(m.Canonical(), (Manifest{"1.3.1", "linux", "amd64", strings.Repeat("ab", 32)}).Canonical()) {
		t.Error("kanonik gösterim kararlı değil")
	}
	bad := []Manifest{
		{Version: "1.3.1\nx", OS: "linux", Arch: "amd64", SHA256: m.SHA256},
		{Version: "", OS: "linux", Arch: "amd64", SHA256: m.SHA256},
		{Version: "1.3.1", OS: "Linux", Arch: "amd64", SHA256: m.SHA256},
		{Version: "1.3.1", OS: "linux", Arch: "amd64", SHA256: strings.ToUpper(m.SHA256)},
		{Version: "1.3.1", OS: "linux", Arch: "amd64", SHA256: "abc"},
	}
	for _, b := range bad {
		if b.Validate() == nil {
			t.Errorf("geçersiz bildirim kabul edildi: %+v", b)
		}
	}
}

func TestSignVerify(t *testing.T) {
	pub, priv := newKey(t)
	m := Manifest{Version: "1.3.1", OS: "linux", Arch: "amd64", SHA256: sum([]byte("program"))}
	sig, err := Sign(priv, m)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(pub, m, sig); err != nil {
		t.Fatalf("geçerli imza reddedildi: %v", err)
	}
	// Bozuk imza.
	bad := append([]byte(nil), sig...)
	bad[0] ^= 1
	if !errors.Is(Verify(pub, m, bad), ErrBadSignature) {
		t.Error("bozuk imza kabul edildi")
	}
	// Yanlış anahtar.
	other, _ := newKey(t)
	if !errors.Is(Verify(other, m, sig), ErrBadSignature) {
		t.Error("başka anahtarın imzası kabul edildi")
	}
	// Bildirim değişti (program değiştirildi, sürüm/platform oynandı).
	for _, mm := range []Manifest{
		{m.Version, m.OS, m.Arch, sum([]byte("baska program"))},
		{"1.3.2", m.OS, m.Arch, m.SHA256},
		{m.Version, "windows", m.Arch, m.SHA256},
		{m.Version, m.OS, "arm64", m.SHA256},
	} {
		if Verify(pub, mm, sig) == nil {
			t.Errorf("değiştirilmiş bildirim kabul edildi: %+v", mm)
		}
	}
	// Kısa imza / anahtar.
	if Verify(pub, m, sig[:10]) == nil || Verify(pub[:5], m, sig) == nil {
		t.Error("kısa imza/anahtar kabul edildi")
	}
	// Signed sarmalayıcı.
	s := Signed{Manifest: m, Sig: base64.StdEncoding.EncodeToString(sig)}
	if err := s.Verify(pub); err != nil {
		t.Fatal(err)
	}
	s.Sig = "***"
	if s.Verify(pub) == nil {
		t.Error("base64 olmayan imza kabul edildi")
	}
}

func TestVersions(t *testing.T) {
	ok := map[string]Version{
		"1.3.1":        {1, 3, 1, ""},
		"v1.3.1":       {1, 3, 1, ""},
		"1.2.0-rc1":    {1, 2, 0, "rc1"},
		"2.0.0+abc123": {2, 0, 0, ""},
	}
	for s, want := range ok {
		got, is := ParseVersion(s)
		if !is || got != want {
			t.Errorf("%q → %+v %v", s, got, is)
		}
	}
	for _, s := range []string{"977e923", "dev", "test", "1.3", "1.3.1.4", "1.x.0", "", "1.3.1 "} {
		if _, is := ParseVersion(s); is && s != "1.3.1 " {
			t.Errorf("%q sürüm sayıldı", s)
		}
	}
	cases := []struct {
		cur, target string
		want        bool
	}{
		{"1.3.0", "1.3.1", true},
		{"1.3.1", "1.3.0", false}, // sürüm düşürme yok
		{"1.3.1", "1.3.1", false},
		{"1.3.1", "2.0.0", true},
		{"1.9.9", "1.10.0", true},
		{"1.3.1-rc1", "1.3.1", true},
		{"1.3.1", "1.3.2-rc1", true},
		{"1.3.1", "1.3.1-rc1", false},
		{"1.3.1-rc1", "1.3.1-rc2", true},
		{"1.3.1-rc.2", "1.3.1-rc.10", true},
		{"977e923", "1.3.1", false}, // commit özeti derlemesi güncellenmez
		{"1.3.0", "977e923", false}, // commit özeti sunulmaz
		{"dev", "1.3.1", false},
		{"v1.3.0", "v1.3.1", true},
	}
	for _, c := range cases {
		if got := Newer(c.cur, c.target); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, %v bekleniyordu", c.cur, c.target, got, c.want)
		}
	}
}

// fakeAgent geçici dizinde "çalışan program" ve ona bağlı bir Updater kurar.
func fakeAgent(t *testing.T, version string) (*Updater, string) {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "uptime")
	if err := os.WriteFile(exe, []byte("eski program "+version), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Updater{Exe: exe, Version: version, OS: "linux", Arch: "amd64"}, exe
}

func offerFor(t *testing.T, priv ed25519.PrivateKey, version string, body []byte) Offer {
	t.Helper()
	m := Manifest{Version: version, OS: "linux", Arch: "amd64", SHA256: sum(body)}
	sig, err := Sign(priv, m)
	if err != nil {
		t.Fatal(err)
	}
	return Offer{Signed: Signed{Manifest: m, Sig: base64.StdEncoding.EncodeToString(sig)}, URL: "/api/probe/binary?os=linux&arch=amd64"}
}

func serve(body []byte) Fetch {
	return func(_ context.Context, _ string, w io.Writer) error {
		_, err := w.Write(body)
		return err
	}
}

func TestInstallSwapsAtomically(t *testing.T) {
	pub, priv := newKey(t)
	useTestKey(t, pub)
	u, exe := fakeAgent(t, "1.3.0")
	newBody := []byte("yeni program 1.3.1")
	fetched := ""
	fetch := func(ctx context.Context, url string, w io.Writer) error {
		fetched = url
		_, err := w.Write(newBody)
		return err
	}
	if err := u.Install(context.Background(), offerFor(t, priv, "1.3.1", newBody), fetch); err != nil {
		t.Fatal(err)
	}
	if fetched != "/api/probe/binary?os=linux&arch=amd64" {
		t.Errorf("indirme adresi %q", fetched)
	}
	got, _ := os.ReadFile(exe)
	if !bytes.Equal(got, newBody) {
		t.Fatalf("program değişmedi: %q", got)
	}
	st, _ := os.Stat(exe)
	if st.Mode().Perm()&0o111 == 0 {
		t.Error("yeni program yürütülebilir değil")
	}
	old, _ := os.ReadFile(exe + ".old")
	if string(old) != "eski program 1.3.0" {
		t.Errorf(".old eski program değil: %q", old)
	}
	if _, err := os.Stat(exe + ".new"); !errors.Is(err, os.ErrNotExist) {
		t.Error(".new kalıntısı kaldı")
	}
	var m marker
	if err := readJSON(exe+".update", &m); err != nil || m.From != "1.3.0" || m.To != "1.3.1" || m.Starts != 0 {
		t.Fatalf("işaret: %+v %v", m, err)
	}

	// Yeni sürüm başladı, panele ulaştı: işaret ve .old temizlenir.
	nu := &Updater{Exe: exe, Version: "1.3.1", OS: "linux", Arch: "amd64"}
	rep, err := nu.Startup()
	if err != nil || rep != nil {
		t.Fatalf("ilk başlatma: %v %+v", err, rep)
	}
	nu.Confirm()
	for _, f := range []string{".update", ".old", ".update-failed"} {
		if _, err := os.Stat(exe + f); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s silinmedi", f)
		}
	}
}

func TestInstallRefusals(t *testing.T) {
	pub, priv := newKey(t)
	useTestKey(t, pub)
	body := []byte("yeni program")
	ctx := context.Background()
	unchanged := func(t *testing.T, exe string) {
		t.Helper()
		got, _ := os.ReadFile(exe)
		if !strings.HasPrefix(string(got), "eski program") {
			t.Fatalf("program değişti: %q", got)
		}
		if _, err := os.Stat(exe + ".new"); !errors.Is(err, os.ErrNotExist) {
			t.Error(".new kalıntısı kaldı")
		}
	}

	t.Run("bozuk imza", func(t *testing.T) {
		u, exe := fakeAgent(t, "1.3.0")
		o := offerFor(t, priv, "1.3.1", body)
		o.Sig = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 64))
		if err := u.Install(ctx, o, serve(body)); err == nil || !strings.Contains(err.Error(), "imza") {
			t.Fatalf("hata: %v", err)
		}
		unchanged(t, exe)
	})
	t.Run("yanlış anahtar", func(t *testing.T) {
		_, other := newKey(t)
		u, exe := fakeAgent(t, "1.3.0")
		if err := u.Install(ctx, offerFor(t, other, "1.3.1", body), serve(body)); err == nil {
			t.Fatal("başka anahtarla imzalı teklif kabul edildi")
		}
		unchanged(t, exe)
	})
	t.Run("değiştirilmiş program", func(t *testing.T) {
		u, exe := fakeAgent(t, "1.3.0")
		// İmza doğru programa ait; panel başka bir şey gönderiyor.
		err := u.Install(ctx, offerFor(t, priv, "1.3.1", body), serve([]byte("zararli")))
		if err == nil || !strings.Contains(err.Error(), "SHA-256") {
			t.Fatalf("hata: %v", err)
		}
		unchanged(t, exe)
	})
	t.Run("sürüm düşürme", func(t *testing.T) {
		u, exe := fakeAgent(t, "1.3.1")
		if err := u.Install(ctx, offerFor(t, priv, "1.3.0", body), serve(body)); err == nil {
			t.Fatal("eski sürüm kabul edildi")
		}
		if err := u.Install(ctx, offerFor(t, priv, "1.3.1", body), serve(body)); err == nil {
			t.Fatal("aynı sürüm kabul edildi")
		}
		unchanged(t, exe)
	})
	t.Run("commit özeti derlemesi", func(t *testing.T) {
		u, exe := fakeAgent(t, "977e923")
		if err := u.Install(ctx, offerFor(t, priv, "1.3.1", body), serve(body)); err == nil {
			t.Fatal("commit derlemesi güncellendi")
		}
		u, exe = fakeAgent(t, "1.3.0")
		if err := u.Install(ctx, offerFor(t, priv, "977e923", body), serve(body)); err == nil {
			t.Fatal("commit derlemesine güncellendi")
		}
		unchanged(t, exe)
	})
	t.Run("başka platform", func(t *testing.T) {
		u, exe := fakeAgent(t, "1.3.0")
		o := offerFor(t, priv, "1.3.1", body)
		u.Arch = "arm64"
		if err := u.Install(ctx, o, serve(body)); err == nil {
			t.Fatal("başka mimarinin programı kabul edildi")
		}
		unchanged(t, exe)
	})
	t.Run("yabancı adres", func(t *testing.T) {
		u, exe := fakeAgent(t, "1.3.0")
		o := offerFor(t, priv, "1.3.1", body)
		o.URL = "https://kotu.example/uptime"
		if err := u.Install(ctx, o, serve(body)); err == nil {
			t.Fatal("yabancı indirme adresi kabul edildi")
		}
		unchanged(t, exe)
	})
	t.Run("boyut sınırı", func(t *testing.T) {
		u, exe := fakeAgent(t, "1.3.0")
		u.MaxSize = 8
		if err := u.Install(ctx, offerFor(t, priv, "1.3.1", body), serve(body)); err == nil || !strings.Contains(err.Error(), "sınır") {
			t.Fatalf("hata: %v", err)
		}
		unchanged(t, exe)
	})
	t.Run("indirme hatası", func(t *testing.T) {
		u, exe := fakeAgent(t, "1.3.0")
		fetch := func(context.Context, string, io.Writer) error { return errors.New("ağ koptu") }
		if err := u.Install(ctx, offerFor(t, priv, "1.3.1", body), fetch); err == nil {
			t.Fatal("hata bekleniyordu")
		}
		unchanged(t, exe)
	})
	t.Run("kapalı", func(t *testing.T) {
		u, exe := fakeAgent(t, "1.3.0")
		u.Disabled = "off"
		if err := u.Install(ctx, offerFor(t, priv, "1.3.1", body), serve(body)); err == nil {
			t.Fatal("kapalı güncelleyici kurdu")
		}
		if u.Platform() != "linux/amd64;off" {
			t.Errorf("platform %q", u.Platform())
		}
		unchanged(t, exe)
	})
}

func TestStartupRollback(t *testing.T) {
	pub, priv := newKey(t)
	useTestKey(t, pub)
	u, exe := fakeAgent(t, "1.3.0")
	newBody := []byte("yeni program 1.3.1")
	if err := u.Install(context.Background(), offerFor(t, priv, "1.3.1", newBody), serve(newBody)); err != nil {
		t.Fatal(err)
	}
	// Yeni sürüm üç kez başlar ama Confirm'e (panele) hiç ulaşamaz.
	nu := &Updater{Exe: exe, Version: "1.3.1", OS: "linux", Arch: "amd64"}
	for i := 1; i <= maxStarts; i++ {
		rep, err := nu.Startup()
		if err != nil || rep != nil {
			t.Fatalf("başlatma %d: %v %+v", i, err, rep)
		}
	}
	// Dördüncü başlatma: geri dönüş.
	if _, err := nu.Startup(); !errors.Is(err, ErrRestart) {
		t.Fatalf("geri dönüş bekleniyordu: %v", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "eski program 1.3.0" {
		t.Fatalf("eski program geri gelmedi: %q", got)
	}
	failed, _ := os.ReadFile(exe + ".failed")
	if !bytes.Equal(failed, newBody) {
		t.Error("çalışamayan program .failed olarak saklanmadı")
	}
	// Eski program başlar: panele bildirilecek not vardır; Confirm siler.
	rep, err := u.Startup()
	if err != nil || rep == nil || rep.Status != StatusRolledBack || rep.Version != "1.3.1" || !strings.Contains(rep.Error, "3 kez") {
		t.Fatalf("rapor: %+v %v", rep, err)
	}
	// İkinci başlatmada da (henüz bildirilmediyse) not durur.
	if rep2, _ := u.Startup(); rep2 == nil || rep2.Status != StatusRolledBack {
		t.Fatalf("not silindi: %+v", rep2)
	}
	u.Confirm()
	if rep3, _ := u.Startup(); rep3 != nil {
		t.Fatalf("not Confirm sonrası kaldı: %+v", rep3)
	}
}

func TestStartupStaleMarkers(t *testing.T) {
	u, exe := fakeAgent(t, "1.3.0")
	// Eski sürüm çalışıyor ama işaret yeni sürüme geçildiğini söylüyor:
	// başarısızlık notu yazılır ve bildirilir.
	writeJSON(exe+".update", marker{From: "1.3.0", To: "1.3.1"})
	os.WriteFile(exe+".new", []byte("kalinti"), 0o600)
	rep, err := u.Startup()
	if err != nil || rep == nil || rep.Status != StatusFailed || rep.Version != "1.3.1" {
		t.Fatalf("rapor: %+v %v", rep, err)
	}
	if _, err := os.Stat(exe + ".new"); !errors.Is(err, os.ErrNotExist) {
		t.Error(".new kalıntısı silinmedi")
	}
	u.Confirm()
	// İlgisiz işaret: sessizce silinir.
	writeJSON(exe+".update", marker{From: "1.0.0", To: "1.1.0"})
	if rep, err := u.Startup(); err != nil || rep != nil {
		t.Fatalf("ilgisiz işaret: %+v %v", rep, err)
	}
	if _, err := os.Stat(exe + ".update"); !errors.Is(err, os.ErrNotExist) {
		t.Error("ilgisiz işaret silinmedi")
	}
	// Updater yok: hiçbir şey yapılmaz.
	var nilU *Updater
	if rep, err := nilU.Startup(); rep != nil || err != nil {
		t.Fatal("nil güncelleyici")
	}
	nilU.Confirm()
}

func TestNewDetectsReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root her dizine yazabilir")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "uptime")
	os.WriteFile(exe, []byte("x"), 0o755)
	os.Chmod(dir, 0o555)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	u := &Updater{Exe: exe, Version: "1.3.0", OS: "linux", Arch: "amd64"}
	if err := u.checkWritable(); err == nil {
		t.Fatal("salt okunur dizin yazılabilir sayıldı")
	}
}

func TestKeyPEMRoundTrip(t *testing.T) {
	_, priv := newKey(t)
	pemData, err := KeyToPEM(priv)
	if err != nil {
		t.Fatal(err)
	}
	back, err := KeyFromPEM(pemData)
	if err != nil || !back.Equal(priv) {
		t.Fatalf("geri okuma: %v", err)
	}
	if _, err := KeyFromPEM([]byte("-----BEGIN RSA PRIVATE KEY-----\nAA==\n-----END RSA PRIVATE KEY-----\n")); err == nil {
		t.Error("RSA PEM kabul edildi")
	}
}
