package engine

import (
	"context"
	"math"
	"time"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/schedule"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// unknown: monitörün onaylanmış (UP/DOWN) bir durumu henüz yok.
const unknown = -1

type runner struct {
	e       *Engine
	m       store.Monitor
	checker check.Checker
	cancel  context.CancelFunc
	done    chan struct{}
	pushCh  chan check.Result

	retries   int // art arda başarısız deneme (PENDING'de)
	confirmed int // son onaylanmış durum: StatusUp, StatusDown veya unknown
	downBeats int // DOWN'dayken art arda kontrol sayısı (hatırlatma için)

	locs *locationSet // nil: tek konumlu (yalnızca ana sunucu); bkz. locations.go
	plan schedule.Planner

	// Olay ayrıntıları (incidents.go).
	incidentID  int64                 // açık olayın kimliği (0: yok)
	preDown     []store.IncidentEvent // olay açılmadan önceki başarısız denemeler
	lastCause   string                // işlem geçmişine yazılan son hata
	maintLogged bool                  // olay sürerken "bakım başladı" yazıldı
	locPrev     map[int64]locMark     // çok konumlu: olay sürerken konumların son yazılan durumu
	locLearn    bool                  // yeniden başlatıldı: açık olayda konumların ilk sonuçları yalnızca öğrenilir

	// Kısmi kesinti (partial.go; yalnızca çok konumlu).
	partialID     int64             // açık kısmi kesinti olayı (0: yok)
	partialPrev   map[int64]locMark // kısmi olayda konumların son yazılan durumu
	partialFailed []string          // kısmi olay boyunca çalışmayan konumlar (olay verisi)
	partialLearn  bool              // yeniden başlatıldı: konumların ilk sonuçları yalnızca öğrenilir
	// Tam kesintiye dönüşerek kapanan kısmi olay: tam kesinti bitince konum
	// hâlâ çalışmıyorsa aynı olay yeniden açılır (yeni 🟡 gitmez, geçmiş bölünmez).
	escalatedPartial int64
	resolvedID       int64 // bu sonuçta kapanan normal olay (partialStep bağlantı için)

	// Monitör UP'a döndüğü halde veritabanında kapatılamamış olay: sonraki
	// her sonuçta (yeni olay açılmadan önce) yeniden kapatılmaya çalışılır.
	unresolved *pendingResolve

	// Yavaş yanıt (slow.go): son başarılı kontrollerin yanıt süreleri (en çok
	// SlowChecks) ve açık yavaş yanıt olayı.
	slowPings []int64
	slowID    int64
}

// pendingResolve kapatılamamış olayın kimliği ve çözülme anı.
type pendingResolve struct {
	id  int64
	at  time.Time
	msg string
}

// initialConfirmed yeniden başlatmada sahte bildirim gitmesin diye son
// onaylanmış durumu veritabanından çıkarır. Kayıtlı durum UP/DOWN ise o
// geçerlidir; bekliyorsa (tekrar deneniyordu) açık olay DOWN'u, daha önce
// kaydedilmiş bir değişim UP'ı gösterir; hiçbiri yoksa bilinmiyor.
//
// DOWN kaydedilmiş ama olayı açılamamışsa (ör. yazım yarıda kaldıysa) olay
// burada tamamlanır; böylece kesinti süresi düzelince doğru hesaplanır.
func (r *runner) initialConfirmed(ctx context.Context) int {
	r.restorePartial(ctx)
	r.restoreSlow(ctx)
	started, err := r.e.store.OpenIncidentStart(ctx, r.m.ID)
	hasIncident := err == nil && started > 0
	if hasIncident {
		r.incidentID, _ = r.e.store.OpenIncidentID(ctx, r.m.ID)
		r.lastCause = r.m.LastMessage
		r.locPrev, r.locLearn = nil, true
		// Bakım kaydı yeniden başlatmadan önce yazıldıysa (bitişi yazılmamış
		// başlangıç) ikinci kez yazılmaz; bakım bitince bitişi yazılır.
		r.maintLogged, _ = r.e.store.InMaintLogged(ctx, r.incidentID)
		// Olay bir konum kesintisinden dönüştüyse, tam kesinti bitince konum
		// hâlâ çalışmıyorsa o kısmi olay yeniden açılır (yeniden başlatma
		// bu bağı unutturmasın).
		r.escalatedPartial, _ = r.e.store.EscalatedPartialOf(ctx, r.incidentID)
		// Hatırlatma sayacı olay boyunca kaydedilen DOWN kontrollerinden kurulur
		// (olayı açan ilk kontrol sayılmaz; bkz. process): yeniden başlatma
		// hatırlatma sıklığını sıfırlamaz.
		if n, err := r.e.store.CountBeats(ctx, r.m.ID, store.StatusDown, started); err == nil && n > 0 {
			r.downBeats = n - 1
		}
	}
	switch {
	case r.m.Status == store.StatusDown:
		if err == nil && !hasIncident {
			since := r.m.LastChangeAt
			if since == 0 {
				since = r.m.LastCheckAt
			}
			if since == 0 {
				since = r.e.now().Unix()
			}
			id, err := r.e.store.StartIncident(ctx, r.m.ID, since, r.m.LastMessage)
			if err != nil {
				r.e.log.Error("eksik olay tamamlanamadı", "monitor", r.m.Name, "hata", err)
			}
			r.incidentID, r.lastCause = id, r.m.LastMessage
		}
		return store.StatusDown
	case r.m.Status == store.StatusUp:
		if hasIncident {
			// UP kaydedilmiş ama olay kapatılamamış (UP yazıldıktan sonra çökme
			// veya kapatma hatası): olay UP'a dönüş anında kapatılır; yoksa açık
			// kalır ve sonraki kesinti yeni olay açamaz.
			at := time.Unix(r.m.LastChangeAt, 0)
			if r.m.LastChangeAt < started {
				at = r.e.now()
			}
			r.unresolved = &pendingResolve{id: r.incidentID, at: at, msg: r.m.LastMessage}
			r.incidentID, r.lastCause = 0, ""
			r.retryResolve(ctx)
		}
		return store.StatusUp
	case hasIncident:
		return store.StatusDown
	case r.m.LastChangeAt > 0:
		return store.StatusUp
	}
	return unknown
}

func (r *runner) unit(n int) time.Duration { return time.Duration(n) * r.e.cfg.Unit }

// nextDelay sıradaki kontrole kadar bekleme. Kontroller monitöre özgü ortak
// zaman ızgarasına oturur (bkz. schedule): kontrol noktaları da aynı ızgarayı
// kullandığından tüm konumlar aynı anda kontrol eder. ran: kontrol az önce
// yapıldı; false: zamanlayıcı yalnızca yeniden kuruluyor.
func (r *runner) nextDelay(ran bool) time.Duration {
	every := r.unit(r.m.Interval)
	if r.retrying() && r.m.RetryInterval > 0 {
		every = r.unit(r.m.RetryInterval)
	}
	if r.m.Type == check.TypePush {
		// Push: süre son sinyalden itibaren sayılır, ızgaraya bağlanmaz;
		// tolerans (grace_sec) beklenen aralığın üstüne eklenir.
		return every + r.pushGrace()
	}
	return r.plan.Delay(time.Now(), every, ran)
}

// pushGrace push monitörünün toleransı (aralık birimi cinsinden): sinyal
// beklenen aralık + tolerans geçmeden "sinyal gelmedi" sayılmaz.
func (r *runner) pushGrace() time.Duration { return r.unit(check.PushGrace(r.m.Config)) }

// retrying tekrar deneme aralığında mı kontrol edilmeli: genel durum
// "tekrar deneniyor" ya da (çok konumlu) ana sunucunun kendi kontrolü
// başarısız ve deneme hakkı bitmemiş. İkincisi, genel durumun "çalışıyor"
// kaldığı kurallarda (ör. "tüm konumlar çalışmıyorsa") da ana sunucunun
// tekrar denemelerini tekrar deneme aralığında yapar.
func (r *runner) retrying() bool {
	if r.m.Status == store.StatusPending && r.m.LastCheckAt > 0 {
		return true
	}
	if r.locs == nil || r.locs.local == nil {
		return false
	}
	l := r.locs.local
	return l.have && !l.res.Up && !l.res.Pending && l.fails > 0 && l.fails <= r.rules().maxRetries
}

func (r *runner) loop(ctx context.Context) {
	defer close(r.done)
	first := r.e.jitter(r.unit(r.m.Interval))
	if r.m.Type == check.TypePush {
		// Push'ta ilk kontrol şimdiden tam bir aralık (+ tolerans) sonra: uygulama
		// kapalıyken veya monitör durdurulmuşken gelemeyen sinyaller yüzünden sahte
		// DOWN olmasın.
		first = r.unit(r.m.Interval) + r.pushGrace()
	}
	timer := time.NewTimer(first)
	defer timer.Stop()
	var wake <-chan struct{} // tek konumlu monitörde nil: hiç tetiklenmez
	if r.locs != nil {
		wake = r.locs.wake
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			if r.locationWake() {
				resetTimer(timer, r.nextDelay(false))
			}
		case res := <-r.pushCh:
			r.process(res)
			resetTimer(timer, r.nextDelay(true))
		case <-timer.C:
			if r.locs != nil {
				if !r.locationTick(ctx) {
					return
				}
				timer.Reset(r.nextDelay(true))
				continue
			}
			var res check.Result
			if r.m.Type == check.TypePush {
				res = r.checker.Check(ctx, r.m.Config) // "sinyal gelmedi"
			} else {
				res = r.runCheck(ctx)
			}
			if ctx.Err() != nil {
				return // durduruldu: yarım kalan kontrol kaydedilmez
			}
			r.process(res)
			timer.Reset(r.nextDelay(true))
		}
	}
}

