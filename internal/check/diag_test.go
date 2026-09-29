package check

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// runT kontrolü verilen zaman aşımıyla çalıştırır ve süresini döner.
func runT(t *testing.T, typ string, cfg any, timeout time.Duration) (Result, time.Duration) {
	t.Helper()
	c, ok := Get(typ)
	if !ok {
		t.Fatalf("tip kayıtlı değil: %s", typ)
	}
	raw, _ := json.Marshal(cfg)
	norm, err := c.Normalize(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	start := time.Now()
	res := c.Check(ctx, norm)
	return res, time.Since(start)
}

func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// diagOf başarısız sonucun tanısı; JSON'a gidip geldiği halini döner (saklanan biçim).
func diagOf(t *testing.T, res Result, kind string) *Diag {
	t.Helper()
	if res.Up || res.Detail == nil || res.Detail.Diag == nil {
		t.Fatalf("tanı yok: %+v", res)
	}
	b, _ := json.Marshal(res.Detail)
	var d Detail
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if d.Kind != kind || d.Method != "" || d.URL != "" {
		t.Fatalf("tür %q (beklenen %q), metot %q: %s", d.Kind, kind, d.Method, b)
	}
	if strings.Contains(string(b), `"method"`) || strings.Contains(string(b), `"url"`) {
		t.Errorf("HTTP dışı tanıda boş istek alanları olmamalı: %s", b)
	}
	return d.Diag
}

func TestTCPDiagSuccessNoDetail(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	res, _ := runT(t, "tcp", map[string]any{"host": "127.0.0.1", "port": l.Addr().(*net.TCPAddr).Port}, 3*time.Second)
	if !res.Up || res.Detail != nil {
		t.Fatalf("başarılı kontrolde ayrıntı olmamalı: %+v", res)
	}
}

func TestTCPDiagRefused(t *testing.T) {
	port := closedPort(t)
	res, took := runT(t, "tcp", map[string]any{"host": "127.0.0.1", "port": port}, 3*time.Second)
	g := diagOf(t, res, "tcp")
	if g.Phase != PhaseConnect || g.ErrorClass != ClassRefused || g.Port != port ||
		g.Target != net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) || g.TimeoutMs < 2900 || g.TimeoutMs > 3000 {
		t.Fatalf("tanı: %+v", g)
	}
	if len(g.Resolved) != 1 || g.Resolved[0] != "127.0.0.1" {
		t.Errorf("çözülen IP: %v", g.Resolved)
	}
	if len(g.Attempts) != 1 || g.Attempts[0].IP != "127.0.0.1" || g.Attempts[0].Result != ClassRefused {
		t.Errorf("denemeler: %+v", g.Attempts)
	}
	if !strings.Contains(g.RawError, "connection refused") {
		t.Errorf("ham hata: %q", g.RawError)
	}
	if g.Ping == nil || g.Ping.Target != "127.0.0.1" {
		t.Fatalf("ping yok: %+v", g.Ping)
	}
	if g.Ping.Error == "" && (g.Ping.Sent != 1 || g.Ping.Received != 1 || g.Ping.LossPct != 0) {
		t.Errorf("localhost ping: %+v", g.Ping)
	}
	if g.Ping.Error != "" && g.Ping.Error != "unavailable" {
		t.Errorf("ping hatası: %+v", g.Ping)
	}
	if took > 2*time.Second {
		t.Errorf("reddedilen bağlantının tanısı çok uzun sürdü: %v", took)
	}
}

// Ad üzerinden: localhost birden çok IP'ye çözülebilir; her biri denenir.
func TestTCPDiagResolvesName(t *testing.T) {
	port := closedPort(t)
	res, _ := runT(t, "tcp", map[string]any{"host": "localhost", "port": port}, 3*time.Second)
	g := diagOf(t, res, "tcp")
	if len(g.Resolved) == 0 || len(g.Attempts) == 0 || len(g.Attempts) > diagMaxTries {
		t.Fatalf("çözümleme/denemeler: %+v", g)
	}
	seen := map[string]bool{}
	for _, a := range g.Attempts {
		if seen[a.IP] {
			t.Errorf("aynı IP iki kez denendi: %+v", g.Attempts)
		}
		seen[a.IP] = true
		if a.Result == ClassOK {
			t.Errorf("kapalı port açık görünüyor: %+v", a)
		}
	}
}

