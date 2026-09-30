package engine

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Çok konumlu kontrol ----------------------------------------------------------------
//
// Konum ayarı yapılmış bir monitörde (store.LocationSetup) sonuçlar birden çok
// konumdan gelir: ana sunucu (kendi kontrolü, "local") ve uzak kontrol
// noktaları (probe; sonuçları API üzerinden ProbeResults ile gelir). Ayar
// yapılmamış monitörler eskisi gibi tek konumlu çalışır; bu dosyadaki kod
// onlar için hiç devreye girmez.
//
// Her konumun kendi son sonucu ve art arda hata sayacı vardır: bir konum
// max_retries tekrar denemeden sonra da başarısızsa "çalışmıyor", daha azsa
// "tekrar deneniyor" sayılır. Monitörün genel sonucu kesinti kuralından
// (down_when) çıkar ve normal durum makinesine (process) verilir; tekrar
// deneme konum başına yapıldığı için process'te ikinci kez uygulanmaz.
//
// Son sonucu 3 kontrol aralığından eski olan konum "bilinmiyor" sayılır ve
// kurala katılmaz (DOWN sayılmaz). Hiçbir konumdan güncel sonuç yoksa monitör
// PENDING olur.
//
// Yeni eklenen (veya runner yeniden başlatıldığında sonucu henüz gelmemiş)
// konum, ilk sonucu için tanınan süre (locRules.grace) boyunca "ilk sonuç
// bekleniyor" (waiting) sayılır: "sonuç gelmeyen" listesine girmez, kurala oy
// vermez ama DOWN kararında hâlâ "çalışıyor" oyu verebilecek bir konum olarak
// paydada kalır — yalnızca henüz oy vermemiş bir konum yüzünden DOWN denmez
// (ör. "all" kuralında ana sunucu çalışmıyor, uzak konum bekleniyorsa monitör
// DOWN değil "tekrar deneniyor" olur). Süre dolar da sonuç gelmezse konum
// "bilinmiyor"a döner (eski davranış).
//
// Kayıt sıklığı: monitörün kendi zamanlayıcısında (aralık / tekrar deneme
// aralığı) bir kayıt yazılır. Uzak bir sonuç genel durumu değiştiriyorsa
// (ör. UP → DOWN) beklemeden hemen kaydedilir.

// LocalProbeID ana sunucunun konum kimliği.
const LocalProbeID = 0

// LocalName ana sunucunun konum adı (mesajlarda).
const LocalName = "Ana sunucu"

// NoLocationData hiçbir konumdan güncel sonuç yokken kaydedilen mesaj.
const NoLocationData = "Kontrol noktalarından sonuç gelmiyor"

// maxInbox runner'ın işlemediği uzak sonuç kuyruğunun sınırı.
const maxInbox = 256

// RemoteCapable tip uzak kontrol noktasında çalıştırılabilir mi? Push (sinyal
// ana sunucuya gelir) ve grup (ana sunucudaki durumları okur) çalıştırılamaz.
func RemoteCapable(typ string) bool {
	return typ != check.TypePush && typ != check.TypeGroup
}

// ProbeResult kontrol noktasından gelen tek sonuç. Time sunucu saatine
// çevrilmiş kontrol zamanıdır.
type ProbeResult struct {
	MonitorID int64
	Time      time.Time
	Result    check.Result
}

// LocationStatus bir konumun son durumu (arayüz için).
type LocationStatus struct {
	ProbeID     int64  `json:"probe_id"` // 0: ana sunucu
	Name        string `json:"name"`
	Status      string `json:"status"` // up | down | retrying | waiting | unknown
	LastCheckAt int64  `json:"last_check_at"`
	PingMs      int64  `json:"ping_ms"`
	Message     string `json:"message"`
}

type location struct {
	probeID int64
	name    string
	added   time.Time // konumun bu runner'a eklendiği an (ilk sonuç süresi buradan sayılır)
	have    bool
	at      time.Time
	res     check.Result  // ters mod uygulanmış
	fails   int           // art arda başarısız sonuç
	detail  *check.Detail // son başarısız sonucun istek/yanıtı (olay kaydı için)
	gone    bool          // kontrol noktasının bağlantısı koptu (bkz. SetProbeConnected)
	goneAt  time.Time     // kopma anı
}

