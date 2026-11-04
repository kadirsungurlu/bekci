package betikajan

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betik"
)

const (
	capTTL         = 10 * time.Second
	flushEvery     = 5 * time.Second
	maxBackoff     = 5 * time.Minute
	seenEvery      = int64(3600)
	forwardJump    = int64(48 * 3600)
	maxMemEvents   = 256
	stopWait       = 20 * time.Second
	maxNormalSlots = 1
	maxSignedSlots = 1
)

// Options Agent ayarları. Yollar ve saat yalnız testte değişir.
type Options struct {
	Paths    Paths
	Log      *slog.Logger
	Now      func() time.Time
	Exec     Executor
	Platform *Platform
	Jitter   func(max time.Duration) time.Duration
}

// Agent betik çalıştırmanın ajan tarafı. Varsayılan kapalıdır: her karar yerel
// dosyaları yeniden okur, panelden gelen bir şey bunları değiştiremez.
type Agent struct {
	paths    Paths
	log      *slog.Logger
	now      func() time.Time
	exec     Executor
	platform Platform
	jitter   func(max time.Duration) time.Duration
	store    *Store
	journal  *Journal
	queue    *diskQueue
	capTTL   time.Duration

	secure atomic.Bool

	mu      sync.Mutex
	running map[string]*runCtl
	events  []betik.Event

	// capMu mu'dan ayrıdır: open dosya kilidi alır, kilit altındaki kod da mu alır.
	capMu  sync.Mutex
	capAt  time.Time
	capVal string
	capKey string

	wg    sync.WaitGroup
	kickC chan struct{}
}

type runCtl struct {
	signed    bool
	cancel    context.CancelFunc
	ctx       context.Context
	cancelled atomic.Bool
	lost      atomic.Bool
}

// New ajan tarafını kurar; kapalıysa hiçbir iş kabul etmez.
func New(o Options) *Agent {
	if o.Paths == (Paths{}) {
		o.Paths = DefaultPaths()
	}
	if o.Log == nil {
		o.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Platform == nil {
		p := DetectPlatform()
		o.Platform = &p
	}
	if o.Exec == nil {
		o.Exec = &Systemd{PrivatePIDs: o.Platform.PrivatePIDs, Hide: []string{o.Paths.StateDir, o.Paths.LogDir}}
	}
	if o.Jitter == nil {
		o.Jitter = randomJitter
	}
	return &Agent{
		paths: o.Paths, log: o.Log, now: o.Now, exec: o.Exec, platform: *o.Platform, jitter: o.Jitter,
		store:   NewStore(o.Paths.StateDir),
		journal: &Journal{dir: o.Paths.LogDir, log: o.Log},
		queue:   &diskQueue{dir: filepath.Join(o.Paths.StateDir, queueDir)},
		capTTL:  capTTL, running: map[string]*runCtl{}, kickC: make(chan struct{}, 1),
	}
}

func randomJitter(limit time.Duration) time.Duration {
	if limit <= 0 {
		return 0
	}
	var b [8]byte
	rand.Read(b[:])
	return time.Duration(binary.BigEndian.Uint64(b[:]) % uint64(limit))
}

// SetPanelSecure bağlantı https ve PROBE_ALLOW_INSECURE kapalıysa true.
func (a *Agent) SetPanelSecure(v bool) { a.secure.Store(v) }

// open §6 adım 1: özellik açık mı, yerel dosyalar sağlam mı. Sorun varsa kapalılık
// nedeni döner.
func (a *Agent) open() (*Config, ed25519.PrivateKey, string) {
	if !a.platform.OK {
		return nil, nil, betik.ReasonPlatform
	}
	if !a.secure.Load() {
		return nil, nil, betik.ReasonInsecure
	}
	cfg, err := LoadConfig(a.paths.Config)
	if errors.Is(err, ErrNoConfig) {
		return nil, nil, betik.ReasonMissing
	}
	if err != nil {
		a.log.Warn("betik yapılandırması geçersiz, özellik kapalı", "hata", err)
		return nil, nil, betik.ReasonConfig
	}
	if !cfg.Enabled {
		return nil, nil, betik.ReasonDisabled
	}
	key, err := a.store.LoadKey()
	if err != nil {
		a.log.Warn("ajan anahtarı okunamadı, özellik kapalı", "hata", err)
		return nil, nil, betik.ReasonState
	}
	if _, err := a.store.Read(); err != nil {
		a.log.Warn("betik durumu geçersiz, özellik kapalı (uptime betik onar)", "hata", err)
		return nil, nil, betik.ReasonState
	}
	return cfg, key, ""
}

func reasonCode(reason string) string {
	switch reason {
	case betik.ReasonMissing, betik.ReasonDisabled:
		return betik.CodeDisabled
	case betik.ReasonConfig:
		return betik.CodeConfigInvalid
	case betik.ReasonState:
		return betik.CodeStateInvalid
	case betik.ReasonInsecure:
		return betik.CodeInsecurePanel
	}
	return betik.CodePlatform
}

// Capability X-Probe-Scripts ve (açıksa) X-Probe-Script-Key değerleri. Her
// istekte çağrılır; kısa süre önbelleklidir.
func (a *Agent) Capability() (string, string) {
	a.capMu.Lock()
	defer a.capMu.Unlock()
	if a.capVal != "" && time.Since(a.capAt) < a.capTTL {
		return a.capVal, a.capKey
	}
	_, key, reason := a.open()
	a.capAt = time.Now()
	a.capVal, a.capKey = betik.Capability(reason == "", reason), ""
	if reason == "" {
		a.capKey = betik.EncodePublicKey(key.Public().(ed25519.PublicKey))
	}
	return a.capVal, a.capKey
}

// Busy çalışan ya da başlamak üzere olan betik var mı: ajan güncellemesi bunu
// bekler.
func (a *Agent) Busy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.running) > 0
}

