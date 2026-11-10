package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"testing"
	"time"
)

// checkStores varsayılan kip (PG'de eklenti varsa Timescale) ve PG'de ayrıca
// düz PG (TimescaleOff).
func checkStores(t *testing.T) map[string]*Store {
	t.Helper()
	out := map[string]*Store{}
	open := func(name string, o Options) {
		s, err := OpenWith(testTarget(t), time.UTC, o)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		out[name] = s
	}
	open("varsayilan", Options{})
	if os.Getenv("UPTIME_TEST_PG") != "" {
		open("duz-pg", Options{TimescaleOff: true})
	}
	return out
}

func checkRes(name, status string, t int64, samples ...CheckSample) CheckResult {
	ms, _ := json.Marshal(samples)
	return CheckResult{Name: name, Source: "plugin", T: t, Status: status, Metrics: ms, Samples: samples}
}

func TestCheckStateAndSeries(t *testing.T) {
	ctx := context.Background()
	for mode, s := range checkStores(t) {
		t.Run(mode+"-"+string(s.DBMode()), func(t *testing.T) {
			id := secServer(t, s, "chk")
			base := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC).Unix()
			ch, err := s.SaveCheckResult(ctx, id, checkRes("mysql", "ok", base, CheckSample{"conn", 10}), base)
			if err != nil || !ch.New || !ch.Enabled || ch.StatusSince != base {
				t.Fatalf("ilk kayıt: %+v %v", ch, err)
			}
			for i, st := range []string{"unknown", "unknown", "unknown", "critical"} {
				ts := base + int64(i+1)*600
				ch, err = s.SaveCheckResult(ctx, id, checkRes("mysql", st, ts, CheckSample{"conn", float64(20 + i*10)}), ts)
				if err != nil {
					t.Fatal(err)
				}
				if st == "unknown" && ch.UnknownStreak != i+1 {
					t.Fatalf("unknown sayacı %d: %+v", i, ch)
				}
			}
			if ch.UnknownStreak != 0 || ch.Prev != "unknown" || ch.StatusSince != base+2400 {
				t.Fatalf("critical: %+v", ch)
			}
			// Eski sonuç durumu ezmez.
			if ch, err = s.SaveCheckResult(ctx, id, checkRes("mysql", "ok", base+30, CheckSample{"conn", 1}), base+3000); err != nil || !ch.Stale {
				t.Fatalf("eski sonuç: %+v %v", ch, err)
			}
			c, err := s.GetCheck(ctx, id, "mysql")
			if err != nil || c.Status != "critical" || c.LastRun != base+2400 || c.LastOK != base {
				t.Fatalf("durum: %+v %v", c, err)
			}
			raw, err := s.CheckSeries(ctx, id, "mysql", "conn", base-1, base+7200, false)
			if err != nil {
				t.Fatal(err)
			}
			hourly, err := s.CheckSeries(ctx, id, "mysql", "conn", base-3600, base+7200, true)
			if err != nil {
				t.Fatal(err)
			}
			if !s.postgres {
				if len(raw) != 0 || len(hourly) != 0 {
					t.Fatalf("SQLite örnek saklamaz: %v %v", raw, hourly)
				}
				return
			}
			if len(raw) != 6 {
				t.Fatalf("ham seri: %+v", raw)
			}
			// 08:00 kovası: 10, 20, 30, 40, 50 ve eski sonuç 1 -> ort 151/6, en az 1, en çok 50.
			if len(hourly) != 1 || hourly[0].T != base || hourly[0].Min != 1 || hourly[0].Max != 50 || math.Abs(hourly[0].Avg-151.0/6) > 1e-9 {
				t.Fatalf("saatlik: %+v", hourly)
			}
			// Budama (düz kipte Go; Timescale'de politika).
			if err := s.PruneChecks(ctx, base+40*86400); err != nil {
				t.Fatal(err)
			}
			if s.DBMode() != DBModeTimescale {
				if raw, _ = s.CheckSeries(ctx, id, "mysql", "conn", 0, base+7200, false); len(raw) != 0 {
					t.Fatalf("budanmadı: %+v", raw)
				}
				if hourly, _ = s.CheckSeries(ctx, id, "mysql", "conn", 0, base+7200, true); len(hourly) != 1 {
					t.Fatalf("saatlik özet 365 gün kalmalı: %+v", hourly)
				}
			}
		})
	}
}

