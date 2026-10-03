// Package metrics sunucu takibinin ortak veri tipleridir: ajan ("uptime probe")
// bunları toplayıp gönderir, ana sunucu saklar ve arayüze sunar. Alan adları
// JSON sözleşmesidir (docs/PLAN.md §12); değiştirmek ajan ile sunucu arasında
// uyumsuzluk yaratır, yeni alanlar sadece eklenir.
package metrics

import "strings"

// Sınırlar: bozuk veya kötü niyetli bir ajanın veritabanını şişirmemesi için
// sunucu gelen örneği Sanitize ile bunlara kırpar.
const (
	MaxDisks      = 32
	MaxTemps      = 32
	MaxContainers = 200
	MaxText       = 128
	MaxBodyBytes  = 256 << 10
)

// Sample ajanın tek gönderimi. Stats ile Unavailable'dan yalnızca biri doludur.
type Sample struct {
	Time        int64  `json:"time"`                  // unix milisaniye (ajan saati)
	Host        *Host  `json:"host,omitempty"`        // sabit bilgiler; her örnekte gönderilir
	Stats       *Stats `json:"stats,omitempty"`       // ölçümler
	Unavailable string `json:"unavailable,omitempty"` // metrik toplanamıyorsa nedeni (ör. host bağlanmamış)
}

// Host sunucunun sabit (nadiren değişen) bilgileri.
type Host struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`       // "linux"
	Platform string `json:"platform"` // "ubuntu 24.04", "almalinux 9.4"
	Kernel   string `json:"kernel"`   // "6.8.0-45-generic"
	Arch     string `json:"arch"`     // "x86_64"
	CPUModel string `json:"cpu_model"`
	Cores    int    `json:"cores"`   // fiziksel çekirdek
	Threads  int    `json:"threads"` // mantıksal çekirdek (yük/çekirdek hesabında kullanılır)
	MemTotal uint64 `json:"mem_total"`
	BootTime int64  `json:"boot_time"` // unix saniye
	Docker   bool   `json:"docker"`    // Docker API'sine erişilebiliyor mu
}

// Stats bir örnekteki ölçümler. Hızlar (Bps) iki örnek arasındaki farktan
// hesaplanır; ajan ilk örneği yalnızca sayaçları hazırlamak için alır, göndermez.
// Toplanmış (rollup) kayıtlarda değerler kova ortalamasıdır; CPUMax kovadaki
// en yüksek CPU'dur (1 dakikalık örnekte boştur).
type Stats struct {
	CPU    float64 `json:"cpu"`               // % (0-100, tüm çekirdekler)
	CPUMax float64 `json:"cpu_max,omitempty"` // yalnızca toplanmış kayıtlarda

	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`

	MemTotal  uint64 `json:"mem_total"`
	MemUsed   uint64 `json:"mem_used"`  // önbellek/tampon hariç (free'deki "used")
	MemCache  uint64 `json:"mem_cache"` // önbellek + tampon
	SwapTotal uint64 `json:"swap_total"`
	SwapUsed  uint64 `json:"swap_used"`

	DiskReadBps  float64 `json:"disk_read_bps"` // bayt/sn, tüm fiziksel diskler
	DiskWriteBps float64 `json:"disk_write_bps"`
	NetRxBps     float64 `json:"net_rx_bps"` // bayt/sn, sanal olmayan arayüzlerin toplamı (lo, veth, docker*, br-* hariç)
	NetTxBps     float64 `json:"net_tx_bps"`

	Uptime int64 `json:"uptime"` // saniye

	Disks      []Disk      `json:"disks,omitempty"`
	Temps      []Temp      `json:"temps,omitempty"`
	Containers []Container `json:"containers,omitempty"`
}

// Disk bağlı bir dosya sistemi (sanal/geçici olanlar hariç: tmpfs, overlay, squashfs…).
type Disk struct {
	Mount  string `json:"mount"`  // host'taki bağlama noktası, ör. "/" veya "/data"
	Device string `json:"device"` // "/dev/sda1"
	FS     string `json:"fs"`     // "ext4"
	Total  uint64 `json:"total"`
	Used   uint64 `json:"used"`
}

// Temp bir sıcaklık sensörü (°C).
type Temp struct {
	Name string  `json:"name"`
	C    float64 `json:"c"`
}

