package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/check"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
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
	Status      string `json:"status"` // up | down | retrying | unknown
	LastCheckAt int64  `json:"last_check_at"`
	PingMs      int64  `json:"ping_ms"`
	Message     string `json:"message"`
}

type location struct {
	probeID int64
	name    string
	have    bool
	at      time.Time
	res     check.Result  // ters mod uygulanmış
	fails   int           // art arda başarısız sonuç
	detail  *check.Detail // son başarısız sonucun istek/yanıtı (olay kaydı için)
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
	ls := &locationSet{
		downWhen: setup.DownWhen, byProbe: map[int64]*location{},
		started: e.now(), wake: make(chan struct{}, 1),
	}
	if setup.IncludeLocal {
		ls.local = &location{probeID: LocalProbeID, name: LocalName}
		ls.locs = append(ls.locs, ls.local)
	}
	for _, id := range setup.ProbeIDs { // kimliğe göre sıralı
		p, ok := probes[id]
		if !ok || !p.Active {
			continue
		}
		l := &location{probeID: id, name: p.Name}
		ls.locs = append(ls.locs, l)
		ls.byProbe[id] = l
	}
	initial := ls.statuses(ls.started, 0, 0) // henüz sonuç yok: hepsi "bilinmiyor"
	ls.snap.Store(&initial)
	return ls
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
	// kopyası okunur (loadLocations ilk kopyayı "bilinmiyor" olarak kurar).
	return *r.locs.snap.Load(), true
}

// staleAfter bu süreden eski konum sonucu "bilinmiyor" sayılır.
func (r *runner) staleAfter() time.Duration {
	return 3 * r.unit(max(r.m.Interval, r.m.RetryInterval))
}

// drainInbox kuyruktaki uzak sonuçları konumlara uygular.
func (r *runner) drainInbox() {
	ls := r.locs
	ls.mu.Lock()
	items := ls.inbox
	ls.inbox = nil
	ls.mu.Unlock()
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
		res.Up = !res.Up
		if !res.Up {
			res.Message = "Ters mod: hedef erişilebilir (" + res.Message + ")"
		}
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
		res := r.runCheck(ctx)
		if ctx.Err() != nil {
			return false // durduruldu: yarım kalan kontrol kaydedilmez
		}
		r.applyLocation(ls.local, r.e.now(), res)
		r.drainInbox() // kontrol sürerken gelenler
	}
	now := r.e.now()
	if !ls.anyData() && now.Sub(ls.started) < r.staleAfter() {
		// Başlangıçta kontrol noktalarına ilk sonuçları göndermeleri için süre
		// tanınır; hemen "sonuç gelmiyor" yazılmaz.
		r.publishSnapshot(now)
		return true
	}
	r.process(r.aggregate(now))
	r.publishSnapshot(now)
	return true
}

// locationWake uzak sonuç geldiğinde çağrılır. Genel durum değiştiyse
// sonucu hemen kaydeder ve true döner (zamanlayıcı yeniden kurulur).
func (r *runner) locationWake() bool {
	r.drainInbox()
	now := r.e.now()
	defer r.publishSnapshot(now)
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
	locUnknown  = "unknown"
)

func classify(l *location, now time.Time, staleAfter time.Duration, maxRetries int) string {
	switch {
	case !l.have || now.Sub(l.at) > staleAfter:
		return locUnknown
	case l.res.Up:
		return locUp
	case l.res.Pending || l.fails <= maxRetries:
		return locRetrying
	}
	return locDown
}

func (ls *locationSet) statuses(now time.Time, staleAfter time.Duration, maxRetries int) []LocationStatus {
	out := make([]LocationStatus, len(ls.locs))
	for i, l := range ls.locs {
		st := LocationStatus{ProbeID: l.probeID, Name: l.name, Status: classify(l, now, staleAfter, maxRetries), PingMs: -1}
		if l.have {
			st.LastCheckAt, st.PingMs, st.Message = l.at.Unix(), l.res.PingMs, l.res.Message
		}
		out[i] = st
	}
	return out
}

func (r *runner) publishSnapshot(now time.Time) {
	st := r.locs.statuses(now, r.staleAfter(), r.m.MaxRetries)
	r.locs.snap.Store(&st)
}

// aggregate konum sonuçlarını kesinti kuralına göre tek sonuca indirir.
func (r *runner) aggregate(now time.Time) check.Result {
	return aggregateLocations(r.locs.locs, r.locs.downWhen, now, r.staleAfter(), r.m.MaxRetries)
}

// aggregateLocations genel sonucu hesaplar:
//   - güncel sonucu olmayan konumlar sayılmaz; hiç yoksa PENDING,
//   - kural sağlanıyorsa (any: en az biri, majority: yarıdan fazlası, all:
//     hepsi çalışmıyor) DOWN,
//   - tekrar denenen konumlar da çalışmıyor sayılınca kural sağlanıyorsa
//     PENDING (tekrar deneniyor),
//   - aksi halde UP.
//
// Ping, çalışan konumların ortalamasıdır; SSL bilgisi ilk (ana sunucu önce)
// güncel konumdan alınır.
func aggregateLocations(locs []*location, downWhen string, now time.Time, staleAfter time.Duration, maxRetries int) check.Result {
	var failing, stale []string
	var up []*location
	var n, down, retrying int
	var cert *check.CertInfo
	for _, l := range locs {
		st := classify(l, now, staleAfter, maxRetries)
		if st == locUnknown {
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
	quorum := func(d int) bool {
		switch downWhen {
		case store.DownWhenMajority:
			return d > n/2
		case store.DownWhenAll:
			return d == n
		}
		return d >= 1
	}
	switch {
	case quorum(down):
		return check.Result{PingMs: -1, Message: strings.Join(failing, "; ") + suffix, Cert: cert}
	case quorum(down + retrying):
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