type inboxItem struct {
	probeID int64
	res     ProbeResult
}

type locationSet struct {
	downWhen string
	locs     []*location // sıra: ana sunucu (varsa), sonra kontrol noktaları (kimliğe göre)
	local    *location   // nil: ana sunucu konumlardan biri değil
	byProbe  map[int64]*location
	started  time.Time

	mu    sync.Mutex // inbox yalnızca bununla korunur; geri kalan her şey runner goroutine'inde
	inbox []inboxItem
	wake  chan struct{}

	snap atomic.Pointer[[]LocationStatus]
}

// loadLocations monitörün konum ayarını okur; ayar yoksa veya tip uzak
// kontrole uygun değilse nil döner (tek konumlu, eski davranış). Yalnızca
// etkin kontrol noktaları konum sayılır.
func (e *Engine) loadLocations(m store.Monitor) *locationSet {
	if !RemoteCapable(m.Type) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setup, err := e.store.MonitorLocations(ctx, m.ID)
	var probes map[int64]store.Probe
	if err == nil && setup.Configured() {
		probes, err = e.store.ProbesByIDs(ctx, setup.ProbeIDs)
	}
	if err != nil {
		// Monitör izlemesiz kalmasın: ana sunucudan kontrol edilir.
		e.log.Error("konum ayarı okunamadı, yalnızca ana sunucudan kontrol edilecek", "monitor", m.Name, "hata", err)
		return nil
	}
	if !setup.Configured() {
		return nil
	}
	now := e.now()
	ls := &locationSet{
		downWhen: setup.DownWhen, byProbe: map[int64]*location{},
		started: now, wake: make(chan struct{}, 1),
	}
	if setup.IncludeLocal {
		ls.local = &location{probeID: LocalProbeID, name: LocalName, added: now}
		ls.locs = append(ls.locs, ls.local)
	}
	for _, id := range setup.ProbeIDs { // kimliğe göre sıralı
		p, ok := probes[id]
		if !ok || !p.Active {
			continue
		}
		l := &location{probeID: id, name: p.Name, added: now}
		ls.locs = append(ls.locs, l)
		ls.byProbe[id] = l
	}
	ls.publish(now, e.locRulesFor(m)) // henüz sonuç yok: hepsi "ilk sonuç bekleniyor"
	return ls
}

// retiredLocs durdurulan runner'ın konum sonuçları (Engine.retired).
type retiredLocs struct {
	m    store.Monitor
	locs *locationSet
	at   time.Time
}

// retireKeep durdurulan runner'ın konum sonuçlarının yeni runner'a
// aktarılabileceği en uzun ara. Düzenleme (Remove → kayıt → Reload), konum
// ayarı ve kontrol noktası değişikliği bu sürede biter; durdurulup çok sonra
// yeniden başlatılan monitöre sonuçlar aktarılmaz (baştan başlar).
const retireKeep = 10 * time.Second

// retire durdurulan (goroutine'i bitmiş) runner'ın konumlarını kısa süre saklar.
func (e *Engine) retire(r *runner) {
	if r.locs == nil {
		return
	}
	now := e.now()
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, old := range e.retired {
		if now.Sub(old.at) > retireKeep {
			delete(e.retired, id)
		}
	}
	e.retired[r.m.ID] = retiredLocs{m: r.m, locs: r.locs, at: now}
}

