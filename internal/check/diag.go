package check

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

// Bağlantı tanısı (HTTP dışı tipler) ---------------------------------------------------------
//
// HTTP kontrolünde olay sayfası isteği ve yanıtı gösterir; diğer tiplerde
// (TCP, ping, DNS, veritabanları, SMTP…) bunun yerine yapılandırılmış bir
// bağlantı tanısı (Diag) tutulur: hedef, çözülen IP'ler, IP başına bağlantı
// denemeleri, başarısız olunan aşama, hata sınıfı, tek paketlik ping ve DNS
// yanıtı. Alanlar serbest metin yerine kod taşır; arayüz etiketleri ve
// "sunucu ayakta ama port kapalı" gibi özeti dile göre kurar.
//
// Tanı YALNIZCA başarısız kontrolde toplanır (başarılı kontrolde ek iş yok).
// Ek ağ tanısı (çözümleme, diğer IP'ler, ping) monitörün kalan süresiyle,
// zaman aşımı dolmuşsa en fazla diagGrace kadar ek süreyle sınırlıdır.

// Hata sınıfları (Diag.ErrorClass, DiagAttempt.Result).
const (
	ClassOK          = "ok"
	ClassTimeout     = "timeout"
	ClassRefused     = "refused"
	ClassReset       = "reset"
	ClassUnreachable = "unreachable"
	ClassDNSNotFound = "dns_notfound"
	ClassDNSError    = "dns_error"
	ClassTLS         = "tls_error"
	ClassClosed      = "closed"
	ClassAuth        = "auth"
	ClassNotFound    = "not_found"
	ClassMismatch    = "mismatch"
	ClassProtocol    = "protocol"
	ClassRcode       = "rcode"
	ClassNoRecord    = "no_record"
	ClassNoMessage   = "no_message"
	ClassUnhealthy   = "unhealthy"
	ClassState       = "state"
	ClassConfig      = "config"
	ClassOther       = "other"
)

// Aşamalar (Diag.Phase).
const (
	PhaseDNS       = "dns"
	PhaseConnect   = "connect"
	PhaseTLS       = "tls"
	PhaseGreeting  = "greeting"
	PhaseEHLO      = "ehlo"
	PhaseSTARTTLS  = "starttls"
	PhaseAuth      = "auth"
	PhaseQuery     = "query"
	PhaseSubscribe = "subscribe"
	PhaseHandshake = "handshake"
	PhaseSend      = "send"
	PhaseResponse  = "response"
	PhaseRPC       = "rpc"
	PhaseConfig    = "config"
	PhasePing      = "ping"
)

var (
	diagClasses = codeSet(ClassOK, ClassTimeout, ClassRefused, ClassReset, ClassUnreachable, ClassDNSNotFound,
		ClassDNSError, ClassTLS, ClassClosed, ClassAuth, ClassNotFound, ClassMismatch, ClassProtocol, ClassRcode,
		ClassNoRecord, ClassNoMessage, ClassUnhealthy, ClassState, ClassConfig, ClassOther)
	diagPhases = codeSet(PhaseDNS, PhaseConnect, PhaseTLS, PhaseGreeting, PhaseEHLO, PhaseSTARTTLS, PhaseAuth,
		PhaseQuery, PhaseSubscribe, PhaseHandshake, PhaseSend, PhaseResponse, PhaseRPC, PhaseConfig, PhasePing)
	// netClasses ağ katmanı hataları: bunlarda ek ağ tanısı (çözümleme, IP
	// denemeleri, ping) yapılır.
	netClasses = codeSet(ClassTimeout, ClassRefused, ClassReset, ClassUnreachable, ClassDNSNotFound, ClassDNSError)
)

func codeSet(codes ...string) map[string]bool {
	m := make(map[string]bool, len(codes))
	for _, c := range codes {
		m[c] = true
	}
	return m
}

