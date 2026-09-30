package i18n

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestCheckCatalog: her çevirinin fiilleri Türkçeyle aynı sayıda, biçim
// hatasız ve Türkçe harfsiz olmalı.
func TestCheckCatalog(t *testing.T) {
	verbsOf := func(s string) []string {
		var out []string
		for _, v := range tokRe.FindAllString(s, -1) {
			if v != "%%" {
				out = append(out, v)
			}
		}
		return out
	}
	for tr, en := range checkEN {
		if strings.TrimSpace(en) == "" {
			t.Errorf("%q: boş çeviri", tr)
			continue
		}
		trv, env := verbsOf(tr), verbsOf(en)
		if len(trv) != len(env) {
			t.Errorf("%q: fiil sayısı farklı (%v / %v): %q", tr, trv, env, en)
		}
		if turkishLetters.MatchString(en) {
			t.Errorf("%q: çeviride Türkçe harf var: %q", tr, en)
		}
		if _, dup := errorsEN[tr]; dup && errorsEN[tr] != en {
			t.Errorf("%q: errorsEN ile farklı çeviri: %q / %q", tr, en, errorsEN[tr])
		}
	}
	// Derlenmiş kalıpların biçimi: örnek değerlerle "%!" üretmemeli.
	msgTable.once.Do(msgTable.compile)
	for _, p := range msgTable.patterns {
		args := make([]any, len(p.verbs))
		for i := range args {
			args[i] = "1"
		}
		if out := fmt.Sprintf(p.en, args...); strings.Contains(out, "%!") {
			t.Errorf("%q: biçim hatası: %q", p.en, out)
		}
	}
}