// Yönlendirilemeyen adres: bağlantı zaman aşımına uğrar; tanı zaman aşımını
// en fazla diagGrace kadar aşar.
func TestTCPDiagTimeout(t *testing.T) {
	res, took := runT(t, "tcp", map[string]any{"host": "10.255.255.1", "port": 4444}, 400*time.Millisecond)
	g := diagOf(t, res, "tcp")
	if g.ErrorClass == ClassUnreachable {
		t.Skipf("bu ortamda adres doğrudan erişilemez: %+v", g)
	}
	if g.ErrorClass != ClassTimeout || g.Phase != PhaseConnect || res.Message != "Zaman aşımı" {
		t.Fatalf("tanı: %+v (%q)", g, res.Message)
	}
	if len(g.Attempts) != 1 || g.Attempts[0].IP != "10.255.255.1" || g.Attempts[0].Result != ClassTimeout {
		t.Errorf("denemeler: %+v", g.Attempts)
	}
	if g.Ping == nil || (g.Ping.Error == "" && g.Ping.Sent != 1) {
		t.Errorf("ping: %+v", g.Ping)
	}
	if took > 400*time.Millisecond+diagGrace+500*time.Millisecond {
		t.Errorf("tanı süre sınırını aştı: %v", took)
	}
}

// Monitör durdurulursa (üst bağlam iptal) ağ tanısı beklemez.
func TestNetDiagnoseCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := &Diag{ErrorClass: ClassTimeout}
	start := time.Now()
	netDiagnose(ctx, g, "10.255.255.1", 4444, true, nil, true)
	if time.Since(start) > 100*time.Millisecond || g.Ping != nil || len(g.Attempts) != 0 {
		t.Fatalf("iptalde tanı yapılmamalı: %+v", g)
	}
}

func TestPingDiag(t *testing.T) {
	up, _ := runT(t, "ping", map[string]any{"host": "127.0.0.1", "count": 1}, 3*time.Second)
	if strings.Contains(up.Message, "permission denied") || strings.Contains(up.Message, "operation not permitted") {
		g := diagOf(t, up, "ping")
		if g.Ping == nil || g.Ping.Error != "unavailable" || g.Phase != PhasePing {
			t.Fatalf("izin yok tanısı: %+v", g)
		}
		t.Skipf("bu ortamda yetkisiz ping kapalı: %s", up.Message)
	}
	if !up.Up || up.Detail != nil {
		t.Fatalf("başarılı ping'de ayrıntı olmamalı: %+v", up)
	}
	res, _ := runT(t, "ping", map[string]any{"host": "10.255.255.1", "count": 1}, 600*time.Millisecond)
	if res.Up {
		t.Skip("yönlendirilemeyen adres yanıt verdi")
	}
	g := diagOf(t, res, "ping")
	if g.Ping == nil || g.Ping.Target != "10.255.255.1" || g.Ping.Received != 0 || g.Phase != PhasePing {
		t.Fatalf("ping tanısı: %+v %+v", g, g.Ping)
	}
	if g.Ping.Sent > 0 && g.Ping.LossPct != 100 {
		t.Errorf("kayıp: %+v", g.Ping)
	}
	if g.ErrorClass != ClassTimeout && g.ErrorClass != ClassUnreachable {
		t.Errorf("sınıf: %q", g.ErrorClass)
	}
}