// Diag HTTP dışı bir kontrolün başarısızlık tanısı.
type Diag struct {
	Target       string        `json:"target,omitempty"`        // host:port, adres veya URI (kimlik bilgisi olmadan)
	Port         int           `json:"port,omitempty"`          // ağ tanısının portu (0: yok)
	Resolved     []string      `json:"resolved,omitempty"`      // çözülen IP'ler
	ResolveMs    int64         `json:"resolve_ms,omitempty"`    // ad çözümleme süresi
	ResolveError string        `json:"resolve_error,omitempty"` // çözümleme hatası (sınıf kodu)
	Attempts     []DiagAttempt `json:"attempts,omitempty"`      // IP başına bağlantı denemeleri
	TimeoutMs    int64         `json:"timeout_ms,omitempty"`    // monitörün zaman aşımı
	ElapsedMs    int64         `json:"elapsed_ms,omitempty"`    // kontrolün başarısız olana dek süresi
	Phase        string        `json:"phase,omitempty"`         // başarısız olunan aşama (Phase*)
	ErrorClass   string        `json:"error_class,omitempty"`   // hata sınıfı (Class*)
	RawError     string        `json:"raw_error,omitempty"`     // ham hata (gizli değerler maskeli)
	Banner       string        `json:"banner,omitempty"`        // sunucunun karşılama satırı (ör. SMTP 220)
	Ping         *DiagPing     `json:"ping,omitempty"`
	DNS          *DiagDNS      `json:"dns,omitempty"`
}

// DiagAttempt tek IP'ye bağlantı denemesi.
type DiagAttempt struct {
	IP        string `json:"ip"`
	Result    string `json:"result"` // ok | refused | timeout | reset | unreachable | dns_error | tls_error | other
	ElapsedMs int64  `json:"elapsed_ms"`
	Error     string `json:"error,omitempty"`
}

// DiagPing tek paketlik (veya ping monitöründe tüm) ICMP yankı sonucu.
type DiagPing struct {
	Target   string  `json:"target"`
	Sent     int     `json:"sent"`
	Received int     `json:"received"`
	LossPct  float64 `json:"loss_pct"`
	MinMs    float64 `json:"min_ms,omitempty"`
	AvgMs    float64 `json:"avg_ms,omitempty"`
	MaxMs    float64 `json:"max_ms,omitempty"`
	Error    string  `json:"error,omitempty"` // unavailable (ICMP izni yok) | failed
	RawError string  `json:"raw_error,omitempty"`
}

// DiagDNS DNS monitörünün sorgusu ve aldığı yanıt.
type DiagDNS struct {
	Server    string   `json:"server"`
	Type      string   `json:"type"`
	Query     string   `json:"query"`
	Transport string   `json:"transport,omitempty"` // udp | tcp
	Rcode     string   `json:"rcode,omitempty"`     // NOERROR, NXDOMAIN, SERVFAIL, REFUSED… ya da "timeout"
	Answers   []string `json:"answers,omitempty"`
	ElapsedMs int64    `json:"elapsed_ms,omitempty"`
}

// Tanı sınırları.
const (
	diagGrace      = 2500 * time.Millisecond // zaman aşımı dolmuşsa ağ tanısına tanınan en fazla ek süre
	diagMaxBudget  = 5 * time.Second         // ağ tanısının en fazla süresi
	diagPingWait   = 2 * time.Second         // tek paketlik ping'in en fazla bekleme süresi
	diagResolveMax = 1500 * time.Millisecond // ad çözümlemenin en fazla süresi
	diagMaxTries   = 4                       // en fazla denenen IP sayısı
	diagMaxIPs     = 8
	diagMaxAnswers = 20
	diagMaxBanner  = 512
	diagMaxMs      = 600_000
)

