package engine

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/check"
	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store/storetest"
)

// fakeChecker sonucu testten değiştirilebilen sahte monitör tipi.
type fakeChecker struct {
	mu  sync.Mutex
	res check.Result
}

func (f *fakeChecker) set(r check.Result)                                   { f.mu.Lock(); f.res = r; f.mu.Unlock() }
func (f *fakeChecker) Normalize(c json.RawMessage) (json.RawMessage, error) { return c, nil }
func (f *fakeChecker) Target(json.RawMessage) string                        { return "sahte-hedef" }
func (f *fakeChecker) Check(context.Context, json.RawMessage) check.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.res
}

var fake = &fakeChecker{res: check.Result{Up: true, PingMs: 10}}

func init() { check.Register("sahte", fake) }

type fakeNotifier struct {
	mu     sync.Mutex
	events []notify.Event
}

func (n *fakeNotifier) Notify(ev notify.Event) {
	n.mu.Lock()
	n.events = append(n.events, ev)
	n.mu.Unlock()
}
func (n *fakeNotifier) kinds() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []string
	for _, e := range n.events {
		out = append(out, e.Kind)
	}
	return out
}

type fixture struct {
	st     *store.Store
	target string // veritabanı adresi (ham SQL gereken testler için)
	e      *Engine
	n      *fakeNotifier
	clock  time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	target := storetest.Target(t)
	st, err := store.Open(target, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fixture{st: st, target: target, n: &fakeNotifier{}, clock: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)}
	f.e = New(st, f.n, NewHub(), slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{Unit: time.Millisecond, BaseURL: "https://uptime.test"})
	f.e.now = func() time.Time { return f.clock }
	return f
}

func (f *fixture) monitor(t *testing.T, mut func(*store.Monitor)) store.Monitor {
	t.Helper()
	m := store.Monitor{Name: "site", Type: "sahte", Active: true, Interval: 60, RetryInterval: 20, Timeout: 1000, Config: json.RawMessage(`{}`)}
	if mut != nil {
		mut(&m)
	}
	if err := f.st.CreateMonitor(context.Background(), &m, nil); err != nil {
		t.Fatal(err)
	}
	return m
}

// runnerFor döngüsüz bir runner kurar; process doğrudan çağrılır.
func (f *fixture) runnerFor(t *testing.T, id int64) *runner {
	t.Helper()
	m, err := f.st.GetMonitor(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner{e: f.e, m: m, checker: fake}
	r.confirmed = r.initialConfirmed(context.Background())
	return r
}

func up() check.Result             { return check.Result{Up: true, PingMs: 10, Message: "200 OK"} }
func down(msg string) check.Result { return check.Result{PingMs: -1, Message: msg} }

func TestStateMachine(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 2; m.ResendEvery = 2 })
	r := f.runnerFor(t, m.ID)

	steps := []struct {
		res    check.Result
		status int
	}{
		{up(), store.StatusUp},                     // ilk sonuç: bildirim yok
		{down("502"), store.StatusPending},         // 1. deneme
		{down("502"), store.StatusPending},         // 2. deneme
		{down("502"), store.StatusDown},            // DOWN bildirimi + olay
		{down("502"), store.StatusDown},            // hatırlatma sayacı 1
		{down("502"), store.StatusDown},            // hatırlatma sayacı 2 → hatırlatma
		{up(), store.StatusUp},                     // UP bildirimi
		{down("zaman aşımı"), store.StatusPending}, // yeni kesinti yine tekrar denemeyle başlar
		{up(), store.StatusUp},                     // bekleyenden dönüş: bildirim yok
	}
	for i, s := range steps {
		f.clock = f.clock.Add(time.Minute)
		r.process(s.res)
		if r.m.Status != s.status {
			t.Fatalf("adım %d: durum %d, %d bekleniyordu", i, r.m.Status, s.status)
		}
	}

	want := []string{notify.KindDown, notify.KindReminder, notify.KindUp}
	if got := f.n.kinds(); !equal(got, want) {
		t.Fatalf("bildirimler %v, %v bekleniyordu", got, want)
	}
	upEv := f.n.events[2]
	if upEv.Downtime != 3*time.Minute || upEv.Target != "sahte-hedef" || upEv.URL != "https://uptime.test/#/monitors/1" {
		t.Errorf("UP olayı yanlış: %+v", upEv)
	}

	incidents, _ := f.st.ListIncidents(ctx, store.IncidentFilter{})
	if len(incidents) != 1 || incidents[0].ResolvedAt == 0 || incidents[0].Cause != "502" {
		t.Fatalf("olaylar yanlış: %+v", incidents)
	}

	// "Up since" sadece onaylanmış değişimlerde güncellenir: bekleyenden
	// dönüş (son adım) son değişim zamanını bozmaz.
	got, _ := f.st.GetMonitor(ctx, m.ID)
	upAt := time.Date(2026, 9, 27, 10, 7, 0, 0, time.UTC).Unix()
	if got.LastChangeAt != upAt {
		t.Errorf("last_change_at = %d, %d bekleniyordu", got.LastChangeAt, upAt)
	}
}

