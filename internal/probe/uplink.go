package probe

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// OOB hat yedeği (463). Ajanın Main'e giden bağlantıları (iş listesi, sonuç,
// kalp atışı, metrik, güncelleme) etkin hattın dialer'ını kullanır; denetimler
// (check/*) ve mesh OS yönlendirmesinde kalır.
//
// Geçiş: ana hattan Main'e art arda taşıma hatası (bağlantı kurulamadı, zaman
// aşımı; 4xx/5xx sayılmaz) >= kopukluk eşiği (panelin LossThreshold'u, vars. 3)
// ve OOB'den GET /healthz başarılı -> OOB. Mesh varsa ve eşlerden en az birine
// ana hattan erişim varsa geçilmez (sorun Main tarafında, hat sağlam).
// Dönüş: OOB'de en az OOBMinStay (vars. 5 dk) + ana hattan art arda 3 başarılı
// /healthz. OOB'nin kendisi de koparsa (art arda eşik kadar hata) bekleme
// süresi aranmaz, ana hattan tek başarılı /healthz yeter.
// Geçişte açık bağlantılar kesilir (hat bağlamı iptal + CloseIdleConnections):
// uzun yoklama yeni hattan hemen yeniden kurulur. Etkin hat her istekte
// X-Probe-Uplink başlığıyla panele bildirilir; bildirimi panel gönderir.

// uplinkRestoreOK dönüş için ana hattan art arda başarılı /healthz sayısı.
const uplinkRestoreOK = 3

type dialFunc = func(ctx context.Context, network, addr string) (net.Conn, error)

// uplink hat durumu. Sıfır değeri OOB'siz çalışır (başlık yok, geçiş yok).
type uplink struct {
	onOOB   atomic.Bool
	lossThr atomic.Int64 // panelin kopukluk eşiği (kalp atışı zamanlaması); 0 = 3

	mu       sync.Mutex
	cfg      OOBConfig
	override string // PROBE_OOB_IFACE
	bind     oobBind
	bindErr  error // cfg.IP != "" iken arayüz bulunamadı
	fails    int   // etkin hatta art arda taşıma hatası
	since    time.Time
	okStreak int // dönüş denemesinde art arda başarı
	peerNote bool
	lineCtx  context.Context
	lineStop context.CancelFunc

	// Bağlantı kurucular (newUplinkHTTP doldurur; nil: geçiş yapılamaz, ör. testte verilen HTTPClient).
	primary dialFunc
	oobDial dialFunc // bind'e göre (yeniden) kurulur
	base    *net.Dialer
	wrap    func(*net.Dialer) dialFunc // DNS önbellekli dialer sarmalayıcı
	tr      *http.Transport

	// Testte değiştirilebilir.
	resolve func(ip, override string) (oobBind, error)
}

func newUplink(override string) *uplink {
	u := &uplink{override: override, resolve: resolveOOB}
	u.lineCtx, u.lineStop = context.WithCancel(context.Background())
	return u
}

// dial Transport.DialContext: etkin hattın dialer'ı.
func (u *uplink) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if u.onOOB.Load() {
		u.mu.Lock()
		d := u.oobDial
		u.mu.Unlock()
		if d != nil {
			return d(ctx, network, addr)
		}
	}
	return u.primary(ctx, network, addr)
}

// setConfig panelden gelen OOB ayarını uygular; değiştiyse arayüzü yeniden çözer.
// Değişiklik ya da arayüz sonucu loglanır.
func (u *uplink) setConfig(cfg OOBConfig, log func(msg string, args ...any)) {
	u.mu.Lock()
	if cfg == u.cfg {
		u.mu.Unlock()
		return
	}
	u.cfg = cfg
	u.bind, u.bindErr, u.oobDial = oobBind{}, nil, nil
	if cfg.IP != "" {
		u.bind, u.bindErr = u.resolve(cfg.IP, u.override)
		if u.bindErr == nil && u.wrap != nil {
			u.oobDial = u.wrap(u.bind.dialer(u.base))
		}
	}
	bind, err := u.bind, u.bindErr
	u.mu.Unlock()
	switch {
	case cfg.IP == "":
		log("OOB hattı tanımlı değil")
		if u.onOOB.Load() {
			u.switchTo(false, time.Now())
		}
	case err != nil:
		log("OOB arayüzü bulunamadı; OOB'ye geçilemez", "oob_ip", cfg.IP, "hata", err)
	default:
		log("OOB hattı hazır", "oob_ip", cfg.IP, "arayüz", bind.Iface, "kaynak", bind.Local.String(), "ad", cfg.Name)
	}
}

// headers etkin hat başlıkları (OOB tanımlı değilse yok).
func (u *uplink) headers(h http.Header) {
	u.mu.Lock()
	ip, missing := u.cfg.IP, u.bindErr != nil
	u.mu.Unlock()
	if ip == "" {
		return
	}
	if u.onOOB.Load() {
		h.Set(HeaderUplink, "oob")
	} else {
		h.Set(HeaderUplink, "primary")
	}
	if missing {
		h.Set(HeaderOOBIface, "missing")
	} else {
		h.Set(HeaderOOBIface, "ok")
	}
}