func TestMessage(t *testing.T) {
	cases := []struct{ in, want string }{
		// Düz metin ve basit kalıplar
		{"Bağlantı reddedildi", "Connection refused"},
		{"Zaman aşımı", "Timeout"},
		{"Port açık", "Port open"},
		{`Kelime bulunamadı: "Merhaba"`, `Keyword not found: "Merhaba"`},
		{"Alan adı bulunamadı: ornek.invalid", "Domain not found: ornek.invalid"},
		{"Yanıt yok (3 paketin hiçbiri dönmedi)", "No reply (none of the 3 packets came back)"},
		{"3/3 paket", "3/3 packets"},
		// Sertifika bitişi: Türkçe tarih arayüzün İngilizce biçimine çevrilir; eski ISO kayıtlar olduğu gibi.
		{"Sertifika geçerli, bitiş: 24.11.2026", "Certificate valid, expires: Nov 24, 2026"},
		{"Sertifika geçerli, bitiş: 2026-11-24", "Certificate valid, expires: 2026-11-24"},
		{"2/3 paket, %33 kayıp", "2/3 packets, 33% loss"},
		{"Sorgu başarılı (1 satır)", "Query successful (1 row)"},
		{"Sorgu başarılı (12 satır)", "Query successful (12 rows)"},
		{"A kaydı bulunamadı", "No A record found"},
		{"Beklenen değer yok (1.2.3.4); gelen: 5.6.7.8, 9.9.9.9", "Expected value not found (1.2.3.4); got: 5.6.7.8, 9.9.9.9"},
		{`"sensors/x" konusunda mesaj gelmedi (zaman aşımı)`, `No message on topic "sensors/x" (timeout)`},
		{"JSON: data.status beklenen değerle eşleşmedi (beklenen: == ok)", "JSON: data.status did not match the expected value (expected: == ok)"},
		{"OID değeri: 42 (beklenen: > 50)", "OID value: 42 (expected: > 50)"},
		{"Konteyner çalışmıyor (durum: sona erdi)", "Container is not running (state: exited)"},
		{"Endpoint geçersiz: soket yolu boş", "Invalid endpoint: socket path is empty"},
		{"(ikili içerik, 2048 bayt)", "(binary content, 2048 bytes)"},
		// Ham ağ hatası İngilizce kalır; sarmalayıcı çevrilir.
		{"Proxy hatası: Zaman aşımı", "Proxy error: Timeout"},
		{"Sorgu çalıştırılamadı: dial tcp 10.0.0.5:5432: connect: connection refused",
			"Could not run the query: dial tcp 10.0.0.5:5432: connect: connection refused"},
		{"OAuth2 token alınamadı: yanıtta access_token yok", "Could not get OAuth2 token: no access_token in the response"},
		// Motor sarmalayıcıları (iç içe)
		{"Bakımda", "In maintenance"},
		{"Bakımda (Bağlantı reddedildi)", "In maintenance (Connection refused)"},
		{"Ters mod: hedef erişilebilir (200 OK)", "Upside down mode: target is reachable (200 OK)"},
		{"Bakımda (Ters mod: hedef erişilebilir (Port açık))", "In maintenance (Upside down mode: target is reachable (Port open))"},
		// Grup
		{"2/5 alt monitör çalışmıyor: API, Web", "2/5 child monitors down: API, Web"},
		{"2/5 alt monitör çalışmıyor: API, Web (1 bakımda, 2 durdurulmuş)", "2/5 child monitors down: API, Web (1 in maintenance, 2 paused)"},
		{"Tüm alt monitörler çalışıyor (4)", "All child monitors up (4)"},
		{"Tüm alt monitörler çalışıyor (4) (1 durdurulmuş)", "All child monitors up (4) (1 paused)"},
		{"1 alt monitör bekliyor: DB", "1 child monitor pending: DB"},
		{"Değerlendirilecek alt monitör yok (3 bakımda)", "No child monitors to evaluate (3 in maintenance)"},
		{"11/12 alt monitör çalışmıyor: a, b, c, d, e, f, g, h, i, j ve 1 diğer", "11/12 child monitors down: a, b, c, d, e, f, g, h, i, j and 1 more"},
		{"3 monitör", "3 monitors"},
		// Çok konumlu birleşik mesaj
		{"Ana sunucu: Bağlantı reddedildi; İstanbul: Zaman aşımı", "Main server: Connection refused; İstanbul: Timeout"},
		{"Ana sunucu: Zaman aşımı (sonuç gelmeyen: Frankfurt, Ana sunucu)", "Main server: Timeout (no results from: Frankfurt, Main server)"},
		{"2/3 konum çalışıyor — Frankfurt: HTTP 503 Service Unavailable", "2/3 locations up — Frankfurt: HTTP 503 Service Unavailable"},
		{"2/3 konum çalışıyor — Ana sunucu: Bağlantı reddedildi; Paris: Zaman aşımı (sonuç gelmeyen: Oslo)",
			"2/3 locations up — Main server: Connection refused; Paris: Timeout (no results from: Oslo)"},
		{"Bakımda (Ana sunucu: A kaydı bulunamadı; Paris: A kaydı bulunamadı)", "In maintenance (Main server: No A record found; Paris: No A record found)"},
		{"Ana sunucu: Ters mod: hedef erişilebilir (200 OK)", "Main server: Upside down mode: target is reachable (200 OK)"},
		{"Kontrol noktalarından sonuç gelmiyor", "No results from check locations"},
		{"Çalışmıyor: Bağlantı reddedildi", "Down: Connection refused"},
		// Olay geçmişi
		{"Monitör durduruldu; olay kapatıldı", "Monitor paused; incident closed"},
		{"Bakım penceresi başladı; kontroller sürüyor, bildirim gönderilmiyor", "Maintenance window started; checks continue, no notifications are sent"},
		// Zaten İngilizce / bilinmeyen: değişmez
		{"200 OK", "200 OK"},
		{"HTTP 503 Service Unavailable", "HTTP 503 Service Unavailable"},
		{"PONG", "PONG"},
		{"Get \"https://x\": dial tcp: lookup x: no such host", "Get \"https://x\": dial tcp: lookup x: no such host"},
		{"Hiç görülmemiş bir mesaj", "Hiç görülmemiş bir mesaj"},
		{"", ""},
	}
	for _, c := range cases {
		for range 2 { // ikinci tur önbellekten
			if got := Message(EN, c.in); got != c.want {
				t.Errorf("Message(en, %q)\n = %q\n  %q bekleniyordu", c.in, got, c.want)
			}
		}
		if got := Message(TR, c.in); got != c.in {
			t.Errorf("Message(tr, %q) değişmemeli: %q", c.in, got)
		}
	}
}

