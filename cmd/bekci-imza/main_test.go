package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betik"
	"github.com/kadirsungurlu/bekci/internal/betikkayit"
)

const testParola = "dogru-at-pil-zimba"

func TestMain(m *testing.M) {
	scryptN = scryptNMin
	os.Exit(m.Run())
}

type sonuc struct {
	out, err string
	hata     error
}

func calistir(stdin string, now time.Time, args ...string) sonuc {
	var o, e bytes.Buffer
	a := &app{in: bufio.NewReader(strings.NewReader(stdin)), out: &o, err: &e, now: func() time.Time { return now },
		shellcheck: "shellcheck"}
	err := a.run(args)
	return sonuc{o.String(), e.String(), err}
}

func anahtarUret(t *testing.T, yuva string) (string, ed25519.PublicKey) {
	t.Helper()
	path := filepath.Join(t.TempDir(), yuva+".anahtar")
	r := calistir(testParola+"\n", time.Now(), "anahtar-uret", "--yuva", yuva, "--cikti", path)
	if r.hata != nil {
		t.Fatal(r.hata)
	}
	m := regexp.MustCompile(`acik_anahtar: (\S+)`).FindStringSubmatch(r.out)
	if m == nil {
		t.Fatal(r.out)
	}
	pub, err := betik.ParsePublicKey(m[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.out, betik.KeyID(pub)) {
		t.Fatal("anahtar kimliği basılmadı")
	}
	return path, pub
}

func TestAnahtar(t *testing.T) {
	path, pub := anahtarUret(t, slotYayin)
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatal(fi.Mode(), err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), testParola) {
		t.Fatal("parola dosyada")
	}
	a := &app{err: &bytes.Buffer{}}
	k, err := a.anahtarOku(path, slotYayin)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := k.ac([]byte(testParola))
	if err != nil || !pub.Equal(priv.Public()) {
		t.Fatal(err)
	}
	if _, err := k.ac([]byte("yanlis-parola-1")); err == nil {
		t.Fatal("yanlış parola geçti")
	}
	if _, err := a.anahtarOku(path, slotOnay); err == nil {
		t.Fatal("yuva denetlenmedi")
	}
	// Yuva dosyada değiştirilirse parola doğru olsa da açılmaz.
	k.Slot = slotOnay
	if _, err := k.ac([]byte(testParola)); err == nil {
		t.Fatal("yuva ek doğrulanan veri değil")
	}
	if r := calistir(testParola+"\n", time.Now(), "anahtar-uret", "--yuva", "yayin", "--cikti", path); r.hata == nil {
		t.Fatal("var olan anahtarın üstüne yazıldı")
	}
	short := filepath.Join(t.TempDir(), "k")
	if r := calistir("kisa\n", time.Now(), "anahtar-uret", "--yuva", "yayin", "--cikti", short); r.hata == nil {
		t.Fatal("kısa parola geçti")
	}
	if _, err := os.Stat(short); err == nil {
		t.Fatal("başarısız üretim dosya bıraktı")
	}
}

const ornekBildirim = `{"schema":1,"id":"ornek","version":3,"interpreter":"sh","run_as":"root","platforms":["linux"],
"approval":"none","groups":["web"],"params":[{"name":"gun","type":"int","min":7,"max":90}],
"timeout_sec":60,"output_max":1024,"min_interval_sec":60,"max_per_day":2,"jitter_sec":0}`

func imzalaDosya(t *testing.T, key, man, govde string, now time.Time) (string, sonuc) {
	t.Helper()
	dir := t.TempDir()
	mp, gp := filepath.Join(dir, "m.json"), filepath.Join(dir, "g.sh")
	os.WriteFile(mp, []byte(man), 0o644)
	os.WriteFile(gp, []byte(govde), 0o644)
	r := calistir(testParola+"\n", now, "imzala", "--anahtar", key, "--bildirim", mp, "--govde", gp, "--cikti", dir)
	return dir, r
}

