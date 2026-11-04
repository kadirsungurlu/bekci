package betik

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func key(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestSignVerify(t *testing.T) {
	pub, priv := key(t)
	other, _ := key(t)
	raw, err := Sign(priv, KindManifest, []byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	e, err := ParseEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Verify(KindManifest, pub) {
		t.Fatal("geçerli imza reddedildi")
	}
	if e.Verify(KindTicket, pub) {
		t.Fatal("başka tür kabul edildi")
	}
	if e.Verify(KindManifest, other) {
		t.Fatal("başka anahtar kabul edildi")
	}
	if !e.VerifyAny(KindManifest, []ed25519.PublicKey{other, pub}) {
		t.Fatal("küme içindeki anahtar bulunamadı")
	}
	e.Payload[0] ^= 1
	if e.Verify(KindManifest, pub) {
		t.Fatal("değiştirilmiş payload kabul edildi")
	}
}

func TestEnvelopeStrict(t *testing.T) {
	_, priv := key(t)
	raw, _ := Sign(priv, KindManifest, []byte(`{}`))
	var w map[string]string
	json.Unmarshal(raw, &w)
	for name, in := range map[string]string{
		"bilinmeyen alan": strings.Replace(string(raw), `{"key"`, `{"x":1,"key"`, 1),
		"yinelenen":       strings.Replace(string(raw), `{"key"`, `{"key":"`+w["key"]+`","key"`, 1),
		"büyük harf":      strings.Replace(string(raw), `"key"`, `"Key"`, 1),
		"sonda veri":      string(raw) + "{}",
		"büyük zarf":      `{"key":"` + strings.Repeat("a", MaxEnvelope) + `"}`,
		"kısa imza":       `{"key":"` + w["key"] + `","payload":"e30=","sig":"AAAA"}`,
		"boş":             "",
	} {
		if _, err := ParseEnvelope([]byte(in)); err == nil {
			t.Errorf("%s: kabul edildi", name)
		}
	}
}

func manifest() *Manifest {
	mn, mx := int64(7), int64(90)
	return &Manifest{
		Schema: 1, ID: "journal-temizle", Version: 1, SHA256: Digest([]byte("x")), Interpreter: "sh", RunAs: "root",
		Platforms: []string{"linux"}, Approval: "none", Groups: []string{"web"},
		Params:     []Param{{Name: "gun", Type: "int", Min: &mn, Max: &mx}, {Name: "ad", Type: "name"}, {Name: "tur", Type: "enum", Values: []string{"a", "b"}}, {Name: "zorla", Type: "bool"}},
		TimeoutSec: 60, OutputMax: 1024, MaxPerDay: 4, NotBefore: 1000, NotAfter: 2000,
	}
}

func TestManifestLimits(t *testing.T) {
	if err := manifest().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, f := range map[string]func(*Manifest){
		"ömür":        func(m *Manifest) { m.NotAfter = m.NotBefore + MaxManifestLife + 1 },
		"timeout":     func(m *Manifest) { m.TimeoutSec = MaxTimeoutSec + 1 },
		"çıktı":       func(m *Manifest) { m.OutputMax = MaxOutput + 1 },
		"yorumlayıcı": func(m *Manifest) { m.Interpreter = "bash" },
		"run_as":      func(m *Manifest) { m.RunAs = "www-data" },
		"platform":    func(m *Manifest) { m.Platforms = []string{"windows"} },
		"onay":        func(m *Manifest) { m.Approval = "hic" },
		"grupsuz":     func(m *Manifest) { m.Groups = nil },
		"id":          func(m *Manifest) { m.ID = "../x" },
		"joker id":    func(m *Manifest) { m.ID = "*" },
		"int sınırsız": func(m *Manifest) {
			m.Params[0].Max = nil
		},
		"serbest metin": func(m *Manifest) { m.Params[1].Type = "text" },
		"komut türü":    func(m *Manifest) { m.Params[1].Type = "command" },
	} {
		m := manifest()
		f(m)
		if m.Validate() == nil {
			t.Errorf("%s: kabul edildi", name)
		}
	}
}

func TestParseManifestStrict(t *testing.T) {
	good, _ := json.Marshal(manifest())
	if _, err := ParseManifest(good); err != nil {
		t.Fatal(err)
	}
	s := string(good)
	if _, err := ParseManifest([]byte(strings.Replace(s, `"schema":1`, `"schema":2`, 1))); !errors.Is(err, ErrSchema) {
		t.Fatalf("schema: %v", err)
	}
	for name, in := range map[string]string{
		"bilinmeyen alan": strings.Replace(s, `"schema":1`, `"schema":1,"extra":1`, 1),
		"yinelenen":       strings.Replace(s, `"schema":1`, `"schema":1,"approval":"none"`, 1),
		"büyük harf":      strings.Replace(s, `"approval"`, `"Approval"`, 1),
		"sonda veri":      s + " 1",
	} {
		if _, err := ParseManifest([]byte(in)); err == nil {
			t.Errorf("%s: kabul edildi", name)
		}
	}
}

func TestParseParams(t *testing.T) {
	m := manifest()
	ok := `{"gun":30,"ad":"nginx.service","tur":"a","zorla":true}`
	env, err := ParseParams(m, []byte(ok))
	if err != nil {
		t.Fatal(err)
	}
	if env["BEKCI_P_GUN"] != "30" || env["BEKCI_P_AD"] != "nginx.service" || env["BEKCI_P_ZORLA"] != "true" {
		t.Fatalf("%v", env)
	}
	bad := map[string]string{
		"eksik":       `{"gun":30}`,
		"fazla":       `{"gun":30,"ad":"a","tur":"a","zorla":true,"x":1}`,
		"aralık":      `{"gun":3,"ad":"a","tur":"a","zorla":true}`,
		"ondalık":     `{"gun":30.5,"ad":"a","tur":"a","zorla":true}`,
		"üs":          `{"gun":3e1,"ad":"a","tur":"a","zorla":true}`,
		"tire":        `{"gun":30,"ad":"-rf","tur":"a","zorla":true}`,
		"nul":         `{"gun":30,"ad":"a\u0000b","tur":"a","zorla":true}`,
		"satır sonu":  `{"gun":30,"ad":"a\nb","tur":"a","zorla":true}`,
		"komut":       `{"gun":30,"ad":"$(id)","tur":"a","zorla":true}`,
		"ters tırnak": "{\"gun\":30,\"ad\":\"`id`\",\"tur\":\"a\",\"zorla\":true}",
		"boşluk":      `{"gun":30,"ad":"a b","tur":"a","zorla":true}`,
		"enum dışı":   `{"gun":30,"ad":"a","tur":"c","zorla":true}`,
		"bool metin":  `{"gun":30,"ad":"a","tur":"a","zorla":"true"}`,
		"yinelenen":   `{"gun":30,"gun":31,"ad":"a","tur":"a","zorla":true}`,
		"dizi":        `[1]`,
		"null":        `null`,
		"boş":         ``,
	}
	for name, in := range bad {
		if _, err := ParseParams(m, []byte(in)); err == nil {
			t.Errorf("%s: kabul edildi", name)
		}
	}
}

func TestApproval(t *testing.T) {
	if MaxApproval("none", "signed") != "signed" || MaxApproval("panel", "none") != "panel" || MaxApproval("x", "none") != "signed" {
		t.Fatal("MaxApproval")
	}
}

func TestRunJobBase64(t *testing.T) {
	var j RunJob
	in := `{"run_id":"a","manifest":{"key":"k"},"body":"` + base64.StdEncoding.EncodeToString([]byte("gövde")) + `","params":"e30="}`
	if err := json.Unmarshal([]byte(in), &j); err != nil {
		t.Fatal(err)
	}
	if string(j.Body) != "gövde" || string(j.Params) != "{}" {
		t.Fatalf("%q %q", j.Body, j.Params)
	}
}
