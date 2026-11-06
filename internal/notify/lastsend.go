package notify

import (
	"context"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// SendStatus bir kanala yapılan son gönderimin sonucu (canlı topoloji).
// DB'de tutulur (store.channel_last_send): yeniden başlatmada kaybolmaz.
type SendStatus = store.ChannelSend

func (d *Dispatcher) noteSend(channelID int64, at time.Time, err error) {
	if d.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if werr := d.store.SetChannelSend(ctx, channelID, at.Unix(), sendErrorText(err)); werr != nil && d.log != nil {
		d.log.Error("kanal son gönderim yazılamadı", "kanal", channelID, "hata", werr)
	}
}

// sendErrorText saklanacak hata metni: adres (belirteç içerebilir) çıkarılır, kısaltılır.
func sendErrorText(err error) string {
	if err == nil {
		return ""
	}
	msg := []rune(redactURLError(err).Error())
	if len(msg) > 240 {
		msg = msg[:240]
	}
	if len(msg) == 0 {
		return "?"
	}
	return string(msg)
}

// LastSends kanal başına son gönderim sonucu.
func (d *Dispatcher) LastSends(ctx context.Context) (map[int64]SendStatus, error) {
	return d.store.ChannelSends(ctx)
}