func TestDNSDiag(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := dns.NewServeMux()
	mux.HandleFunc("kadir.app.", func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		if r.Question[0].Qtype == dns.TypeA {
			rr, _ := dns.NewRR("kadir.app. 60 IN A 94.130.174.47")
			m.Answer = append(m.Answer, rr)
		}
		w.WriteMsg(m)
	})
	mux.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeNameError)
		w.WriteMsg(m)
	})
	srv := &dns.Server{PacketConn: pc, Handler: mux}
	go srv.ActivateAndServe()
	defer srv.Shutdown()
	port := pc.LocalAddr().(*net.UDPAddr).Port
	server := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	ok, _ := runT(t, "dns", map[string]any{"host": "kadir.app", "server": "127.0.0.1", "port": port}, 3*time.Second)
	if !ok.Up || ok.Detail != nil {
		t.Fatalf("başarılı sorguda ayrıntı olmamalı: %+v", ok)
	}

	res, took := runT(t, "dns", map[string]any{"host": "yok.example", "server": "127.0.0.1", "port": port}, 3*time.Second)
	g := diagOf(t, res, "dns")
	d := g.DNS
	if d == nil || d.Rcode != "NXDOMAIN" || d.Query != "yok.example" || d.Type != "A" || d.Server != server ||
		d.Transport != "udp" || len(d.Answers) != 0 {
		t.Fatalf("NXDOMAIN tanısı: %+v", d)
	}
	if g.ErrorClass != ClassRcode || g.Phase != PhaseResponse || g.Ping != nil || len(g.Attempts) != 0 {
		t.Errorf("yanıt alınan sorguda ağ tanısı yapılmamalı: %+v", g)
	}
	if took > time.Second {
		t.Errorf("NXDOMAIN tanısı ek süre almamalı: %v", took)
	}

	res, _ = runT(t, "dns", map[string]any{"host": "kadir.app", "server": "127.0.0.1", "port": port, "expected": "1.2.3.4"}, 3*time.Second)
	g = diagOf(t, res, "dns")
	if g.ErrorClass != ClassMismatch || g.DNS.Rcode != "NOERROR" || len(g.DNS.Answers) != 1 || g.DNS.Answers[0] != "A 94.130.174.47" {
		t.Errorf("beklenen değer tanısı: %+v %+v", g, g.DNS)
	}
	res, _ = runT(t, "dns", map[string]any{"host": "kadir.app", "server": "127.0.0.1", "port": port, "record_type": "TXT"}, 3*time.Second)
	if g = diagOf(t, res, "dns"); g.ErrorClass != ClassNoRecord || g.DNS.Type != "TXT" {
		t.Errorf("kayıt yok tanısı: %+v", g)
	}

	// Yanıt vermeyen sunucu: zaman aşımı; sunucuya ping atılır.
	silent, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	res, took = runT(t, "dns", map[string]any{"host": "kadir.app", "server": "127.0.0.1", "port": silent.LocalAddr().(*net.UDPAddr).Port}, 300*time.Millisecond)
	g = diagOf(t, res, "dns")
	if g.ErrorClass != ClassTimeout || g.DNS.Rcode != "timeout" || g.Phase != PhaseQuery || g.Ping == nil || g.Ping.Target != "127.0.0.1" {
		t.Fatalf("zaman aşımı tanısı: %+v %+v", g, g.Ping)
	}
	if g.Port != 0 || len(g.Attempts) != 0 {
		t.Errorf("UDP tipinde TCP denemesi yapılmamalı: %+v", g)
	}
	if took > 300*time.Millisecond+diagGrace+500*time.Millisecond {
		t.Errorf("süre: %v", took)
	}
}

func TestSMTPDiag(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			fmt.Fprint(conn, "554 mx.ornek.test hizmet disi\r\n")
			conn.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	res, took := runT(t, "smtp", map[string]any{"host": "127.0.0.1", "port": port}, 3*time.Second)
	g := diagOf(t, res, "smtp")
	if g.Phase != PhaseGreeting || g.ErrorClass != ClassProtocol || !strings.Contains(g.Banner, "hizmet disi") {
		t.Fatalf("banner tanısı: %+v", g)
	}
	if g.Ping != nil || len(g.Attempts) != 0 || took > time.Second {
		t.Errorf("protokol hatasında ağ tanısı yapılmamalı: %+v (%v)", g, took)
	}

	res, _ = runT(t, "smtp", map[string]any{"host": "127.0.0.1", "port": closedPort(t)}, 3*time.Second)
	if g = diagOf(t, res, "smtp"); g.Phase != PhaseConnect || g.ErrorClass != ClassRefused || len(g.Attempts) != 1 {
		t.Fatalf("kapalı port: %+v", g)
	}
}

