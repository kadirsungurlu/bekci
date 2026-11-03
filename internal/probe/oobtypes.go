package probe

// OOB tel sözleşmesi (panel <-> ajan). Panel tarafı (internal/api/probes_oob.go)
// aynı alanları ve başlıkları kullanır; değiştirirken iki tarafı birlikte güncelle.

// OOBConfig panelin iş listesi ve kalp atışı yanıtında verdiği ikinci hat
// ayarı. IP boş: OOB yok (ajan ana hatta kalır, başlık göndermez).
type OOBConfig struct {
	IP   string `json:"ip"`
	Name string `json:"name"`
}

// Ajan başlıkları (OOB tanımlıysa her istekte).
const (
	// HeaderUplink etkin hat: "primary" | "oob".
	HeaderUplink = "X-Probe-Uplink"
	// HeaderOOBIface OOB arayüzü bulundu mu: "ok" | "missing".
	HeaderOOBIface = "X-Probe-OOB-Iface"
)
