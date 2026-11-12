package servers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func cert(loc, fp string, until time.Time) metrics.InvCert {
	return metrics.InvCert{Source: "file", Location: loc, Subject: "a.example", NotAfter: until.Unix(), Fingerprint: fp}
}

func (e *testEnv) inv(certs ...metrics.InvCert) {
	e.t.Helper()
	if err := e.svc.IngestInventory(context.Background(), e.getProbe(), metrics.Inventory{Certs: certs}); err != nil {
		e.t.Fatal(err)
	}
}

func TestCertStage(t *testing.T) {
	th := []int{14, 7, 1}
	for days, want := range map[int]int{30: 0, 15: 0, 14: 14, 8: 14, 7: 7, 2: 7, 1: 1, 0: 1, -1: certExpired} {
		if got := certStage(days, th); got != want {
			t.Errorf("%d gün: %d, beklenen %d", days, got, want)
		}
	}
	if certStage(1, nil) != 0 {
		t.Error("eşik yoksa uyarı yok")
	}
	if certLevel(14, th) != store.LevelWarning || certLevel(1, th) != store.LevelCritical || certLevel(certExpired, th) != store.LevelCritical {
		t.Error("seviye")
	}
}

// 14/7/1 eşiklerinin her biri bir kez bildirilir, tek olay açılır, süresi
// dolunca kritik; yenilenince olay kapanır.
func TestCertAlertLifecycle(t *testing.T) {
	e := newTestEnv(t)
	e.send(cpu(1))
	e.notif.take()
	until := e.clock.Add(20*24*time.Hour + time.Hour)
	c := cert("/etc/a.pem", "fp1", until)
	dup := c
	dup.Source, dup.Location = "port", "127.0.0.1:443"

	e.inv(c, dup)
	if n := e.notif.take(); len(n) != 0 || len(e.serverIncidents()) != 0 {
		t.Fatalf("erken uyarı: %+v", n)
	}
	step := func(days int) []notify.Event {
		e.clock = until.Add(-time.Duration(days)*24*time.Hour - time.Hour)
		e.inv(c, dup)
		e.inv(c, dup) // aynı aşamada ikinci tur bildirim üretmez
		return e.notif.take()
	}
	n := step(13)
	if len(n) != 1 || n[0].Kind != notify.KindServerAlert || n[0].Metric != "cert" || n[0].Level != store.LevelWarning || n[0].Value != 13 || n[0].IncidentID == 0 {
		t.Fatalf("14 eşiği: %+v", n)
	}
	if n = step(12); len(n) != 0 {
		t.Fatalf("aynı eşikte tekrar: %+v", n)
	}
	if n = step(6); len(n) != 1 || n[0].Level != store.LevelWarning || n[0].Threshold != 7 {
		t.Fatalf("7 eşiği: %+v", n)
	}
	if n = step(0); len(n) != 1 || n[0].Level != store.LevelCritical {
		t.Fatalf("1 eşiği: %+v", n)
	}
	if n = step(-2); len(n) != 1 || n[0].Value >= 0 {
		t.Fatalf("süresi doldu: %+v", n)
	}
	list := e.serverIncidents()
	if len(list) != 1 || list[0].ResolvedAt != 0 {
		t.Fatalf("tek açık olay: %+v", list)
	}
	d := store.ParseServerIncidentData(list[0].Data)
	if d.Metric != "cert" || d.Mount != "a.example" || d.Level != store.LevelCritical {
		t.Fatalf("olay verisi: %+v", d)
	}
	if got := store.ServerIncidentCause("en", list[0].Kind, d); got != "Certificate a.example has expired" {
		t.Fatalf("neden: %q", got)
	}
	// Yenilendi: yeni parmak izi, olay kapanır, tek "çözüldü" bildirimi.
	e.clock = e.clock.Add(time.Minute)
	e.inv(cert("/etc/a.pem", "fp2", e.clock.Add(90*24*time.Hour)))
	n = e.notif.take()
	if len(n) != 1 || n[0].Kind != notify.KindServerResolved || n[0].Metric != "cert" {
		t.Fatalf("yenilenme: %+v", n)
	}
	if ev := n[0]; ev.Title() == "" {
		t.Fatal("başlık")
	}
	if list = e.serverIncidents(); list[0].ResolvedAt == 0 {
		t.Fatal("olay kapanmadı")
	}
}

// Ajan gelmese de saatlik denetim günü ilerletir; yoksayılan sertifika uyarmaz;
// eşikler ayarlanabilir, boş liste kapatır.
func TestCertCheckIgnoreSettings(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.send(cpu(1))
	e.notif.take()
	c := cert("/etc/a.pem", "fp1", e.clock.Add(10*24*time.Hour+time.Hour))
	e.inv(c)
	if n := e.notif.take(); len(n) != 1 {
		t.Fatalf("ilk: %+v", n)
	}
	e.clock = e.clock.Add(4 * 24 * time.Hour)
	e.svc.CheckCerts(ctx)
	if n := e.notif.take(); len(n) != 1 || n[0].Threshold != 7 {
		t.Fatalf("saatlik denetim: %+v", n)
	}
	if err := e.st.SetCertIgnored(ctx, e.probe.ID, "fp1", true); err != nil {
		t.Fatal(err)
	}
	e.svc.CheckCert(ctx, e.getProbe())
	if n := e.notif.take(); len(n) != 1 || n[0].Kind != notify.KindServerResolved {
		t.Fatalf("yoksayma kapatır: %+v", n)
	}
	e.inv(c)
	if n := e.notif.take(); len(n) != 0 {
		t.Fatalf("yoksayılan uyardı: %+v", n)
	}
	if err := e.st.SetCertIgnored(ctx, e.probe.ID, "fp1", false); err != nil {
		t.Fatal(err)
	}
	st, _ := e.st.LoadSettings(ctx)
	st.ServerCertDays = []int{}
	if err := e.st.SaveSettings(ctx, st); err != nil {
		t.Fatal(err)
	}
	e.inv(c)
	if n := e.notif.take(); len(n) != 0 {
		t.Fatalf("eşik kapalıyken uyardı: %+v", n)
	}
}

