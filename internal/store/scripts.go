package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betikkayit"
)

// Betik kütüphanesi ve kurcalama belli çalıştırma kaydı (docs/BETIK-GUVENLIK-680.md
// §8-9). Panel güven kökü değildir: burada saklanan zarflar imzalıdır, ajan her
// şeyi yeniden doğrular. Özellik PostgreSQL ister; SQLite'ta kapalıdır (D8).
//
// script_runs yalnız eklenen olay akışıdır; son durum olaydan türetilir.
// script_queue teslim bekleyen işlerin değişken kuyruğudur, kayıt değildir.
func init() {
	RegisterMigrationDialect(48, `SELECT 1`, `
CREATE TABLE scripts (
	script_id   TEXT   NOT NULL,
	version     BIGINT NOT NULL,
	envelope    TEXT   NOT NULL,
	body        BYTEA  NOT NULL,
	sha256      TEXT   NOT NULL,
	approval    TEXT   NOT NULL,
	manifest    TEXT   NOT NULL,
	key_id      TEXT   NOT NULL,
	not_before  BIGINT NOT NULL,
	not_after   BIGINT NOT NULL,
	uploaded_by BIGINT NOT NULL,
	uploaded_by_name TEXT NOT NULL,
	uploaded_at BIGINT NOT NULL,
	PRIMARY KEY (script_id, version)
);
CREATE TABLE script_revokes (
	script_id     TEXT   NOT NULL,
	below_version BIGINT NOT NULL,
	envelope      TEXT   NOT NULL,
	uploaded_by_name TEXT NOT NULL,
	uploaded_at   BIGINT NOT NULL,
	PRIMARY KEY (script_id, below_version)
);
CREATE TABLE script_keys (
	key_id     TEXT PRIMARY KEY,
	slot       TEXT NOT NULL CHECK (slot IN ('yayin', 'onay')),
	public_key TEXT NOT NULL,
	name       TEXT NOT NULL,
	added_by_name TEXT NOT NULL,
	added_at   BIGINT NOT NULL
);
CREATE TABLE script_hosts (
	probe_id    BIGINT PRIMARY KEY REFERENCES probes(id) ON DELETE CASCADE,
	host_key    TEXT   NOT NULL DEFAULT '',
	public_key  TEXT   NOT NULL DEFAULT '',
	pending_key TEXT   NOT NULL DEFAULT '',
	pending_pub TEXT   NOT NULL DEFAULT '',
	receipt_seq  BIGINT NOT NULL DEFAULT 0,
	receipt_hash TEXT  NOT NULL DEFAULT '',
	reason      TEXT   NOT NULL DEFAULT '',
	approved_at BIGINT NOT NULL DEFAULT 0,
	approved_by_name TEXT NOT NULL DEFAULT '',
	schemas     TEXT   NOT NULL DEFAULT '',
	state       TEXT   NOT NULL DEFAULT '',
	seen_at     BIGINT NOT NULL DEFAULT 0
);
CREATE TABLE script_runs (
	time     BIGINT NOT NULL,
	seq      BIGINT NOT NULL,
	run_id   TEXT   NOT NULL,
	kind     TEXT   NOT NULL,
	actor_id BIGINT NOT NULL DEFAULT 0,
	data     TEXT   NOT NULL,
	prev     TEXT   NOT NULL,
	hash     TEXT   NOT NULL,
	PRIMARY KEY (time, seq)
);
CREATE INDEX script_runs_seq ON script_runs(seq DESC);
CREATE INDEX script_runs_run ON script_runs(run_id, seq);
CREATE INDEX script_runs_kind ON script_runs(kind, seq DESC);
CREATE TABLE script_run_outputs (
	run_id     TEXT PRIMARY KEY,
	output     BYTEA  NOT NULL,
	sha256     TEXT   NOT NULL,
	truncated  BIGINT NOT NULL DEFAULT 0,
	created_at BIGINT NOT NULL
);
CREATE INDEX script_run_outputs_time ON script_run_outputs(created_at);
CREATE TABLE script_log_checkpoint (
	seq        BIGINT PRIMARY KEY,
	hash       TEXT   NOT NULL,
	time       BIGINT NOT NULL,
	cutoff     BIGINT NOT NULL,
	created_at BIGINT NOT NULL
);
CREATE TABLE script_queue (
	run_id       TEXT PRIMARY KEY,
	probe_id     BIGINT NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	script_id    TEXT   NOT NULL,
	version      BIGINT NOT NULL,
	params       TEXT   NOT NULL,
	ticket       TEXT   NOT NULL DEFAULT '',
	queued_at    BIGINT NOT NULL,
	expires_at   BIGINT NOT NULL,
	delivered_at BIGINT NOT NULL DEFAULT 0,
	cancel       BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX script_queue_probe ON script_queue(probe_id);
CREATE TABLE script_control (
	id          BIGINT PRIMARY KEY CHECK (id = 1),
	stopped     BIGINT NOT NULL DEFAULT 0,
	stopped_at  BIGINT NOT NULL DEFAULT 0,
	stopped_by_name TEXT NOT NULL DEFAULT '',
	channel_ids TEXT   NOT NULL DEFAULT '[]'
);
INSERT INTO script_control (id) VALUES (1);
CREATE FUNCTION script_append_only() RETURNS trigger LANGUAGE plpgsql AS '
BEGIN
	IF TG_TABLE_NAME = ''script_runs'' AND TG_OP = ''DELETE''
		AND current_setting(''bekci.script_prune'', true) = ''on'' THEN
		RETURN OLD;
	END IF;
	RAISE EXCEPTION ''% yalniz eklenir (%)'', TG_TABLE_NAME, TG_OP;
END';
CREATE FUNCTION script_runs_prune(cutoff BIGINT) RETURNS BIGINT LANGUAGE plpgsql SECURITY DEFINER AS '
DECLARE n BIGINT;
BEGIN
	PERFORM set_config(''bekci.script_prune'', ''on'', true);
	DELETE FROM script_runs WHERE time < cutoff;
	GET DIAGNOSTICS n = ROW_COUNT;
	PERFORM set_config(''bekci.script_prune'', ''off'', true);
	RETURN n;
END';
CREATE TRIGGER script_runs_guard BEFORE UPDATE OR DELETE ON script_runs
	FOR EACH ROW EXECUTE FUNCTION script_append_only();
CREATE TRIGGER script_runs_guard_trunc BEFORE TRUNCATE ON script_runs
	FOR EACH STATEMENT EXECUTE FUNCTION script_append_only();
CREATE TRIGGER scripts_guard BEFORE UPDATE OR DELETE ON scripts
	FOR EACH ROW EXECUTE FUNCTION script_append_only();
CREATE TRIGGER scripts_guard_trunc BEFORE TRUNCATE ON scripts
	FOR EACH STATEMENT EXECUTE FUNCTION script_append_only();
CREATE TRIGGER script_log_checkpoint_guard BEFORE UPDATE OR DELETE ON script_log_checkpoint
	FOR EACH ROW EXECUTE FUNCTION script_append_only();
`)
}