func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

func (r *runner) runCheck(ctx context.Context) (res check.Result) {
	select {
	case r.e.sem <- struct{}{}:
	case <-ctx.Done():
		return check.Result{PingMs: -1}
	}
	defer func() { <-r.e.sem }()
	defer func() {
		if p := recover(); p != nil {
			r.e.log.Error("kontrol paniği", "monitor", r.m.Name, "panik", p)
			res = check.Result{PingMs: -1, Message: "İç hata"}
		}
	}()
	cctx, cancel := context.WithTimeout(ctx, r.unit(r.m.Timeout))
	defer cancel()
	return r.checker.Check(cctx, r.m.Config)
}

// process bir sonucu durum makinesinden geçirir, kaydeder ve gerekirse bildirir.
//
// Veritabanı işlemleri runner'ın context'ini değil kendi kısa süreli
// context'ini kullanır: monitör düzenlenirken veya uygulama kapanırken
// kontrol sonucu, olay ve durum yarım yazılmış halde kalmasın.
func (r *runner) process(res check.Result) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := r.e.now()
	// Çok konumlu monitörde ters mod ve tekrar deneme konum başına uygulanmıştır.
	if r.m.UpsideDown && !res.Pending && r.locs == nil {
		res = invertUpsideDown(res)
	}

	// Bakım penceresinde kontrol yine yapılır ama sonuç MAINTENANCE olarak
	// kaydedilir: onaylanmış durum (confirmed) değişmez, olay açılmaz/kapanmaz,
	// bildirim gitmez. Pencere bitince ilk kontrol normal değerlendirilir.
	inMaint := r.e.InMaintenance(r.m.ID, now)

	status := store.StatusDown
	retried := false // olay açılmadan önceki bir tekrar deneme hakkı kullanıldı
	prevRetries := r.retries
	switch {
	case inMaint:
		status = store.StatusMaintenance
		if res.Message != "" {
			res.Message = "Bakımda (" + res.Message + ")"
		} else {
			res.Message = "Bakımda"
		}
	case res.Up:
		status = store.StatusUp
	case res.Pending:
		status = store.StatusPending // belirsiz sonuç: tekrar deneme hakkı harcanmaz
	case r.locs == nil && r.confirmed != store.StatusDown && r.retries < r.m.MaxRetries:
		status = store.StatusPending
		r.retries++
		retried = true
	}
	if status != store.StatusPending {
		r.retries = 0
	}

	var lastChange int64
	prevConfirmed := r.confirmed
	if status != store.StatusPending && status != store.StatusMaintenance && status != prevConfirmed {
		lastChange = now.Unix()
		r.confirmed = status
	}

	err := r.e.store.RecordBeat(ctx, store.BeatUpdate{
		Beat:         store.Beat{MonitorID: r.m.ID, Time: now.Unix(), Status: status, PingMs: res.PingMs, Message: res.Message},
		LastChangeAt: lastChange,
	})
	if err != nil {
		// Kaydedilemeyen sonuç için bildirim gönderilmez; durum bellekte de
		// değişmez (deneme sayacı dahil: kaydedilmeyen deneme hak düşürmez),
		// bir sonraki kontrol aynı geçişi tekrar dener.
		r.e.log.Error("kontrol sonucu kaydedilemedi", "monitor", r.m.Name, "hata", err)
		r.confirmed, r.retries = prevConfirmed, prevRetries
		return
	}
	r.m.Status, r.m.LastCheckAt, r.m.LastMessage = status, now.Unix(), res.Message
	r.m.LastPingMs = res.PingMs
	if lastChange > 0 {
		r.m.LastChangeAt = lastChange
	}
	// Önceki UP'ta kapatılamayan olay, yeni bir olay açılmadan önce kapatılır.
	r.retryResolve(ctx)

	// Olay açılmadan önceki denemeler bellekte bekler (çok konumluda tekrar
	// deneme konum başınadır; birleşik "tekrar deneniyor" sonucu kaydedilir).
	switch {
	case status == store.StatusUp:
		r.preDown = nil
	case r.incidentID == 0 && prevConfirmed != store.StatusDown &&
		(retried || (r.locs != nil && status == store.StatusPending && res.Message != NoLocationData)):
		r.rememberRetry(now, res)
	}
	resolving := status == store.StatusUp && prevConfirmed == store.StatusDown
	if !resolving {
		r.incidentProgress(ctx, now, status, inMaint, res)
	}

	switch {
	case status == store.StatusDown && prevConfirmed != store.StatusDown:
		r.downBeats = 0
		r.openIncident(ctx, now, res)
		r.e.log.Warn("monitör DOWN", "monitor", r.m.Name, "neden", res.Message)
		r.notify(notify.KindDown, now, res.Message, 0)

	case resolving:
		// Olay önce kapatılır (kaydedilen UP durumu ile açık olay arasındaki
		// pencere kısa kalsın); son değişimler ve çözülme kaydı sonra yazılır.
		started, err := r.e.store.ResolveIncident(ctx, r.m.ID, now.Unix())
		if err != nil {
			// Olay açık kalmasın: sonraki sonuçlarda yeniden denenir (retryResolve).
			r.e.log.Error("olay kapatılamadı, sonraki kontrolde yeniden denenecek", "monitor", r.m.Name, "hata", err)
		}
		r.incidentProgress(ctx, now, status, inMaint, res)
		var downtime time.Duration
		if started > 0 {
			downtime = now.Sub(time.Unix(started, 0))
		}
		if err != nil {
			r.unresolved = &pendingResolve{id: r.incidentID, at: now, msg: res.Message}
		} else {
			r.resolveEvent(ctx, r.incidentID, now, res.Message, downtime)
		}
		r.e.log.Info("monitör tekrar UP", "monitor", r.m.Name, "kesinti", downtime.Round(time.Second))
		r.notify(notify.KindUp, now, res.Message, downtime)
		r.resolvedID = r.incidentID
		r.incidentID, r.locPrev, r.maintLogged, r.locLearn = 0, nil, false, false

	case status == store.StatusDown:
		r.downBeats++
		if r.m.ResendEvery > 0 && r.downBeats%r.m.ResendEvery == 0 {
			started, _ := r.e.store.OpenIncidentStart(ctx, r.m.ID)
			var downtime time.Duration
			if started > 0 {
				downtime = now.Sub(time.Unix(started, 0))
			}
			r.addEvents(ctx, r.incidentID, store.IncidentEvent{Time: now.Unix(), Kind: store.EventReminder,
				Message: "Hatırlatma bildirimi", Data: store.EventData(map[string]int64{"downtime": int64(downtime / time.Second)})})
			r.notify(notify.KindReminder, now, res.Message, downtime)
		}
	}

	// Kısmi kesinti (çok konumlu): normal olay açılıp kapandıktan sonra.
	r.partialStep(ctx, now, status, inMaint)
	// Yavaş yanıt: başarılı kontrollerin ortalaması eşiğe göre.
	r.slowStep(ctx, now, status, inMaint, res)

	// Sertifika döndüren her tip (http, grpc, smtp, websocket, tlscert…) için;
	// bakımda SSL uyarısı gönderilmez.
	if res.Cert != nil && !inMaint {
		r.handleCert(ctx, now, res.Cert)
	}

	r.e.hub.Publish("beat", map[string]any{
		"monitor_id": r.m.ID, "status": status, "time": now.Unix(), "ping": res.PingMs,
		"message": res.Message, "last_change_at": r.m.LastChangeAt, "cert_expires_at": r.m.CertExpiresAt,
		"slow": r.m.Slow,
	})
}

