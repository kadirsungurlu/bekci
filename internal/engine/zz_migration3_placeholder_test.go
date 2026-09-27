package engine

import "github.com/kadirsa1105/uptime-kadir-app/internal/store"

// Bu dalda migration 3 (paralel geliştirilen başka bir özellik) yok; testlerde
// boş bir yer tutucu kaydedilir (gerçek migration kayıtlıysa RegisterMigration
// panikler ve yer tutucu atlanır). Dallar birleştirildiğinde silinebilir.
func init() {
	defer func() { recover() }()
	store.RegisterMigration(3, "SELECT 1")
}