// Veritabanı tipleri: bağlantı hatası tanısı; şifre hiçbir alanda görünmez.
func TestDBDiagConnRefusedMasksPassword(t *testing.T) {
	port := closedPort(t)
	const secret = "cok-gizli-sifre-42"
	cases := []struct {
		typ string
		cfg map[string]any
	}{
		{"postgres", map[string]any{"host": "127.0.0.1", "port": port, "username": "u", "password": secret, "sslmode": "disable"}},
		{"mysql", map[string]any{"host": "127.0.0.1", "port": port, "username": "u", "password": secret}},
		{"redis", map[string]any{"host": "127.0.0.1", "port": port, "password": secret}},
		{"mongodb", map[string]any{"uri": fmt.Sprintf("mongodb://u:%s@127.0.0.1:%d/?directConnection=true", secret, port)}},
		{"mqtt", map[string]any{"broker_url": fmt.Sprintf("tcp://127.0.0.1:%d", port), "username": "u", "password": secret}},
	}
	for _, c := range cases {
		t.Run(c.typ, func(t *testing.T) {
			res, _ := runT(t, c.typ, c.cfg, 2*time.Second)
			g := diagOf(t, res, c.typ)
			if g.ErrorClass != ClassRefused {
				t.Errorf("sınıf %q (beklenen refused): %+v", g.ErrorClass, g)
			}
			if g.Phase != PhaseConnect {
				t.Errorf("aşama %q: %+v", g.Phase, g)
			}
			if len(g.Attempts) == 0 || g.Attempts[0].Result != ClassRefused || g.Port != port {
				t.Errorf("denemeler: %+v", g.Attempts)
			}
			b, _ := json.Marshal(res.Detail)
			if strings.Contains(string(b), secret) {
				t.Errorf("şifre sızdı: %s", b)
			}
		})
	}
}

func TestMaskDiagRemote(t *testing.T) {
	cfg := json.RawMessage(`{"uri":"mongodb://kadir:uzun-sifre-99@db.example:27017/app","password":"ikinci-gizli","community":"topluluk-gizli","headers":"Authorization: Bearer tok-gizli-777\nAccept: text/plain"}`)
	d := &Detail{Kind: "mongodb", Diag: &Diag{
		Target:   "mongodb://kadir:uzun-sifre-99@db.example:27017/app",
		RawError: "auth failed for ikinci-gizli with topluluk-gizli; token tok-gizli-777; url postgres://x:baska-sifre@h/db",
		Banner:   "hello uzun-sifre-99",
		Attempts: []DiagAttempt{{IP: "10.0.0.1", Result: "refused", Error: "ikinci-gizli"}},
		Ping:     &DiagPing{Target: "10.0.0.1", RawError: "tok-gizli-777"},
	}}
	MaskDetail("mongodb", cfg, d)
	b, _ := json.Marshal(d)
	for _, s := range []string{"uzun-sifre-99", "ikinci-gizli", "topluluk-gizli", "tok-gizli-777", "baska-sifre"} {
		if strings.Contains(string(b), s) {
			t.Errorf("%q maskelenmedi: %s", s, b)
		}
	}
	if !strings.Contains(d.Diag.Target, "db.example:27017") || !strings.Contains(d.Diag.RawError, "postgres://x:") {
		t.Errorf("maskeleme fazla sildi: %+v", d.Diag)
	}
}

func TestRedactURLsIn(t *testing.T) {
	cases := map[string]string{
		"dial mongodb://u:p4ss@h:1/x failed": "dial mongodb://u:" + detailMaskedValue + "@h:1/x failed",
		"tcp://h:1883 refused":               "tcp://h:1883 refused",
		"a://u@h b://:pw@h":                  "a://u@h b://:" + detailMaskedValue + "@h",
		"yok":                                "yok",
	}
	for in, want := range cases {
		if got := redactURLsIn(in); got != want {
			t.Errorf("%q → %q, %q bekleniyordu", in, got, want)
		}
	}
}

