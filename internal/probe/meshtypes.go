package probe

// Mesh tel sözleşmesi (panel <-> ajan). Panel tarafı (internal/api) aynı JSON
// alanlarını kullanır; değiştirirken iki tarafı birlikte güncelle.

// MeshPeer GET /api/probe/mesh/peers yanıtındaki eş.
type MeshPeer struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Addr eşin dinlediği host:port; "" eş dinlemiyor (NAT ya da MESH_ADDR=-).
	Addr string `json:"addr"`
	// Key çift HMAC anahtarı (hex, 32 bayt); yalnız bu ajanın çiftleri gelir.
	Key string `json:"key"`
}

// MeshPeersResponse GET /api/probe/mesh/peers yanıtı.
type MeshPeersResponse struct {
	// Self isteği yapan ajanın kimliği (mesh çerçevelerinde gönderen id).
	Self    int64      `json:"self"`
	Version string     `json:"version"`
	Peers   []MeshPeer `json:"peers"`
}

// MeshLinkReport ajanın bir eşe giden yönlü bağlantı ölçümü (Src = ajan).
type MeshLinkReport struct {
	Peer       int64  `json:"peer"`
	State      string `json:"state"` // up | down | unknown
	RTTms      int64  `json:"rtt_ms"`
	LossStreak int    `json:"loss_streak"`
	LastOKAt   int64  `json:"last_ok_at"` // unix sn; 0: hiç
}

// MeshTiming panelin bildirdiği etkin zamanlama (store.ProbeTiming JSON'u).
type MeshTiming struct {
	MeshEnabled   bool `json:"mesh_enabled"`
	PingSec       int  `json:"ping_sec"`
	LossThreshold int  `json:"loss_threshold"`
	PanelSec      int  `json:"panel_sec"`
}

// MeshHeartbeat POST /api/probe/mesh gövdesi (kalp atışı, panel aralığında).
type MeshHeartbeat struct {
	Links []MeshLinkReport `json:"links"`
	// ListenPort ajanın mesh dinleme portu; 0: dinlemiyor.
	ListenPort int `json:"listen_port"`
	// MainLoss ajanın bir önceki başarılı kalp atışından beri art arda
	// başaramadığı kalp atışı sayısı (probe -> Main yönü).
	MainLoss     int    `json:"main_loss"`
	PeersVersion string `json:"peers_version"`
}

// MeshHeartbeatResponse kalp atışı yanıtı.
type MeshHeartbeatResponse struct {
	Timing       MeshTiming `json:"timing"`
	PeersVersion string     `json:"peers_version"`
	ServerTime   int64      `json:"server_time"`
}
