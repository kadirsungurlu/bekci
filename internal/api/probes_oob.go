package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/probe"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// OOB ikinci hat (460): panelde girilen OOB IP'si ajana iş listesi ve kalp
// atışı yanıtıyla gider; ajan etkin hattını ve arayüz durumunu her istekte
// başlıkla bildirir (tel sözleşmesi: probe/oobtypes.go). IP kilidi OOB
// IP'sini de kabul eder (probe_guard.go).

// probeOOBConfig ajana gönderilen OOB ayarı.
func probeOOBConfig(p store.Probe) probe.OOBConfig {
	if p.OOBIP == "" {
		return probe.OOBConfig{}
	}
	return probe.OOBConfig{IP: p.OOBIP, Name: p.OOBLabel()}
}

// probeUplinkHeaders ajanın hat başlıklarını işler: hat değiştiyse kaydeder,
// motoru (oy düşürme + bildirim) ve paneli bilgilendirir; arayüz uyarısını
// günceller. OOB tanımlı değilse başlıklar yok sayılır.
func (s *Server) probeUplinkHeaders(r *http.Request, p *store.Probe, now time.Time) {
	if p.OOBIP == "" {
		return
	}
	ctx := r.Context()
	if up := strings.ToLower(strings.TrimSpace(r.Header.Get(probe.HeaderUplink))); (up == store.UplinkPrimary || up == store.UplinkOOB) && up != p.Uplink {
		changed, err := s.store.SetProbeUplink(ctx, p.ID, up, now.Unix())
		if err != nil {
			s.log.Warn("ajan hattı yazılamadı", "ajan", p.Name, "hata", err)
		} else if changed {
			prev := *p
			p.Uplink, p.UplinkSince = up, now.Unix()
			s.engine.SetProbeUplink(ctx, prev, up == store.UplinkOOB, now, true)
			if p.Kind == store.ProbeKindServer {
				s.servers.Publish(ctx, p.ID)
			}
		}
	}
	if v := r.Header.Get(probe.HeaderOOBIface); v == "ok" || v == "missing" {
		if missing := v == "missing"; missing != p.OOBIfaceMissing {
			if err := s.store.SetProbeOOBIfaceMissing(ctx, p.ID, missing); err != nil {
				s.log.Warn("OOB arayüz durumu yazılamadı", "ajan", p.Name, "hata", err)
			} else {
				p.OOBIfaceMissing = missing
				s.hub.Publish("probe", map[string]any{"probe_id": p.ID, "name": p.Name, "oob_iface_missing": missing})
			}
		}
	}
}