func TestMessageCacheBounded(t *testing.T) {
	c := &boundedCache{max: 4}
	for i := range 10 {
		c.put(strconv.Itoa(i), "x")
	}
	if len(c.m) > 4 {
		t.Fatalf("önbellek sınırı aşıldı: %d", len(c.m))
	}
	if _, ok := c.get("9"); !ok {
		t.Fatal("son girdi önbellekte olmalı")
	}
}

// Üretici kapsamı -------------------------------------------------------------
//
// TestProducerCoverage kontrol mesajlarını üreten kaynak dosyaları tarar:
// Türkçe her dize sabiti (günlük/log çağrıları hariç) çeviri kataloğunda
// (checkEN veya errorsEN) karşılanmalı. Tam mesaj olan sabit birebir anahtar
// olmalı; birleştirme parçası ("Proxy hatası: " + …) veya fmt biçimi
// ("%d/%d alt monitör çalışmıyor: %s%s") ise her sabit parçası bir anahtarın
// içinde geçmeli. Yeni bir kontrol mesajı eklenip çevirisi unutulursa bu test
// hatayı dosya:satır ile gösterir.

// producerDirs tümüyle taranan paketler (test dosyaları hariç).
var producerDirs = []string{"../check", "../engine", "../metrics", "../servers"}

// producerFuncs başka paketlerde kontrol sonucu, olay geçmişi veya izleyici
// mesajı üreten fonksiyonlar (dosyanın geri kalanı API hata mesajları ya da
// ajanın kendi komut satırı/günlük metinleridir).
var producerFuncs = map[string][]string{
	"../probe/probe.go":           {"checkOnce"},
	"../store/incident_events.go": {"AddIncidentEvents"},
	"../notify/dispatcher.go":     {"incidentEvent"},
	"../api/monitors.go":          {"incidentNote"},
	"../api/viewer_messages.go":   {"groupMessageFor"},
}

// turkishWords Türkçe harf içermeyen Türkçe kelimeler (ör. "STARTTLS reddedildi").
var turkishWords = regexp.MustCompile(`(?i)\b(yok|ve|veya|ile|ama|reddedildi|bulundu|durumu|hata|bildirildi|sunucu|erdi|gelmedi|sinyal|kaydi|olmali|gerekli|beklenen|degil|mesaj|anahtar|servis|sertifika|konum|kontrol|monitor|paket|sorgu|ayar|olay|alt)\b`)

func isTurkish(s string) bool { return turkishLetters.MatchString(s) || turkishWords.MatchString(s) }

// isLogCall slog çağrısı mı (e.log.Error(…), c.opts.Log.Warn(…))? Bunların
// metni kullanıcıya gitmez.
func isLogCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Debug", "Info", "Warn", "Error":
	default:
		return false
	}
	var name strings.Builder
	var walk func(e ast.Expr)
	walk = func(e ast.Expr) {
		switch x := e.(type) {
		case *ast.SelectorExpr:
			walk(x.X)
			name.WriteString("." + x.Sel.Name)
		case *ast.Ident:
			name.WriteString(x.Name)
		}
	}
	walk(sel.X)
	return strings.Contains(strings.ToLower(name.String()), "log")
}

type producerLit struct {
	pos string
	val string
}

func collectLits(t *testing.T, fset *token.FileSet, root ast.Node) []producerLit {
	var out []producerLit
	ast.Inspect(root, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ImportSpec, *ast.Field:
			return false
		case *ast.CallExpr:
			if isLogCall(x) {
				return false
			}
		case *ast.BasicLit:
			if x.Kind != token.STRING || strings.HasPrefix(x.Value, "`") {
				return true
			}
			s, err := strconv.Unquote(x.Value)
			if err != nil {
				t.Fatalf("%s: %v", fset.Position(x.Pos()), err)
			}
			if isTurkish(s) {
				out = append(out, producerLit{fset.Position(x.Pos()).String(), s})
			}
		}
		return true
	})
	return out
}

