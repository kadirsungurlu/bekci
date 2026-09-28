package metrics

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Docker konteyner istatistikleri Docker Engine API'sinden okunur
// (internal/check/docker.go'daki unix soket istemcisiyle aynı yaklaşım).
// Soket salt okunur bağlanabilir; yalnızca GET istekleri yapılır.

const dockerParallel = 8 // aynı anda en fazla stats isteği

type dockerClient struct {
	hc   *http.Client
	base string
	sock string // unix soketi ise yolu (yoksa Docker kurulu değil sayılır)
}

// dockerEndpoint ayarlardan Docker adresini seçer: DockerSocket, DOCKER_HOST
// veya varsayılan soket. "-" Docker'ı kapatır.
func dockerEndpoint(o Options) string {
	if o.DockerSocket != "" {
		if o.DockerSocket == "-" || strings.Contains(o.DockerSocket, "://") {
			return o.DockerSocket
		}
		return "unix://" + o.DockerSocket
	}
	if h := strings.TrimSpace(o.Getenv("DOCKER_HOST")); h != "" {
		return h
	}
	return "unix:///var/run/docker.sock"
}

func newDockerClient(endpoint string) *dockerClient {
	switch {
	case strings.HasPrefix(endpoint, "unix://"):
		sock := strings.TrimPrefix(endpoint, "unix://")
		if sock == "" {
			return nil
		}
		tr := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			},
			MaxIdleConnsPerHost: dockerParallel,
		}
		return &dockerClient{hc: &http.Client{Transport: tr}, base: "http://docker", sock: sock}
	case strings.HasPrefix(endpoint, "tcp://"):
		return &dockerClient{hc: &http.Client{}, base: "http://" + strings.TrimPrefix(endpoint, "tcp://")}
	case strings.HasPrefix(endpoint, "http://"), strings.HasPrefix(endpoint, "https://"):
		return &dockerClient{hc: &http.Client{}, base: strings.TrimRight(endpoint, "/")}
	default:
		return nil // "-" veya bilinmeyen biçim: Docker kapalı
	}
}

type dockerListItem struct {
	ID    string   `json:"Id"`
	Names []string `json:"Names"`
}