// sanitize uzak kontrol noktasından gelen tanıyı sınırlar: kodlar izin
// listesine, sayılar makul aralığa, metinler uzunluk sınırına çekilir.
func (g *Diag) sanitize() {
	g.Target = cleanLine(g.Target, detailMaxURL)
	g.Port = clampInt(g.Port, 0, 65535)
	g.Resolved = cleanList(g.Resolved, diagMaxIPs, 64)
	g.ResolveMs = clampMs(g.ResolveMs)
	g.ResolveError = cleanCode(g.ResolveError, diagClasses)
	if len(g.Attempts) > diagMaxIPs {
		g.Attempts = g.Attempts[:diagMaxIPs]
	}
	for i := range g.Attempts {
		a := &g.Attempts[i]
		a.IP = cleanLine(a.IP, 64)
		a.Result = cleanCode(a.Result, diagClasses)
		if a.Result == "" {
			a.Result = ClassOther
		}
		a.ElapsedMs = clampMs(a.ElapsedMs)
		a.Error = cleanLine(a.Error, detailMaxError)
	}
	g.TimeoutMs = clampMs(g.TimeoutMs)
	g.ElapsedMs = clampMs(g.ElapsedMs)
	g.Phase = cleanCode(g.Phase, diagPhases)
	g.ErrorClass = cleanCode(g.ErrorClass, diagClasses)
	g.RawError = cleanLine(g.RawError, detailMaxError)
	g.Banner = cleanBytes(g.Banner, diagMaxBanner)
	if p := g.Ping; p != nil {
		p.Target = cleanLine(p.Target, 64)
		p.Sent = clampInt(p.Sent, 0, 100)
		p.Received = clampInt(p.Received, 0, p.Sent)
		p.LossPct = clampFloat(p.LossPct, 0, 100)
		p.MinMs, p.AvgMs, p.MaxMs = clampFloat(p.MinMs, 0, diagMaxMs), clampFloat(p.AvgMs, 0, diagMaxMs), clampFloat(p.MaxMs, 0, diagMaxMs)
		p.Error = cleanCode(p.Error, codeSet("unavailable", "failed"))
		p.RawError = cleanLine(p.RawError, detailMaxError)
	}
	if d := g.DNS; d != nil {
		d.Server = cleanLine(d.Server, 300)
		d.Type = cleanLine(d.Type, 10)
		d.Query = cleanLine(d.Query, 300)
		d.Transport = cleanCode(d.Transport, codeSet("udp", "tcp"))
		d.Rcode = cleanLine(d.Rcode, 20)
		d.Answers = cleanList(d.Answers, diagMaxAnswers, 300)
		d.ElapsedMs = clampMs(d.ElapsedMs)
	}
}

func cleanCode(s string, allowed map[string]bool) string {
	if s == "" || allowed[s] {
		return s
	}
	return ClassOther
}

// cleanKind monitör tipi adı: küçük harf, rakam ve alt çizgi, en fazla 20 karakter.
func cleanKind(s string) string {
	if len(s) > 20 {
		return ""
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return ""
		}
	}
	return s
}

func cleanList(in []string, maxN, maxLen int) []string {
	if len(in) > maxN {
		in = in[:maxN]
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = cleanLine(s, maxLen); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cleanBytes çok satırlı olabilecek kısa metni (banner) n bayta sınırlar;
// satır sonları boşluğa çevrilir.
func cleanBytes(s string, n int) string {
	s = cleanLine(s, n)
	for len(s) > n {
		s = strings.ToValidUTF8(s[:n], "")
	}
	return s
}

func clampInt(v, lo, hi int) int { return max(lo, min(v, hi)) }
func clampMs(v int64) int64      { return max(0, min(v, diagMaxMs)) }
func clampFloat(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return max(lo, min(v, hi))
}

// Hata sınıflandırma ------------------------------------------------------------------------

// classifyErr hatayı sınıf koduna çevirir. Sürücülerin hataları her zaman
// sarmalanmadığından (ör. MongoDB topoloji hatası) metne de bakılır.
func classifyErr(ctx context.Context, err error) string {
	if err == nil {
		return ""
	}
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var unknownAuth x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var invalidCert x509.CertificateInvalidError
	var recordErr tls.RecordHeaderError
	var alertErr tls.AlertError
	var protoErr *textproto.Error // ör. SMTP 5xx yanıtı
	switch {
	case errors.As(err, &dnsErr):
		if dnsErr.IsNotFound {
			return ClassDNSNotFound
		}
		return ClassDNSError
	case errors.Is(err, syscall.ECONNREFUSED):
		return ClassRefused
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return ClassReset
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH):
		return ClassUnreachable
	case errors.As(err, &certErr), errors.As(err, &unknownAuth), errors.As(err, &hostErr),
		errors.As(err, &invalidCert), errors.As(err, &recordErr), errors.As(err, &alertErr):
		return ClassTLS
	case errors.Is(err, context.DeadlineExceeded), os.IsTimeout(err):
		// Sürücü süre dolana dek yeniden denemiş olabilir (ör. MongoDB sunucu
		// seçimi): metindeki asıl neden daha açıklayıcıdır.
		switch c := classifyText(err.Error()); c {
		case ClassRefused, ClassUnreachable, ClassDNSNotFound, ClassReset, ClassAuth, ClassTLS:
			return c
		}
		return ClassTimeout
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return ClassClosed
	case errors.As(err, &protoErr):
		return ClassProtocol
	}
	if c := classifyText(err.Error()); c != ClassOther {
		return c
	}
	if ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ClassTimeout
	}
	return ClassOther
}

