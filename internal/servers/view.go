package servers

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/kadirsungurlu/bekci/internal/agentupdate"
	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Sunucu durumları (View.State).
const (
	StateOnline      = "online"
	StateOffline     = "offline"     // son örnek OfflineAfter'dan eski
	StateUnavailable = "unavailable" // ajan "toplayamıyorum" dedi
	StateWaiting     = "waiting"     // hiç örnek gelmedi
	StateDisabled    = "disabled"    // ajan veya metrik toplama kapalı
)

// State ajanın sunucu takibindeki durumu (bağlantı bilgisi olmadan; bkz. StateOf).
func State(p store.Probe, now time.Time) string { return stateWith(p, now, false) }

// StateOf ajanın durumu; uzun yoklama bağlantısı koptuysa (yeni ajanlar)
// OfflineAfter beklenmeden çevrimdışı.
func (s *Service) StateOf(p store.Probe, now time.Time) string {
	_, gone := s.goneAt(p.ID)
	return stateWith(p, now, gone)
}

func stateWith(p store.Probe, now time.Time, gone bool) string {
	stale := func(t int64) bool { return now.Sub(time.Unix(t, 0)) > OfflineAfter }
	switch {
	case !p.Active || !p.Metrics:
		return StateDisabled
	case gone && p.MetricsAt > 0:
		// Ajan kapandı, silindi ya da ağ koptu: son örneğin yaşı beklenmez.
		return StateOffline
	case p.MetricsNote != "" && (p.MetricsAt == 0 || !stale(p.LastSeenAt)):
		// Ajan kapanırsa (son istek de eski) ve önceden örnek göndermişse çevrimdışı sayılır.
		return StateUnavailable
	case p.MetricsAt == 0:
		return StateWaiting
	case stale(p.MetricsAt):
		return StateOffline
	}
	return StateOnline
}

// View arayüzün gördüğü sunucu (docs/PLAN.md §12.9 ServerView).
type View struct {
	ID             int64          `json:"id"`
	Name           string         `json:"name"`
	Active         bool           `json:"active"`
	Metrics        bool           `json:"metrics"`
	State          string         `json:"state"`
	Note           string         `json:"note"`
	Interval       int            `json:"interval"`
	LastSeenAt     int64          `json:"last_seen_at"`
	MetricsAt      int64          `json:"metrics_at"`
	Version        string         `json:"version"`
	Host           *metrics.Host  `json:"host"`
	Latest         *metrics.Stats `json:"latest"`
	ContainerCount int            `json:"container_count"`
	TempMax        *float64       `json:"temp_max"`
	Firing         []string       `json:"firing"`
	// IP kilidi yalnızca yöneticiye gösterilir: API katmanı yönetici olmayan
	// kullanıcıya giden liste/detay yanıtında ve canlı akıştaki "server"
	// olayında bu alanları atar (HideIPLock, api.viewerEvent).
	IPLock   bool   `json:"ip_lock"`   // yalnızca kilitli IP'den bağlanabilir mi
	LockedIP string `json:"locked_ip"` // sabitlenmiş IP(ler) (boş: henüz bağlanmadı)
	// IP ajanın son bağlandığı adres (yalnızca yöneticiye, HideIPLock atar).
	IP string `json:"ip,omitempty"`
	// CPUHist liste ve canlı akışta son bir saatin dakikalık CPU değerleri
	// (eskiden yeniye; küçük grafik için). Bellekteki uyarı geçmişinden gelir,
	// veritabanına gidilmez; ajan açılıştan beri veri göndermediyse boştur.
	CPUHist []float64 `json:"cpu_hist,omitempty"`
	// InMaintenance sunucu şu an bir bakım penceresinde (uyarı ve çevrimdışı
	// bildirimi gitmez, yeni uyarı açılmaz).
	InMaintenance bool `json:"in_maintenance"`
	// Update ajanın güncelleme durumu (API katmanı hesaplar, SetUpdateStatus;
	// nil: hesaplayıcı yok).
	Update *agentupdate.Status `json:"update,omitempty"`
}

