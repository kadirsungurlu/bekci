package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kadirsungurlu/bekci/internal/probe"
)

// meshRunner kontrol noktaları arası TCP ping'i (internal/probe/meshtcp.go)
// ajana bağlar. Mesh ilk eş listesi gelince başlatılır: sunucu ajanlarına
// panel eş listesi vermez (404), onlarda port hiç açılmaz.
type meshRunner struct {
	ctx    context.Context
	addr   string // MESH_ADDR; "" dinleme yok
	log    *slog.Logger
	mu     sync.Mutex
	mesh   *probe.Mesh
	timing probe.MeshTiming
}

func newMeshRunner(ctx context.Context, addr string, log *slog.Logger) *meshRunner {
	if addr == "-" {
		addr = ""
	}
	return &meshRunner{ctx: ctx, addr: addr, log: log,
		timing: probe.MeshTiming{MeshEnabled: true, PingSec: 5, LossThreshold: 3, PanelSec: 5}}
}

func (r *meshRunner) cur() *probe.Mesh {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mesh
}

// Report / ListenPort: probe.MeshSource.
func (r *meshRunner) Report() []probe.MeshLinkReport {
	if m := r.cur(); m != nil {
		return m.Report()
	}
	return nil
}

func (r *meshRunner) ListenPort() int {
	if m := r.cur(); m != nil {
		return m.ListenPort()
	}
	return 0
}

func (r *meshRunner) onTiming(t probe.MeshTiming) {
	r.mu.Lock()
	r.timing = t
	m := r.mesh
	r.mu.Unlock()
	if m != nil && t.PingSec > 0 && t.LossThreshold > 0 {
		m.SetTiming(time.Duration(t.PingSec)*time.Second, t.LossThreshold)
	}
}

func (r *meshRunner) onPeers(p probe.MeshPeersResponse) {
	r.mu.Lock()
	if r.mesh == nil {
		if len(p.Peers) == 0 || p.Self == 0 {
			r.mu.Unlock()
			return
		}
		t := r.timing
		m := probe.NewMesh(probe.MeshConfig{SelfID: p.Self, ListenAddr: r.addr,
			PingInterval: time.Duration(max(t.PingSec, 1)) * time.Second, LossThreshold: max(t.LossThreshold, 1),
			Logf: func(f string, a ...any) { r.log.Debug("mesh", "msg", fmt.Sprintf(f, a...)) }})
		if err := m.Start(r.ctx); err != nil {
			// Port açılamadıysa yalnız giden bağlantılarla ölçülür.
			r.log.Warn("mesh dinleyicisi açılamadı; yalnız giden bağlantı", "addr", r.addr, "err", err)
			m = probe.NewMesh(probe.MeshConfig{SelfID: p.Self, PingInterval: time.Duration(max(t.PingSec, 1)) * time.Second,
				LossThreshold: max(t.LossThreshold, 1)})
			if err := m.Start(r.ctx); err != nil {
				r.mu.Unlock()
				r.log.Warn("mesh başlatılamadı", "err", err)
				return
			}
		}
		r.mesh = m
		go func() { <-r.ctx.Done(); m.Close() }()
	}
	m := r.mesh
	r.mu.Unlock()
	m.SetPeers(m.ListenPort() > 0, p.Peers)
}