// classifyText sürücü hatalarını metinden sınıflandırır.
func classifyText(s string) string {
	s = strings.ToLower(s)
	has := func(subs ...string) bool {
		for _, sub := range subs {
			if strings.Contains(s, sub) {
				return true
			}
		}
		return false
	}
	switch {
	case has("connection refused", "actively refused"):
		return ClassRefused
	case has("no such host"):
		return ClassDNSNotFound
	case has("no route to host", "network is unreachable", "host is unreachable", "host is down"):
		return ClassUnreachable
	case has("connection reset", "broken pipe", "forcibly closed"):
		return ClassReset
	case has("password authentication failed", "authentication failed", "access denied for user", "login failed",
		"wrongpass", "noauth", "not authorized", "bad user name or password", "unauthenticated", "auth error"):
		return ClassAuth
	case has("x509:", "tls:", "certificate"):
		return ClassTLS
	case has("does not exist", "unknown database", "cannot open database"):
		return ClassNotFound
	case has("i/o timeout", "deadline exceeded", "timed out", "timeout"):
		return ClassTimeout
	case has("eof"):
		return ClassClosed
	}
	return ClassOther
}

// attemptResult deneme sonucu kodu: bağlantı katmanı sınıfları dışındakiler "other".
func attemptResult(class string) string {
	switch class {
	case ClassTimeout, ClassRefused, ClassReset, ClassUnreachable, ClassTLS:
		return class
	case ClassDNSNotFound, ClassDNSError:
		return ClassDNSError
	case "":
		return ClassOK
	}
	return ClassOther
}

// Tanı toplayıcı --------------------------------------------------------------------------

// diagRun bir kontrolün tanısını toplar. Kontrol başında oluşturulur ve
// defer ile attach çağrılır; başarılı kontrolde attach hiçbir şey yapmaz.
//
//	dg := newDiag(ctx, "smtp", raw, addr)
//	defer dg.attach(ctx, &res)
//	…
//	if err != nil { dg.fail(PhaseConnect, err); return down(…) }
type diagRun struct {
	kind   string
	cfg    json.RawMessage
	target string
	start  time.Time

	host    string // ağ tanısının hedefi; boşsa ağ tanısı yapılmaz
	port    int
	tcp     bool  // TCP bağlantı denemeleri yapılsın mı (UDP tiplerinde yalnızca çözümleme + ping)
	dialErr error // kontrolün kendi TCP bağlantısının hatası (ilk deneme olarak kaydedilir)

	phase  string
	class  string
	err    error
	banner string
	ping   *DiagPing
	dns    *DiagDNS
}

func newDiag(kind string, cfg json.RawMessage, target string) *diagRun {
	return &diagRun{kind: kind, cfg: cfg, target: target, start: time.Now()}
}

// network ağ tanısının hedefini ayarlar (tcp: IP başına TCP bağlantı denemesi).
func (d *diagRun) network(host string, port int, tcp bool) *diagRun {
	d.host, d.port, d.tcp = host, port, tcp
	return d
}

// fail başarısız olunan aşamayı ve hatayı kaydeder; sınıf hatadan çıkarılır.
func (d *diagRun) fail(ctx context.Context, phase string, err error) {
	d.phase, d.err = phase, err
	d.class = classifyErr(ctx, err)
}

// failDial kontrolün kendi TCP bağlantısının hatasını kaydeder: ad
// çözülemediyse aşama dns, değilse connect. Hatadaki IP ilk deneme olarak
// tanıya yazılır ve yeniden denenmez.
func (d *diagRun) failDial(ctx context.Context, err error) {
	phase := PhaseConnect
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		phase = PhaseDNS
	}
	d.fail(ctx, phase, err)
	d.dialErr = err
}

