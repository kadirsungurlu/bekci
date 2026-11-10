package eklentiajan

// Gerçek systemd birimi testi: BEKCI_TEST_SYSTEMD=1, root, systemd >= 254.
// Kafesin gerçekten uygulandığını eklentinin kendi gözünden ölçer.

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/eklenti"
)

func needSystemd(t *testing.T) *Systemd {
	t.Helper()
	if os.Getenv("BEKCI_TEST_SYSTEMD") != "1" {
		t.Skip("BEKCI_TEST_SYSTEMD=1 değil")
	}
	if os.Geteuid() != 0 {
		t.Skip("root gerekli")
	}
	e := NewSystemd()
	if e == nil {
		t.Skip("systemd >= 254 yok")
	}
	return e
}

// kafesBody kafes özelliklerini JSON metrik olarak raporlar (1 = engellendi).
const kafesBody = `
w=0; touch /etc/bekci-eklenti-yazma 2>/dev/null || w=1
v=0; touch /var/lib/bekci-eklenti-yazma 2>/dev/null || v=1
e=1; [ -r /etc/uptime-agent.env ] && cat /etc/uptime-agent.env >/dev/null 2>&1 && e=0
k=1; ls /etc/bekci >/dev/null 2>&1 && k=0
n=$(ls /sys/class/net | grep -vc '^lo$')
p=$(ls -d /proc/[0-9]* | wc -l)
t=0; echo x > /tmp/x && t=1
printf '{"status":"ok","message":"kafes","metrics":{"etc_ro":{"value":%s},"var_ro":{"value":%s},"env_hidden":{"value":%s},"bekci_hidden":{"value":%s},"net_ifaces":{"value":%s},"uid":{"value":%s},"pids":{"value":%s},"tmp_ok":{"value":%s}}}' \
  "$w" "$v" "$e" "$k" "$n" "$(id -u)" "$p" "$t"
echo hata-kanali >&2
`

func TestSystemdKafes(t *testing.T) {
	e := needSystemd(t)
	if _, err := os.Stat("/etc/uptime-agent.env"); err != nil {
		os.WriteFile("/etc/uptime-agent.env", []byte("PROBE_TOKEN=gizli\n"), 0o600)
		defer os.Remove("/etc/uptime-agent.env")
	}
	o := e.Run(context.Background(), Spec{Unit: NewUnit("kafes"), RunAs: "nobody", Body: []byte(kafesBody), Timeout: 20 * time.Second})
	if o.Err != nil || o.ExitCode != 0 {
		t.Fatalf("rc %d hata %v çıktı %q stderr %q", o.ExitCode, o.Err, o.Stdout, o.Stderr)
	}
	r := eklenti.Parse("json", o.ExitCode, o.Stdout)
	got := map[string]float64{}
	for _, m := range r.Metrics {
		got[m.Name] = m.Value
	}
	t.Logf("kafes: %v stderr=%q süre=%v", got, o.Stderr, o.Elapsed)
	want := map[string]float64{"etc_ro": 1, "var_ro": 1, "env_hidden": 1, "bekci_hidden": 1, "net_ifaces": 0, "uid": 65534, "tmp_ok": 1}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, beklenen %v", k, got[k], v)
		}
	}
	if e.PrivatePIDs && got["pids"] > 5 {
		t.Errorf("PrivatePIDs: %v süreç görünüyor", got["pids"])
	}
	if !strings.Contains(string(o.Stderr), "hata-kanali") || strings.Contains(string(o.Stdout), "hata-kanali") {
		t.Errorf("stdout/stderr ayrımı: %q / %q", o.Stdout, o.Stderr)
	}
	// Ağ açık manifest: PrivateNetwork yok.
	o = e.Run(context.Background(), Spec{Unit: NewUnit("kafes"), RunAs: "root", Network: true, Body: []byte(kafesBody), Timeout: 20 * time.Second})
	r = eklenti.Parse("json", o.ExitCode, o.Stdout)
	for _, m := range r.Metrics {
		if m.Name == "net_ifaces" && m.Value < 1 {
			t.Errorf("network=true iken arayüz yok")
		}
		if m.Name == "uid" && m.Value != 0 {
			t.Errorf("run_as root iken uid %v", m.Value)
		}
	}
}

func TestSystemdNagiosZamanAsimiCikti(t *testing.T) {
	e := needSystemd(t)
	o := e.Run(context.Background(), Spec{Unit: NewUnit("nagios"), RunAs: "nobody",
		Body: []byte("echo 'CRITICAL - disk | kullanim=95%;80;90;0;100'; exit 2"), Timeout: 10 * time.Second})
	r := eklenti.Parse("nagios", o.ExitCode, o.Stdout)
	if o.ExitCode != 2 || r.Status != eklenti.Critical || len(r.Metrics) != 1 {
		t.Fatalf("nagios rc %d %+v", o.ExitCode, r)
	}
	// Eklentinin kendi 1 dönüşü zaman aşımı sayılmaz (systemd-run de 1 döner).
	o = e.Run(context.Background(), Spec{Unit: NewUnit("uyari"), RunAs: "nobody", Body: []byte("echo 'WARNING - x'; exit 1"), Timeout: 10 * time.Second})
	if o.TimedOut || o.ExitCode != 1 {
		t.Fatalf("exit 1: %+v", o)
	}
	start := time.Now()
	o = e.Run(context.Background(), Spec{Unit: NewUnit("uyku"), RunAs: "nobody", Body: []byte("sleep 60"), Timeout: 2 * time.Second})
	if !o.TimedOut || time.Since(start) > 25*time.Second {
		t.Fatalf("zaman aşımı: %+v süre %v", o, time.Since(start))
	}
	o = e.Run(context.Background(), Spec{Unit: NewUnit("buyuk"), RunAs: "nobody",
		Body: []byte("i=0; while [ $i -lt 3000 ]; do echo 'satir satir satir satir satir satir'; i=$((i+1)); done"), Timeout: 10 * time.Second})
	if !o.Truncated || len(o.Stdout) != eklenti.MaxOutput+1 {
		t.Fatalf("büyük çıktı: kesik %v uzunluk %d", o.Truncated, len(o.Stdout))
	}
}

func TestSystemdZamanlayiciUctanUca(t *testing.T) {
	e := needSystemd(t)
	f := newFixture(t)
	f.body = []byte("echo 'OK - yuk | load=0.25;1;2'")
	f.config(Digest(f.body))
	s := f.sched(e)
	s.Now = time.Now
	m := f.manifest(func(m map[string]any) {
		m["not_before"] = time.Now().Add(-time.Hour).Unix()
		m["not_after"] = time.Now().Add(time.Hour).Unix()
	})
	it := f.item(m, f.priv, 60)
	if it.Body != base64.StdEncoding.EncodeToString(f.body) {
		t.Fatal("gövde")
	}
	r := s.RunOne(context.Background(), it)
	if r.Error != "" || r.Status != eklenti.OK || len(r.Metrics) != 1 || r.Metrics[0].Value != 0.25 {
		t.Fatalf("sonuç %+v", r)
	}
}