// adoptLocations yeniden başlatılan runner'a (düzenleme, konum ayarı veya
// kontrol noktası değişikliği) eski runner'ın konum sonuçlarını aktarır: hâlâ
// atanmış konumlar son sonuçlarını korur, yalnızca yeni eklenen konumlar "ilk
// sonuç bekleniyor" olur. Kontrolün kendisi (tip, ayar, ters mod) değiştiyse
// eski sonuçlar yeni hedefe ait değildir, aktarılmaz. Çağıran e.mu'yu tutar.
func (e *Engine) adoptLocations(m store.Monitor, ls *locationSet) {
	old, ok := e.retired[m.ID]
	delete(e.retired, m.ID)
	now := e.now()
	if !ok || now.Sub(old.at) > retireKeep || old.m.Type != m.Type ||
		old.m.UpsideDown != m.UpsideDown || !bytes.Equal(old.m.Config, m.Config) {
		return
	}
	prev := make(map[int64]*location, len(old.locs.locs))
	for _, l := range old.locs.locs {
		prev[l.probeID] = l
	}
	for _, l := range ls.locs {
		if p := prev[l.probeID]; p != nil {
			l.added, l.have, l.at, l.res, l.fails, l.detail = p.added, p.have, p.at, p.res, p.fails, p.detail
		}
	}
	// Eski runner'ın işlemediği uzak sonuçlar kaybolmasın.
	old.locs.mu.Lock()
	for _, it := range old.locs.inbox {
		if ls.byProbe[it.probeID] != nil {
			ls.inbox = append(ls.inbox, it)
		}
	}
	old.locs.mu.Unlock()
	if len(ls.inbox) > 0 {
		ls.wake <- struct{}{} // kanal boş ve 1 kapasiteli: bloklamaz
	}
	ls.publish(now, e.locRulesFor(m))
}

// ProbeResults kontrol noktasının sonuçlarını ilgili runner'lara iletir ve
// kabul edilen sonuç sayısını döner. Çağıranı bekletmez: sonuçlar runner'ın
// kuyruğuna eklenir, runner uyandırılır. Monitör durdurulmuşsa veya bu
// kontrol noktası monitörün (etkin) konumlarından biri değilse sonuç atlanır.
func (e *Engine) ProbeResults(probeID int64, results []ProbeResult) int {
	n := 0
	for _, res := range results {
		e.mu.Lock()
		r := e.runners[res.MonitorID]
		e.mu.Unlock()
		if r == nil || r.locs == nil || r.locs.byProbe[probeID] == nil {
			continue
		}
		ls := r.locs
		ls.mu.Lock()
		if len(ls.inbox) >= maxInbox {
			ls.inbox = ls.inbox[1:] // en eskisi düşer
		}
		ls.inbox = append(ls.inbox, inboxItem{probeID: probeID, res: res})
		ls.mu.Unlock()
		select {
		case ls.wake <- struct{}{}:
		default:
		}
		n++
	}
	return n
}

// LocationStatuses monitörün konumlarının son durumu; monitör çok konumlu
// değilse veya çalışmıyorsa ok=false.
func (e *Engine) LocationStatuses(monitorID int64) ([]LocationStatus, bool) {
	e.mu.Lock()
	r := e.runners[monitorID]
	e.mu.Unlock()
	if r == nil || r.locs == nil {
		return nil, false
	}
	// Konum durumu runner goroutine'ine aittir; buradan yalnızca yayınlanmış
	// kopyası okunur (loadLocations ilk kopyayı "ilk sonuç bekleniyor" olarak kurar).
	return *r.locs.snap.Load(), true
}

// locRules konum durumunu belirleyen süreler (monitör ayarından).
type locRules struct {
	staleAfter time.Duration // bu süreden eski sonuç "bilinmiyor"
	grace      time.Duration // hiç sonuç vermemiş konum bu süre "ilk sonuç bekleniyor"
	maxRetries int
	goneGrace  time.Duration // kopan kontrol noktasının son "çalışıyor" sonucu bu süre geçerli
}

// firstResultSlack ilk sonuç süresine eklenen pay (aralık birimi): ajanın
// sonuç gönderme aralığı (5 sn) ve ağ gecikmesi.
const firstResultSlack = 15