// failConn sürücünün bağlanma hatasını (bağlantı + oturum açma birlikte)
// kaydeder; aşama hata sınıfından çıkarılır.
func (d *diagRun) failConn(ctx context.Context, err error) {
	d.fail(ctx, PhaseConnect, err)
	switch d.class {
	case ClassDNSNotFound, ClassDNSError:
		d.phase = PhaseDNS
	case ClassTLS:
		d.phase = PhaseTLS
	case ClassAuth, ClassNotFound:
		d.phase = PhaseAuth
	}
}

// failTLSDial TLS ile bağlanma hatası: TCP bağlantısı kurulamadıysa dns/connect,
// kurulduysa tls aşaması.
func (d *diagRun) failTLSDial(ctx context.Context, err error) {
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		d.failDial(ctx, err)
		return
	}
	d.fail(ctx, PhaseTLS, err)
	if d.class == ClassOther {
		d.class = ClassTLS
	}
}

// failWebSocket el sıkışma hatası: HTTP yanıtı geldiyse handshake aşaması
// (401/403 yetki, 404 bulunamadı), gelmediyse bağlantı hatası.
func (d *diagRun) failWebSocket(ctx context.Context, err error, resp *http.Response) {
	if resp == nil || resp.StatusCode == 0 {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			d.failDial(ctx, err)
			return
		}
		d.failConn(ctx, err)
		return
	}
	d.phase, d.err = PhaseHandshake, err
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		d.class = ClassAuth
	case http.StatusNotFound:
		d.class = ClassNotFound
	default:
		d.class = ClassProtocol
	}
}

// failClass hata nesnesi olmayan başarısızlık (ör. beklenen değer eşleşmedi).
func (d *diagRun) failClass(phase, class string) {
	d.phase, d.class, d.err = phase, class, nil
}

// attach başarısız sonuca tanıyı ekler (başarılı veya zaten ayrıntılı sonuçta
// hiçbir şey yapmaz). Ağ katmanı hatasında ek ağ tanısı yapılır.
func (d *diagRun) attach(ctx context.Context, res *Result) {
	if res.Up || res.Pending || res.Detail != nil {
		return
	}
	g := &Diag{Target: d.target, Phase: d.phase, ErrorClass: d.class, Banner: d.banner, Ping: d.ping, DNS: d.dns,
		ElapsedMs: time.Since(d.start).Milliseconds()}
	if dl, ok := ctx.Deadline(); ok {
		g.TimeoutMs = dl.Sub(d.start).Milliseconds()
	}
	g.RawError = res.Message
	if d.err != nil {
		g.RawError = d.err.Error()
	}
	if g.ErrorClass == "" {
		// Aşaması kaydedilmemiş başarısızlık: sınıf mesajdan çıkarılır.
		g.ErrorClass = classifyText(res.Message)
		if g.ErrorClass == ClassOther && res.Message == "Zaman aşımı" {
			g.ErrorClass = ClassTimeout
		}
	}
	if d.host != "" && netClasses[g.ErrorClass] {
		netDiagnose(ctx, g, d.host, d.port, d.tcp, d.dialErr, d.ping == nil)
	}
	det := &Detail{Kind: d.kind, Diag: g}
	maskDiag(det.Diag, diagSecrets(d.cfg))
	det.Sanitize()
	res.Detail = det
}

// Ağ tanısı ---------------------------------------------------------------------------------

