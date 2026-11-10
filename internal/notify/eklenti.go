package notify

import (
	"context"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// ExtKind eklentinin (Pro) kendi olay türü. Kanallar Channels ile seçilir (nil:
// sunucu/monitör kanalları), başlık Title ile üretilir; tekilleştirme anahtarı
// tür + Mount + zaman. Türler Kinds süzgecinde yoktur.
type ExtKind struct {
	Channels func(ctx context.Context, st *store.Store, ev Event) ([]store.Notification, error)
	Title    func(e Event) string
}

var extKinds = map[string]ExtKind{}

// RegisterKind eklenti olay türü kaydeder (init içinde; açık derlemede çağrılmaz).
func RegisterKind(kind string, k ExtKind) {
	if _, dup := extKinds[kind]; dup {
		panic("notify.RegisterKind: " + kind + " iki kez kaydedildi")
	}
	extKinds[kind] = k
}
