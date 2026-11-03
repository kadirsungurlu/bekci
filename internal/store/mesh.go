package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// 40: mesh (kontrol noktaları arası ve Main <-> probe bağlantı sağlığı).
//
//   - probes.mesh_ping_sec / mesh_loss_threshold / panel_sec: probe başına
//     ezme; NULL genel ayarı (AppSettings) devralır.
//   - probes.mesh_addr: eşlerin bu probe'a bağlanacağı host:port; boş: görülen
//     IP + MeshDefaultPort. "-" probe'un dinlemediğini (NAT) bildirir.
//   - probes.fallback_notifier: Main'e ulaşılamayınca bu probe yedek bildirici
//     olabilir (440'ta kullanılır; varsayılan açık).
//   - probe_links: yönlü bağlantı durumu (src ölçer, dst ölçülen; 0 = Main).
//     Main dış anahtar olamadığından probe silinince satırlar elle silinir.
//   - probe_pair_keys: çift başına HMAC anahtarı (a_id < b_id).
func init() {
	RegisterMigration(40, `
ALTER TABLE probes ADD COLUMN mesh_ping_sec INTEGER;
ALTER TABLE probes ADD COLUMN mesh_loss_threshold INTEGER;
ALTER TABLE probes ADD COLUMN panel_sec INTEGER;
ALTER TABLE probes ADD COLUMN mesh_addr TEXT NOT NULL DEFAULT '';
ALTER TABLE probes ADD COLUMN fallback_notifier INTEGER NOT NULL DEFAULT 1;
CREATE TABLE probe_links (
	src_id      INTEGER NOT NULL,
	dst_id      INTEGER NOT NULL,
	state       TEXT NOT NULL DEFAULT 'unknown',
	rtt_ms      INTEGER NOT NULL DEFAULT 0,
	loss_streak INTEGER NOT NULL DEFAULT 0,
	last_ok_at  INTEGER NOT NULL DEFAULT 0,
	updated_at  INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (src_id, dst_id)
);
CREATE TABLE probe_pair_keys (
	a_id       INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	b_id       INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	pair_key   TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	PRIMARY KEY (a_id, b_id)
);
`)
}

// Mesh varsayılanları ve sınırları.
const (
	MeshDefaultPort      = 7480
	DefaultMeshPingSec   = 5
	DefaultMeshLoss      = 3
	DefaultProbePanelSec = 5
	MeshSecMin           = 1
	MeshSecMax           = 60
	MeshLossMin          = 1
	MeshLossMax          = 10
	// MeshMinWindowSec eşik x aralık alt sınırı (tek paket kaybında yanlış alarm olmasın).
	MeshMinWindowSec = 3
	MeshAddrMax      = 255
)

// Bağlantı durumları (probe_links.state).
const (
	LinkUp      = "up"
	LinkDown    = "down"
	LinkUnknown = "unknown"
)

// MeshMainID probe_links'te Main'i (panel) gösterir.
const MeshMainID int64 = 0

// ProbeMesh probe başına mesh alanları (Probe içinde gömülü). nil: genel ayar.
type ProbeMesh struct {
	MeshPingSec       *int   `json:"mesh_ping_sec"`
	MeshLossThreshold *int   `json:"mesh_loss_threshold"`
	PanelSec          *int   `json:"panel_sec"`
	MeshAddr          string `json:"mesh_addr"`
	FallbackNotifier  bool   `json:"fallback_notifier"`
}

// ProbeTiming bir probe için geçerli (etkin) zamanlama.
type ProbeTiming struct {
	MeshEnabled   bool `json:"mesh_enabled"`
	PingSec       int  `json:"ping_sec"`
	LossThreshold int  `json:"loss_threshold"`
	PanelSec      int  `json:"panel_sec"`
}

// OfflineAfter Main-probe kenarının "down" sayılacağı sessizlik süresi
// (eşik x panel aralığı; varsayılan 3 x 5 = 15 sn).
func (t ProbeTiming) OfflineAfter() time.Duration {
	return time.Duration(t.LossThreshold*t.PanelSec) * time.Second
}

