package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/kadirsungurlu/bekci/internal/probe"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Mesh: ajan kalp atışı ve eş listesi (tel sözleşmesi: probe/meshtypes.go).
//
// POST /api/probe/mesh ajanın panel aralığındaki kalp atışıdır: Main <-> probe
// kenarlarını ve ajanın eşlere ölçtüğü kenarları probe_links'e yazar, etkin
// zamanlamayı döner. GET /api/probe/mesh/peers ajanın eşlerini (adres + çift
// anahtarı) verir; ajan yanıttaki peers_version değişince yeniden ister.

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("POST /api/probe/mesh", s.probeOnly(s.probeMeshHeartbeat))
		mux.Handle("GET /api/probe/mesh/peers", s.probeOnly(s.probeMeshPeers))
	})
}

// meshBodyMax kalp atışı gövdesinin üst sınırı (eş başına ~100 bayt).
const meshBodyMax = 256 << 10

// meshState ajanların bildirdiği dinleme portları (yalnız bellekte; ajan her
// kalp atışında yeniden bildirir) ve "mesh" yayınının seyreltilmesi.
// Sıfır değeri kullanılabilir.
type meshState struct {
	mu    sync.Mutex
	ports map[int64]int
	pubAt time.Time
}

func (m *meshState) setPort(id int64, port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ports == nil {
		m.ports = map[int64]int{}
	}
	if port <= 0 || port > 65535 {
		delete(m.ports, id)
		return
	}
	m.ports[id] = port
}

func (m *meshState) port(id int64) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ports[id]
}

// shouldPublish en fazla every aralığında bir true döner.
func (m *meshState) shouldPublish(now time.Time, every time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.pubAt.IsZero() && now.Sub(m.pubAt) < every {
		return false
	}
	m.pubAt = now
	return true
}

func meshTimingOf(t store.ProbeTiming) probe.MeshTiming {
	return probe.MeshTiming{MeshEnabled: t.MeshEnabled, PingSec: t.PingSec, LossThreshold: t.LossThreshold, PanelSec: t.PanelSec}
}

func (s *Server) probeMeshHeartbeat(w http.ResponseWriter, r *http.Request) {
	p := probeFrom(r)
	if p.Kind != store.ProbeKindLocation {
		// Sunucu ajanı mesh'e katılmaz; 404 eski panel gibi geri çekilmesini sağlar.
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	var hb probe.MeshHeartbeat
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, meshBodyMax)).Decode(&hb); err != nil {
		writeError(w, http.StatusBadRequest, "Geçersiz istek gövdesi")
		return
	}
	ctx := r.Context()
	now := s.now()
	set := s.engine.Settings()
	others, err := s.meshOthers(ctx, p.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	known := make(map[int64]bool, len(others))
	for _, o := range others {
		known[o.ID] = true
	}
	ts := now.Unix()
	links := []store.ProbeLink{
		// Main kalp atışını aldı.
		{Src: store.MeshMainID, Dst: p.ID, State: store.LinkUp, LastOKAt: ts, UpdatedAt: ts},
		// Ajan Main'e ulaştı; öncesindeki başarısız deneme sayısı bilgi olarak.
		{Src: p.ID, Dst: store.MeshMainID, State: store.LinkUp, LossStreak: max(hb.MainLoss, 0), RTTms: min(max(hb.MainRTTms, 0), 600000),
			LastOKAt: ts, UpdatedAt: ts},
	}
	seen := map[int64]bool{}
	for _, l := range hb.Links {
		if !known[l.Peer] || seen[l.Peer] || !store.ValidLinkState(l.State) {
			continue // bilinmeyen eş, tekrar ya da geçersiz durum
		}
		seen[l.Peer] = true
		links = append(links, store.ProbeLink{Src: p.ID, Dst: l.Peer, State: l.State, RTTms: max(l.RTTms, 0),
			LossStreak: max(l.LossStreak, 0), LastOKAt: max(min(l.LastOKAt, ts), 0), UpdatedAt: ts})
	}
	if err := s.store.UpsertProbeLinks(ctx, links); err != nil {
		s.dbError(w, err)
		return
	}
	s.mesh.setPort(p.ID, hb.ListenPort)
	s.engine.ProbeHeartbeat(p.ID, now)

	peers, err := s.meshPeersOf(ctx, p, others, set.MeshOn(), now)
	if err != nil {
		s.dbError(w, err)
		return
	}
	// Panel ekranı için güncel bağlantı listesi; en sık genel panel aralığında bir.
	if s.mesh.shouldPublish(now, time.Duration(set.MeshTiming().PanelSec)*time.Second) {
		if all, err := s.store.ListProbeLinks(ctx); err == nil {
			s.hub.Publish("mesh", map[string]any{"links": all})
		}
	}
	writeJSON(w, http.StatusOK, probe.MeshHeartbeatResponse{
		Timing:       meshTimingOf(store.EffectiveProbeTiming(p, set)),
		PeersVersion: peers.Version,
		ServerTime:   now.UnixMilli(),
		OOB:          probeOOBConfig(p),
	})
}

func (s *Server) probeMeshPeers(w http.ResponseWriter, r *http.Request) {
	p := probeFrom(r)
	if p.Kind != store.ProbeKindLocation {
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	others, err := s.meshOthers(r.Context(), p.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	resp, err := s.meshPeersOf(r.Context(), p, others, s.engine.Settings().MeshOn(), s.now())
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// meshOthers ajanın olası eşleri: etkin kontrol noktaları (kendisi hariç).
func (s *Server) meshOthers(ctx context.Context, self int64) ([]store.Probe, error) {
	all, err := s.store.ListProbesOfKind(ctx, store.ProbeKindLocation)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, p := range all {
		if p.Active && p.ID != self {
			out = append(out, p)
		}
	}
	return out, nil
}

// meshPeersOf ajanın eş listesi ve sürümü. Mesh kapalıysa liste boştur.
// Sürüm eşlerin kimlik, adres ve anahtarından türetilir: eş eklenince,
// silinince/pasifleşince, adresi ya da anahtarı değişince sürüm de değişir.
func (s *Server) meshPeersOf(ctx context.Context, self store.Probe, others []store.Probe, on bool, now time.Time) (probe.MeshPeersResponse, error) {
	peers := []probe.MeshPeer{}
	if on {
		for _, o := range others {
			key, err := s.store.PairKey(ctx, self.ID, o.ID, now.Unix())
			if err != nil {
				return probe.MeshPeersResponse{}, err
			}
			peers = append(peers, probe.MeshPeer{ID: o.ID, Name: o.Name, Addr: s.meshAddrOf(o), Key: key})
		}
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].ID < peers[j].ID })
	h := sha256.New()
	for _, p := range peers {
		h.Write([]byte(strconv.FormatInt(p.ID, 10) + "|" + p.Addr + "|" + p.Key + "\n"))
	}
	return probe.MeshPeersResponse{Self: self.ID, Version: hex.EncodeToString(h.Sum(nil))[:16], Peers: peers}, nil
}

// meshAddrOf eşin dinleme adresi: "-" dinlemiyor; elle girilmiş adres olduğu
// gibi; boşsa ajanın bildirdiği port ile görülen IP; port bilinmiyorsa "".
func (s *Server) meshAddrOf(p store.Probe) string {
	switch {
	case p.MeshAddr == "-":
		return ""
	case p.MeshAddr != "":
		return p.MeshAddr
	}
	if port := s.mesh.port(p.ID); port > 0 && p.LastIP != "" {
		return net.JoinHostPort(p.LastIP, strconv.Itoa(port))
	}
	return ""
}
