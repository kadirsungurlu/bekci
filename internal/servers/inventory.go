package servers

import (
	"context"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

const (
	// InventoryInterval ajanın envanter toplama aralığı (sn); iş listesinde
	// inventory_interval olarak gider. Ajan yalnız değişince (en geç 12 turda
	// bir) gönderir.
	InventoryInterval = 300
	// certCheckEvery sertifika eşiklerinin ajandan bağımsız gözden geçirilme
	// aralığı (ajan kapalıyken de gün sayılır).
	certCheckEvery = time.Hour
	// certExpired sertifikanın süresi doldu aşaması.
	certExpired = -1
)

// InventoryIntervalFor iş listesine yazılacak aralık (0: envanter toplanmaz).
func InventoryIntervalFor(p store.Probe) int {
	if p.Kind != store.ProbeKindServer || !p.Metrics {
		return 0
	}
	return InventoryInterval
}

// IngestInventory ajanın envanterini saklar ve sertifika uyarılarını
// değerlendirir. Metriği kapalı sunucu ve kontrol noktası yok sayılır.
func (s *Service) IngestInventory(ctx context.Context, p store.Probe, inv metrics.Inventory) error {
	if p.Kind != store.ProbeKindServer || !p.Metrics {
		return nil
	}
	inv.Sanitize()
	now := s.now()
	// Olaylar zaman çizelgesine yalnız kayıt başarılıysa yazılır (aksi halde
	// sonraki tur aynı farkı yeniden görür).
	prev, err := s.store.LoadInventory(ctx, p.ID)
	if err != nil {
		return err
	}
	if err := s.store.SaveInventory(ctx, p.ID, now.Unix(), inv); err != nil {
		return err
	}
	s.timeline(ctx, inventoryChanges(p.ID, prev, inv, now.Unix())...)
	st, err := s.store.LoadSettings(ctx)
	if err != nil {
		return err
	}
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	return s.evalCerts(ctx, p, st.ServerCertDays, now)
}

// CheckCerts tüm sunucuların sertifika eşiklerini gözden geçirir: envanter
// gelmese de gün ilerler, devre dışı sunucunun açık uyarıları kapanır.
func (s *Service) CheckCerts(ctx context.Context) {
	probes, err := s.store.ListProbesOfKind(ctx, store.ProbeKindServer)
	if err != nil {
		s.log.Error("sertifika denetimi: sunucular okunamadı", "hata", err)
		return
	}
	st, err := s.store.LoadSettings(ctx)
	if err != nil {
		s.log.Error("sertifika denetimi: ayarlar okunamadı", "hata", err)
		return
	}
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	now := s.now()
	for _, p := range probes {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := s.evalCerts(ctx, p, st.ServerCertDays, now); err != nil {
			s.log.Error("sertifika denetimi", "sunucu", p.Name, "hata", err)
		}
	}
}

// CheckCert tek sunucuyu hemen değerlendirir (kullanıcı sertifikayı yoksayınca).
func (s *Service) CheckCert(ctx context.Context, p store.Probe) {
	st, err := s.store.LoadSettings(ctx)
	if err != nil {
		s.log.Error("sertifika denetimi: ayarlar okunamadı", "hata", err)
		return
	}
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	if err := s.evalCerts(ctx, p, st.ServerCertDays, s.now()); err != nil {
		s.log.Error("sertifika denetimi", "sunucu", p.Name, "hata", err)
	}
}

// certStage kalan güne göre aşama: süresi dolduysa certExpired, eşiklerin
// (büyükten küçüğe) içine girdiyse aşılan en küçük eşik, yoksa 0.
func certStage(days int, thresholds []int) int {
	if len(thresholds) == 0 {
		return 0
	}
	if days < 0 {
		return certExpired
	}
	stage := 0
	for _, t := range thresholds {
		if days <= t {
			stage = t
		}
	}
	return stage
}

// certLevel son eşik ve süresi dolmuş sertifika kritik, öncekiler uyarıdır.
func certLevel(stage int, thresholds []int) string {
	if stage == certExpired || stage == slices.Min(thresholds) {
		return store.LevelCritical
	}
	return store.LevelWarning
}

// tighter yeni aşama bildirilenden daha acil mi?
func tighter(stage, was int) bool { return stage != 0 && stage < was }

// shortLived sertifikanın ömrü eşiğin iki katından kısa mı: Caddy iç CA'sının
// 12 saatlik ya da ACME'nin 6 günlük sertifikaları kendiliğinden yenilenir,
// geniş eşikler bunlar için sürekli yanlış alarm olurdu. Süresi dolan her
// zaman bildirilir.
func shortLived(c store.InvCert, stage int) bool {
	return stage > 0 && c.NotBefore > 0 && c.NotAfter-c.NotBefore < int64(2*stage)*86400
}

func certDaysLeft(notAfter int64, now time.Time) int {
	return int(math.Floor(float64(notAfter-now.Unix()) / 86400))
}

// evalCerts tek sunucunun sertifikalarını eşiklerle karşılaştırır (evalMu
// altında). Parmak izi başına tek uyarı: aynı sertifika dosyada ve portta
// görünse de bir olay açılır; yoksayılan satırlar sayılmaz. Bir eşik aşılınca
// bir kez bildirilir, sertifika yenilenince (ya da görünmez olunca) olay kapanır.
func (s *Service) evalCerts(ctx context.Context, p store.Probe, thresholds []int, now time.Time) error {
	alerts, err := s.store.CertAlerts(ctx, p.ID)
	if err != nil {
		return err
	}
	host := hostOf(p)
	if !p.Active || !p.Metrics {
		for _, a := range alerts {
			if err := s.store.CloseCertAlert(ctx, a.ID, now.Unix(), store.ServerIncidentResolveDisabled); err != nil {
				return err
			}
		}
		return nil
	}
	if s.InMaintenance(p.ID, now) {
		return nil // bakımda olay açılmaz; bitince aynı eşik yeniden değerlendirilir
	}
	certs, err := s.store.ServerCerts(ctx, p.ID)
	if err != nil {
		return err
	}
	byFP := map[string]store.InvCert{}
	ignored := map[string]bool{}
	for _, c := range certs {
		if c.Ignored {
			ignored[c.Fingerprint] = true
		}
		if _, ok := byFP[c.Fingerprint]; !ok && c.Fingerprint != "" {
			byFP[c.Fingerprint] = c
		}
	}
	open := map[string]store.CertAlert{}
	for _, a := range alerts {
		open[a.Fingerprint] = a
	}
	rule := func(id int64, stage int) *store.ServerAlert {
		return &store.ServerAlert{ID: id, ProbeID: p.ID, Metric: "cert", Threshold: float64(stage),
			WarnThreshold: float64(stage), Level: certLevel(stage, thresholds)}
	}
	for fp, c := range byFP {
		days := certDaysLeft(c.NotAfter, now)
		stage := certStage(days, thresholds)
		if shortLived(c, stage) {
			stage = 0
		}
		a, isOpen := open[fp]
		if stage == 0 || ignored[fp] {
			continue // kapatma aşağıda: open'da kalır
		}
		delete(open, fp)
		v := float64(days)
		if !isOpen {
			a = store.CertAlert{ServerID: p.ID, Fingerprint: fp, Name: certName(c), NotAfter: c.NotAfter, Stage: stage}
			r := rule(0, stage)
			if err := s.store.OpenCertAlert(ctx, &a, *r, v, now.Unix()); err != nil {
				return err
			}
			r.ID = -a.ID
			ev := s.event(notify.KindServerAlert, p, host, r, v, now, a.Name)
			ev.IncidentID, _ = s.store.OpenServerIncidentID(ctx, r.ID)
			s.notifier.Notify(ev)
			continue
		}
		if err := s.store.UpdateServerIncidentValue(ctx, -a.ID, v); err != nil {
			return err
		}
		if !tighter(stage, a.Stage) {
			continue
		}
		r := rule(-a.ID, stage)
		if err := s.store.SetCertAlertStage(ctx, a.ID, stage); err != nil {
			return err
		}
		if err := s.store.SetServerIncidentLevel(ctx, r.ID, r.Level, v, now.Unix()); err != nil {
			return err
		}
		ev := s.event(notify.KindServerAlert, p, host, r, v, now, a.Name)
		ev.IncidentID, _ = s.store.OpenServerIncidentID(ctx, r.ID)
		s.notifier.Notify(ev)
	}
	for _, a := range open {
		id, _ := s.store.OpenServerIncidentID(ctx, -a.ID)
		if err := s.store.CloseCertAlert(ctx, a.ID, now.Unix(), store.ServerIncidentResolveCert); err != nil {
			return err
		}
		r := &store.ServerAlert{ID: -a.ID, ProbeID: p.ID, Metric: "cert"}
		ev := s.event(notify.KindServerResolved, p, host, r, 0, now, a.Name)
		ev.IncidentID = id
		s.notifier.Notify(ev)
	}
	return nil
}

// certName uyarıda görünen ad: konu, yoksa yer.
func certName(c store.InvCert) string {
	if n := strings.TrimSpace(c.Subject); n != "" {
		return n
	}
	return c.Location
}
