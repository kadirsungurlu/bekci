package uygulama

import (
	"log/slog"

	"github.com/kadirsungurlu/bekci/internal/probe"
)

// Eklenti noktaları: açık derlemede boştur. Pro derlemesi (bekci-pro, ic/ ağacı
// go -overlay ile bu pakete eklenir) init içinde doldurur; bkz. docs/PRO-PAKET.md
// "Derin eklentiler".

// ekKomutlar veritabanı gerektirmeyen ek alt komutlar (`uptime <ad> ...`).
var ekKomutlar = map[string]func(args []string) error{}

// ajanEklentisi ajan tarafı eklenti üreticisi (Pro: betik yürütücü); nil = yok.
var ajanEklentisi func(log *slog.Logger) probe.AgentExtension

func newAgentExtension(log *slog.Logger) probe.AgentExtension {
	if ajanEklentisi == nil {
		return nil
	}
	return ajanEklentisi(log)
}
