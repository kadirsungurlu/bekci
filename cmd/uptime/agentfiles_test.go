package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEnvFileRoundTrip(t *testing.T) {
	kv := map[string]string{"PROBE_SERVER": "https://uptime.kadir.app", "PROBE_TOKEN": "upr_abc", "ADDR": "-"}
	text := formatEnvFile(kv)
	if !strings.HasPrefix(text, "# ") || !strings.Contains(text, "PROBE_TOKEN=upr_abc\r\n") {
		t.Fatalf("biçim: %q", text)
	}
	if got := parseEnvFile(text); !reflect.DeepEqual(got, kv) {
		t.Errorf("geri okuma %v", got)
	}
	// Elle düzenlenmiş dosya: BOM, boşluk, tırnak, yorum, bozuk satır.
	got := parseEnvFile("\ufeffPROBE_SERVER = \"https://x\"\n# yorum\n\nbozuk satır\n=değer\nMETRICS='0'\r\nLOG_LEVEL=debug")
	want := map[string]string{"PROBE_SERVER": "https://x", "METRICS": "0", "LOG_LEVEL": "debug"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("elle düzenlenmiş: %v", got)
	}
}

func TestMergeAgentEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	// İlk kurulum: sağlık uç noktası kapalı gelir.
	got, err := mergeAgentEnv(map[string]string{}, env(map[string]string{"PROBE_SERVER": "https://a", "PROBE_TOKEN": "upr_1", "PATH": `C:\x`}))
	if err != nil || !reflect.DeepEqual(got, map[string]string{"PROBE_SERVER": "https://a", "PROBE_TOKEN": "upr_1", "ADDR": "-"}) {
		t.Fatalf("ilk kurulum %v %v", got, err)
	}
	// Güncelleme: verilen değer eskisinin yerine geçer, verilmeyen korunur.
	cur := map[string]string{"PROBE_SERVER": "https://a", "PROBE_TOKEN": "upr_1", "ADDR": ":9090", "METRICS": "0"}
	got, err = mergeAgentEnv(cur, env(map[string]string{"PROBE_TOKEN": "upr_2"}))
	if err != nil || got["PROBE_TOKEN"] != "upr_2" || got["ADDR"] != ":9090" || got["METRICS"] != "0" || cur["PROBE_TOKEN"] != "upr_1" {
		t.Errorf("güncelleme %v %v", got, err)
	}
	// http sunucu: Windows kurulum komutu PROBE_ALLOW_INSECURE'ı ortamla verir.
	got, err = mergeAgentEnv(map[string]string{}, env(map[string]string{"PROBE_SERVER": "http://a", "PROBE_TOKEN": "upr_1", "PROBE_ALLOW_INSECURE": "1"}))
	if err != nil || got["PROBE_ALLOW_INSECURE"] != "1" {
		t.Errorf("PROBE_ALLOW_INSECURE aktarılmalı: %v %v", got, err)
	}
	if _, err := mergeAgentEnv(map[string]string{}, env(map[string]string{"PROBE_SERVER": "https://a"})); err == nil {
		t.Error("token'sız kurulum hata vermeli")
	}
	if _, err := mergeAgentEnv(map[string]string{}, env(map[string]string{"PROBE_SERVER": "https://a", "PROBE_TOKEN": "upr_1\nADDR=:1"})); err == nil {
		t.Error("satır sonu içeren değer reddedilmeli")
	}
}

func TestRotatingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "alt", "agent.log")
	r, err := openRotatingFile(p, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"12345\n", "6789\n", "abcdef\n", "gh\n"} {
		if _, err := r.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	r.Close()
	cur, _ := os.ReadFile(p)
	old, _ := os.ReadFile(p + ".1")
	if string(cur) != "abcdef\ngh\n" || string(old) != "6789\n" {
		t.Errorf("döndürme: güncel %q, eski %q", cur, old)
	}
	// Yeniden açılınca mevcut boyuttan devam eder.
	r, _ = openRotatingFile(p, 10)
	r.Write([]byte("12345678\n"))
	r.Close()
	cur, _ = os.ReadFile(p)
	old, _ = os.ReadFile(p + ".1")
	if string(cur) != "12345678\n" || string(old) != "abcdef\ngh\n" {
		t.Errorf("yeniden açma: güncel %q, eski %q", cur, old)
	}
}