// request isteği hat bağlamına bağlar: hat değişince istek iptal olur. Dönen
// gen, sonuç bildirilirken (observe) aynı hatta mı kalındığını gösterir.
func (u *uplink) request(ctx context.Context) (context.Context, context.Context, func()) {
	u.mu.Lock()
	lc := u.lineCtx
	u.mu.Unlock()
	rctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(lc, cancel)
	return rctx, lc, func() { stop(); cancel() }
}

// observe bir Main isteğinin taşıma sonucunu sayar. lc isteğin başladığı hat
// bağlamı: arada hat değiştiyse (iptal) sayılmaz.
func (u *uplink) observe(lc context.Context, transportErr bool) {
	if lc.Err() != nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if transportErr {
		u.fails++
	} else {
		u.fails = 0
	}
}

// peersUp ana hattan en az bir eşe erişim var mı (mesh ölçütü).
func peersUp(src MeshSource) bool {
	if src == nil {
		return false
	}
	for _, l := range src.Report() {
		if l.State == "up" {
			return true
		}
	}
	return false
}

// tick bir değerlendirme turu. healthz(oob) ilgili hattan GET /healthz yapar.
// Dönen değer: hat değişti mi.
func (u *uplink) tick(ctx context.Context, now time.Time, threshold int, minStay time.Duration, peers bool,
	healthz func(ctx context.Context, oob bool) error, log func(msg string, args ...any)) bool {
	u.mu.Lock()
	ready := u.cfg.IP != "" && u.bindErr == nil && u.oobDial != nil
	fails, since := u.fails, u.since
	u.mu.Unlock()
	if threshold <= 0 {
		threshold = 3
	}
	if !u.onOOB.Load() {
		if !ready || fails < threshold {
			u.setPeerNote(false)
			return false
		}
		if peers {
			if !u.setPeerNote(true) {
				log("ana hattan Main'e ulaşılamıyor ama eşlere ulaşılıyor; sorun Main tarafında, OOB'ye geçilmiyor", "hata_sayisi", fails)
			}
			return false
		}
		if err := healthz(ctx, true); err != nil {
			log("ana hat kopuk, OOB'den de Main'e ulaşılamadı", "hata", err)
			return false
		}
		u.switchTo(true, now)
		log("ana hattan Main'e ulaşılamıyor; OOB hattına geçildi", "hat", u.name(), "hata_sayisi", fails)
		return true
	}
	oobBroken := fails >= threshold
	if !oobBroken && now.Sub(since) < minStay {
		return false
	}
	if err := healthz(ctx, false); err != nil {
		u.mu.Lock()
		u.okStreak = 0
		u.mu.Unlock()
		return false
	}
	u.mu.Lock()
	u.okStreak++
	n := u.okStreak
	u.mu.Unlock()
	if n < uplinkRestoreOK && !oobBroken {
		return false
	}
	u.switchTo(false, now)
	log("ana hatta dönüldü", "oob_suresi", now.Sub(since).Round(time.Second).String())
	return true
}

func (u *uplink) setPeerNote(on bool) (was bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	was, u.peerNote = u.peerNote, on
	return was
}

func (u *uplink) name() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.cfg.Name != "" {
		return u.cfg.Name
	}
	return "OOB"
}

// switchTo hattı değiştirir: sayaçlar sıfırlanır, eski hattaki istekler iptal
// edilir, boştaki bağlantılar kapatılır.
func (u *uplink) switchTo(oob bool, now time.Time) {
	u.mu.Lock()
	u.onOOB.Store(oob)
	u.fails, u.okStreak, u.since = 0, 0, now
	u.lineStop()
	u.lineCtx, u.lineStop = context.WithCancel(context.Background())
	tr := u.tr
	u.mu.Unlock()
	if tr != nil {
		tr.CloseIdleConnections()
	}
}

// healthzClient tek hattan /healthz için istemci (TLS ayarları ana Transport'tan).
func (u *uplink) healthzClient(oob bool) *http.Client {
	u.mu.Lock()
	d := u.primary
	if oob {
		d = u.oobDial
	}
	tr := u.tr
	u.mu.Unlock()
	if d == nil || tr == nil {
		return nil
	}
	t := tr.Clone()
	t.DialContext = d
	t.DisableKeepAlives = true
	return &http.Client{Transport: t, Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// oobLog hat olaylarının logu.
func (c *Client) oobLog(msg string, args ...any) { c.log.Info(msg, args...) }

// uplinkLoop her panel aralığında hattı değerlendirir.
func (c *Client) uplinkLoop(ctx context.Context) {
	if c.up.wrap == nil {
		return // verilen HTTPClient: hat geçişi yok
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(c.panelEvery()):
		}
		peers := peersUp(c.cfg.MeshSource)
		c.up.tick(ctx, time.Now(), int(c.up.lossThr.Load()), c.cfg.OOBMinStay, peers, c.healthz, c.oobLog)
	}
}

// healthz belirtilen hattan Main'in /healthz ucunu dener (yalnız 2xx başarı).
func (c *Client) healthz(ctx context.Context, oob bool) error {
	hc := c.up.healthzClient(oob)
	if hc == nil {
		return errNoLine
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.String()+"/healthz", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "uptime-probe/"+c.cfg.Version)
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return &statusError{resp.StatusCode}
	}
	return nil
}

var errNoLine = errors.New("hat hazır değil")