func init() {
	RegisterMigrationDialect(49, `SELECT 1`, `
CREATE TRIGGER script_log_checkpoint_guard_trunc BEFORE TRUNCATE ON script_log_checkpoint
	FOR EACH STATEMENT EXECUTE FUNCTION script_append_only();
`)
}

// ScriptRow kütüphanedeki bir betik sürümü (satır değişmez).
type ScriptRow struct {
	ScriptID       string
	Version        int64
	Envelope       string
	Body           []byte
	SHA256         string
	Approval       string
	Manifest       string // bildirim payload JSON metni
	KeyID          string
	NotBefore      int64
	NotAfter       int64
	UploadedBy     int64
	UploadedByName string
	UploadedAt     int64
}

// ErrScriptExists aynı id+sürüm zaten yüklü.
var ErrScriptExists = errors.New("bu betik sürümü zaten yüklü")

const scriptCols = "script_id, version, envelope, body, sha256, approval, manifest, key_id, not_before, not_after, uploaded_by, uploaded_by_name, uploaded_at"

func scanScript(row interface{ Scan(...any) error }) (ScriptRow, error) {
	var r ScriptRow
	err := row.Scan(&r.ScriptID, &r.Version, &r.Envelope, &r.Body, &r.SHA256, &r.Approval, &r.Manifest,
		&r.KeyID, &r.NotBefore, &r.NotAfter, &r.UploadedBy, &r.UploadedByName, &r.UploadedAt)
	return r, err
}