func TestCheckConfigLimitRules(t *testing.T) {
	ctx := context.Background()
	for mode, s := range checkStores(t) {
		t.Run(mode, func(t *testing.T) {
			id := secServer(t, s, "chk")
			for i := 0; i < MaxChecksPerServer; i++ {
				if _, err := s.SaveCheckResult(ctx, id, checkRes(fmt.Sprintf("c%d", i), "ok", 100), 100); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.SaveCheckResult(ctx, id, checkRes("fazla", "ok", 100), 100); !errors.Is(err, ErrCheckLimit) {
				t.Fatalf("sınır: %v", err)
			}
			if _, err := s.SetCheckConfig(ctx, id, "fazla", CheckConfig{Source: "plugin"}, 100); !errors.Is(err, ErrCheckLimit) {
				t.Fatalf("yapılandırma sınırı: %v", err)
			}
			// Manifestli eklenti işe girer; manifestsiz eklenti girmez.
			man, body, iv := `{"key":"x"}`, "aGk=", 300
			if _, err := s.SetCheckConfig(ctx, id, "c1", CheckConfig{Manifest: &man, Body: &body, Interval: &iv}, 100); err != nil {
				t.Fatal(err)
			}
			off := false
			if _, err := s.SetCheckConfig(ctx, id, "c2", CheckConfig{Enabled: &off}, 100); err != nil {
				t.Fatal(err)
			}
			jobs, err := s.CheckJobs(ctx, id)
			if err != nil || len(jobs) != 1 || jobs[0].Name != "c1" || jobs[0].Interval != 300 || string(jobs[0].Manifest) != man || jobs[0].Body != body {
				t.Fatalf("işler: %+v %v", jobs, err)
			}
			if got, err := s.CheckManifest(ctx, id, "c1"); err != nil || got != man {
				t.Fatalf("manifest: %q %v", got, err)
			}
			// Kurallar: varsayılan durum kuralı tekil; değiştirme diğer kuralları korur.
			for i := 0; i < 2; i++ {
				if err := s.EnsureCheckStatusRule(ctx, id, "c1"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.ReplaceServerAlerts(ctx, id, []ServerAlert{{Metric: "cpu", Threshold: 90, Minutes: 5, Active: true}}, 100); err != nil {
				t.Fatal(err)
			}
			if err := s.EnsureCheckStatusRule(ctx, id, "c1"); err != nil {
				t.Fatal(err)
			}
			rules := []ServerAlert{{Metric: MetricCheck, Mount: "c1", Threshold: 7, Minutes: 2, Active: true},
				{Metric: MetricCheck, Mount: "c1/lag", Threshold: 5, WarnThreshold: 10, Below: true, Minutes: 1, Active: true}}
			if _, err := s.ReplaceCheckAlerts(ctx, id, "c1", rules, 100); err != nil {
				t.Fatal(err)
			}
			all, err := s.ServerAlerts(ctx, id)
			if err != nil || len(all) != 3 {
				t.Fatalf("tüm kurallar: %+v %v", all, err)
			}
			got, _ := s.CheckAlerts(ctx, id, "c1")
			if len(got) != 2 || got[0].Threshold != 7 || !got[1].Below || got[1].WarnThreshold != 10 {
				t.Fatalf("denetim kuralları: %+v", got)
			}
			if _, err := s.GetCheck(ctx, id, "yok"); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("yok: %v", err)
			}
		})
	}
}