// Handle panelden gelen listeyi işler; yoklama döngüsünü bekletmez, yürütme ayrı
// goroutine'dedir. Eski panelde hiç çağrılmaz.
func (a *Agent) Handle(l *betik.JobList) {
	if l == nil {
		return
	}
	for _, id := range l.Cancel {
		a.cancelRun(id)
	}
	runs := l.Runs
	if len(runs) > betik.MaxRunsPerResponse {
		runs = runs[:betik.MaxRunsPerResponse]
	}
	cfg, key, reason := a.open()
	if reason != "" {
		for i := range runs {
			if !a.isRunning(runs[i].RunID) {
				a.reject(safeRunID(runs[i].RunID), "", reasonCode(reason), "kapalı: "+reason)
			}
		}
		return
	}
	a.applyRevokes(cfg, l.Revokes)
	hostKey := betik.KeyID(key.Public().(ed25519.PublicKey))
	for i := range runs {
		a.start(cfg, hostKey, &runs[i])
	}
}

func safeRunID(s string) string {
	if betik.IsRunID(s) {
		return s
	}
	return ""
}

func (a *Agent) applyRevokes(cfg *Config, zarflar []json.RawMessage) {
	if len(zarflar) == 0 {
		return
	}
	if len(zarflar) > betik.MaxRevokesPerResp {
		zarflar = zarflar[:betik.MaxRevokesPerResp]
	}
	err := a.store.Update(func(st *State) error {
		if r := checkClock(st, a.now().Unix()); r != nil {
			return r
		}
		for _, z := range zarflar {
			if err := ApplyRevoke(cfg, st, z); err != nil {
				a.log.Warn("betik iptal zarfı reddedildi", "hata", err)
			}
		}
		return nil
	})
	if err != nil {
		a.log.Warn("betik iptalleri işlenemedi", "hata", err)
	}
}

func (a *Agent) isRunning(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running[id] != nil
}

