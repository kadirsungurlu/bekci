package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Mesh ayarlarının yönetim tarafı: genel ayar (PUT /api/settings) ve probe
// başına ezmeler (PUT /api/probes/{id}). Veri modeli store/mesh.go.

// probeMeshView kontrol noktası yanıtındaki mesh alanları (sunucu ajanında yok):
// ezmeler (null = genel) ve genel + ezme birleşimi etkin zamanlama.
type probeMeshView struct {
	store.ProbeMesh
	Timing store.ProbeTiming `json:"timing"`
}

func (s *Server) probeMeshOf(p store.Probe) *probeMeshView {
	if p.Kind != store.ProbeKindLocation {
		return nil
	}
	return &probeMeshView{ProbeMesh: p.ProbeMesh, Timing: store.EffectiveProbeTiming(p, s.engine.Settings())}
}

// writeTimingError alan hatasıysa 400 + {"error","field"} yazar ve true döner.
func writeTimingError(w http.ResponseWriter, err error) bool {
	var te *store.TimingError
	if !errors.As(err, &te) {
		return false
	}
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": te.Msg, "field": te.Field})
	return true
}

// meshProbeInput PUT /api/probes/{id} gövdesindeki isteğe bağlı mesh alanları.
// Sayılar: tamsayı ezme, null genel değere dönüş, alan yok: değişmez.
type meshProbeInput struct {
	MeshPingSec       json.RawMessage `json:"mesh_ping_sec"`
	MeshLossThreshold json.RawMessage `json:"mesh_loss_threshold"`
	PanelSec          json.RawMessage `json:"panel_sec"`
	MeshAddr          *string         `json:"mesh_addr"`
	FallbackNotifier  *bool           `json:"fallback_notifier"`
}

func parseMeshOverride(raw json.RawMessage, field string, cur *int) (*int, error) {
	if len(raw) == 0 {
		return cur, nil
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, &store.TimingError{Field: field, Msg: field + " tamsayı veya null olmalı"}
	}
	return &n, nil
}

// apply eski değerlere gönderilen alanları uygular, etkin zamanlamayı ve
// adresi doğrular. Dönen değer kaydedilecek ProbeMesh'tir.
func (in meshProbeInput) apply(old store.Probe, st store.AppSettings) (store.ProbeMesh, error) {
	m := old.ProbeMesh
	var err error
	if m.MeshPingSec, err = parseMeshOverride(in.MeshPingSec, "mesh_ping_sec", m.MeshPingSec); err != nil {
		return m, err
	}
	if m.MeshLossThreshold, err = parseMeshOverride(in.MeshLossThreshold, "mesh_loss_threshold", m.MeshLossThreshold); err != nil {
		return m, err
	}
	if m.PanelSec, err = parseMeshOverride(in.PanelSec, "panel_sec", m.PanelSec); err != nil {
		return m, err
	}
	if in.MeshAddr != nil {
		if m.MeshAddr, err = store.NormalizeMeshAddr(*in.MeshAddr); err != nil {
			return m, err
		}
	}
	if in.FallbackNotifier != nil {
		m.FallbackNotifier = *in.FallbackNotifier
	}
	p := old
	p.ProbeMesh = m
	if err := store.ValidateProbeTiming(store.EffectiveProbeTiming(p, st)); err != nil {
		return m, err
	}
	return m, nil
}

// meshChanges işlem kaydı için değişen mesh alanları (Türkçe saklanır).
func meshChanges(old, in store.ProbeMesh) []string {
	var out []string
	num := func(a, b *int, label string) {
		if sameIntPtr(a, b) {
			return
		}
		out = append(out, label+": "+meshVal(a)+" → "+meshVal(b))
	}
	num(old.MeshPingSec, in.MeshPingSec, "mesh ping aralığı")
	num(old.MeshLossThreshold, in.MeshLossThreshold, "kopukluk eşiği")
	num(old.PanelSec, in.PanelSec, "panel haberleşme aralığı")
	if old.MeshAddr != in.MeshAddr {
		out = append(out, fmt.Sprintf("mesh adresi: %q → %q", old.MeshAddr, in.MeshAddr))
	}
	if old.FallbackNotifier != in.FallbackNotifier {
		out = append(out, map[bool]string{true: "yedek bildirici açıldı", false: "yedek bildirici kapatıldı"}[in.FallbackNotifier])
	}
	return out
}

func meshVal(p *int) string {
	if p == nil {
		return "genel"
	}
	return fmt.Sprint(*p)
}

func sameIntPtr(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