// Toplanamayan bölüm son bilinen hâlini korur; ilk görülme değişmez; kaybolan silinir.
func TestSaveInventorySections(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	p := e.getProbe()
	e.send(cpu(1))
	first := metrics.Inventory{
		Services:   []metrics.InvService{{Name: "nginx", Active: "active", Sub: "running"}, {Name: "eski", Active: "inactive"}},
		Containers: []metrics.InvContainer{{Name: "web", Image: "x:1", State: "running", RestartCount: 1, Command: "x --password=p1"}},
		Ports:      []metrics.InvPort{{Proto: "tcp", Addr: "0.0.0.0", Port: 80}},
		Disks:      []metrics.InvDisk{{Mount: "/", Device: "/dev/sda1", FS: "ext4", Total: 1 << 40}},
	}
	if err := e.svc.IngestInventory(ctx, p, first); err != nil {
		t.Fatal(err)
	}
	t0 := e.clock.Unix()
	e.clock = e.clock.Add(time.Hour)
	next := metrics.Inventory{
		Services: []metrics.InvService{{Name: "nginx", Active: "failed", Sub: "failed"}},
		Notes:    map[string]string{metrics.InvContainers: metrics.NoteFailed},
		Ports:    []metrics.InvPort{}, Disks: first.Disks,
	}
	if err := e.svc.IngestInventory(ctx, p, next); err != nil {
		t.Fatal(err)
	}
	inv, err := e.st.LoadInventory(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Services) != 1 || inv.Services[0].Active != "failed" || inv.Services[0].FirstSeen != t0 || inv.Services[0].LastSeen != e.clock.Unix() {
		t.Fatalf("servis: %+v", inv.Services)
	}
	if len(inv.Containers) != 1 || inv.Containers[0].RestartCount != 1 || inv.Notes[metrics.InvContainers] != metrics.NoteFailed {
		t.Fatalf("konteyner korunmalı: %+v %v", inv.Containers, inv.Notes)
	}
	if strings.Contains(inv.Containers[0].Command, "p1") {
		t.Fatal("komut panelde de maskelenmeli")
	}
	if len(inv.Ports) != 0 || inv.Disks[0].Total != 1<<40 {
		t.Fatalf("port/disk: %+v %+v", inv.Ports, inv.Disks)
	}
	// Kontrol noktası ve metriği kapalı sunucu yok sayılır.
	cp := p
	cp.Metrics = false
	if err := e.svc.IngestInventory(ctx, cp, metrics.Inventory{Ports: []metrics.InvPort{{Proto: "tcp", Addr: "x", Port: 1}}}); err != nil {
		t.Fatal(err)
	}
	if inv, _ = e.st.LoadInventory(ctx, p.ID); len(inv.Ports) != 0 {
		t.Fatal("metriği kapalı sunucudan yazıldı")
	}
}

func TestShortLivedCertNoAlarm(t *testing.T) {
	now := int64(1_800_000_000)
	day := int64(86400)
	cases := []struct {
		life  int64
		stage int
		want  bool
	}{
		{day / 2, 1, true},  // Caddy iç CA: 12 saat
		{6 * day, 7, true},  // ACME kısa ömürlü
		{6 * day, 1, false}, // son gün yine bildirilir
		{90 * day, 14, false},
		{90 * day, 0, false},
	}
	for _, c := range cases {
		got := shortLived(store.InvCert{NotBefore: now, NotAfter: now + c.life}, c.stage)
		if got != c.want {
			t.Errorf("ömür %d, aşama %d: %v", c.life, c.stage, got)
		}
	}
	if shortLived(store.InvCert{NotAfter: now}, 1) {
		t.Error("başlangıcı bilinmeyen sertifika susturulmamalı")
	}
}

// Windows sertifika deposu kaydı (source "store", 751) Linux dosya
// sertifikası gibi inv_certs'e yazılır ve sertifika uyarısına düşer.
func TestWindowsStoreCertAlert(t *testing.T) {
	e := newTestEnv(t)
	e.send(cpu(1))
	e.notif.take()
	c := metrics.InvCert{Source: "store", Location: `LocalMachine\WebHosting\ABCDEF (IIS 0.0.0.0:443)`, Subject: "iis.example",
		NotAfter: e.clock.Add(5 * 24 * time.Hour).Unix(), Fingerprint: strings.Repeat("ab", 32)}
	e.inv(c)
	n := e.notif.take()
	if len(n) != 1 || n[0].Kind != notify.KindServerAlert || n[0].Metric != "cert" || n[0].Threshold != 7 {
		t.Fatalf("uyarı: %+v", n)
	}
	list, err := e.st.ServerCerts(context.Background(), e.getProbe().ID)
	if err != nil || len(list) != 1 || list[0].Source != "store" || list[0].Location != c.Location {
		t.Fatalf("inv_certs: %+v %v", list, err)
	}
	d := store.ParseServerIncidentData(e.serverIncidents()[0].Data)
	if d.Metric != "cert" || d.Mount != "iis.example" {
		t.Fatalf("olay: %+v", d)
	}
}
