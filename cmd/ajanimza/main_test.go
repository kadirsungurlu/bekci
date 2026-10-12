package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/agentupdate"
)

func TestKeygenSignVerify(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "k.pem")
	var out bytes.Buffer
	if err := run([]string{"anahtar-uret", "-cikti", key}, &out); err != nil {
		t.Fatal(err)
	}
	pub := strings.TrimSpace(out.String())
	if _, err := agentupdate.ParsePublicKey(pub); err != nil {
		t.Fatalf("açık anahtar çıktısı: %q %v", pub, err)
	}
	if st, _ := os.Stat(key); st.Mode().Perm() != 0o600 {
		t.Errorf("anahtar izni %v", st.Mode().Perm())
	}
	// Var olan anahtarın üstüne yazılmaz.
	if err := run([]string{"anahtar-uret", "-cikti", key}, &out); err == nil {
		t.Error("anahtar dosyası ezildi")
	}

	bin := filepath.Join(dir, "uptime")
	win := filepath.Join(dir, "uptime-windows-amd64.exe")
	os.WriteFile(bin, []byte("linux programi"), 0o755)
	os.WriteFile(win, []byte("windows programi"), 0o755)
	sigs := filepath.Join(dir, "agents")
	out.Reset()
	if err := run([]string{"imzala", "-anahtar", key, "-surum", "1.3.1", "-cikti", sigs,
		"linux/amd64=" + bin, "windows/amd64=" + win}, &out); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"uptime-linux-amd64.sig", "uptime-windows-amd64.sig"} {
		if _, err := os.Stat(filepath.Join(sigs, f)); err != nil {
			t.Errorf("%s yazılmadı", f)
		}
	}
	verifyArgs := []string{"dogrula", "-surum", "1.3.1", "-cikti", sigs, "-acik-anahtar", pub, "linux/amd64=" + bin, "windows/amd64=" + win}
	if err := run(verifyArgs, &out); err != nil {
		t.Fatalf("doğrulama: %v\n%s", err, out.String())
	}
	// Gömülü (proje) anahtarıyla doğrulanamaz: anahtar farklı.
	if err := run([]string{"dogrula", "-surum", "1.3.1", "-cikti", sigs, "linux/amd64=" + bin}, &out); err == nil {
		t.Error("başka anahtarın imzası gömülü anahtarla geçti")
	}
	// Sürüm uyuşmazlığı ve değiştirilmiş program.
	if err := run([]string{"dogrula", "-surum", "1.3.2", "-cikti", sigs, "-acik-anahtar", pub, "linux/amd64=" + bin}, &out); err == nil {
		t.Error("yanlış sürüm geçti")
	}
	os.WriteFile(bin, []byte("degistirildi"), 0o755)
	if err := run(verifyArgs, &out); err == nil {
		t.Error("değiştirilmiş program geçti")
	}
	// Eksik argümanlar.
	if err := run([]string{"imzala", "-anahtar", key}, &out); err == nil {
		t.Error("eksik bayraklar kabul edildi")
	}
	if err := run([]string{"imzala", "-anahtar", key, "-surum", "1", "-cikti", sigs, "bozuk"}, &out); err == nil {
		t.Error("bozuk hedef kabul edildi")
	}
	if err := run(nil, &out); err == nil {
		t.Error("komutsuz çağrı kabul edildi")
	}
}