// locRulesFor monitörün konum süreleri.
//
// İlk sonuç süresi (grace) en kötü durum için hesaplanır; iki sınırın büyüğüdür:
//   - kontrol noktası işi yeni öğreniyorsa: iş listesini eski usulle en geç
//     ProbePollAfter'da bir yoklar (uzun yoklamada ~1 sn), yeni işi en geç
//     min(aralık, 10) sonra başlatır (eski ajanlar; yeniler ~1 sn), kontrol
//     en fazla zaman aşımı kadar sürer, sonuç birkaç saniye içinde gönderilir:
//     ProbePollAfter + min(aralık, 10) + zaman aşımı + 15;
//   - işi zaten çalıştırıyorsa (runner yeniden başladı ama iş değişmedi, ör.
//     sunucu güncellemesi): sıradaki planlı kontrol en geç bir aralık sonradır;
//     staleAfter (3 aralık) kadar beklenir — eski başlangıç payıyla aynı.
//
// Uzun yoklama sayesinde yeni işte "bekleniyor" durumu pratikte birkaç saniye
// sürer; bu süre yalnızca ajan çevrimdışıyken veya yavaşken dolar.
func (e *Engine) locRulesFor(m store.Monitor) locRules {
	u := func(n int) time.Duration { return time.Duration(n) * e.cfg.Unit }
	stale := 3 * u(max(m.Interval, m.RetryInterval))
	grace := max(stale, u(ProbePollAfter+min(m.Interval, 10)+m.Timeout+firstResultSlack))
	return locRules{staleAfter: stale, grace: grace, maxRetries: m.MaxRetries, goneGrace: e.goneGrace()}
}

func (r *runner) rules() locRules { return r.e.locRulesFor(r.m) }

// staleAfter bu süreden eski konum sonucu "bilinmiyor" sayılır.
func (r *runner) staleAfter() time.Duration { return r.rules().staleAfter }

// drainInbox kuyruktaki uzak sonuçları konumlara uygular ve kontrol
// noktalarının bağlantı durumunu tazeler.
func (r *runner) drainInbox() {
	ls := r.locs
	ls.mu.Lock()
	items := ls.inbox
	ls.inbox = nil
	ls.mu.Unlock()
	for _, l := range ls.locs {
		if l != ls.local {
			l.goneAt, l.gone = r.e.probeGoneAt(l.probeID)
		}
	}
	for _, it := range items {
		if l := ls.byProbe[it.probeID]; l != nil {
			r.applyLocation(l, it.res.Time, it.res.Result)
		}
	}
}

// applyLocation bir konumun yeni sonucunu işler. Eski tarihli (sıra dışı
// gelen) sonuç yok sayılır.
func (r *runner) applyLocation(l *location, at time.Time, res check.Result) {
	if l.have && at.Before(l.at) {
		return
	}
	if r.m.UpsideDown && !res.Pending {
		res = invertUpsideDown(res)
	}
	if l.have && at.Sub(l.at) > r.staleAfter() {
		l.fails = 0 // uzun süre sessiz kalan konumun eski hataları sayılmaz
	}
	switch {
	case res.Up:
		l.fails, l.detail = 0, nil
	case !res.Pending:
		l.fails++
		if res.Detail != nil {
			l.detail = res.Detail
		}
	}
	res.Detail = nil // konum durumunda tutulmaz; son hatanınki l.detail'de
	l.have, l.at, l.res = true, at, res
}

// locationTick zamanlayıcıda çağrılır: ana sunucu konumlardan biriyse kontrolü
// yapar, sonuçları birleştirip kaydeder. Durdurulduysa false döner.
func (r *runner) locationTick(ctx context.Context) bool {
	ls := r.locs
	r.drainInbox()
	if ls.local != nil {
		// Konumun zamanı kontrolün başlangıcı: kontrol noktaları da başlangıcı
		// gönderir, aynı ızgara anında yapılan kontroller aynı saniyeyi gösterir.
		started := r.e.now()
		res := r.runCheck(ctx)
		if ctx.Err() != nil {
			return false // durduruldu: yarım kalan kontrol kaydedilmez
		}
		r.applyLocation(ls.local, started, res)
		r.drainInbox() // kontrol sürerken gelenler
	}
	now := r.e.now()
	// Konum durumu kayıttan ÖNCE yayınlanır: veritabanında yeni genel durumu
	// gören okuyucu (API) konumların eski ("bilinmiyor") halini görmesin.
	r.publishSnapshot(now)
	if !ls.anyData() && now.Sub(ls.started) < r.rules().grace {
		// Başlangıçta kontrol noktalarına ilk sonuçları göndermeleri için süre
		// tanınır; hemen "sonuç gelmiyor" yazılmaz.
		return true
	}
	r.process(r.aggregate(now))
	return true
}