// Container bir Docker konteyneri. Eski ajanlar yalnızca çalışanları, State
// olmadan gönderir; yeni ajanlar durmuş/yeniden başlayan konteynerleri de
// State ile (running | restarting | exited | paused | dead | created) bildirir;
// çalışmayan konteynerde ölçüm alanları sıfırdır.
type Container struct {
	ID       string  `json:"id"`   // kısa (12 karakter)
	Name     string  `json:"name"` // baştaki "/" olmadan
	CPU      float64 `json:"cpu"`  // % (0-100, tüm host'a göre; 4 çekirdekte tek çekirdeği dolduran = 25)
	Mem      uint64  `json:"mem"`  // bayt (önbellek hariç, docker stats'taki değer)
	MemLimit uint64  `json:"mem_limit,omitempty"`
	NetRxBps float64 `json:"net_rx_bps"`
	NetTxBps float64 `json:"net_tx_bps"`
	State    string  `json:"state,omitempty"`
}

// Running konteyner çalışıyor mu (State boşsa eski ajan: listede olan çalışır).
func (c Container) Running() bool { return c.State == "" || c.State == "running" }

// MemPct RAM kullanım yüzdesi.
func (s *Stats) MemPct() float64 { return pct(s.MemUsed, s.MemTotal) }

// SwapPct swap kullanım yüzdesi (swap yoksa 0).
func (s *Stats) SwapPct() float64 { return pct(s.SwapUsed, s.SwapTotal) }

// DiskPct en dolu diskin kullanım yüzdesi.
func (s *Stats) DiskPct() float64 {
	m := 0.0
	for _, d := range s.Disks {
		m = max(m, pct(d.Used, d.Total))
	}
	return m
}

// TempMax en sıcak sensör (sensör yoksa 0, false).
func (s *Stats) TempMax() (float64, bool) {
	if len(s.Temps) == 0 {
		return 0, false
	}
	m := s.Temps[0].C
	for _, t := range s.Temps[1:] {
		m = max(m, t.C)
	}
	return m, true
}

func pct(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(used) / float64(total)
}

// Sanitize gelen örneği sınırlara kırpar ve anlamsız değerleri düzeltir
// (negatif/NaN sayılar 0, yüzdeler 0-100, metinler MaxText).
func (s *Sample) Sanitize() {
	s.Unavailable = clip(s.Unavailable)
	if h := s.Host; h != nil {
		for _, p := range []*string{&h.Hostname, &h.OS, &h.Platform, &h.Kernel, &h.Arch, &h.CPUModel} {
			*p = clip(*p)
		}
		h.Cores, h.Threads = max(h.Cores, 0), max(h.Threads, 0)
		h.BootTime = max(h.BootTime, 0)
	}
	st := s.Stats
	if st == nil {
		return
	}
	st.CPU, st.CPUMax = pctVal(st.CPU), pctVal(st.CPUMax)
	for _, p := range []*float64{&st.Load1, &st.Load5, &st.Load15, &st.DiskReadBps, &st.DiskWriteBps, &st.NetRxBps, &st.NetTxBps} {
		*p = nonNeg(*p)
	}
	st.MemUsed, st.SwapUsed = min(st.MemUsed, st.MemTotal), min(st.SwapUsed, st.SwapTotal)
	st.Uptime = max(st.Uptime, 0)
	if len(st.Disks) > MaxDisks {
		st.Disks = st.Disks[:MaxDisks]
	}
	for i := range st.Disks {
		d := &st.Disks[i]
		d.Mount, d.Device, d.FS = clip(d.Mount), clip(d.Device), clip(d.FS)
		d.Used = min(d.Used, d.Total)
	}
	if len(st.Temps) > MaxTemps {
		st.Temps = st.Temps[:MaxTemps]
	}
	for i := range st.Temps {
		t := &st.Temps[i]
		t.Name = clip(t.Name)
		if t.C != t.C || t.C < -100 || t.C > 200 { // NaN veya anlamsız
			t.C = 0
		}
	}
	if len(st.Containers) > MaxContainers {
		st.Containers = st.Containers[:MaxContainers]
	}
	for i := range st.Containers {
		c := &st.Containers[i]
		c.ID, c.Name, c.State = clip(c.ID), clip(c.Name), clip(c.State)
		c.CPU = pctVal(c.CPU)
		c.NetRxBps, c.NetTxBps = nonNeg(c.NetRxBps), nonNeg(c.NetTxBps)
	}
}

func clip(s string) string {
	s = strings.ToValidUTF8(strings.TrimSpace(s), "")
	if r := []rune(s); len(r) > MaxText {
		return string(r[:MaxText])
	}
	return s
}

func nonNeg(v float64) float64 {
	if v != v || v < 0 || v > 1e15 { // NaN, negatif veya taşma
		return 0
	}
	return v
}

func pctVal(v float64) float64 { return min(nonNeg(v), 100) }
