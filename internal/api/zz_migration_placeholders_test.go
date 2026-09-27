package api

import "github.com/kadirsa1105/uptime-kadir-app/internal/store"

// Bu dalda migration 3 ve 4 (paralel geliştirilen başka özellikler) yok;
// testlerde boş yer tutucular kaydedilir (store paketinin init'leri bu
// dosyadan önce çalışır; gerçek migration kayıtlıysa RegisterMigration panikler
// ve yer tutucu atlanır). Dallar birleştirildiğinde silinebilir.
func init() {
	for _, v := range []int{3, 4} {
		func() {
			defer func() { recover() }()
			store.RegisterMigration(v, "SELECT 1")
		}()
	}
}