type dockerStats struct {
	Read     string `json:"read"`
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"` // ns, kümülatif
		} `json:"cpu_usage"`
	} `json:"cpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
}

func (d *dockerClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := d.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return fmt.Errorf("Docker API yanıtı: %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out)
}

// contCounters bir konteynerin önceki örnekteki sayaçları.
type contCounters struct {
	at     time.Time
	cpu    uint64
	rx, tx uint64
}

// containers çalışan konteynerleri ve istatistiklerini okur; Docker'a
// ulaşılamıyorsa (nil, false). Yeni başlayan konteynerin ilk örneğinde
// fark olmadığı için CPU ve ağ 0 gelir. İstatistiği süre sınırında
// okunamayan konteyner bu örnekte hiç gönderilmez (CPU/bellek 0 görünüp
// grafiklerde yanlış düşüş oluşturmasın).
func (c *Collector) containers(ctx context.Context, threads int, hostMem uint64) ([]Container, bool) {
	d := c.docker
	if d == nil {
		return nil, false
	}
	if d.sock != "" && !fileExists(d.sock) {
		c.prevCont = nil
		return nil, false // Docker kurulu değil veya soket bağlanmamış: sessizce atla
	}
	ctx, cancel := context.WithTimeout(ctx, c.opts.DockerTimeout)
	defer cancel()
	var list []dockerListItem
	if err := d.get(ctx, "/containers/json", &list); err != nil {
		if msg := err.Error(); msg != c.dockerErr {
			c.dockerErr = msg
			c.opts.Log.Warn("Docker API'sine ulaşılamıyor, konteyner istatistikleri alınamadı", "hata", err)
		}
		c.dockerOK, c.prevCont = false, nil
		return nil, false
	}
	if !c.dockerOK {
		c.opts.Log.Info("Docker konteynerleri izleniyor", "konteyner", len(list))
	}
	c.dockerOK, c.dockerErr = true, ""

	for i := range list {
		if len(list[i].Names) > 0 {
			list[i].Names[0] = strings.TrimPrefix(list[i].Names[0], "/")
		} else {
			list[i].Names = []string{shortID(list[i].ID)}
		}
	}
	slices.SortFunc(list, func(a, b dockerListItem) int { return cmp.Compare(a.Names[0], b.Names[0]) })
	if len(list) > MaxContainers {
		list = list[:MaxContainers]
	}

	stats := make([]*dockerStats, len(list))
	sem := make(chan struct{}, dockerParallel)
	var wg sync.WaitGroup
	for i, it := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var s dockerStats
			if err := d.get(ctx, "/containers/"+url.PathEscape(it.ID)+"/stats?stream=false&one-shot=true", &s); err == nil {
				stats[i] = &s
			}
		}()
	}
	wg.Wait()

	now := c.opts.Now()
	cur := make(map[string]contCounters, len(list))
	out := make([]Container, 0, len(list))
	for i, it := range list {
		ct := Container{ID: shortID(it.ID), Name: it.Names[0]}
		prev, hasPrev := c.prevCont[it.ID]
		s := stats[i]
		if s == nil {
			if hasPrev {
				cur[it.ID] = prev // okunamadı: sonraki örnek daha uzun aralıkla hesaplar
			}
			continue // değerler bilinmiyor: sıfır gönderilmez, konteyner bu örnekte atlanır
		}
		cc := contCounters{at: now, cpu: s.CPUStats.CPUUsage.TotalUsage}
		if t, err := time.Parse(time.RFC3339Nano, s.Read); err == nil && t.Year() > 2000 {
			cc.at = t // Docker'ın ölçüm anı; istekler farklı sürelerde yanıtlansa da doğru aralık
		}
		for _, n := range s.Networks {
			cc.rx += n.RxBytes
			cc.tx += n.TxBytes
		}
		cur[it.ID] = cc
		ct.Mem = containerMem(s.MemoryStats.Usage, s.MemoryStats.Stats)
		if l := s.MemoryStats.Limit; l > 0 && (hostMem == 0 || l < hostMem) {
			ct.MemLimit = l // sınırsız konteynerde Docker host belleğini döner; gönderilmez
		}
		if hasPrev {
			el := cc.at.Sub(prev.at)
			ct.CPU = containerCPU(prev.cpu, cc.cpu, el, threads)
			if sec := el.Seconds(); sec > 0 {
				if cc.rx >= prev.rx {
					ct.NetRxBps = math.Round(float64(cc.rx-prev.rx) / sec)
				}
				if cc.tx >= prev.tx {
					ct.NetTxBps = math.Round(float64(cc.tx-prev.tx) / sec)
				}
			}
		}
		out = append(out, ct)
	}
	c.prevCont = cur
	return out, true
}

// containerCPU konteynerin CPU kullanımı, tüm host'a göre yüzde: 4 çekirdekli
// host'ta bir çekirdeği dolduran konteyner 25 (docker stats'ta 100 görünür).
func containerCPU(prevNs, curNs uint64, elapsed time.Duration, threads int) float64 {
	if curNs < prevNs || elapsed <= 0 || threads <= 0 {
		return 0 // yeniden başlatılan konteynerde sayaç sıfırlanır
	}
	return round2(min(100*float64(curNs-prevNs)/(float64(elapsed.Nanoseconds())*float64(threads)), 100))
}

// containerMem docker stats'taki bellek değeri: kullanım - inactive_file
// (cgroup v1'de total_inactive_file); sayfa önbelleğinin geri alınabilir kısmı sayılmaz.
func containerMem(usage uint64, st map[string]uint64) uint64 {
	if v, ok := st["total_inactive_file"]; ok && v < usage {
		return usage - v
	}
	if v, ok := st["inactive_file"]; ok && v < usage {
		return usage - v
	}
	return usage
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