func TestNewMonitorDownAlertsImmediately(t *testing.T) {
	f := newFixture(t)
	m := f.monitor(t, nil)
	r := f.runnerFor(t, m.ID)
	r.process(down("Bağlantı reddedildi"))
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown}) {
		t.Fatalf("yeni ve çalışmayan monitör için DOWN bekleniyordu: %v", got)
	}
}

func TestRestartDoesNotRealert(t *testing.T) {
	f := newFixture(t)
	m := f.monitor(t, nil)
	r := f.runnerFor(t, m.ID)
	r.process(down("502"))

	// Uygulama yeniden başladı: yeni runner veritabanından durumu okur.
	r2 := f.runnerFor(t, m.ID)
	if r2.confirmed != store.StatusDown {
		t.Fatalf("açık olay varken DOWN bekleniyordu: %d", r2.confirmed)
	}
	r2.process(down("502"))
	f.clock = f.clock.Add(time.Hour)
	r2.process(up())
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown, notify.KindUp}) {
		t.Fatalf("bildirimler %v", got)
	}
	if f.n.events[1].Downtime != time.Hour {
		t.Errorf("kesinti süresi olay başlangıcından hesaplanmalı: %v", f.n.events[1].Downtime)
	}
}

func TestUpsideDown(t *testing.T) {
	f := newFixture(t)
	m := f.monitor(t, func(m *store.Monitor) { m.UpsideDown = true })
	r := f.runnerFor(t, m.ID)
	r.process(down("Bağlantı reddedildi"))
	if r.m.Status != store.StatusUp {
		t.Fatal("ters modda erişilemeyen hedef UP sayılmalı")
	}
	r.process(up())
	if r.m.Status != store.StatusDown || f.n.kinds()[0] != notify.KindDown {
		t.Fatal("ters modda erişilebilen hedef DOWN sayılmalı")
	}
}

func TestCertNotices(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.monitor(t, func(m *store.Monitor) { m.Type = "http"; m.Config = json.RawMessage(`{"url":"https://kadir.app"}`) })
	r := f.runnerFor(t, m.ID)
	cert := &check.CertInfo{NotAfter: f.clock.Add(10*24*time.Hour + time.Hour), Issuer: "Let's Encrypt"}

	res := up()
	res.Cert = cert
	r.process(res)
	r.process(res) // aynı eşik ikinci kez bildirilmez
	if got := f.n.kinds(); !equal(got, []string{notify.KindCert}) {
		t.Fatalf("tek SSL uyarısı bekleniyordu: %v", got)
	}
	if f.n.events[0].CertDays != 10 {
		t.Errorf("kalan gün 10 olmalı: %d", f.n.events[0].CertDays)
	}
	// 7 günün altına inince yeni eşik.
	f.clock = f.clock.Add(4 * 24 * time.Hour)
	r.process(res)
	if got := f.n.kinds(); len(got) != 2 || f.n.events[1].CertDays != 6 {
		t.Fatalf("7 gün eşiği bekleniyordu: %v", f.n.events)
	}
	got, _ := f.st.GetMonitor(ctx, m.ID)
	if got.CertExpiresAt != cert.NotAfter.Unix() || got.CertIssuer != "Let's Encrypt" {
		t.Errorf("sertifika bilgisi kaydedilmedi: %+v", got)
	}
}

func TestCertThreshold(t *testing.T) {
	th := []int{21, 14, 7, 3, 1}
	cases := map[int]int{30: -1, 21: 21, 15: 21, 10: 14, 7: 7, 2: 3, 0: 1, -5: 1}
	for days, want := range cases {
		got, ok := certThreshold(days, th)
		if (want == -1 && ok) || (want != -1 && got != want) {
			t.Errorf("%d gün → %d %v, %d bekleniyordu", days, got, ok, want)
		}
	}
}