func okuImzali(t *testing.T, path string, pub ed25519.PublicKey) (*betik.Manifest, []byte) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v imzaliBetik
	if err := betik.DecodeStrict(raw, &v); err != nil {
		t.Fatal(err)
	}
	env, err := betik.ParseEnvelope(v.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !env.Verify(betik.KindManifest, pub) {
		t.Fatal("imza tutmuyor")
	}
	if env.Verify(betik.KindTicket, pub) {
		t.Fatal("tür ayrımı yok")
	}
	m, err := betik.ParseManifest(env.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if m.SHA256 != betik.Digest(v.Body) {
		t.Fatal("sha256 gövdeyle tutmuyor")
	}
	return m, v.Body
}

func TestImzala(t *testing.T) {
	key, pub := anahtarUret(t, slotYayin)
	now := time.Unix(1_800_000_000, 0)
	govde := "#!/bin/sh\necho \"$BEKCI_P_GUN\"\n"
	dir, r := imzalaDosya(t, key, ornekBildirim, govde, now)
	if r.hata != nil {
		t.Fatal(r.hata, r.err)
	}
	m, body := okuImzali(t, filepath.Join(dir, "ornek-3.betik.json"), pub)
	if string(body) != govde || m.ID != "ornek" || m.Version != 3 {
		t.Fatal(m)
	}
	if m.NotBefore != now.Unix() || m.NotAfter != now.Add(90*24*time.Hour).Unix() {
		t.Fatal(m.NotBefore, m.NotAfter)
	}
	// Verilen tarih korunur, 180 günü aşan red.
	man := strings.Replace(ornekBildirim, `"jitter_sec":0`, `"jitter_sec":0,"not_before":1800000100,"not_after":1800000200`, 1)
	dir, r = imzalaDosya(t, key, man, govde, now)
	if r.hata != nil {
		t.Fatal(r.hata)
	}
	if m, _ := okuImzali(t, filepath.Join(dir, "ornek-3.betik.json"), pub); m.NotAfter != 1800000200 {
		t.Fatal(m.NotAfter)
	}
	man = strings.Replace(ornekBildirim, `"jitter_sec":0`, `"jitter_sec":0,"not_before":1800000000,"not_after":1900000000`, 1)
	if _, r = imzalaDosya(t, key, man, govde, now); r.hata == nil {
		t.Fatal("uzun ömür geçti")
	}
	// Bilinmeyen alan ve onay anahtarı reddedilir.
	if _, r = imzalaDosya(t, key, strings.Replace(ornekBildirim, `"id"`, `"fazla":1,"id"`, 1), govde, now); r.hata == nil {
		t.Fatal("bilinmeyen alan geçti")
	}
	onay, _ := anahtarUret(t, slotOnay)
	if _, r = imzalaDosya(t, onay, ornekBildirim, govde, now); r.hata == nil {
		t.Fatal("onay anahtarı bildirim imzaladı")
	}
}

func TestGovdeDenetle(t *testing.T) {
	m := &betik.Manifest{Params: []betik.Param{{Name: "gun", Type: "bool"}}}
	kotu := map[string]string{
		"tırnaksız":      "echo $BEKCI_P_GUN\n",
		"süslü":          "echo ${BEKCI_P_GUN}\n",
		"varsayılanlı":   "x=${BEKCI_P_GUN:-1}\n",
		"ikamede":        "echo \"$(rm -rf $BEKCI_P_GUN)\"\n",
		"ters tırnakta":  "echo \"`rm $BEKCI_P_GUN`\"\n",
		"tek tırnakta":   "echo '$BEKCI_P_GUN'\n",
		"tanımsız":       "echo \"$BEKCI_P_YOK\"\n",
		"eval":           "eval \"$x\"\n",
		"noktalı eval":   "true; eval \"$x\"\n",
		"kaçışlı eval":   "\\eval \"$x\"\n",
		"ikamede eval":   "y=$(eval \"$x\")\n",
		"sh -c":          "sh -c \"$x\"\n",
		"tam yol sh -c":  "/bin/sh -ec \"rm $x\"\n",
		"heredoc":        "cat <<X\n$BEKCI_P_GUN\nX\n",
		"kapanmamış":     "echo \"a\n",
		"tek kapanmamış": "echo 'a\n",
		"boş":            "",
		"NUL":            "echo a\x00\n",
		"büyük":          strings.Repeat("#", betik.MaxBody+1),
	}
	for ad, g := range kotu {
		if err := govdeDenetle([]byte(g), m); err == nil {
			t.Errorf("%s: geçti", ad)
		}
	}
	iyi := []string{
		"echo \"$BEKCI_P_GUN\"\n",
		"echo \"${BEKCI_P_GUN}d\"\n",
		"# eval burada yorum\necho \"a eval b\"\n",
		"echo 'sh -c $x'\n",
		"x=\"$(echo \"$BEKCI_P_GUN\")\"\n",
		"echo \\$BEKCI_P_GUN\n",
		"case \"$x\" in\na) echo \"$BEKCI_P_GUN\" ;;\nesac\n",
		"medieval=1\n",
		"echo $#\n",
	}
	for _, g := range iyi {
		if err := govdeDenetle([]byte(g), m); err != nil {
			t.Errorf("%q: %v", g, err)
		}
	}
}

func TestShellcheck(t *testing.T) {
	var e bytes.Buffer
	a := &app{err: &e, shellcheck: "shellcheck-yok-xyz"}
	if err := a.shellcheckCalistir([]byte("echo a\n")); err != nil || !strings.Contains(e.String(), "UYARI") {
		t.Fatal(err, e.String())
	}
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck yok")
	}
	a.shellcheck = "shellcheck"
	if err := a.shellcheckCalistir([]byte("#!/bin/sh\ncd /tmp\n")); err == nil {
		t.Fatal("shellcheck bulgusu durdurmadı")
	}
}