// AddScript yeni sürüm ekler. Zarfın doğrulanması çağıranın işidir.
func (s *Store) AddScript(ctx context.Context, r ScriptRow) error {
	res, err := s.db.ExecContext(ctx, `INSERT INTO scripts (`+scriptCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT DO NOTHING`, r.ScriptID, r.Version, r.Envelope, r.Body, r.SHA256, r.Approval, r.Manifest,
		r.KeyID, r.NotBefore, r.NotAfter, r.UploadedBy, r.UploadedByName, r.UploadedAt)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrScriptExists
	}
	return nil
}

// ListScripts tüm sürümler, id artan, sürüm azalan.
func (s *Store) ListScripts(ctx context.Context) ([]ScriptRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+scriptCols+` FROM scripts ORDER BY script_id, version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScriptRow
	for rows.Next() {
		r, err := scanScript(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetScript bir sürüm; yoksa sql.ErrNoRows.
func (s *Store) GetScript(ctx context.Context, id string, version int64) (ScriptRow, error) {
	return scanScript(s.db.QueryRowContext(ctx, `SELECT `+scriptCols+` FROM scripts WHERE script_id = ? AND version = ?`, id, version))
}

// ScriptRevoke imzalı iptal zarfı.
type ScriptRevoke struct {
	ScriptID       string `json:"script_id"`
	BelowVersion   int64  `json:"below_version"`
	Envelope       string `json:"envelope"`
	UploadedByName string `json:"uploaded_by_name"`
	UploadedAt     int64  `json:"uploaded_at"`
}

// AddScriptRevoke iptal zarfını saklar (aynısı tekrar yüklenirse yok sayılır).
func (s *Store) AddScriptRevoke(ctx context.Context, r ScriptRevoke) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO script_revokes (script_id, below_version, envelope, uploaded_by_name, uploaded_at)
		VALUES (?,?,?,?,?) ON CONFLICT DO NOTHING`, r.ScriptID, r.BelowVersion, r.Envelope, r.UploadedByName, r.UploadedAt)
	return err
}

// ListScriptRevokes tüm iptal zarfları.
func (s *Store) ListScriptRevokes(ctx context.Context) ([]ScriptRevoke, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT script_id, below_version, envelope, uploaded_by_name, uploaded_at
		FROM script_revokes ORDER BY script_id, below_version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScriptRevoke{}
	for rows.Next() {
		var r ScriptRevoke
		if err := rows.Scan(&r.ScriptID, &r.BelowVersion, &r.Envelope, &r.UploadedByName, &r.UploadedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ScriptKey panelin bildiği açık anahtar: yalnız yüklemede erken hata yakalamak
// içindir; ajan kendi yerel güven listesine bakar.
type ScriptKey struct {
	KeyID       string `json:"key_id"`
	Slot        string `json:"slot"`
	PublicKey   string `json:"public_key"`
	Name        string `json:"name"`
	AddedByName string `json:"added_by_name"`
	AddedAt     int64  `json:"added_at"`
}

// AddScriptKey anahtar ekler.
func (s *Store) AddScriptKey(ctx context.Context, k ScriptKey) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO script_keys (key_id, slot, public_key, name, added_by_name, added_at)
		VALUES (?,?,?,?,?,?)`, k.KeyID, k.Slot, k.PublicKey, k.Name, k.AddedByName, k.AddedAt)
	return err
}

// DeleteScriptKey anahtarı listeden çıkarır; yoksa false.
func (s *Store) DeleteScriptKey(ctx context.Context, keyID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM script_keys WHERE key_id = ?`, keyID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ListScriptKeys tüm anahtarlar.
func (s *Store) ListScriptKeys(ctx context.Context) ([]ScriptKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key_id, slot, public_key, name, added_by_name, added_at FROM script_keys ORDER BY slot, added_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScriptKey{}
	for rows.Next() {
		var k ScriptKey
		if err := rows.Scan(&k.KeyID, &k.Slot, &k.PublicKey, &k.Name, &k.AddedByName, &k.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ScriptHost ajanın betik durumu ve onaylı kimliği.
type ScriptHost struct {
	ProbeID        int64  `json:"probe_id"`
	HostKey        string `json:"host_key"`
	PublicKey      string `json:"-"`
	PendingKey     string `json:"pending_key"`
	PendingPub     string `json:"-"`
	ReceiptSeq     int64  `json:"receipt_seq"`
	ReceiptHash    string `json:"-"`
	ApprovedAt     int64  `json:"approved_at"`
	ApprovedByName string `json:"approved_by_name"`
	Schemas        string `json:"schemas"`
	State          string `json:"state"`
	Reason         string `json:"reason"`
	SeenAt         int64  `json:"seen_at"`
}

const scriptHostCols = "probe_id, host_key, public_key, pending_key, pending_pub, receipt_seq, receipt_hash, approved_at, approved_by_name, schemas, state, reason, seen_at"

func scanScriptHost(row interface{ Scan(...any) error }) (ScriptHost, error) {
	var h ScriptHost
	err := row.Scan(&h.ProbeID, &h.HostKey, &h.PublicKey, &h.PendingKey, &h.PendingPub, &h.ReceiptSeq, &h.ReceiptHash,
		&h.ApprovedAt, &h.ApprovedByName, &h.Schemas, &h.State, &h.Reason, &h.SeenAt)
	return h, err
}

// ObserveScriptHost ajanın yetenek başlığını işler. Yeni anahtar onaylı anahtarın
// yerine geçmez, bekleyen olarak yazılır (§7, D14). keyChanged: bekleyen anahtar
// bu çağrıyla değişti. hostKey boşsa (state=off) anahtar alanlarına dokunulmaz.
func (s *Store) ObserveScriptHost(ctx context.Context, probeID int64, hostKey, pub, schemas, state, reason string, now int64) (keyChanged bool, err error) {
	err = s.tx(ctx, func(tx *Tx) error {
		if _, e := tx.ExecContext(ctx, `INSERT INTO script_hosts (probe_id) VALUES (?) ON CONFLICT DO NOTHING`, probeID); e != nil {
			return e
		}
		var cur, pending, pendingPub string
		if e := tx.QueryRowContext(ctx, `SELECT host_key, pending_key, pending_pub FROM script_hosts WHERE probe_id = ? FOR UPDATE`, probeID).
			Scan(&cur, &pending, &pendingPub); e != nil {
			return e
		}
		if hostKey != "" && hostKey != cur && hostKey != pending {
			pending, pendingPub, keyChanged = hostKey, pub, true
		}
		if hostKey != "" && hostKey == cur {
			pending, pendingPub = "", ""
		}
		_, e := tx.ExecContext(ctx, `UPDATE script_hosts SET pending_key = ?, pending_pub = ?, schemas = ?, state = ?, reason = ?, seen_at = ? WHERE probe_id = ?`,
			pending, pendingPub, schemas, state, reason, now, probeID)
		return e
	})
	return keyChanged, err
}

// ScriptHostInfo bir ajanın kaydı; yoksa boş değer.
func (s *Store) ScriptHostInfo(ctx context.Context, probeID int64) (ScriptHost, error) {
	h, err := scanScriptHost(s.db.QueryRowContext(ctx, `SELECT `+scriptHostCols+` FROM script_hosts WHERE probe_id = ?`, probeID))
	if errors.Is(err, sql.ErrNoRows) {
		return ScriptHost{ProbeID: probeID}, nil
	}
	return h, err
}

// ListScriptHosts bilinen tüm ajan kayıtları.
func (s *Store) ListScriptHosts(ctx context.Context) ([]ScriptHost, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+scriptHostCols+` FROM script_hosts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScriptHost{}
	for rows.Next() {
		h, err := scanScriptHost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ApproveHostKey bekleyen anahtarı onaylar; makbuz zinciri kapanır, yeni anahtar
// sıfırdan başlar (D14). Beklenen anahtar değişmişse false.
func (l *ScriptLog) ApproveHostKey(ctx context.Context, probeID int64, key, byName string, now int64) (pub string, ok bool, err error) {
	if err = l.tx.QueryRowContext(ctx, `SELECT pending_pub FROM script_hosts WHERE probe_id = ? AND pending_key = ? FOR UPDATE`, probeID, key).Scan(&pub); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	_, err = l.tx.ExecContext(ctx, `UPDATE script_hosts SET host_key = pending_key, public_key = pending_pub, pending_key = '', pending_pub = '',
		receipt_seq = 0, receipt_hash = '', approved_at = ?, approved_by_name = ? WHERE probe_id = ?`, now, byName, probeID)
	return pub, err == nil, err
}

// HostForReceipt onaylı anahtar ve makbuz zinciri başı (satır kilidiyle).
func (l *ScriptLog) HostForReceipt(ctx context.Context, probeID int64) (ScriptHost, error) {
	return scanScriptHost(l.tx.QueryRowContext(ctx, `SELECT `+scriptHostCols+` FROM script_hosts WHERE probe_id = ? FOR UPDATE`, probeID))
}

// SetReceiptHead makbuz zinciri başını ilerletir.
func (l *ScriptLog) SetReceiptHead(ctx context.Context, probeID, seq int64, hash string) error {
	_, err := l.tx.ExecContext(ctx, `UPDATE script_hosts SET receipt_seq = ?, receipt_hash = ? WHERE probe_id = ?`, seq, hash, probeID)
	return err
}

// ScriptControl acil durdurma ve bildirim kanalları.
type ScriptControl struct {
	Stopped       bool    `json:"stopped"`
	StoppedAt     int64   `json:"stopped_at"`
	StoppedByName string  `json:"stopped_by_name"`
	ChannelIDs    []int64 `json:"channel_ids"`
}

// ScriptControlState geçerli durum.
func (s *Store) ScriptControlState(ctx context.Context) (ScriptControl, error) {
	var c ScriptControl
	var stopped int64
	var ch string
	if err := s.db.QueryRowContext(ctx, `SELECT stopped, stopped_at, stopped_by_name, channel_ids FROM script_control WHERE id = 1`).
		Scan(&stopped, &c.StoppedAt, &c.StoppedByName, &ch); err != nil {
		return c, err
	}
	c.Stopped = stopped != 0
	c.ChannelIDs = []int64{}
	_ = json.Unmarshal([]byte(ch), &c.ChannelIDs)
	return c, nil
}

// SetScriptChannels betik bildirimlerinin gideceği kanallar.
func (s *Store) SetScriptChannels(ctx context.Context, ids []int64) error {
	b, _ := json.Marshal(ids)
	_, err := s.db.ExecContext(ctx, `UPDATE script_control SET channel_ids = ? WHERE id = 1`, string(b))
	return err
}

// ScriptLog açık bir kayıt işlemi: tüm ekleme ve durum kararları zincir kilidi
// altında sıralıdır, "son durumu oku + olay ekle" bu yüzden yarışmaz.
type ScriptLog struct {
	tx   *Tx
	head string
	seq  int64
	last int64
	now  int64
}

// scriptChainLockKey script_runs zincir başı danışma kilidi.
const scriptChainLockKey = 0x75707432

// ScriptTx fn'i zincir kilidi altında tek işlemde çalıştırır.
func (s *Store) ScriptTx(ctx context.Context, fn func(*ScriptLog) error) error {
	if !s.postgres {
		return errors.New("betik özelliği PostgreSQL ister")
	}
	return s.tx(ctx, func(tx *Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(?)`, int64(scriptChainLockKey)); err != nil {
			return err
		}
		l := &ScriptLog{tx: tx, head: betikkayit.Genesis, now: time.Now().Unix()}
		err := tx.QueryRowContext(ctx, `SELECT seq, hash, time FROM script_runs ORDER BY seq DESC LIMIT 1`).Scan(&l.seq, &l.head, &l.last)
		if errors.Is(err, sql.ErrNoRows) {
			// Tablo saklama nedeniyle boşaldıysa zincir son checkpoint'ten sürer.
			err = tx.QueryRowContext(ctx, `SELECT seq, hash, time FROM script_log_checkpoint ORDER BY seq DESC LIMIT 1`).Scan(&l.seq, &l.head, &l.last)
			if errors.Is(err, sql.ErrNoRows) {
				err = nil
			}
		}
		if err != nil {
			return err
		}
		return fn(l)
	})
}

// Append olay ekler. data JSON'a çevrilir ve bu baytlar özetlenir.
func (l *ScriptLog) Append(ctx context.Context, runID, kind string, actorID int64, data any) (betikkayit.Event, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return betikkayit.Event{}, err
	}
	e := betikkayit.Event{Seq: l.seq + 1, Time: max(l.now, l.last), RunID: runID, Kind: kind, ActorID: actorID, Data: string(raw), Prev: l.head}
	e.Hash = betikkayit.Hash(l.head, e)
	if _, err := l.tx.ExecContext(ctx, `INSERT INTO script_runs (time, seq, run_id, kind, actor_id, data, prev, hash) VALUES (?,?,?,?,?,?,?,?)`,
		e.Time, e.Seq, e.RunID, e.Kind, e.ActorID, e.Data, e.Prev, e.Hash); err != nil {
		return e, err
	}
	l.seq, l.head, l.last = e.Seq, e.Hash, e.Time
	return e, nil
}

// Requested çalıştırmanın başlatma olayı; run yoksa sql.ErrNoRows.
func (l *ScriptLog) Requested(ctx context.Context, runID string) (betikkayit.Event, error) {
	return scanScriptEvent(l.tx.QueryRowContext(ctx, `SELECT `+scriptEventCols+` FROM script_runs WHERE run_id = ? AND kind = 'requested' ORDER BY seq LIMIT 1`, runID))
}

// LastKind çalıştırmanın son olayı; run yoksa sql.ErrNoRows.
func (l *ScriptLog) LastKind(ctx context.Context, runID string) (betikkayit.Event, error) {
	return scanScriptEvent(l.tx.QueryRowContext(ctx, `SELECT `+scriptEventCols+` FROM script_runs WHERE run_id = ? ORDER BY seq DESC LIMIT 1`, runID))
}

// Enqueue işi teslim kuyruğuna koyar.
func (l *ScriptLog) Enqueue(ctx context.Context, j ScriptJob) error {
	_, err := l.tx.ExecContext(ctx, `INSERT INTO script_queue (run_id, probe_id, script_id, version, params, ticket, queued_at, expires_at)
		VALUES (?,?,?,?,?,?,?,?)`, j.RunID, j.ProbeID, j.ScriptID, j.Version, j.Params, j.Ticket, j.QueuedAt, j.ExpiresAt)
	return err
}

// Dequeue işi kuyruktan çıkarır.
func (l *ScriptLog) Dequeue(ctx context.Context, runID string) error {
	_, err := l.tx.ExecContext(ctx, `DELETE FROM script_queue WHERE run_id = ?`, runID)
	return err
}

// RequestCancel çalışan işe durdurma isteği işaretler (ajan bir sonraki
// yoklamada alır); ajanda yerel durdurma bundan bağımsızdır.
func (l *ScriptLog) RequestCancel(ctx context.Context, runID string) error {
	_, err := l.tx.ExecContext(ctx, `UPDATE script_queue SET cancel = 1 WHERE run_id = ?`, runID)
	return err
}

// RestoreForCancel iptalde kuyruktan çıkmış ama ajanın kabul ettiği işi yalnız
// durdurma isteği olarak geri koyar: delivered_at dolu olduğundan yeniden
// teslim edilmez, makbuz gelmezse no_receipt ile kapanır.
func (l *ScriptLog) RestoreForCancel(ctx context.Context, j ScriptJob) error {
	_, err := l.tx.ExecContext(ctx, `INSERT INTO script_queue (run_id, probe_id, script_id, version, params, ticket, queued_at, expires_at, delivered_at, cancel)
		VALUES (?,?,?,?,?,'',?,?,?,1) ON CONFLICT (run_id) DO UPDATE SET cancel = 1`,
		j.RunID, j.ProbeID, j.ScriptID, j.Version, j.Params, j.QueuedAt, j.ExpiresAt, j.QueuedAt)
	return err
}

// Stopped acil durdurma bayrağı, zincir kilidi altında.
func (l *ScriptLog) Stopped(ctx context.Context) (bool, error) {
	var v int64
	err := l.tx.QueryRowContext(ctx, `SELECT stopped FROM script_control WHERE id = 1`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return v != 0, err
}

// MarkDelivered ajanın "accepted" bildirdiği zamanı yazar (sözleşme §4: bu ana
// kadar her yoklamada yeniden gönderilir); ilk bildirimde true döner.
func (l *ScriptLog) MarkDelivered(ctx context.Context, runID string, now int64) (bool, error) {
	res, err := l.tx.ExecContext(ctx, `UPDATE script_queue SET delivered_at = ? WHERE run_id = ? AND delivered_at = 0`, now, runID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// SaveOutput kısaltılmış çıktıyı saklar.
func (l *ScriptLog) SaveOutput(ctx context.Context, runID string, out []byte, sha string, truncated bool, now int64) error {
	var tr int64
	if truncated {
		tr = 1
	}
	_, err := l.tx.ExecContext(ctx, `INSERT INTO script_run_outputs (run_id, output, sha256, truncated, created_at) VALUES (?,?,?,?,?)
		ON CONFLICT (run_id) DO NOTHING`, runID, out, sha, tr, now)
	return err
}

// SetStopped acil durdurma bayrağı. Durdurmada kuyruktaki teslim edilmemiş işler
// çıkarılır, teslim edilenlere durdurma işaretlenir; etkilenen run_id'ler döner.
func (l *ScriptLog) SetStopped(ctx context.Context, stopped bool, byName string, now int64) (pending, running []string, err error) {
	v := int64(0)
	if stopped {
		v = 1
	}
	if _, err = l.tx.ExecContext(ctx, `UPDATE script_control SET stopped = ?, stopped_at = ?, stopped_by_name = ? WHERE id = 1`, v, now, byName); err != nil {
		return nil, nil, err
	}
	if !stopped {
		return nil, nil, nil
	}
	rows, err := l.tx.QueryContext(ctx, `SELECT run_id, delivered_at FROM script_queue`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var at int64
		if err = rows.Scan(&id, &at); err != nil {
			return nil, nil, err
		}
		if at == 0 {
			pending = append(pending, id)
		} else {
			running = append(running, id)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	rows.Close()
	if _, err = l.tx.ExecContext(ctx, `DELETE FROM script_queue WHERE delivered_at = 0`); err != nil {
		return nil, nil, err
	}
	_, err = l.tx.ExecContext(ctx, `UPDATE script_queue SET cancel = 1`)
	return pending, running, err
}

// ScriptJob teslim kuyruğundaki iş.
type ScriptJob struct {
	RunID       string
	ProbeID     int64
	ScriptID    string
	Version     int64
	Params      string
	Ticket      string
	QueuedAt    int64
	ExpiresAt   int64
	DeliveredAt int64
	Cancel      bool
}

// ScriptJobsFor ajanın kuyruğu: teslim edilmemişler ve durdurma isteği olanlar.
func (s *Store) ScriptJobsFor(ctx context.Context, probeID, now int64) ([]ScriptJob, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT run_id, probe_id, script_id, version, params, ticket, queued_at, expires_at, delivered_at, cancel
		FROM script_queue WHERE probe_id = ? AND ((delivered_at = 0 AND expires_at > ?) OR cancel = 1) ORDER BY queued_at LIMIT 8`, probeID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScriptJob
	for rows.Next() {
		var j ScriptJob
		var c int64
		if err := rows.Scan(&j.RunID, &j.ProbeID, &j.ScriptID, &j.Version, &j.Params, &j.Ticket, &j.QueuedAt, &j.ExpiresAt, &j.DeliveredAt, &c); err != nil {
			return nil, err
		}
		j.Cancel = c != 0
		out = append(out, j)
	}
	return out, rows.Err()
}

// StaleScriptJobs ajan kabul etmiş ama cutoff'tan beri makbuz göndermemiş işler.
func (s *Store) StaleScriptJobs(ctx context.Context, cutoff int64) ([]string, error) {
	return s.stringList(ctx, `SELECT run_id FROM script_queue WHERE delivered_at > 0 AND delivered_at < ?`, cutoff)
}

// LastScriptEvent verilen türdeki son olay; yoksa ok=false.
func (s *Store) LastScriptEvent(ctx context.Context, kind string) (betikkayit.Event, bool, error) {
	e, err := scanScriptEvent(s.db.QueryRowContext(ctx, `SELECT `+scriptEventCols+` FROM script_runs WHERE kind = ? ORDER BY seq DESC LIMIT 1`, kind))
	if errors.Is(err, sql.ErrNoRows) {
		return e, false, nil
	}
	return e, err == nil, err
}

// ScriptEventAt bir seq'teki olayın özeti; saklama nedeniyle yoksa ok=false.
func (s *Store) ScriptEventAt(ctx context.Context, seq int64) (hash string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT hash FROM script_runs WHERE seq = ?`, seq).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return hash, err == nil, err
}

// ExpiredScriptJobs süresi dolmuş, teslim edilmemiş işler.
func (s *Store) ExpiredScriptJobs(ctx context.Context, now int64) ([]string, error) {
	return s.stringList(ctx, `SELECT run_id FROM script_queue WHERE delivered_at = 0 AND expires_at <= ?`, now)
}

func (s *Store) stringList(ctx context.Context, q string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

const scriptEventCols = "seq, time, run_id, kind, actor_id, data, prev, hash"

func scanScriptEvent(row interface{ Scan(...any) error }) (betikkayit.Event, error) {
	var e betikkayit.Event
	err := row.Scan(&e.Seq, &e.Time, &e.RunID, &e.Kind, &e.ActorID, &e.Data, &e.Prev, &e.Hash)
	return e, err
}

func collectEvents(rows *sql.Rows) ([]betikkayit.Event, error) {
	defer rows.Close()
	out := []betikkayit.Event{}
	for rows.Next() {
		e, err := scanScriptEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ScriptRunEvents bir çalıştırmanın olayları, eskiden yeniye.
func (s *Store) ScriptRunEvents(ctx context.Context, runID string) ([]betikkayit.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+scriptEventCols+` FROM script_runs WHERE run_id = ? ORDER BY seq`, runID)
	if err != nil {
		return nil, err
	}
	return collectEvents(rows)
}

// ScriptRunFilter geçmiş süzgeci.
type ScriptRunFilter struct {
	Before int64 // seq; 0: en yeni
	Limit  int
}

// ScriptRunSummary liste satırı: başlatma olayı + son olay.
type ScriptRunSummary struct {
	Requested betikkayit.Event
	Last      betikkayit.Event
}

// ListScriptRuns son çalıştırmalar, yeniden eskiye.
func (s *Store) ListScriptRuns(ctx context.Context, f ScriptRunFilter) ([]ScriptRunSummary, error) {
	q := `SELECT ` + scriptEventCols + ` FROM script_runs WHERE kind = 'requested'`
	args := []any{}
	if f.Before > 0 {
		q += ` AND seq < ?`
		args = append(args, f.Before)
	}
	q += ` ORDER BY seq DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	reqs, err := collectEvents(rows)
	if err != nil {
		return nil, err
	}
	out := make([]ScriptRunSummary, 0, len(reqs))
	for _, r := range reqs {
		last, err := scanScriptEvent(s.db.QueryRowContext(ctx, `SELECT `+scriptEventCols+` FROM script_runs WHERE run_id = ? ORDER BY seq DESC LIMIT 1`, r.RunID))
		if err != nil {
			return nil, err
		}
		out = append(out, ScriptRunSummary{r, last})
	}
	return out, nil
}

// ScriptRequestsSince kullanıcının since'ten beri başlattığı çalıştırma sayısı.
func (s *Store) ScriptRequestsSince(ctx context.Context, actorID, since int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM script_runs WHERE kind = 'requested' AND actor_id = ? AND time >= ?`, actorID, since).Scan(&n)
	return n, err
}

// ScriptOutput çıktı; yoksa ok=false.
func (s *Store) ScriptOutput(ctx context.Context, runID string) (out []byte, sha string, truncated bool, ok bool, err error) {
	var tr int64
	err = s.db.QueryRowContext(ctx, `SELECT output, sha256, truncated FROM script_run_outputs WHERE run_id = ?`, runID).Scan(&out, &sha, &tr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", false, false, nil
	}
	return out, sha, tr != 0, err == nil, err
}

// ExportScriptLog zincirin tamamı (kalan önek + checkpoint'ler).
func (s *Store) ExportScriptLog(ctx context.Context) (betikkayit.Export, error) {
	x := betikkayit.Export{Format: 1, ExportedAt: time.Now().Unix(), Checkpoints: []betikkayit.Checkpoint{}}
	rows, err := s.db.QueryContext(ctx, `SELECT seq, hash, time, cutoff, created_at FROM script_log_checkpoint ORDER BY seq`)
	if err != nil {
		return x, err
	}
	defer rows.Close()
	for rows.Next() {
		var c betikkayit.Checkpoint
		if err := rows.Scan(&c.Seq, &c.Hash, &c.Time, &c.Cutoff, &c.CreatedAt); err != nil {
			return x, err
		}
		x.Checkpoints = append(x.Checkpoints, c)
	}
	if err := rows.Err(); err != nil {
		return x, err
	}
	rows.Close()
	ev, err := s.db.QueryContext(ctx, `SELECT `+scriptEventCols+` FROM script_runs ORDER BY seq`)
	if err != nil {
		return x, err
	}
	x.Events, err = collectEvents(ev)
	return x, err
}

// ScriptChainHead zincir başı.
func (s *Store) ScriptChainHead(ctx context.Context) (seq int64, hash string, err error) {
	err = s.ScriptTx(ctx, func(l *ScriptLog) error { seq, hash = l.seq, l.head; return nil })
	return seq, hash, err
}

// PruneScriptLog saklama: zincirin yalnız en eski öneki düşürülür, önce son
// özet checkpoint'e yazılır (§8.6). Timescale'de tam parçalar drop_chunks ile
// gider; düz PG'de dar yetkili işlev satır siler. Düşen satır sayısı parça
// sınırına yuvarlandığından cutoff'tan biraz eski kayıt kalabilir.
func (s *Store) PruneScriptLog(ctx context.Context, keep time.Duration) (dropped bool, err error) {
	if !s.postgres {
		return false, nil
	}
	cutoff := time.Now().Add(-keep).Unix()
	return dropped, s.ScriptTx(ctx, func(l *ScriptLog) error {
		boundary, ext := cutoff, ""
		if s.mode == DBModeTimescale || s.mode == DBModeTimescaleApache {
			ext = s.tsExt
			var end sql.NullInt64
			e := l.tx.QueryRowContext(ctx, `SELECT max(range_end_integer) FROM timescaledb_information.chunks
				WHERE hypertable_name = 'script_runs' AND hypertable_schema = current_schema() AND range_end_integer <= ?`, cutoff).Scan(&end)
			if e != nil {
				return e
			}
			if !end.Valid {
				return nil
			}
			boundary = end.Int64
		}
		var seq, at int64
		var hash string
		e := l.tx.QueryRowContext(ctx, `SELECT seq, hash, time FROM script_runs WHERE time < ? ORDER BY seq DESC LIMIT 1`, boundary).Scan(&seq, &hash, &at)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		if _, e = l.tx.ExecContext(ctx, `INSERT INTO script_log_checkpoint (seq, hash, time, cutoff, created_at) VALUES (?,?,?,?,?)`,
			seq, hash, at, boundary, l.now); e != nil {
			return e
		}
		if ext != "" {
			_, e = l.tx.ExecContext(ctx, fmt.Sprintf(`SELECT %s.drop_chunks('script_runs', older_than => CAST(%d AS BIGINT))`, ext, boundary))
		} else {
			_, e = l.tx.ExecContext(ctx, `SELECT script_runs_prune(?)`, boundary)
		}
		if e != nil {
			return e
		}
		dropped = true
		_, e = l.Append(ctx, "", "checkpoint", 0, map[string]any{"seq": seq, "hash": hash, "cutoff": boundary})
		return e
	})
}

// CleanScriptOutputs saklama süresi geçen çıktıları siler.
func (s *Store) CleanScriptOutputs(ctx context.Context, keep time.Duration) error {
	if !s.postgres {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM script_run_outputs WHERE created_at < ?`, time.Now().Add(-keep).Unix())
	return err
}

// tsScriptRuns script_runs'ı hypertable yapar (30 gün parça, 90 gün sonra
// sıkıştırma). Saklama kendi akışıyla çalışır (PruneScriptLog); retention
// politikası checkpoint yazamayacağı için eklenmez. Var olan veri taşınırken
// koruma tetikleyicileri kapatılır.
func (s *Store) tsScriptRuns(ctx context.Context, ext string, full bool) error {
	done, err := s.tsHas(ctx, "hypertables", "script_runs")
	if err != nil || done {
		return err
	}
	const day = 86400
	stmts := []string{
		`ALTER TABLE script_runs DISABLE TRIGGER script_runs_guard`,
		`ALTER TABLE script_runs DISABLE TRIGGER script_runs_guard_trunc`,
		fmt.Sprintf(`SELECT %s.create_hypertable('script_runs', %s.by_range('time', CAST(%d AS BIGINT)), migrate_data => true)`, ext, ext, 30*day),
		fmt.Sprintf(`SELECT %s.set_integer_now_func('script_runs', 'bekci_now')`, ext),
		`ALTER TABLE script_runs ENABLE TRIGGER script_runs_guard`,
		`ALTER TABLE script_runs ENABLE TRIGGER script_runs_guard_trunc`,
	}
	if full {
		stmts = append(stmts,
			`ALTER TABLE script_runs SET (timescaledb.compress, timescaledb.compress_orderby = 'time DESC, seq DESC')`,
			fmt.Sprintf(`SELECT %s.add_compression_policy('script_runs', compress_after => CAST(%d AS BIGINT))`, ext, 90*day))
	}
	return s.tsExec(ctx, stmts)
}
