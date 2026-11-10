package uygulama

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kadirsungurlu/bekci/kanca"
)

// kancaBaslat Pro modülünün başlangıç kancalarını sırayla çalıştırır. Hata ya
// da panik günlüğe yazılır; panel çalışmayı sürdürür.
func kancaBaslat(ctx context.Context, p *kanca.Panel, log *slog.Logger) {
	for i, f := range kanca.Baslangiclar() {
		if err := kancaCagir(ctx, p, f); err != nil {
			log.Error("pro başlangıç kancası başarısız", "sira", i, "hata", err)
		}
	}
}

func kancaCagir(ctx context.Context, p *kanca.Panel, f func(context.Context, *kanca.Panel) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panik: %v", r)
		}
	}()
	return f(ctx, p)
}