func (r *runner) notify(kind string, now time.Time, msg string, downtime time.Duration) {
	ev := notify.Event{
		Kind: kind, MonitorID: r.m.ID, MonitorName: r.m.Name, MonitorType: r.m.Type,
		Target: r.checker.Target(r.m.Config), Message: msg, Time: now, Downtime: downtime,
		URL: r.e.MonitorURL(r.m.ID),
	}
	if r.incidentID != 0 {
		// "Detay" bağlantısı olay sayfasına gider; gönderim sonuçları olaya yazılır.
		ev.IncidentID, ev.IncidentURL = r.incidentID, r.e.IncidentURL(r.incidentID)
	}
	switch kind {
	case notify.KindDown, notify.KindReminder:
		ev.Locations = r.locationNotes(now)
	case notify.KindUp:
		// Düzelme bildiriminde hâlâ çalışmayan konumlar ayrı satırda: tam
		// kesinti bitti ama konum kesintisi sürüyorsa ikinci bir 🟡 gitmez,
		// bilgi bu bildirimde verilir.
		ev.Locations = r.downLocationNotes(now)
	}
	r.e.notifier.Notify(ev)
}

// downLocationNotes çok konumlu monitörde deneme hakkı bitmiş (çalışmıyor)
// konumlar; tek konumluda veya hepsi çalışıyorsa nil.
func (r *runner) downLocationNotes(now time.Time) []notify.LocationNote {
	if r.locs == nil {
		return nil
	}
	var out []notify.LocationNote
	for _, s := range r.locs.statuses(now, r.rules()) {
		if s.Status == locDown {
			out = append(out, notify.LocationNote{Name: s.Name, Message: s.Message})
		}
	}
	return out
}