// Gerçek döngüyle uçtan uca: zamanlayıcı, push ve durdurma.
func TestLoopPushAndRemove(t *testing.T) {
	f := newFixture(t)
	f.e.now = time.Now
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fake.set(up())
	active := f.monitor(t, func(m *store.Monitor) { m.Name = "aktif"; m.Interval = 20 })
	push := f.monitor(t, func(m *store.Monitor) {
		m.Name, m.Type, m.Interval, m.PushToken = "push", check.TypePush, 150, "tok123"
	})
	if err := f.e.Start(ctx); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "aktif monitör UP", func() bool {
		m, _ := f.st.GetMonitor(ctx, active.ID)
		return m.Status == store.StatusUp
	})

	if err := f.e.Push(ctx, "tok123", true, "", 5); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "push UP", func() bool {
		m, _ := f.st.GetMonitor(ctx, push.ID)
		return m.Status == store.StatusUp && m.LastMessage == "Push alındı"
	})
	if err := f.e.Push(ctx, "yok", true, "", -1); err != ErrPushNotFound {
		t.Errorf("bilinmeyen token: %v", err)
	}
	// Sinyal kesilince DOWN.
	waitFor(t, "push DOWN", func() bool {
		m, _ := f.st.GetMonitor(ctx, push.ID)
		return m.Status == store.StatusDown
	})

	// Durdurulan monitöre artık kayıt yazılmaz.
	f.e.Remove(active.ID)
	before, _ := f.st.GetMonitor(ctx, active.ID)
	time.Sleep(80 * time.Millisecond)
	after, _ := f.st.GetMonitor(ctx, active.ID)
	if before.LastCheckAt != after.LastCheckAt {
		t.Error("durdurulan monitör çalışmaya devam etti")
	}
	if err := f.e.Push(ctx, "tok123", true, "", -1); err != nil {
		t.Errorf("push hâlâ çalışmalı: %v", err)
	}
	f.e.Remove(push.ID)
	if err := f.e.Push(ctx, "tok123", true, "", -1); err != ErrPushPaused {
		t.Errorf("durdurulmuş push: %v", err)
	}
	cancel()
	f.e.Wait()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("beklenen durum oluşmadı: %s", what)
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Bulgu: DOWN kaydedilmiş ama olayı yazılamamış monitör yeniden başlatılınca
// ikinci bir DOWN bildirimi gitmemeli; eksik olay tamamlanmalı.
func TestRestartDownWithoutIncident(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 1 })
	downAt := f.clock.Unix()
	if err := f.st.RecordBeat(ctx, store.BeatUpdate{
		Beat:         store.Beat{MonitorID: m.ID, Time: downAt, Status: store.StatusDown, PingMs: -1, Message: "502"},
		LastChangeAt: downAt,
	}); err != nil {
		t.Fatal(err)
	}
	r := f.runnerFor(t, m.ID)
	if r.confirmed != store.StatusDown {
		t.Fatalf("kayıtlı DOWN durumu korunmalı: %d", r.confirmed)
	}
	if started, _ := f.st.OpenIncidentStart(ctx, m.ID); started != downAt {
		t.Fatalf("eksik olay DOWN zamanıyla açılmalı: %d", started)
	}
	f.clock = f.clock.Add(time.Minute)
	r.process(down("502"))
	f.clock = f.clock.Add(time.Minute)
	r.process(up())
	if got := f.n.kinds(); !equal(got, []string{notify.KindUp}) {
		t.Fatalf("sadece UP bildirimi bekleniyordu: %v", got)
	}
	if f.n.events[0].Downtime != 2*time.Minute {
		t.Errorf("kesinti süresi DOWN anından hesaplanmalı: %v", f.n.events[0].Downtime)
	}
}

// Bulgu: durdurup başlatınca "şu andan beri" zamanı eski kesintide kalmamalı.
func TestResumeResetsLastChange(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.monitor(t, nil)
	r := f.runnerFor(t, m.ID)
	r.process(down("502"))
	downAt := f.clock.Unix()

	if err := f.st.SetMonitorActive(ctx, m.ID, false); err != nil {
		t.Fatal(err)
	}
	f.st.ResolveIncident(ctx, m.ID, f.clock.Unix())
	if err := f.st.SetMonitorActive(ctx, m.ID, true); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(time.Hour)
	r2 := f.runnerFor(t, m.ID)
	if got, _ := f.st.GetMonitor(ctx, m.ID); got.LastMessage != "" || got.LastChangeAt != 0 {
		t.Fatalf("yeniden başlatmada eski mesaj/zaman temizlenmeli: %q %d", got.LastMessage, got.LastChangeAt)
	}
	if r2.confirmed != unknown {
		t.Fatalf("yeniden başlatılan monitörün durumu bilinmiyor olmalı: %d", r2.confirmed)
	}
	r2.process(up())
	got, _ := f.st.GetMonitor(ctx, m.ID)
	if got.LastChangeAt == downAt || got.LastChangeAt != f.clock.Unix() {
		t.Errorf("last_change_at yeni UP anı olmalı: %d (DOWN anı %d)", got.LastChangeAt, downAt)
	}
	if kinds := f.n.kinds(); !equal(kinds, []string{notify.KindDown}) {
		t.Errorf("yeniden başlatma sonrası UP için bildirim gitmemeli: %v", kinds)
	}
}

// Bulgu: uzun kapalı kalma sonrası push monitörü hemen DOWN olmamalı.
func TestPushGraceAfterRestart(t *testing.T) {
	f := newFixture(t)
	f.e.now = time.Now
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := f.monitor(t, func(m *store.Monitor) {
		m.Type, m.Interval, m.PushToken = check.TypePush, 300, "tok-grace"
	})
	// Son sinyal çok eskiden (uygulama uzun süre kapalıydı).
	old := time.Now().Add(-time.Hour).Unix()
	f.st.RecordBeat(ctx, store.BeatUpdate{Beat: store.Beat{MonitorID: m.ID, Time: old, Status: store.StatusUp, PingMs: -1}, LastChangeAt: old})
	if err := f.e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond) // aralığın yarısı
	got, _ := f.st.GetMonitor(ctx, m.ID)
	if got.Status == store.StatusDown {
		t.Fatal("tam bir aralık dolmadan DOWN olmamalı")
	}
	cancel()
	f.e.Wait()
}
