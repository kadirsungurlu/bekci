package agentupdate

// Status panelin bir ajan için hesapladığı güncelleme durumu (arayüz rozeti).
// Panel tarafında (internal/api) hesaplanır; sunucu görünümüne ve kontrol
// noktası listesine aynı biçimde girer.
type Status struct {
	// State:
	//   current      ajan panelle aynı sürümde
	//   outdated     güncelleme var (otomatik açıksa ajan ilk yoklamada alır)
	//   updating     ajan güncellemeyi aldı, indiriyor / yeniden başlıyor
	//   failed       son güncelleme başarısız (Note nedeni; tekrar denemek için "Şimdi güncelle")
	//   unsupported  ajan bu özellikten önceki bir sürüm: bir kez yeniden kurulmalı
	//   unsigned     panelin bu platform için derlemesi imzasız: güncelleme sunulamaz
	//   unversioned  panel ya da ajan sürüm numarası taşımıyor (commit derlemesi)
	//   newer        ajan panelden daha yeni (panel geri alındı)
	//   off          ajan tarafında kapalı (AUTO_UPDATE=0)
	//   readonly     ajanın program dizini yazılamıyor (ör. uygulama imajından çalışıyor)
	//   unknown      ajan henüz bağlanmadı (sürüm bilinmiyor)
	State string `json:"state"`
	// Panel panelin (sunulabilecek) sürümü.
	Panel string `json:"panel"`
	// Note başarısızlık nedeni (failed) ya da ek açıklama.
	Note string `json:"note,omitempty"`
	// Auto ajan başına ayar (nil: genel ayar devralınır); AutoEffective
	// geçerli sonuç.
	Auto          *bool `json:"auto"`
	AutoEffective bool  `json:"auto_effective"`
	// Requested panelden "Şimdi güncelle" istendi ve henüz uygulanmadı.
	Requested bool `json:"requested"`
	// At son durum değişikliğinin zamanı (unix; updating/failed).
	At int64 `json:"at,omitempty"`
}

// Updatable panel bu ajana güncelleme teklif edebilir mi (durum outdated)?
func (s Status) Updatable() bool { return s.State == StateOutdated }

// Durum sabitleri (bkz. Status.State).
const (
	StateCurrent     = "current"
	StateOutdated    = "outdated"
	StateUpdating    = "updating"
	StateFailed      = "failed"
	StateUnsupported = "unsupported"
	StateUnsigned    = "unsigned"
	StateUnversioned = "unversioned"
	StateNewer       = "newer"
	StateOff         = "off"
	StateReadOnly    = "readonly"
	StateUnknown     = "unknown"
)