// notifyPartial konum kesintisi bildirimi (yalnızca monitörde açıksa); olay
// kimliği kısmi olayınki: gönderim sonuçları onun işlem geçmişine yazılır.
func (r *runner) notifyPartial(kind string, now time.Time, incidentID int64, msg string, downtime time.Duration) {
	if r.locs == nil || !r.locs.notify || incidentID == 0 {
		return
	}
	ev := notify.Event{
		Kind: kind, MonitorID: r.m.ID, MonitorName: r.m.Name, MonitorType: r.m.Type,
		Target: r.checker.Target(r.m.Config), Message: msg, Time: now, Downtime: downtime,
		URL: r.e.MonitorURL(r.m.ID), IncidentID: incidentID, IncidentURL: r.e.IncidentURL(incidentID),
	}
	if kind == notify.KindLocationDown {
		ev.Locations = r.locationNotes(now)
	}
	r.e.notifier.Notify(ev)
}

// locationNotes çok konumlu monitörde çalışmayan ve ulaşılamayan konumlar
// (bildirimde ayrı satırlar); tek konumluda nil.
func (r *runner) locationNotes(now time.Time) []notify.LocationNote {
	if r.locs == nil {
		return nil
	}
	var out []notify.LocationNote
	for _, s := range r.locs.statuses(now, r.rules()) {
		switch s.Status {
		case locDown, locRetrying:
			out = append(out, notify.LocationNote{Name: s.Name, Message: s.Message})
		case locUnknown:
			out = append(out, notify.LocationNote{Name: s.Name, NoData: true})
		}
	}
	return out
}