// locationWake uzak sonuç geldiğinde çağrılır. Genel durum değiştiyse
// sonucu hemen kaydeder ve true döner (zamanlayıcı yeniden kurulur).
func (r *runner) locationWake() bool {
	r.drainInbox()
	now := r.e.now()
	// Kayıttan önce yayınlanır (bkz. locationTick).
	r.publishSnapshot(now)
	if !r.locs.anyData() || r.e.InMaintenance(r.m.ID, now) {
		return false
	}
	res := r.aggregate(now)
	status := store.StatusDown
	switch {
	case res.Up:
		status = store.StatusUp
	case res.Pending:
		status = store.StatusPending
	}
	if status == r.m.Status {
		// Genel durum değişmedi (kayıt yazılmaz) ama bir konum çalışmaz/çalışır
		// olmuş olabilir: kısmi kesinti olayı zamanlayıcıyı beklemez.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		r.partialStep(ctx, now, status, false)
		return false
	}
	r.process(res)
	return true
}

func (ls *locationSet) anyData() bool {
	for _, l := range ls.locs {
		if l.have {
			return true
		}
	}
	return false
}

// Konum durumları.
const (
	locUp       = "up"
	locDown     = "down"
	locRetrying = "retrying"
	locWaiting  = "waiting" // hiç sonuç vermedi, ilk sonuç süresi dolmadı
	locUnknown  = "unknown"
)

func classify(l *location, now time.Time, rules locRules) string {
	switch {
	case !l.have && !l.gone && now.Sub(l.added) < rules.grace:
		return locWaiting
	case !l.have || l.gone || now.Sub(l.at) > rules.staleAfter:
		// Sonuç yok, eskidi ya da kontrol noktasının bağlantısı koptu: son
		// sonucu ("çalışıyor") artık geçerli sayılmaz.
		return locUnknown
	case l.res.Up:
		return locUp
	case l.res.Pending || l.fails <= rules.maxRetries:
		return locRetrying
	}
	return locDown
}

func (ls *locationSet) statuses(now time.Time, rules locRules) []LocationStatus {
	out := make([]LocationStatus, len(ls.locs))
	for i, l := range ls.locs {
		st := LocationStatus{ProbeID: l.probeID, Name: l.name, Status: classify(l, now, rules), PingMs: -1}
		if l.have {
			st.LastCheckAt, st.PingMs, st.Message = l.at.Unix(), l.res.PingMs, l.res.Message
			if st.Status == locUnknown {
				// Eski sonucun mesajı ("200 OK") "sonuç yok" durumunda yanıltır.
				st.PingMs, st.Message = -1, ""
			}
		}
		out[i] = st
	}
	return out
}

// publish konum durumlarının kopyasını okuyucular için yayınlar. changed: bir
// konumun durumu değişti; fresh: durumu aynı kalsa da bir konumdan yeni sonuç
// geldi (son kontrol zamanı ilerledi).
func (ls *locationSet) publish(now time.Time, rules locRules) (changed, fresh bool) {
	st := ls.statuses(now, rules)
	prev := ls.snap.Swap(&st)
	if prev == nil || len(*prev) != len(st) {
		return true, true
	}
	for i := range st {
		if (*prev)[i].Status != st[i].Status {
			changed = true
		}
		if (*prev)[i].LastCheckAt != st[i].LastCheckAt {
			fresh = true
		}
	}
	return changed, fresh || changed
}

// publishSnapshot konum durumlarını yayınlar ve canlı akışa "locations" olayı
// gönderir: bir konumun durumu değiştiğinde (changed: true; arayüz liste
// rozetlerini de tazeler) ya da yalnızca yeni sonuç geldiğinde (changed:
// false; açık ayrıntı sayfası konumların "x sn önce"sini hemen günceller —
// uzak konumun sonucu ana sunucunun kontrolünden birkaç saniye sonra gelir).
func (r *runner) publishSnapshot(now time.Time) {
	if changed, fresh := r.locs.publish(now, r.rules()); fresh {
		r.e.hub.Publish("locations", map[string]any{"monitor_id": r.m.ID, "changed": changed})
	}
}

