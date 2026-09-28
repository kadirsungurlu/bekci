package check

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"syscall"
	"testing"
	"time"

	mssql "github.com/microsoft/go-mssqldb"

	"github.com/kadirsungurlu/uptime-kadir-app/internal/i18n"
	"github.com/kadirsungurlu/uptime-kadir-app/internal/store"
)

// Kontrol mesajları Türkçe saklanır ve API'de i18n.Message ile çevrilir.
// Bu test gerçek üreticilerin (describeErr, AggregateGroup, evalJSON,
// compareSNMP, gerçek kontroller) çıktısının İngilizceye TAMAMEN çevrildiğini
// denetler: sonuçta Türkçe harf kalmamalı (örnek verilerde Türkçe harf yok).

var trLetters = regexp.MustCompile(`[çğıöşüÇĞİÖŞÜ]`)

func assertEnglish(t *testing.T, msg string) {
	t.Helper()
	en := i18n.Message(i18n.EN, msg)
	if trLetters.MatchString(en) || (trLetters.MatchString(msg) && en == msg) {
		t.Errorf("çevrilemedi:\n tr: %q\n en: %q", msg, en)
	}
	if i18n.Message(i18n.TR, msg) != msg {
		t.Errorf("tr değişmemeli: %q", msg)
	}
}

func TestMessagesTranslatedDescribeErr(t *testing.T) {
	ctx := context.Background()
	errs := []error{
		context.DeadlineExceeded,
		&net.DNSError{Name: "example.invalid", IsNotFound: true},
		&net.DNSError{Name: "example.com", Err: "server misbehaving"},
		&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED},
		&net.OpError{Op: "dial", Err: syscall.EHOSTUNREACH},
		&net.OpError{Op: "read", Err: syscall.ECONNRESET},
		&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}},
	}
	for _, err := range errs {
		msg := describeErr(ctx, err)
		assertEnglish(t, msg)
		// Sarmalayan mesajlarla birlikte.
		for _, wrap := range []string{"Sorgu çalıştırılamadı: ", "Banner okunamadı: ", "Proxy hatası: ",
			"Yanıt gövdesi okunamadı: ", "SSL handshake hatası: ", "Mesaj gönderilemedi: ", "Bağlanılamadı: "} {
			assertEnglish(t, wrap+msg)
			assertEnglish(t, "Bakımda ("+wrap+msg+")")
			assertEnglish(t, "Ters mod: hedef erişilebilir ("+wrap+msg+")")
		}
	}
	for _, n := range []int32{18456, 4060} {
		assertEnglish(t, mssqlConnErr(ctx, mssql.Error{Number: n}))
	}
	for _, label := range dockerStatusTR {
		assertEnglish(t, "Konteyner çalışmıyor (durum: "+label+")")
	}
}

func TestMessagesTranslatedGroup(t *testing.T) {
	var kids []ChildStatus
	for i := range 14 {
		kids = append(kids, ChildStatus{ID: int64(i), Name: fmt.Sprintf("m%d", i), Active: true, Status: store.StatusDown})
	}
	mixes := [][]ChildStatus{
		nil,
		{{Name: "a", Active: true, Status: store.StatusUp}},
		{{Name: "a", Active: true, Status: store.StatusUp}, {Name: "b", Active: false}},
		{{Name: "a", Active: true, Status: store.StatusUp}, {Name: "b", Active: true, Status: store.StatusMaintenance}, {Name: "c", Active: false}},
		{{Name: "a", Active: true, Status: store.StatusDown}, {Name: "b", Active: true, Status: store.StatusUp}},
		{{Name: "a", Active: true, Status: store.StatusPending}},
		{{Name: "a", Active: true, Status: store.StatusPending}, {Name: "b", Active: true, Status: store.StatusPending}, {Name: "c", Active: true, Status: store.StatusMaintenance}},
		{{Name: "b", Active: true, Status: store.StatusMaintenance}},
		kids,
	}
	for _, mode := range []string{GroupAnyDown, GroupAllDown} {
		for _, m := range mixes {
			assertEnglish(t, AggregateGroup(mode, m).Message)
		}
	}
	for _, n := range []int{1, 3} {
		ids := make([]int64, n)
		raw, _ := json.Marshal(GroupConfig{Mode: GroupAnyDown, MonitorIDs: ids})
		assertEnglish(t, groupChecker{}.Target(raw))
	}
}

func TestMessagesTranslatedCompare(t *testing.T) {
	body := []byte(`{"data":{"status":"ok","n":5}}`)
	for _, c := range [][3]string{
		{"data.missing", "exists", ""}, {"data.missing", "==", "x"}, {"data.status", "==", "fail"},
		{"data.status", ">", "3"}, {"data.n", "<", "2"},
	} {
		ok, msg := evalJSON(body, c[0], c[1], c[2])
		if ok {
			t.Fatalf("%v başarısız olmalıydı", c)
		}
		assertEnglish(t, msg)
	}
	_, msg := evalJSON([]byte("not json"), "a", "exists", "")
	assertEnglish(t, msg)
	for _, c := range [][3]string{{"abc", ">", "3"}, {"5", ">", "7"}, {"x", "==", "y"}} {
		if ok, msg := compareSNMP(c[0], c[1], c[2]); !ok {
			assertEnglish(t, msg)
		}
	}
}

// Gerçek kontroller: kapalı port, anahtar kelime, durum kodu, zaman aşımı.
func TestMessagesTranslatedChecks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			time.Sleep(700 * time.Millisecond)
		}
		fmt.Fprint(w, "hello world")
	}))
	defer srv.Close()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := l.Addr().(*net.TCPAddr).Port
	l.Close()

	run := func(typ string, cfg any, timeout time.Duration) Result {
		t.Helper()
		c, _ := Get(typ)
		raw, _ := json.Marshal(cfg)
		norm, err := c.Normalize(raw)
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return c.Check(ctx, norm)
	}
	cases := []Result{
		run("tcp", map[string]any{"host": "127.0.0.1", "port": closedPort}, 5*time.Second),
		run("http", map[string]any{"url": srv.URL, "keyword": "missing"}, 5*time.Second),
		run("http", map[string]any{"url": srv.URL, "keyword": "hello", "keyword_invert": true}, 5*time.Second),
		run("http", map[string]any{"url": srv.URL + "/slow"}, 300*time.Millisecond),
		run("http", map[string]any{"url": srv.URL, "json_path": "a", "json_op": "exists"}, 5*time.Second),
		run("websocket", map[string]any{"url": "ws://127.0.0.1:" + fmt.Sprint(closedPort)}, 5*time.Second),
		pushChecker{}.Check(context.Background(), nil),
	}
	up := run("tcp", map[string]any{"host": "127.0.0.1", "port": srv.Listener.Addr().(*net.TCPAddr).Port}, 5*time.Second)
	if !up.Up {
		t.Fatalf("tcp açık port: %+v", up)
	}
	cases = append(cases, up)
	for _, r := range cases {
		if r.Message == "" {
			t.Errorf("boş mesaj: %+v", r)
			continue
		}
		assertEnglish(t, r.Message)
	}
}