// invertUpsideDown ters modu uygular: sonuç tersine çevrilir, mesaj neden
// çalışıyor/çalışmıyor sayıldığını söyler. Hedefe ulaşılamadığı için
// "çalışıyor" sayılan sonuçta ham hata ("Bağlantı reddedildi") tek başına
// yanıltıcı olurdu.
func invertUpsideDown(res check.Result) check.Result {
	res.Up = !res.Up
	switch {
	case !res.Up:
		res.Message = "Ters mod: hedef erişilebilir (" + res.Message + ")"
	case res.Message != "":
		res.Message = "Ters mod: hedef erişilemiyor (" + res.Message + ")"
	default:
		res.Message = "Ters mod: hedef erişilemiyor"
	}
	return res
}

// handleCert sertifika bilgisini günceller ve eşiğe girildiyse bir kez uyarır.
func (r *runner) handleCert(ctx context.Context, now time.Time, cert *check.CertInfo) {
	notAfter := cert.NotAfter.Unix()
	if notAfter != r.m.CertExpiresAt || cert.Issuer != r.m.CertIssuer {
		if err := r.e.store.UpdateCert(ctx, r.m.ID, notAfter, cert.Issuer); err != nil {
			r.e.log.Error("sertifika bilgisi yazılamadı", "monitor", r.m.Name, "hata", err)
			return
		}
		r.m.CertExpiresAt, r.m.CertIssuer = notAfter, cert.Issuer
	}
	// Sertifika uyarısını açip kapatabilen tipler CertExpiryChecker'ı uygular;
	// uygulamayan tipler için uyarı her zaman açık kabul edilir.
	if ce, ok := r.checker.(check.CertExpiryChecker); ok && !ce.CertExpiryEnabled(r.m.Config) {
		return
	}
	daysLeft := int(math.Floor(cert.NotAfter.Sub(now).Hours() / 24))
	threshold, ok := certThreshold(daysLeft, r.e.settings.Load().CertDays)
	if !ok {
		return
	}
	sent, err := r.e.store.MarkCertNotice(ctx, r.m.ID, notAfter, threshold)
	if err != nil || !sent {
		return
	}
	r.e.notifier.Notify(notify.Event{
		Kind: notify.KindCert, MonitorID: r.m.ID, MonitorName: r.m.Name, MonitorType: r.m.Type,
		Target: r.checker.Target(r.m.Config), Time: now, CertDays: daysLeft, CertExpires: cert.NotAfter,
		CertIssuer: cert.Issuer, URL: r.e.MonitorURL(r.m.ID),
	})
}

// certThreshold kalan güne uyan en dar eşiği seçer: 10 gün kala eşikler
// [21 14 7 3 1] ise 14 döner. Böylece yeni eklenen, bitmek üzere olan bir
// sertifika için tüm eşiklerin bildirimi aynı anda gitmez.
func certThreshold(daysLeft int, thresholds []int) (int, bool) {
	best, ok := math.MaxInt, false
	for _, t := range thresholds {
		if daysLeft <= t && t < best {
			best, ok = t, true
		}
	}
	return best, ok
}