// acquire eşzamanlılık yuvası ayırır. signed yuvası ayrıdır.
func (a *Agent) acquire(id string, signed bool) *runCtl {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, c := range a.running {
		if c.signed == signed {
			n++
		}
	}
	if (signed && n >= maxSignedSlots) || (!signed && n >= maxNormalSlots) {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &runCtl{signed: signed, cancel: cancel, ctx: ctx}
	a.running[id] = c
	return c
}

func (a *Agent) release(id string) {
	a.mu.Lock()
	c := a.running[id]
	delete(a.running, id)
	a.mu.Unlock()
	if c != nil {
		c.cancel()
	}
}

func (a *Agent) start(cfg *Config, hostKey string, job *betik.RunJob) {
	if !betik.IsRunID(job.RunID) {
		a.reject("", "", betik.CodeRunIDFormat, "run_id biçimi")
		return
	}
	if a.isRunning(job.RunID) {
		return // panel accepted görmeden yeniden gönderdi
	}
	var p *prepared
	var ctl *runCtl
	err := a.store.Update(func(st *State) error {
		now := a.now().Unix()
		var r *reject
		if p, r = prepare(job, cfg, hostKey, st, now); r != nil {
			return r
		}
		if ctl = a.acquire(job.RunID, p.eff == betik.ApprovalSigned); ctl == nil {
			return &reject{code: betik.CodeBusy, script: p.m.ID, detail: "yuva dolu"}
		}
		commit(st, p, now)
		return nil
	})
	if err != nil {
		if ctl != nil {
			a.release(job.RunID)
		}
		var r *reject
		if errors.As(err, &r) {
			a.reject(job.RunID, r.script, r.code, r.detail)
			return
		}
		a.log.Error("betik durumu yazılamadı, iş çalıştırılmadı", "hata", err)
		a.reject(job.RunID, "", betik.CodeStateWrite, err.Error())
		return
	}
	a.push(betik.Event{Kind: betik.EventAccepted, RunID: job.RunID, Time: a.now().Unix()})
	a.wg.Add(1)
	go a.execute(p, ctl)
}

func (a *Agent) reject(runID, script, code, detail string) {
	a.log.Debug("betik red ayrıntısı", "run_id", runID, "kod", code, "ayrıntı", detail)
	now := a.now()
	a.journal.Reject(now, runID, script, code)
	a.push(betik.Event{Kind: betik.EventRejected, RunID: runID, Code: code, Time: now.Unix()})
}

func (a *Agent) push(ev betik.Event) {
	a.mu.Lock()
	if len(a.events) >= maxMemEvents {
		a.events = a.events[1:]
	}
	a.events = append(a.events, ev)
	a.mu.Unlock()
	a.kick()
}

func (a *Agent) kick() {
	select {
	case a.kickC <- struct{}{}:
	default:
	}
}

// cancelRun çalışan birimi durdurur; makbuz "cancelled" olur.
func (a *Agent) cancelRun(id string) {
	a.mu.Lock()
	c := a.running[id]
	a.mu.Unlock()
	if c == nil {
		return
	}
	c.cancelled.Store(true)
	c.cancel()
	go a.exec.Stop(id)
}

// stillValid jitter sonunda işi güncel yerel dosyalarla yeniden denetler:
// bekleme sırasındaki "betik kapat", izin silme, iptal ve bilet süresi geçerli
// olsun. Jitter en çok 1 saattir; ilk karar o süre boyunca bağlayıcı olamaz.
func (a *Agent) stillValid(p *prepared) bool {
	cfg, _, reason := a.open()
	if cfg == nil {
		a.log.Warn("betik bekleme sonunda durduruldu", "run_id", p.runID, "neden", reason)
		return false
	}
	st, err := a.store.Read()
	if err != nil {
		a.log.Warn("betik bekleme sonunda durduruldu", "run_id", p.runID, "hata", err)
		return false
	}
	if _, r := validate(p.job, cfg, p.hostKey, st, a.now().Unix()); r != nil {
		a.log.Warn("betik bekleme sonunda durduruldu", "run_id", p.runID, "kod", r.code)
		return false
	}
	return true
}

func (a *Agent) execute(p *prepared, ctl *runCtl) {
	defer a.wg.Done()
	defer a.release(p.runID)
	if d := time.Duration(p.m.JitterSec) * time.Second; d > 0 {
		t := time.NewTimer(a.jitter(d))
		select {
		case <-t.C:
		case <-ctl.ctx.Done():
			t.Stop()
		}
		if ctl.ctx.Err() == nil && !a.stillValid(p) {
			ctl.cancelled.Store(true)
			ctl.cancel()
		}
	}
	started := a.now().Unix()
	o := Outcome{ExitCode: -1}
	if ctl.ctx.Err() == nil {
		o = a.exec.Run(context.Background(), Spec{
			RunID: p.runID, RunAs: p.m.RunAs, Body: p.body, Env: p.env,
			Timeout: time.Duration(p.m.TimeoutSec) * time.Second, OutputMax: p.m.OutputMax,
		})
	}
	timeout := time.Duration(p.m.TimeoutSec) * time.Second
	status := betik.StatusFailed
	switch {
	case ctl.lost.Load():
		status = betik.StatusLost
	case ctl.cancelled.Load():
		status = betik.StatusCancelled
	case o.Err == nil && o.ExitCode == 0:
		status = betik.StatusOK
	case o.Err == nil && o.Elapsed >= timeout:
		status = betik.StatusTimeout
	}
	if o.Err != nil {
		a.log.Error("betik başlatılamadı", "run_id", p.runID, "hata", o.Err)
	}
	a.finish(p.runID, &ActiveRun{
		ScriptID: p.m.ID, Version: p.m.Version, Manifest: p.manifest, Params: p.params, Ticket: p.ticket,
	}, status, started, &o)
}

// finish §6 adım 9: sürüm tabanı yalnız başarıda yükselir, makbuz imzalanır,
// yerel kayda, sonra panel kuyruğuna gider.
func (a *Agent) finish(id string, act *ActiveRun, status string, started int64, o *Outcome) {
	key, err := a.store.LoadKey()
	if err != nil {
		a.log.Error("makbuz imzalanamadı", "run_id", id, "hata", err)
		return
	}
	now := a.now()
	var env []byte
	var rec *betik.Receipt
	err = a.store.Update(func(st *State) error {
		rec = &betik.Receipt{
			Schema: betik.SchemaVersion, Seq: st.Seq + 1, Prev: st.Prev, RunID: id, ScriptID: act.ScriptID,
			Version: act.Version, ManifestSHA256: act.Manifest, ParamsSHA256: act.Params, TicketSHA256: act.Ticket,
			StartedAt: started, EndedAt: now.Unix(), Status: status, ExitCode: o.ExitCode,
			OutputSHA256: o.OutputSHA256, OutputTruncated: o.Truncated,
		}
		payload := mustJSON(rec)
		var err error
		if env, err = betik.Sign(key, betik.KindReceipt, payload); err != nil {
			return err
		}
		st.Seq, st.Prev = rec.Seq, betik.Digest(payload)
		delete(st.Active, id)
		if status == betik.StatusOK {
			if s := st.script(act.ScriptID); act.Version > s.Floor {
				s.Floor = act.Version
			}
		}
		return nil
	})
	if err != nil {
		a.log.Error("makbuz kaydedilemedi", "run_id", id, "hata", err)
		return
	}
	a.journal.Receipt(now, env, rec)
	dropped, err := a.queue.put(rec.Seq, &betik.Event{
		Kind: betik.EventReceipt, RunID: id, Time: now.Unix(), Receipt: env, Output: o.Output,
	})
	if err != nil {
		a.log.Error("makbuz kuyruğa yazılamadı", "run_id", id, "hata", err)
	}
	if dropped > 0 {
		a.journal.Note(now, betik.NoticeQueueDropped)
		a.push(betik.Event{Kind: betik.EventNotice, Code: betik.NoticeQueueDropped, Time: now.Unix()})
	}
	a.kick()
}

// reconcile açılışta: önceki süreçten kalan birimleri durdurur, makbuzu
// yazılmamış işleri "lost" kapatır.
func (a *Agent) reconcile() {
	if !a.platform.OK {
		return
	}
	units, err := a.exec.Units()
	if err != nil {
		a.log.Warn("betik birimleri listelenemedi", "hata", err)
	}
	st, serr := a.store.Read()
	if serr != nil {
		for _, id := range units {
			a.exec.Stop(id)
		}
		return
	}
	for _, id := range units {
		if st.Active[id] == nil {
			a.log.Warn("kayıtsız betik birimi durduruluyor", "run_id", id)
		}
		a.exec.Stop(id)
	}
	for id, act := range st.Active {
		a.log.Warn("betik ajan kapanırken yarım kaldı", "run_id", id)
		a.finish(id, act, betik.StatusLost, act.AcceptedAt, &Outcome{ExitCode: -1})
	}
	a.store.EnsureReserve()
}

// tick last_seen'i ilerletir, büyük ileri saat atlamasını bildirir.
func (a *Agent) tick() {
	if !a.platform.OK {
		return
	}
	if cfg, _, reason := a.open(); reason != "" || cfg == nil {
		return
	}
	now := a.now()
	jumped := false
	err := a.store.Update(func(st *State) error {
		sec := now.Unix()
		if st.LastSeen > 0 && (sec < st.LastSeen || sec-st.LastSeen < seenEvery) {
			return errNoChange
		}
		jumped = st.LastSeen > 0 && sec-st.LastSeen > forwardJump
		st.LastSeen = sec
		return nil
	})
	if err != nil && !errors.Is(err, errNoChange) {
		a.log.Warn("betik durumu güncellenemedi", "hata", err)
		return
	}
	if err == nil && jumped {
		a.journal.Note(now, betik.NoticeClockForward)
		a.push(betik.Event{Kind: betik.EventNotice, Code: betik.NoticeClockForward, Time: now.Unix()})
	}
	a.store.EnsureReserve()
}

var errNoChange = errors.New("değişiklik yok")

func delivered(status int, err error) bool {
	if err != nil {
		return false
	}
	return status >= 200 && status < 300 || status == 409 || status == 410 || status == 422
}

// flush bekleyen olayları sırayla iletir: önce bellektekiler (accepted, rejected),
// sonra diskteki makbuzlar. Teslim edilemeyen olay kuyrukta kalır.
func (a *Agent) flush(ctx context.Context, send betik.SendFunc) bool {
	for {
		a.mu.Lock()
		if len(a.events) == 0 {
			a.mu.Unlock()
			break
		}
		ev := a.events[0]
		a.mu.Unlock()
		if !delivered(send(ctx, &ev)) {
			return false
		}
		a.mu.Lock()
		if len(a.events) > 0 {
			a.events = a.events[1:]
		}
		a.mu.Unlock()
	}
	for _, name := range a.queue.names() {
		ev, err := a.queue.get(name)
		if err != nil {
			a.queue.del(name) // bozuk kuyruk dosyası; makbuz kayit.log'da
			continue
		}
		if !delivered(send(ctx, ev)) {
			return false
		}
		a.queue.del(name)
	}
	return true
}

// Run olay iletim döngüsü; ctx bitince çalışan betikleri durdurup "lost" yazar.
func (a *Agent) Run(ctx context.Context, send betik.SendFunc) {
	a.reconcile()
	backoff := flushEvery
	next := time.Time{}
	lastTick := time.Time{}
	t := time.NewTicker(flushEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			a.shutdown()
			return
		case <-a.kickC:
		case <-t.C:
		}
		now := time.Now()
		if now.Sub(lastTick) >= 5*time.Minute {
			lastTick = now
			a.tick()
		}
		if now.Before(next) {
			continue
		}
		if a.flush(ctx, send) {
			backoff, next = flushEvery, time.Time{}
			continue
		}
		next = now.Add(backoff)
		backoff = min(backoff*2, maxBackoff)
	}
}

// shutdown ajan kapanıyor: birim PID 1'de olduğundan kendiliğinden ölmez.
func (a *Agent) shutdown() {
	a.mu.Lock()
	ids := make([]string, 0, len(a.running))
	for id, c := range a.running {
		c.lost.Store(true)
		c.cancel()
		ids = append(ids, id)
	}
	a.mu.Unlock()
	for _, id := range ids {
		go a.exec.Stop(id)
	}
	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(stopWait):
		a.log.Warn("betikler zamanında durmadı; açılışta lost yazılır")
	}
}