// Uzak kontrol noktasından gelen tanı sınırlanır ve kodlar izin listesine çekilir.
func TestDiagSanitizeBounds(t *testing.T) {
	long := strings.Repeat("ç", 5000)
	var attempts []DiagAttempt
	for i := range 50 {
		attempts = append(attempts, DiagAttempt{IP: long, Result: "<script>", ElapsedMs: -5, Error: long + fmt.Sprint(i)})
	}
	var answers, ips []string
	for range 100 {
		answers = append(answers, long)
		ips = append(ips, "1.2.3.4\n\x00")
	}
	d := &Detail{Kind: "Kötü tür!", Diag: &Diag{
		Target: long, Port: 99999, Resolved: ips, ResolveMs: math.MaxInt64, ResolveError: "evil",
		Attempts: attempts, TimeoutMs: -1, ElapsedMs: 1 << 50, Phase: "drop table", ErrorClass: "x",
		RawError: long, Banner: strings.Repeat("b", 4000) + "\x1b[31m",
		Ping: &DiagPing{Target: long, Sent: 1000, Received: 5000, LossPct: math.NaN(), MinMs: -1, AvgMs: math.Inf(1), MaxMs: 1e12, Error: "boom", RawError: long},
		DNS:  &DiagDNS{Server: long, Type: long, Query: long, Transport: "quic", Rcode: long, Answers: answers, ElapsedMs: -9},
	}}
	d.Sanitize()
	g := d.Diag
	if d.Kind != "" {
		t.Errorf("tür temizlenmedi: %q", d.Kind)
	}
	if len([]rune(g.Target)) > detailMaxURL+1 || g.Port != 65535 || len(g.Resolved) != diagMaxIPs || g.Resolved[0] != "1.2.3.4" {
		t.Errorf("hedef/port/IP: %d %d %v", len(g.Target), g.Port, g.Resolved)
	}
	if g.ResolveMs != diagMaxMs || g.TimeoutMs != 0 || g.ElapsedMs != diagMaxMs || g.ResolveError != ClassOther {
		t.Errorf("süreler: %+v", g)
	}
	if len(g.Attempts) != diagMaxIPs || g.Attempts[0].Result != ClassOther || g.Attempts[0].ElapsedMs != 0 ||
		len([]rune(g.Attempts[0].Error)) > detailMaxError+1 || len([]rune(g.Attempts[0].IP)) > 65 {
		t.Errorf("denemeler: %d %+v", len(g.Attempts), g.Attempts[0])
	}
	if g.Phase != ClassOther || g.ErrorClass != ClassOther {
		t.Errorf("kodlar: %q %q", g.Phase, g.ErrorClass)
	}
	if len(g.Banner) > diagMaxBanner || strings.ContainsRune(g.Banner, 0x1b) || len([]rune(g.RawError)) > detailMaxError+1 {
		t.Errorf("banner/hata: %d %d", len(g.Banner), len(g.RawError))
	}
	p := g.Ping
	if p.Sent != 100 || p.Received != 100 || p.LossPct != 0 || p.MinMs != 0 || p.AvgMs != diagMaxMs || p.MaxMs != diagMaxMs || p.Error != ClassOther {
		t.Errorf("ping: %+v", p)
	}
	n := g.DNS
	if len(n.Answers) != diagMaxAnswers || len([]rune(n.Answers[0])) > 301 || n.Transport != ClassOther || n.ElapsedMs != 0 || len([]rune(n.Type)) > 11 {
		t.Errorf("dns: %d %+v", len(n.Answers), n.Transport)
	}
	b, _ := json.Marshal(d)
	if len(b) > 64<<10 {
		t.Errorf("temizlenmiş tanı çok büyük: %d bayt", len(b))
	}

	// Eski (yalnızca HTTP) kayıt değişmeden çözülür.
	var old Detail
	json.Unmarshal([]byte(`{"method":"GET","url":"https://x","status":500,"body":"hata"}`), &old)
	old.Sanitize()
	if old.Kind != "" || old.Diag != nil || old.Method != "GET" || old.Status != 500 {
		t.Errorf("eski kayıt: %+v", old)
	}
}

func TestClassifyErr(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		err  error
		want string
	}{
		{&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, ClassRefused},
		{&net.OpError{Op: "read", Err: syscall.ECONNRESET}, ClassReset},
		{&net.OpError{Op: "dial", Err: syscall.EHOSTUNREACH}, ClassUnreachable},
		{&net.OpError{Op: "dial", Err: syscall.ENETUNREACH}, ClassUnreachable},
		{&net.DNSError{Name: "x.invalid", IsNotFound: true}, ClassDNSNotFound},
		{&net.DNSError{Name: "x", Err: "server misbehaving"}, ClassDNSError},
		{context.DeadlineExceeded, ClassTimeout},
		{fmt.Errorf("server selection error: %w, current topology: { Last error: dial tcp 1.2.3.4:27017: connect: connection refused }", context.DeadlineExceeded), ClassRefused},
		{errors.New("pq: password authentication failed for user \"u\""), ClassAuth},
		{errors.New("WRONGPASS invalid username-password pair"), ClassAuth},
		{errors.New("Error 1049 (42000): Unknown database 'x'"), ClassNotFound},
		{errors.New("x509: certificate signed by unknown authority"), ClassTLS},
		{errors.New("No connection could be made because the target machine actively refused it."), ClassRefused},
		{errors.New("tamamen başka"), ClassOther},
	}
	for _, c := range cases {
		if got := classifyErr(ctx, c.err); got != c.want {
			t.Errorf("%v → %q, %q bekleniyordu", c.err, got, c.want)
		}
	}
}