// SetUpdateStatus ajan güncelleme durumunu hesaplayan işlevi bağlar (panel
// sürümü ve imzaları bilen API katmanı); görünümlere ve canlı akışa girer.
func (s *Service) SetUpdateStatus(f func(store.Probe) *agentupdate.Status) { s.updateStatus = f }

// HideIPLock yönetici olmayan kullanıcıya gidecek görünümden IP kilidi
// bilgisini (sunucunun sabitlenmiş IP adresi dahil) çıkarır.
func (v *View) HideIPLock() { v.IPLock, v.LockedIP, v.IP = false, "", "" }

// Localize ajanın Türkçe gönderdiği/saklanan notu ("metrik toplanamıyor"
// nedeni) istenen dile çevirir (bkz. i18n.Message). Canlı akıştaki "server"
// olayı abone başına ayrıca çevrilir.
func (v *View) Localize(lang string) { v.Note = i18n.Message(lang, v.Note) }

// View ajanın görünümü. full=false (liste ve canlı akış) ise son örnekte
// konteyner ve sıcaklık listeleri yer almaz; yerine sayı ve en yüksek değer gelir.
func (s *Service) View(ctx context.Context, p store.Probe, rules []store.ServerAlert, full bool) View {
	v := View{
		ID: p.ID, Name: p.Name, Active: p.Active, Metrics: p.Metrics, State: s.StateOf(p, s.now()),
		Note: p.MetricsNote, Interval: Interval, LastSeenAt: p.LastSeenAt, MetricsAt: p.MetricsAt,
		Version: p.Version, Host: hostOf(p), Firing: []string{},
		IPLock: p.IPLock, LockedIP: p.LockedIP, IP: p.LastIP,
	}
	v.InMaintenance = s.InMaintenance(p.ID, s.now())
	if s.updateStatus != nil {
		v.Update = s.updateStatus(p)
	}
	if st := s.Latest(ctx, p); st != nil {
		for _, c := range st.Containers {
			if c.Running() {
				v.ContainerCount++
			}
		}
		if t, ok := st.TempMax(); ok {
			v.TempMax = &t
		}
		if !full {
			st.Containers, st.Temps = nil, nil
		}
		v.Latest = st
	}
	if !full {
		v.CPUHist = s.cpuHistory(p.ID)
	}
	for _, a := range rules {
		if a.Firing && a.Active {
			v.Firing = append(v.Firing, a.Metric)
		}
	}
	return v
}

// cpuHistory bellekteki son bir saatin CPU değerleri (bir ondalık).
func (s *Service) cpuHistory(id int64) []float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := s.history[id]
	if len(h) == 0 {
		return nil
	}
	out := make([]float64, len(h))
	for i, p := range h {
		out[i] = math.Round(p.cpu*10) / 10
	}
	return out
}

// Grafik serisi -------------------------------------------------------------------------

// Ranges geçerli grafik aralıkları: süre ve çözünürlük (dk).
var Ranges = map[string]struct {
	Span time.Duration
	Res  int
}{
	"1h":  {time.Hour, store.ServerRes1},
	"24h": {24 * time.Hour, store.ServerRes1},
	"7d":  {7 * 24 * time.Hour, store.ServerRes10},
	"30d": {30 * 24 * time.Hour, store.ServerRes60},
}

