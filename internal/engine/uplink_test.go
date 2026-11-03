package engine

import (
	"context"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// OOB'deki ajanın "çalışmıyor" oyu karara katılmaz (465): tablo, her satırda
// OOB'deki konumun sonucu ve beklenen monitör durumu.
func TestOOBLocationNoVote(t *testing.T) {
	for _, c := range []struct {
		name     string
		rule     string
		local    bool // ana sunucu sonucu (up?)
		probeUp  bool
		oob      bool
		wantDown bool
	}{
		{"any: OOB konumu düşük, oy yok", store.DownWhenAny, true, false, true, false},
		{"any: ana hatta düşük, kesinti", store.DownWhenAny, true, false, false, true},
		{"all: OOB konumu + Main düşük, yalnız Main oyu", store.DownWhenAll, false, false, true, true},
		{"all: OOB konumu çalışıyor, Main düşük: OOB oyu yok -> kesinti", store.DownWhenAll, false, true, true, true},
		{"all: ana hatta çalışıyor, Main düşük: kısmi", store.DownWhenAll, false, true, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			fra := f.probe(t, "Frankfurt", true)
			m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 0 })
			r := f.locRunner(t, m, store.LocationSetup{IncludeLocal: true, ProbeIDs: []int64{fra.ID}, DownWhen: c.rule})
			if c.local {
				fake.set(up())
			} else {
				fake.set(down("zaman aşımı"))
			}
			defer fake.set(up())
			if c.oob {
				f.e.SetProbeUplink(ctx, fra, true, f.clock, false)
			}
			r.locationTick(ctx)
			f.clock = f.clock.Add(10 * time.Millisecond)
			res := up()
			if !c.probeUp {
				res = down("Bağlantı reddedildi")
			}
			r.remote(fra, f.clock, res)
			if got := r.m.Status == store.StatusDown; got != c.wantDown {
				t.Fatalf("DOWN=%v bekleniyordu, durum %d (%q)", c.wantDown, r.m.Status, r.m.LastMessage)
			}
			snap := *r.locs.snap.Load()
			if snap[1].OOB != c.oob || (c.oob && snap[1].Status != locUnknown) {
				t.Fatalf("konum kartı: %+v", snap[1])
			}
		})
	}
}

// Hat geçişi bildirimi: çevrimdışı bildirimi açık kontrol noktasında geçiş ve
// dönüş bildirilir (dönüşte OOB'de geçen süre); kapalıysa bildirim yok.
func TestUplinkNotification(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := f.probe(t, "Frankfurt", true)
	p.NotifyOffline, p.OOBIP, p.OOBName = true, "198.51.100.7", "MARS"
	f.e.SetProbeUplink(ctx, p, true, f.clock, true)
	if !f.e.ProbeOnOOB(p.ID) {
		t.Fatal("OOB durumu tutulmalı")
	}
	p.Uplink, p.UplinkSince = store.UplinkOOB, f.clock.Unix()
	f.clock = f.clock.Add(7 * time.Minute)
	f.e.SetProbeUplink(ctx, p, false, f.clock, true)
	evs := f.n.events
	if len(evs) != 2 || evs[0].Kind != notify.KindUplinkFailover || evs[0].UplinkName != "MARS" ||
		evs[1].Kind != notify.KindUplinkRestored || evs[1].Downtime != 7*time.Minute {
		t.Fatalf("bildirimler: %+v", evs)
	}
	if evs[0].Title() != "Frankfurt ana hatta erişemiyor, şu an MARS üzerinden bağlı" {
		t.Fatalf("başlık: %q", evs[0].Title())
	}
	p.NotifyOffline = false
	f.e.SetProbeUplink(ctx, p, true, f.clock, true)
	if len(f.n.events) != 2 {
		t.Fatalf("bildirimi kapalı kontrol noktası bildirmemeli: %d", len(f.n.events))
	}
	// Kayıttaki durum taramayla belleğe alınır (panel yeniden başlasa da).
	f.e.SetProbeUplink(ctx, p, false, f.clock, false)
	if err := f.st.SetProbeOOB(ctx, p.ID, "198.51.100.7", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.SetProbeUplink(ctx, p.ID, store.UplinkOOB, f.clock.Unix()); err != nil {
		t.Fatal(err)
	}
	f.e.scanProbes(ctx, nil)
	if !f.e.ProbeOnOOB(p.ID) {
		t.Fatal("tarama kayıttaki OOB durumunu almalı")
	}
}