func TestIptal(t *testing.T) {
	key, pub := anahtarUret(t, slotYayin)
	r := calistir(testParola+"\n", time.Now(), "iptal", "--anahtar", key, "--id", "ornek", "--alt-surum", "4")
	if r.hata != nil {
		t.Fatal(r.hata)
	}
	env, err := betik.ParseEnvelope(bytes.TrimSpace([]byte(r.out)))
	if err != nil || !env.Verify(betik.KindRevoke, pub) {
		t.Fatal(err)
	}
	if v, err := betik.ParseRevoke(env.Payload); err != nil || v.BelowVersion != 4 || v.ID != "ornek" {
		t.Fatal(v, err)
	}
	if r := calistir(testParola+"\n", time.Now(), "iptal", "--anahtar", key, "--id", "ornek", "--alt-surum", "0"); r.hata == nil {
		t.Fatal("alt sürüm 0 geçti")
	}
}

func biletIstegi(t *testing.T, params string, mutate func(*biletIstek)) string {
	t.Helper()
	q := biletIstek{RunID: strings.Repeat("a", 32), HostKey: betik.Digest([]byte("host")), ScriptID: "servis-yeniden-web", Version: 2,
		ManifestSHA256: betik.Digest([]byte("man")), ParamsSHA256: betik.Digest([]byte(params)), Params: params}
	if mutate != nil {
		mutate(&q)
	}
	b, _ := json.Marshal(q)
	p := filepath.Join(t.TempDir(), "istek.json")
	os.WriteFile(p, b, 0o644)
	return p
}

