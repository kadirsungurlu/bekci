package api

import (
	"context"
	"net/http"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Derin eklenti noktaları: açık derlemede boştur. Pro derlemesi (bekci-pro, ic/
// ağacı go -overlay ile bu pakete eklenir) init içinde doldurur. Rotalar
// RegisterRoutes ile, lisans kapısı proKapi ile bağlanır (docs/PRO-PAKET.md).

// probeJobExtra ajanın iş listesi yanıtına alan ekler (ör. "script_jobs").
type probeJobExtra func(s *Server, ctx context.Context, r *http.Request, p store.Probe, out map[string]any)

var (
	probeJobExtras  []probeJobExtra
	sessionOnlyExt  []func(path string) bool
	backgroundTasks []func(s *Server, ctx context.Context, dataDir string)
)

func (s *Server) addProbeJobExtras(ctx context.Context, r *http.Request, p store.Probe, out map[string]any) {
	for _, f := range probeJobExtras {
		f(s, ctx, r, p, out)
	}
}

// sessionOnlyExtra eklentinin API anahtarına kapalı yolları.
func sessionOnlyExtra(path string) bool {
	for _, f := range sessionOnlyExt {
		if f(path) {
			return true
		}
	}
	return false
}

// RunExtensions eklentilerin arka plan işlerini başlatır (her biri kendi goroutine'inde).
func (s *Server) RunExtensions(ctx context.Context, dataDir string) {
	for _, f := range backgroundTasks {
		go f(s, ctx, dataDir)
	}
}

// extState eklentinin bu sunucuya bağlı durumu (ilk çağrıda mk ile oluşur).
func extState[T any](s *Server, name string, mk func() *T) *T {
	if v, ok := s.ek.Load(name); ok {
		return v.(*T)
	}
	v, _ := s.ek.LoadOrStore(name, mk())
	return v.(*T)
}
