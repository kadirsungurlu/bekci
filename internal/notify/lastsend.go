package notify

import "time"

// SendStatus bir kanala yapılan son gönderimin sonucu (canlı topoloji).
// Bellekte tutulur: panel yeniden başlayınca ilk gönderime kadar boştur.
type SendStatus struct {
	At int64 `json:"at"` // unix sn
	OK bool  `json:"ok"`
}

func (d *Dispatcher) noteSend(channelID int64, at time.Time, err error) {
	d.lastSend.Store(channelID, SendStatus{At: at.Unix(), OK: err == nil})
}

// LastSends kanal başına son gönderim sonucu (yalnız bu açılıştan beri gönderim yapılan kanallar).
func (d *Dispatcher) LastSends() map[int64]SendStatus {
	out := map[int64]SendStatus{}
	d.lastSend.Range(func(k, v any) bool {
		out[k.(int64)] = v.(SendStatus)
		return true
	})
	return out
}