// netDiagnose adı çözer, IP başına TCP bağlantısı dener ve ilk IP'ye tek
// paketlik ping atar. Süre: monitörün kalan süresi (en az diagGrace, en fazla
// diagMaxBudget); üst bağlam iptal edilirse (monitör durduruldu) hemen biter.
// first kontrolün kendi bağlantı hatasıdır: IP'si hatadan okunur ve o IP
// yeniden denenmez.
func netDiagnose(ctx context.Context, g *Diag, host string, port int, tcp bool, first error, ping bool) {
	budget := diagGrace
	if dl, ok := ctx.Deadline(); ok {
		budget = min(max(time.Until(dl), diagGrace), diagMaxBudget)
	}
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), budget)
	defer cancel()
	stop := context.AfterFunc(ctx, func() {
		if errors.Is(ctx.Err(), context.Canceled) {
			cancel()
		}
	})
	defer stop()
	if errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	if tcp && port > 0 {
		g.Port = port
	}
	if g.ErrorClass == ClassDNSNotFound || g.ErrorClass == ClassDNSError {
		// Ad zaten çözülemedi: yeniden denemenin ve ping'in anlamı yok.
		g.ResolveError = g.ErrorClass
		return
	}

	tried := map[string]bool{}
	if first != nil {
		var op *net.OpError
		if errors.As(first, &op) && op.Addr != nil {
			if ta, ok := op.Addr.(*net.TCPAddr); ok {
				ip := ta.IP.String()
				tried[ip] = true
				g.Attempts = append(g.Attempts, DiagAttempt{IP: ip, Result: attemptResult(classifyErr(ctx, first)),
					ElapsedMs: g.ElapsedMs, Error: op.Err.Error()})
			}
		}
	}

	// Ad çözümleme.
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		g.Resolved = []string{ip.String()}
	} else {
		rctx, rcancel := context.WithTimeout(dctx, diagResolveMax)
		t0 := time.Now()
		addrs, err := net.DefaultResolver.LookupIPAddr(rctx, host)
		rcancel()
		g.ResolveMs = max(time.Since(t0).Milliseconds(), 0)
		if err != nil {
			g.ResolveError = classifyErr(rctx, err)
			if g.ResolveError == ClassTimeout {
				g.ResolveError = ClassDNSError
			}
		}
		for _, a := range addrs {
			if len(g.Resolved) < diagMaxIPs {
				g.Resolved = append(g.Resolved, a.IP.String())
			}
		}
	}
	if len(g.Resolved) == 0 {
		for ip := range tried {
			g.Resolved = append(g.Resolved, ip)
		}
	}
	if len(g.Resolved) == 0 {
		return
	}

	// Ping ve diğer IP'lere bağlantı denemeleri birlikte.
	var wg sync.WaitGroup
	var mu sync.Mutex
	if ping {
		wg.Go(func() {
			p := diagPing(dctx, g.Resolved[0], diagPingWait)
			mu.Lock()
			g.Ping = p
			mu.Unlock()
		})
	}
	if tcp && port > 0 {
		n := len(g.Attempts)
		var extra []string
		for _, ip := range g.Resolved {
			if len(tried)+len(extra) >= diagMaxTries {
				break
			}
			if !tried[ip] {
				extra = append(extra, ip)
			}
		}
		results := make([]DiagAttempt, len(extra))
		for i, ip := range extra {
			wg.Go(func() {
				t0 := time.Now()
				a := DiagAttempt{IP: ip}
				conn, err := (&net.Dialer{}).DialContext(dctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
				a.ElapsedMs = max(time.Since(t0).Milliseconds(), 0)
				if err != nil {
					a.Result = attemptResult(classifyErr(dctx, err))
					var op *net.OpError
					if errors.As(err, &op) && op.Err != nil {
						a.Error = op.Err.Error()
					} else {
						a.Error = err.Error()
					}
				} else {
					a.Result = ClassOK
					conn.Close()
				}
				results[i] = a
			})
		}
		wg.Wait()
		g.Attempts = append(g.Attempts[:n], results...)
		return
	}
	wg.Wait()
}

// diagPing hedefe tek ICMP yankı paketi gönderir (ping monitörüyle aynı yöntem:
// yetkisiz ICMP; Windows'ta ham soket). ICMP izni yoksa hata "unavailable" olur.
func diagPing(ctx context.Context, ip string, wait time.Duration) *DiagPing {
	dp := &DiagPing{Target: ip}
	if dl, ok := ctx.Deadline(); ok {
		wait = min(wait, time.Until(dl))
	}
	if wait <= 0 {
		dp.Error = "failed"
		return dp
	}
	p, err := probing.NewPinger(ip)
	if err != nil {
		dp.Error, dp.RawError = "failed", err.Error()
		return dp
	}
	p.SetPrivileged(runtime.GOOS == "windows")
	p.Count = 1
	p.Timeout = wait
	if err := p.RunWithContext(ctx); err != nil && ctx.Err() == nil {
		dp.RawError = err.Error()
		dp.Error = "failed"
		if pingUnavailable(err) {
			dp.Error = "unavailable"
		}
		return dp
	}
	fillPing(dp, p.Statistics())
	return dp
}