// Point grafik noktası (§12.9). Temp en sıcak sensör (yoksa null).
type Point struct {
	T            int64            `json:"t"`
	CPU          float64          `json:"cpu"`
	CPUMax       float64          `json:"cpu_max"`
	Load1        float64          `json:"load1"`
	Load5        float64          `json:"load5"`
	Load15       float64          `json:"load15"`
	MemUsed      uint64           `json:"mem_used"`
	MemCache     uint64           `json:"mem_cache"`
	MemTotal     uint64           `json:"mem_total"`
	SwapUsed     uint64           `json:"swap_used"`
	SwapTotal    uint64           `json:"swap_total"`
	DiskReadBps  float64          `json:"disk_read_bps"`
	DiskWriteBps float64          `json:"disk_write_bps"`
	NetRxBps     float64          `json:"net_rx_bps"`
	NetTxBps     float64          `json:"net_tx_bps"`
	DiskPct      float64          `json:"disk_pct"`
	Disks        []PointDisk      `json:"disks"` // bölüm başına doluluk
	Temp         *float64         `json:"temp"`
	Containers   []PointContainer `json:"containers"`
}

type PointDisk struct {
	Mount string  `json:"mount"`
	Pct   float64 `json:"pct"`
}

type PointContainer struct {
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
	Mem  uint64  `json:"mem"`
}

// Series grafik yanıtı.
type Series struct {
	Range    string  `json:"range"`
	Res      int     `json:"res"`
	From     int64   `json:"from"`
	To       int64   `json:"to"`
	Interval int     `json:"interval"` // noktalar arası (sn); arayüz 2 katından büyük boşlukta çizgiyi keser
	Points   []Point `json:"points"`
}

// Series ajanın rng aralığındaki grafik serisi; rng geçersizse ok=false.
func (s *Service) Series(ctx context.Context, id int64, rng string) (Series, bool, error) {
	r, ok := Ranges[rng]
	if !ok {
		return Series{}, false, nil
	}
	now := s.now().Unix()
	out := Series{Range: rng, Res: r.Res, From: now - int64(r.Span/time.Second), To: now, Interval: r.Res * 60, Points: []Point{}}
	rows, err := s.store.ServerStats(ctx, id, r.Res, out.From, now+1)
	if err != nil {
		return out, true, err
	}
	for _, row := range rows {
		var st metrics.Stats
		if json.Unmarshal(row.Data, &st) != nil {
			continue
		}
		out.Points = append(out.Points, pointFrom(row.Time, &st))
	}
	return out, true, nil
}

func pointFrom(t int64, st *metrics.Stats) Point {
	p := Point{
		T: t, CPU: r2(st.CPU), CPUMax: r2(max(st.CPUMax, st.CPU)),
		Load1: r2(st.Load1), Load5: r2(st.Load5), Load15: r2(st.Load15),
		MemUsed: st.MemUsed, MemCache: st.MemCache, MemTotal: st.MemTotal,
		SwapUsed: st.SwapUsed, SwapTotal: st.SwapTotal,
		DiskReadBps: math.Round(st.DiskReadBps), DiskWriteBps: math.Round(st.DiskWriteBps),
		NetRxBps: math.Round(st.NetRxBps), NetTxBps: math.Round(st.NetTxBps),
		DiskPct: r2(st.DiskPct()), Disks: make([]PointDisk, 0, len(st.Disks)),
		Containers: make([]PointContainer, 0, len(st.Containers)),
	}
	for _, d := range st.Disks {
		pct := 0.0
		if d.Total > 0 {
			pct = 100 * float64(d.Used) / float64(d.Total)
		}
		p.Disks = append(p.Disks, PointDisk{Mount: d.Mount, Pct: r2(pct)})
	}
	if t, ok := st.TempMax(); ok {
		t = r2(t)
		p.Temp = &t
	}
	for _, c := range st.Containers {
		if !c.Running() {
			continue // durmuş konteynerin grafikte çizgisi olmaz
		}
		p.Containers = append(p.Containers, PointContainer{Name: c.Name, CPU: r2(c.CPU), Mem: c.Mem})
	}
	return p
}

// r2 iki ondalık basamağa yuvarlar (yanıt boyutu için).
func r2(v float64) float64 { return math.Round(v*100) / 100 }