// MeshOn mesh genel olarak açık mı (nil: eski kayıt, açık).
func (a AppSettings) MeshOn() bool { return a.MeshEnabled == nil || *a.MeshEnabled }

// MeshTiming genel ayarların etkin değerleri (0 = varsayılan).
func (a AppSettings) MeshTiming() ProbeTiming {
	return ProbeTiming{MeshEnabled: a.MeshOn(), PingSec: orInt(a.MeshPingSec, DefaultMeshPingSec),
		LossThreshold: orInt(a.MeshLossThreshold, DefaultMeshLoss), PanelSec: orInt(a.ProbePanelSec, DefaultProbePanelSec)}
}

func orInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// EffectiveProbeTiming probe ezmeleri ile genel ayarı birleştirir.
func EffectiveProbeTiming(p Probe, s AppSettings) ProbeTiming {
	t := s.MeshTiming()
	if p.MeshPingSec != nil {
		t.PingSec = *p.MeshPingSec
	}
	if p.MeshLossThreshold != nil {
		t.LossThreshold = *p.MeshLossThreshold
	}
	if p.PanelSec != nil {
		t.PanelSec = *p.PanelSec
	}
	return t
}

// TimingError alan adıyla doğrulama hatası (API alan hatası olarak döner).
type TimingError struct {
	Field string
	Msg   string
}

func (e *TimingError) Error() string { return e.Msg }

// ValidateProbeTiming sınırları denetler: aralıklar 1-60 sn, eşik 1-10,
// eşik x aralık >= 3 sn (ping ve panel aralığı için ayrı ayrı).
func ValidateProbeTiming(t ProbeTiming) error {
	if t.PingSec < MeshSecMin || t.PingSec > MeshSecMax {
		return &TimingError{"mesh_ping_sec", fmt.Sprintf("Mesh ping aralığı %d-%d sn olmalı", MeshSecMin, MeshSecMax)}
	}
	if t.LossThreshold < MeshLossMin || t.LossThreshold > MeshLossMax {
		return &TimingError{"mesh_loss_threshold", fmt.Sprintf("Kopukluk eşiği %d-%d olmalı", MeshLossMin, MeshLossMax)}
	}
	if t.PanelSec < MeshSecMin || t.PanelSec > MeshSecMax {
		return &TimingError{"panel_sec", fmt.Sprintf("Panel haberleşme aralığı %d-%d sn olmalı", MeshSecMin, MeshSecMax)}
	}
	if t.LossThreshold*t.PingSec < MeshMinWindowSec {
		return &TimingError{"mesh_loss_threshold", fmt.Sprintf("Kopukluk eşiği x ping aralığı en az %d sn olmalı", MeshMinWindowSec)}
	}
	if t.LossThreshold*t.PanelSec < MeshMinWindowSec {
		return &TimingError{"mesh_loss_threshold", fmt.Sprintf("Kopukluk eşiği x panel aralığı en az %d sn olmalı", MeshMinWindowSec)}
	}
	return nil
}

// NormalizeMeshAddr probe mesh adresini doğrular: boş, "-" ya da host:port.
func NormalizeMeshAddr(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return s, nil
	}
	if len(s) > MeshAddrMax {
		return "", &TimingError{"mesh_addr", "Mesh adresi çok uzun"}
	}
	host, port, err := net.SplitHostPort(s)
	n, perr := strconv.Atoi(port)
	if err != nil || host == "" || perr != nil || n < 1 || n > 65535 || strings.ContainsAny(host, " /\t") {
		return "", &TimingError{"mesh_addr", "Mesh adresi host:port biçiminde olmalı"}
	}
	return s, nil
}

// SetProbeMesh probe başına mesh ayarlarını yazar (doğrulama çağıranda).
func (s *Store) SetProbeMesh(ctx context.Context, id int64, m ProbeMesh) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE probes SET mesh_ping_sec = ?, mesh_loss_threshold = ?, panel_sec = ?, mesh_addr = ?, fallback_notifier = ? WHERE id = ?",
		nullIntPtr(m.MeshPingSec), nullIntPtr(m.MeshLossThreshold), nullIntPtr(m.PanelSec), m.MeshAddr, boolInt(m.FallbackNotifier), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func nullIntPtr(p *int) any {
	if p == nil {
		return nil
	}
	return int64(*p)
}

func scanIntPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

// ProbeLink yönlü bağlantı durumu (Src ölçer, Dst ölçülen; 0 = Main).
type ProbeLink struct {
	Src        int64  `json:"src"`
	Dst        int64  `json:"dst"`
	State      string `json:"state"`
	RTTms      int64  `json:"rtt_ms"`
	LossStreak int    `json:"loss_streak"`
	LastOKAt   int64  `json:"last_ok_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

// ValidLinkState bilinen durum mu.
func ValidLinkState(s string) bool { return s == LinkUp || s == LinkDown || s == LinkUnknown }

// UpsertProbeLinks bağlantı satırlarını yazar (tek işlem).
func (s *Store) UpsertProbeLinks(ctx context.Context, links []ProbeLink) error {
	if len(links) == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *Tx) error {
		for _, l := range links {
			if _, err := tx.ExecContext(ctx, `INSERT INTO probe_links (src_id, dst_id, state, rtt_ms, loss_streak, last_ok_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (src_id, dst_id) DO UPDATE SET state = excluded.state, rtt_ms = excluded.rtt_ms,
					loss_streak = excluded.loss_streak, last_ok_at = excluded.last_ok_at, updated_at = excluded.updated_at`,
				l.Src, l.Dst, l.State, l.RTTms, l.LossStreak, l.LastOKAt, l.UpdatedAt); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListProbeLinks tüm bağlantılar (src, dst sıralı).
func (s *Store) ListProbeLinks(ctx context.Context) ([]ProbeLink, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT src_id, dst_id, state, rtt_ms, loss_streak, last_ok_at, updated_at FROM probe_links ORDER BY src_id, dst_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProbeLink{}
	for rows.Next() {
		var l ProbeLink
		if err := rows.Scan(&l.Src, &l.Dst, &l.State, &l.RTTms, &l.LossStreak, &l.LastOKAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// deleteProbeMesh probe'un bağlantı ve anahtar satırlarını siler (DeleteProbe içinde).
func deleteProbeMesh(ctx context.Context, tx *Tx, id int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM probe_links WHERE src_id = ? OR dst_id = ?", id, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM probe_pair_keys WHERE a_id = ? OR b_id = ?", id, id)
	return err
}

// DropProbePairKeys probe'un tüm çift anahtarlarını siler (belirteç
// yenilenince ya da probe pasifleşince); sonraki istekte yenileri üretilir.
func (s *Store) DropProbePairKeys(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM probe_pair_keys WHERE a_id = ? OR b_id = ?", id, id)
	return err
}

// PairKey çiftin HMAC anahtarı (hex, 32 bayt); yoksa üretir.
func (s *Store) PairKey(ctx context.Context, x, y int64, now int64) (string, error) {
	if x == y || x <= 0 || y <= 0 {
		return "", errors.New("geçersiz çift")
	}
	a, b := x, y
	if a > b {
		a, b = b, a
	}
	var key string
	err := s.db.QueryRowContext(ctx, "SELECT pair_key FROM probe_pair_keys WHERE a_id = ? AND b_id = ?", a, b).Scan(&key)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	key = hex.EncodeToString(buf)
	if _, err := s.db.ExecContext(ctx,
		"INSERT INTO probe_pair_keys (a_id, b_id, pair_key, created_at) VALUES (?, ?, ?, ?) ON CONFLICT (a_id, b_id) DO NOTHING",
		a, b, key, now); err != nil {
		return "", err
	}
	// Yarış: eşzamanlı üretimde kazanan satır geçerli.
	err = s.db.QueryRowContext(ctx, "SELECT pair_key FROM probe_pair_keys WHERE a_id = ? AND b_id = ?", a, b).Scan(&key)
	return key, err
}