func pingUnavailable(err error) bool {
	s := strings.ToLower(err.Error())
	return errors.Is(err, os.ErrPermission) || strings.Contains(s, "permission denied") ||
		strings.Contains(s, "operation not permitted") || strings.Contains(s, "protocol not supported") ||
		strings.Contains(s, "access is denied") || strings.Contains(s, "address family not supported")
}

func fillPing(dp *DiagPing, st *probing.Statistics) {
	if st == nil {
		return
	}
	dp.Sent, dp.Received = st.PacketsSent, st.PacketsRecv
	dp.LossPct = math.Round(st.PacketLoss*10) / 10
	if dp.Sent > 0 && dp.Received == 0 {
		dp.LossPct = 100
	}
	if dp.Received > 0 {
		dp.MinMs, dp.AvgMs, dp.MaxMs = durMs(st.MinRtt), durMs(st.AvgRtt), durMs(st.MaxRtt)
	}
}

// durMs süreyi 0,01 ms hassasiyetle milisaniyeye çevirir.
func durMs(d time.Duration) float64 {
	return math.Round(float64(d.Microseconds())/10) / 100
}

// Gizli değerler --------------------------------------------------------------------------

// diagSecrets monitör ayarındaki gizli değerler: şifreler, SNMP community ve
// parolaları, URI/adreslerdeki şifreler, başlık/metadata değerleri. Tipten
// bağımsız alan adlarıyla toplanır (yeni tipler de kapsanır).
func diagSecrets(cfg json.RawMessage) []string {
	var m map[string]any
	if json.Unmarshal(cfg, &m) != nil {
		return nil
	}
	var out []string
	for k, v := range m {
		s, ok := v.(string)
		if !ok || s == "" {
			continue
		}
		switch k {
		case "password", "community", "auth_password", "priv_password", "basic_pass", "proxy_pass", "oauth_client_secret":
			out = append(out, s)
		case "uri", "url", "broker_url", "endpoint", "proxy_url":
			if u, err := url.Parse(s); err == nil && u.User != nil {
				if p, ok := u.User.Password(); ok {
					out = append(out, p, url.QueryEscape(p), url.PathEscape(p))
				}
			}
		case "headers", "metadata":
			if hs, err := parseHeaders(s); err == nil {
				for _, h := range hs {
					if safeHeaders[strings.ToLower(h[0])] {
						continue
					}
					out = append(out, h[1])
					if f := strings.Fields(h[1]); len(f) > 1 {
						out = append(out, f[len(f)-1])
					}
				}
			}
		}
	}
	return usableSecrets(out)
}

// maskDiag tanıdaki metinlerde gizli değerleri maskeler; adreslerdeki
// kullanıcı şifresini gizler.
func maskDiag(g *Diag, secrets []string) {
	if g == nil {
		return
	}
	redact := func(s string) string {
		for _, sec := range secrets {
			s = strings.ReplaceAll(s, sec, detailMaskedValue)
		}
		return redactURLsIn(s)
	}
	g.Target = redact(g.Target)
	g.RawError = redact(g.RawError)
	g.Banner = redact(g.Banner)
	for i := range g.Attempts {
		g.Attempts[i].Error = redact(g.Attempts[i].Error)
	}
	if g.Ping != nil {
		g.Ping.RawError = redact(g.Ping.RawError)
	}
}

// redactURLsIn metin içindeki "şema://kullanıcı:şifre@" biçimli adreslerin
// şifresini gizler (sürücü hataları bağlantı adresini içerebilir).
func redactURLsIn(s string) string {
	if !strings.Contains(s, "://") {
		return s
	}
	return urlPassRe.ReplaceAllString(s, "${1}"+detailMaskedValue+"@")
}

var urlPassRe = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://[^\s:/@"'<>]*:)[^\s/@"'<>]*@`)

// hostPortOf "host:port" metnini ayırır (port yoksa def).
func hostPortOf(addr string, def int) (string, int) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return strings.Trim(addr, "[]"), def
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return h, def
	}
	return h, n
}

// urlHostPort adresin sunucusunu ve portunu döner (port yoksa şemaya göre).
func urlHostPort(raw string, defaults map[string]int) (string, int) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", 0
	}
	port := defaults[u.Scheme]
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	return u.Hostname(), port
}