// aggregate konum sonuçlarını kesinti kuralına göre tek sonuca indirir.
func (r *runner) aggregate(now time.Time) check.Result {
	return aggregateLocations(r.locs.locs, r.locs.downWhen, now, r.rules())
}

// aggregateLocations genel sonucu hesaplar:
//   - güncel sonucu olmayan konumlar sayılmaz; hiç yoksa PENDING,
//   - kural sağlanıyorsa (any: en az biri, majority: yarıdan fazlası, all:
//     hepsi çalışmıyor) DOWN; ilk sonucu beklenen konumlar bu kararda
//     paydaya girer (henüz "çalışıyor" oyu verebilirler),
//   - kural yalnızca oy vermiş konumlarla sağlanıyorsa ya da tekrar denenen
//     konumlar da çalışmıyor sayılınca sağlanıyorsa PENDING (tekrar deneniyor),
//   - aksi halde UP.
//
// "Sonuç gelmeyen" listesinde yalnızca süresi dolmuş (bilinmiyor) konumlar
// vardır; ilk sonucu beklenenler mesajda yer almaz.
//
// Ping, çalışan konumların ortalamasıdır; SSL bilgisi ilk (ana sunucu önce)
// güncel konumdan alınır.
func aggregateLocations(locs []*location, downWhen string, now time.Time, rules locRules) check.Result {
	var failing, stale []string
	var up []*location
	var n, down, retrying, waiting int
	var cert *check.CertInfo
	for _, l := range locs {
		st := classify(l, now, rules)
		if st == locWaiting {
			waiting++
			continue
		}
		if st == locUnknown {
			if l.gone && l.have && l.res.Up && now.Sub(l.goneAt) < rules.goneGrace {
				// Az önce kopan, son sonucu "çalışıyor" konum: tolerans süresince
				// genel kararda son sonucu geçerli sayılır (kart "sonuç yok"
				// gösterir); ajanın yeniden başlaması yanlış kesinti açmaz.
				n++
				up = append(up, l)
				continue
			}
			stale = append(stale, l.name)
			continue
		}
		n++
		if cert == nil && l.res.Cert != nil {
			cert = l.res.Cert
		}
		switch st {
		case locUp:
			up = append(up, l)
		case locDown:
			down++
			failing = append(failing, l.name+": "+l.res.Message)
		case locRetrying:
			retrying++
			failing = append(failing, l.name+": "+l.res.Message)
		}
	}
	if n == 0 {
		return check.Result{Pending: true, PingMs: -1, Message: NoLocationData}
	}
	suffix := ""
	if len(stale) > 0 {
		suffix = " (sonuç gelmeyen: " + nameList(stale) + ")"
	}
	quorum := func(d, of int) bool {
		switch downWhen {
		case store.DownWhenMajority:
			return d > of/2
		case store.DownWhenAll:
			return d == of
		}
		return d >= 1
	}
	switch {
	case quorum(down, n+waiting):
		return check.Result{PingMs: -1, Message: strings.Join(failing, "; ") + suffix, Cert: cert}
	case quorum(down+retrying, n):
		return check.Result{Pending: true, PingMs: -1, Message: strings.Join(failing, "; ") + suffix, Cert: cert}
	}
	var sum, cnt int64
	for _, l := range up {
		if l.res.PingMs >= 0 {
			sum += l.res.PingMs
			cnt++
		}
	}
	ping := int64(-1)
	if cnt > 0 {
		ping = sum / cnt
	}
	msg := up[0].res.Message
	if len(failing) > 0 {
		msg = fmt.Sprintf("%d/%d konum çalışıyor — %s", len(up), n, strings.Join(failing, "; "))
	}
	return check.Result{Up: true, PingMs: ping, Message: msg + suffix, Cert: cert}
}

// nameList en fazla 10 adı virgülle birleştirir.
func nameList(names []string) string {
	const max = 10
	if len(names) <= max {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:max], ", ") + fmt.Sprintf(" ve %d diğer", len(names)-max)
}