func TestBilet(t *testing.T) {
	key, pub := anahtarUret(t, slotOnay)
	now := time.Unix(1_800_000_000, 0)
	params := `{"servis":"caddy"}`
	host := betik.Digest([]byte("host"))
	pins := filepath.Join(t.TempDir(), "igneler.json")
	os.WriteFile(pins, []byte(fmt.Sprintf(`{%q:"web-1"}`, host)), 0o600)

	req := biletIstegi(t, params, nil)
	r := calistir("evet\n"+testParola+"\n", now, "bilet", "--anahtar", key, "--istek", req, "--onaylayan", "kadir", "--igneler", pins, "--sure", "5m")
	if r.hata != nil {
		t.Fatal(r.hata, r.err)
	}
	for _, s := range []string{"servis-yeniden-web", `servis = "caddy"`, `"web-1"`} {
		if !strings.Contains(r.err, s) {
			t.Fatalf("ekranda %q yok: %s", s, r.err)
		}
	}
	if strings.Contains(r.err, "BİLİNMEYEN") {
		t.Fatal(r.err)
	}
	env, err := betik.ParseEnvelope(bytes.TrimSpace([]byte(r.out)))
	if err != nil || !env.Verify(betik.KindTicket, pub) {
		t.Fatal(err)
	}
	k, err := betik.ParseTicket(env.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if k.RunID != strings.Repeat("a", 32) || k.HostKey != host || k.ScriptID != "servis-yeniden-web" || k.Version != 2 ||
		k.ManifestSHA256 != betik.Digest([]byte("man")) || k.ParamsSHA256 != betik.Digest([]byte(params)) ||
		k.Approver != "kadir" || k.IssuedAt != now.Unix() || k.ExpiresAt != now.Add(5*time.Minute).Unix() {
		t.Fatalf("%+v", k)
	}

	// Bilinmeyen sunucu belirgin uyarı.
	r = calistir("evet\n"+testParola+"\n", now, "bilet", "--anahtar", key, "--istek", req, "--onaylayan", "kadir")
	if r.hata != nil || !strings.Contains(r.err, "BİLİNMEYEN") {
		t.Fatal(r.hata, r.err)
	}
	// "evet" yoksa imza yok.
	for _, ans := range []string{"hayir\n", "Evet\n", "\n", ""} {
		r = calistir(ans+testParola+"\n", now, "bilet", "--anahtar", key, "--istek", req, "--onaylayan", "kadir", "--igneler", pins)
		if r.hata == nil || r.out != "" {
			t.Fatalf("%q ile imzalandı", ans)
		}
	}
	// Özet tutmazsa ekran bile gelmez.
	bad := biletIstegi(t, params, func(q *biletIstek) { q.Params = `{"servis":"nginx"}` })
	r = calistir("evet\n"+testParola+"\n", now, "bilet", "--anahtar", key, "--istek", bad, "--onaylayan", "kadir")
	if r.hata == nil || r.out != "" || !strings.Contains(r.hata.Error(), "params_sha256") {
		t.Fatal(r.hata)
	}
	// Yinelenen anahtar: ekrandaki değer ajanın gördüğünden farklı olabilirdi.
	dup := biletIstegi(t, `{"servis":"caddy","servis":"nginx"}`, nil)
	if r = calistir("evet\n"+testParola+"\n", now, "bilet", "--anahtar", key, "--istek", dup, "--onaylayan", "kadir"); r.hata == nil {
		t.Fatal("yinelenen parametre geçti")
	}
	if r = calistir("evet\n"+testParola+"\n", now, "bilet", "--anahtar", key, "--istek", req, "--onaylayan", "kadir", "--sure", "16m"); r.hata == nil {
		t.Fatal("süre sınırı geçti")
	}
	if r = calistir("evet\nyanlis-parola-1\n", now, "bilet", "--anahtar", key, "--istek", req, "--onaylayan", "kadir"); r.hata == nil || r.out != "" {
		t.Fatal("yanlış parola geçti")
	}
	yayin, _ := anahtarUret(t, slotYayin)
	if r = calistir("evet\n"+testParola+"\n", now, "bilet", "--anahtar", yayin, "--istek", req, "--onaylayan", "kadir"); r.hata == nil {
		t.Fatal("yayın anahtarı bilet imzaladı")
	}
}

// ---- dogrula ----

type zincir struct {
	x    betikkayit.Export
	priv ed25519.PrivateKey
	prev string // makbuz önceki özeti
	rseq int64
	log  []string
}

func yeniZincir(t *testing.T, t0 int64) *zincir {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	z := &zincir{priv: priv, x: betikkayit.Export{Format: 1, ExportedAt: t0 + 10000}}
	z.olay(t0, "", "host_key_seen", fmt.Sprintf(`{"probe_id":1,"host_key":%q,"public_key":%q}`, betik.KeyID(pub), betik.EncodePublicKey(pub)))
	return z
}

func (z *zincir) olay(tm int64, run, kind, data string) {
	prev, seq := betikkayit.Genesis, int64(1)
	if n := len(z.x.Events); n > 0 {
		prev, seq = z.x.Events[n-1].Hash, z.x.Events[n-1].Seq+1
	}
	e := betikkayit.Event{Seq: seq, Time: tm, RunID: run, Kind: kind, Data: data, Prev: prev}
	e.Hash = betikkayit.Hash(prev, e)
	z.x.Events = append(z.x.Events, e)
}

// makbuz imzalı makbuz üretir, ajan kaydına ve (panele=true) zincire ekler.
func (z *zincir) makbuz(t *testing.T, tm int64, panele bool) {
	t.Helper()
	z.rseq++
	run := fmt.Sprintf("%032x", z.rseq)
	d := betik.Digest([]byte("x"))
	r := betik.Receipt{Schema: 1, Seq: z.rseq, Prev: z.prev, RunID: run, ScriptID: "ornek", Version: 1, ManifestSHA256: d,
		ParamsSHA256: d, StartedAt: tm, EndedAt: tm, Status: betik.StatusOK}
	p, _ := json.Marshal(&r)
	env, err := betik.Sign(z.priv, betik.KindReceipt, p)
	if err != nil {
		t.Fatal(err)
	}
	z.prev = betik.Digest(p)
	line, _ := json.Marshal(map[string]any{"t": tm, "kind": "receipt", "run_id": run, "receipt": json.RawMessage(env)})
	z.log = append(z.log, string(line))
	if panele {
		z.olay(tm, run, "receipt", fmt.Sprintf(`{"receipt":%s,"seq":%d}`, env, z.rseq))
	}
}

func (z *zincir) yaz(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	b, _ := json.Marshal(z.x)
	kp, lp := filepath.Join(dir, "kayit.json"), filepath.Join(dir, "kayit.log")
	os.WriteFile(kp, b, 0o644)
	os.WriteFile(lp, []byte(strings.Join(z.log, "\n")+"\n"), 0o640)
	return kp, lp
}

func TestDogrulaSaglam(t *testing.T) {
	const t0 = 1_800_000_000
	z := yeniZincir(t, t0)
	z.makbuz(t, t0+1, true)
	z.makbuz(t, t0+2, true)
	z.olay(t0+3, "", "anchor", "{}")
	kp, lp := z.yaz(t)
	bekle := filepath.Join(t.TempDir(), "bas.json")
	now := time.Unix(t0+3600, 0)
	r := calistir("", now, "dogrula", "--kayit", kp, "--ajan-kayit", lp, "--bekle", bekle, "--en-eski-kontrol", "25h")
	if r.hata != nil {
		t.Fatal(r.hata, r.out)
	}
	if !strings.Contains(r.out, "TAMAM") || !strings.Contains(r.out, "2 makbuz") || strings.Contains(r.out, "UYARI") {
		t.Fatal(r.out)
	}
	// Zincir ilerledi: önceki baş hâlâ içinde.
	z.makbuz(t, t0+4, true)
	kp, lp = z.yaz(t)
	if r = calistir("", now, "dogrula", "--kayit", kp, "--ajan-kayit", lp, "--bekle", bekle); r.hata != nil {
		t.Fatal(r.hata)
	}
}

func TestDogrulaDegistirilmis(t *testing.T) {
	const t0 = 1_800_000_000
	z := yeniZincir(t, t0)
	z.makbuz(t, t0+1, true)
	z.makbuz(t, t0+2, true)
	z.x.Events[1].Data = strings.Replace(z.x.Events[1].Data, `"seq":1`, `"seq":9`, 1)
	kp, _ := z.yaz(t)
	r := calistir("", time.Unix(t0, 0), "dogrula", "--kayit", kp)
	if r.hata == nil || !strings.Contains(r.hata.Error(), "ilk kırık nokta seq 2") {
		t.Fatal(r.hata)
	}
	// Özeti de yeniden hesaplanmış sahte makbuz: zincir sağlam, imza değil.
	s := yeniZincir(t, t0)
	s.makbuz(t, t0+1, true)
	_, baska, _ := ed25519.GenerateKey(rand.Reader)
	s.priv = baska
	s.makbuz(t, t0+2, true)
	kp, _ = s.yaz(t)
	if r = calistir("", time.Unix(t0, 0), "dogrula", "--kayit", kp); r.hata == nil {
		t.Fatal("sahte makbuz geçti: ", r.out)
	}
}

func TestDogrulaGeriAlinmis(t *testing.T) {
	const t0 = 1_800_000_000
	z := yeniZincir(t, t0)
	z.makbuz(t, t0+1, true)
	z.makbuz(t, t0+2, true)
	full, _ := z.yaz(t)
	bekle := filepath.Join(t.TempDir(), "bas.json")
	now := time.Unix(t0+100, 0)
	if r := calistir("", now, "dogrula", "--kayit", full, "--bekle", bekle); r.hata != nil {
		t.Fatal(r.hata)
	}
	onceki, _ := os.ReadFile(bekle)

	// Kırpılmış dışa aktarım kendi içinde sağlam, yalnız --bekle yakalar.
	z.x.Events = z.x.Events[:2]
	kp, _ := z.yaz(t)
	if r := calistir("", now, "dogrula", "--kayit", kp); r.hata != nil {
		t.Fatal(r.hata)
	}
	r := calistir("", now, "dogrula", "--kayit", kp, "--bekle", bekle)
	if r.hata == nil || !strings.Contains(r.hata.Error(), "geri alınmış") {
		t.Fatal(r.hata)
	}
	if b, _ := os.ReadFile(bekle); !bytes.Equal(b, onceki) {
		t.Fatal("başarısız denetim bekle dosyasını değiştirdi")
	}

	// Aynı uzunlukta başka zincir: çatal.
	c := yeniZincir(t, t0)
	c.makbuz(t, t0+1, true)
	c.makbuz(t, t0+2, true)
	kp, _ = c.yaz(t)
	r = calistir("", now, "dogrula", "--kayit", kp, "--bekle", bekle)
	if r.hata == nil || !strings.Contains(r.hata.Error(), "çatal") {
		t.Fatal(r.hata)
	}
}

func TestDogrulaCheckpoint(t *testing.T) {
	const t0 = 1_800_000_000
	z := yeniZincir(t, t0)
	z.makbuz(t, t0+1, true)
	created := int64(t0 + 500*86400)
	son := z.x.Events[len(z.x.Events)-1]
	z.x.Events = nil
	z.x.ExportedAt = created + 1
	z.x.Checkpoints = []betikkayit.Checkpoint{{Seq: son.Seq, Hash: son.Hash, Time: son.Time, Cutoff: created - saklamaSn, CreatedAt: created}}
	kp, _ := z.yaz(t)
	if r := calistir("", time.Unix(created, 0), "dogrula", "--kayit", kp); r.hata != nil {
		t.Fatal(r.hata)
	}
	z.x.Checkpoints[0].CreatedAt, z.x.Checkpoints[0].Cutoff = son.Time+86400, son.Time-1
	z.x.ExportedAt = son.Time + 2*86400
	kp, _ = z.yaz(t)
	r := calistir("", time.Unix(created, 0), "dogrula", "--kayit", kp)
	if r.hata == nil || !strings.Contains(r.hata.Error(), "saklama süresi dolmadan") {
		t.Fatal(r.hata)
	}
}

func TestDogrulaCapaYasi(t *testing.T) {
	const t0 = 1_800_000_000
	z := yeniZincir(t, t0)
	kp, _ := z.yaz(t)
	if r := calistir("", time.Unix(t0, 0), "dogrula", "--kayit", kp, "--en-eski-kontrol", "25h"); r.hata == nil {
		t.Fatal("çapasız geçti")
	}
	z.olay(t0+1, "", "anchor", "{}")
	kp, _ = z.yaz(t)
	if r := calistir("", time.Unix(t0+26*3600, 0), "dogrula", "--kayit", kp, "--en-eski-kontrol", "25h"); r.hata == nil {
		t.Fatal("eski çapa geçti")
	}
}

func TestDogrulaAjanKaydi(t *testing.T) {
	const t0 = 1_800_000_000
	now := time.Unix(t0+20000, 0)
	// Son makbuz panelde kırpılmış.
	z := yeniZincir(t, t0)
	z.makbuz(t, t0+1, true)
	z.makbuz(t, t0+2, false)
	kp, lp := z.yaz(t)
	r := calistir("", now, "dogrula", "--kayit", kp, "--ajan-kayit", lp)
	if r.hata == nil || !strings.Contains(r.hata.Error(), "kırpılmış") {
		t.Fatal(r.hata)
	}
	// Ortadaki makbuz kuyruktan düşmüş: uyarı, kırık değil.
	z = yeniZincir(t, t0)
	z.makbuz(t, t0+1, true)
	z.makbuz(t, t0+2, false)
	z.makbuz(t, t0+3, true)
	kp, lp = z.yaz(t)
	r = calistir("", now, "dogrula", "--kayit", kp, "--ajan-kayit", lp)
	if r.hata != nil || !strings.Contains(r.out, "UYARI: makbuz seq 2") {
		t.Fatal(r.hata, r.out)
	}
	// Ajan kaydından satır silinmiş.
	z.log = append(z.log[:1], z.log[2:]...)
	_, lp = z.yaz(t)
	if r = calistir("", now, "dogrula", "--kayit", kp, "--ajan-kayit", lp); r.hata == nil {
		t.Fatal("silinen satır geçti")
	}
	// Ajan kaydının sonu silinmiş: panel önde.
	z = yeniZincir(t, t0)
	z.makbuz(t, t0+1, true)
	z.makbuz(t, t0+2, true)
	z.log = z.log[:1]
	kp, lp = z.yaz(t)
	if r = calistir("", now, "dogrula", "--kayit", kp, "--ajan-kayit", lp); r.hata == nil {
		t.Fatal("kırpılmış ajan kaydı geçti")
	}
}

// ---- başlangıç betikleri (680 §13) ----

func TestBaslangicBetikleri(t *testing.T) {
	beklenen := map[string]struct {
		onay  string
		aralk int64
		gunde int
		grup  string
	}{
		"journal-temizle":        {"none", 21600, 4, "temizlik"},
		"paket-onbellek":         {"none", 21600, 4, "temizlik"},
		"docker-derleme-temizle": {"panel", 21600, 4, "docker"},
		"docker-imaj-temizle":    {"panel", 86400, 1, "docker"},
		"servis-yeniden":         {"signed", 600, 6, "sistem"},
		"servis-yeniden-web":     {"signed", 600, 6, "web"},
		"sertifika-yenile":       {"signed", 86400, 1, "web"},
	}
	root := filepath.Join("..", "..", "scripts", "betikler")
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	key, pub := anahtarUret(t, slotYayin)
	yasak := regexp.MustCompile(`(?i)^(ssh|sshd|docker|containerd|uptime.*|bekci.*|postgres.*|mysql.*|mariadb.*|redis.*|mongo.*)$`)
	genis := regexp.MustCompile(`system prune|volume|image prune[^\n]* -a|rm -rf`)
	sayi := 0
	out := t.TempDir()
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		sayi++
		id := e.Name()
		want, ok := beklenen[id]
		if !ok {
			t.Errorf("%s: tabloda yok", id)
			continue
		}
		mp, gp := filepath.Join(root, id, "manifest.json"), filepath.Join(root, id, "govde.sh")
		r := calistir(testParola+"\n", time.Now(), "imzala", "--anahtar", key, "--bildirim", mp, "--govde", gp, "--cikti", out)
		if r.hata != nil {
			t.Errorf("%s: %v", id, r.hata)
			continue
		}
		m, body := okuImzali(t, filepath.Join(out, id+"-1.betik.json"), pub)
		if m.ID != id || m.Approval != want.onay || m.MinIntervalSec != want.aralk || m.MaxPerDay != want.gunde ||
			len(m.Groups) != 1 || m.Groups[0] != want.grup || m.RunAs != "root" {
			t.Errorf("%s: %+v", id, m)
		}
		if c, err := exec.Command("sh", "-n", gp).CombinedOutput(); err != nil {
			t.Errorf("%s: sh -n: %v %s", id, err, c)
		}
		if kod, err := kabukTara(string(body)); err != nil || genis.MatchString(kod) {
			t.Errorf("%s: geniş silme", id)
		}
		for _, p := range m.Params {
			for _, v := range p.Values {
				if yasak.MatchString(v) {
					t.Errorf("%s: %s listeye alınmaz", id, v)
				}
			}
		}
	}
	if sayi != len(beklenen) {
		t.Fatalf("%d betik, %d bekleniyor", sayi, len(beklenen))
	}
}