func producerLits(t *testing.T) []producerLit {
	fset := token.NewFileSet()
	var lits []producerLit
	for _, dir := range producerDirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("%s: dosya bulunamadı (%v)", dir, err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			af, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			lits = append(lits, collectLits(t, fset, af)...)
		}
	}
	for f, funcs := range producerFuncs {
		af, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			for _, name := range funcs {
				if fd.Name.Name == name {
					found++
					lits = append(lits, collectLits(t, fset, fd)...)
				}
			}
		}
		if found != len(funcs) {
			t.Fatalf("%s: %v fonksiyonları bulunamadı (yeniden adlandırıldıysa listeyi güncelleyin)", f, funcs)
		}
	}
	return lits
}

// internalErrors taranan paketlerde kullanıcıya gösterilmeyen iç hatalar
// (yalnızca günlüğe düşer; kontrol mesajı olarak saklanmaz).
var internalErrors = map[string]bool{
	"bilinmeyen monitör tipi: %s": true, // engine.start
	"motor başlatılmadı":          true, // engine.start
	"monitör %d zaten çalışıyor":  true, // engine.start
}

// isFragment birleştirmede kullanılan parça mı ("Proxy hatası: ", " (sonuç gelmeyen: ")?
func isFragment(s string) bool {
	return strings.HasSuffix(s, " ") || strings.HasSuffix(s, "(") || strings.HasPrefix(s, " ") ||
		strings.HasPrefix(s, ")") || strings.HasPrefix(s, ",")
}

func TestProducerCoverage(t *testing.T) {
	if _, err := os.Stat("../check"); err != nil {
		t.Skip("kaynak ağacı yok")
	}
	keys := make([]string, 0, len(checkEN)+len(errorsEN))
	for k := range checkEN {
		keys = append(keys, k)
	}
	for k := range errorsEN {
		keys = append(keys, k)
	}
	inSomeKey := func(chunk string) bool {
		for _, k := range keys {
			if strings.Contains(k, chunk) {
				return true
			}
		}
		return false
	}
	lits := producerLits(t)
	if len(lits) < 100 {
		t.Fatalf("beklenenden az Türkçe dize bulundu (%d); tarama bozulmuş olabilir", len(lits))
	}
	for _, l := range lits {
		if internalErrors[l.val] {
			continue
		}
		if _, ok := checkEN[l.val]; ok {
			continue
		}
		if _, ok := errorsEN[l.val]; ok {
			continue
		}
		hasVerb := false
		for _, v := range tokRe.FindAllString(l.val, -1) {
			if v != "%%" {
				hasVerb = true
			}
		}
		if !hasVerb && !isFragment(l.val) {
			t.Errorf("%s: çevirisi yok: %q", l.pos, l.val)
			continue
		}
		for _, chunk := range tokRe.Split(l.val, -1) {
			if chunk = strings.TrimSpace(chunk); chunk != "" && isTurkish(chunk) && !inSomeKey(chunk) {
				t.Errorf("%s: %q parçası (%q) hiçbir çeviri anahtarında yok", l.pos, chunk, l.val)
			}
		}
	}
	t.Logf("%d Türkçe dize tarandı", len(lits))
}

func BenchmarkMessage(b *testing.B) {
	msgs := []string{
		"Bağlantı reddedildi", "200 OK", `Kelime bulunamadı: "ok"`,
		"Ana sunucu: Zaman aşımı; Paris: Bağlantı reddedildi (sonuç gelmeyen: Oslo)",
		"Sorgu çalıştırılamadı: dial tcp 10.0.0.5:5432: connect: connection refused",
	}
	b.Run("cached", func(b *testing.B) {
		for i := 0; b.Loop(); i++ {
			Message(EN, msgs[i%len(msgs)])
		}
	})
	b.Run("uncached", func(b *testing.B) {
		for i := 0; b.Loop(); i++ {
			if _, ok := translateMessage(msgs[i%len(msgs)], 0); !ok && i%len(msgs) != 1 {
				b.Fatal("çevrilemedi")
			}
		}
	})
}
