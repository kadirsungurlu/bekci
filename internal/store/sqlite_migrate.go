package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// SQLite -> PostgreSQL/TimescaleDB taşıma (uptime migrate, tasarım §5.3-5.4).
//
// Akış: kaynak dosyanın tutarlı kopyası alınır (VACUUM INTO) ve kopya bu
// sürümün şemasına yükseltilir; asıl dosyaya yazılmaz, geri dönüş eski imajdır.
// Hedef önce DÜZ PostgreSQL şemasıyla açılır (hypertable yok), tablolar
// parti parti kopyalanır, satır sayıları doğrulanır, sonra hedef normal
// açılışla açılır: var olan veriyle Timescale dönüşümü panelin kendi, testli
// yolundan (ensureTimescale) geçer.
//
// Kesintiye dayanıklılık: her parti, ilerleme kaydıyla (migrate_progress) aynı
// işlemde yazılır. Yeniden çalıştırınca tablo aynı sırayla okunur ve yazılmış
// satır sayısı kadar atlanır. Kaynak arada değiştiyse (parmak izi tutmaz) devam
// edilmez.

// ErrSQLitePending PostgreSQL hedefi boşken veri klasöründe SQLite veritabanı var.
var ErrSQLitePending = errors.New("SQLite verisi taşınmadı")

// SQLiteMigrateOptions taşıma ayarları.
type SQLiteMigrateOptions struct {
	From         string // SQLite dosyası
	To           string // PostgreSQL adresi
	Batch        int    // parti başına satır (0 = 10.000)
	Restart      bool   // yarım kalan taşımayı silip baştan başlar
	DryRun       bool   // yalnız kaynağı okur, plan yazar; hedefe dokunmaz
	WorkDir      string // geçici kopyanın klasörü (boş: kaynağın yanı)
	TimescaleOff bool
	Out          io.Writer // ilerleme çıktısı
	Log          *slog.Logger
	Loc          *time.Location

	afterBatch func(table string, copied int64) error // test: kesinti benzetimi
}

// SQLiteMigrateResult taşıma özeti.
type SQLiteMigrateResult struct {
	Tables    int
	Rows      int64
	Sanitized int64 // PostgreSQL metnine sığmayan karakter temizlenen değer sayısı
	Skipped   []string
	Resumed   bool
	Already   bool // hedefe daha önce taşınmış, kopyalama yapılmadı
	Mode      DBMode
}

const migrateMetaDDL = `
CREATE TABLE IF NOT EXISTS migrate_meta (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	fingerprint TEXT NOT NULL,
	started_at BIGINT NOT NULL,
	finished_at BIGINT NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS migrate_progress (
	tbl TEXT PRIMARY KEY,
	copied BIGINT NOT NULL DEFAULT 0,
	done BOOLEAN NOT NULL DEFAULT FALSE
)`

type migColumn struct{ name, pgType string }

type migTable struct {
	name    string
	cols    []migColumn
	order   string
	total   int64
	dropped []string // hedefte karşılığı olmayan kaynak sütunları
}

// MigrateSQLite SQLite dosyasını boş bir PostgreSQL hedefine taşır.
func MigrateSQLite(ctx context.Context, o SQLiteMigrateOptions) (*SQLiteMigrateResult, error) {
	if o.Batch <= 0 {
		o.Batch = 10000
	}
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Loc == nil {
		o.Loc = time.Local
	}
	if !IsPostgresDSN(o.To) && !o.DryRun {
		return nil, errors.New("hedef PostgreSQL adresi gerekli (DATABASE_URL ya da --to postgres://...)")
	}
	fi, err := os.Stat(o.From)
	if err != nil {
		return nil, fmt.Errorf("kaynak SQLite dosyası okunamadı: %w", err)
	}
	if fi.Size() == 0 {
		return nil, fmt.Errorf("kaynak SQLite dosyası boş: %s", o.From)
	}
	// Panel açıkken kopya alınmaz: sonradan gelen kayıtlar taşınmaz, iki panel
	// aynı anda çalışır. Kilit taşıma boyunca tutulur.
	lk := &Store{opts: Options{LockWait: 3 * time.Second, Log: o.Log}}
	if err := lk.acquireInstanceLock(o.From); err != nil {
		return nil, fmt.Errorf("kaynak veritabanı başka bir süreçte açık, önce paneli durdurun: %w", err)
	}
	defer lk.releaseLock()

	dir := o.WorkDir
	if dir == "" {
		dir = filepath.Dir(o.From)
	}
	// Önceki kesintiden kalan kopyalar (kilit bizde, başka taşıma çalışmıyor).
	if old, err := filepath.Glob(filepath.Join(dir, ".bekci-tasima-*")); err == nil {
		for _, d := range old {
			os.RemoveAll(d)
		}
	}
	tmp, err := os.MkdirTemp(dir, ".bekci-tasima-")
	if err != nil {
		return nil, fmt.Errorf("geçici klasör açılamadı (--work-dir ile başka yer verilebilir): %w", err)
	}
	defer os.RemoveAll(tmp)

	fmt.Fprintf(o.Out, "Kaynak kopyalanıyor: %s (%.1f MB)\n", o.From, float64(fi.Size())/1e6)
	src, err := sqliteWorkingCopy(ctx, o, tmp)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	if o.DryRun {
		tables, skipped, err := planFromSource(ctx, src.db.db)
		if err != nil {
			return nil, err
		}
		res := &SQLiteMigrateResult{Tables: len(tables), Skipped: skipped}
		for _, t := range tables {
			res.Rows += t.total
			fmt.Fprintf(o.Out, "  %-32s %12d satır\n", t.name, t.total)
		}
		return res, nil
	}

	// Hedef: düz şema. Tek-örnek kilidi: hedefte panel çalışıyorsa başlamaz.
	dst, err := OpenWith(o.To, o.Loc, Options{
		Log: o.Log, InstanceLock: true, LockWait: 3 * time.Second, TimescaleOff: true,
	})
	if err != nil {
		return nil, fmt.Errorf("hedef açılamadı: %w", err)
	}
	dstOpen := true
	closeDst := func() {
		if dstOpen {
			dst.Close()
			dstOpen = false
		}
	}
	defer closeDst()

	res := &SQLiteMigrateResult{}
	finished, err := migrateCopy(ctx, o, src, dst, res)
	if err != nil {
		return nil, err
	}
	closeDst()
	if finished {
		res.Already = true
		fmt.Fprintln(o.Out, "Taşıma bu hedefe daha önce tamamlanmış; veri kopyalanmadı.")
		fmt.Fprintln(o.Out, "Uyarı: geri dönüp SQLite ile çalıştıysanız o dönemin verisi aktarılmadı; taşımayı yenilemek için hedef veritabanını boşaltıp komutu tekrar çalıştırın.")
	}

	// Normal açılış: eklenti varsa dönüşüm burada yapılır (panel açılışıyla aynı kod).
	fmt.Fprintln(o.Out, "Hedef açılıyor (TimescaleDB varsa dönüşüm yapılır, büyük veride dakikalar sürebilir)...")
	final, err := OpenWith(o.To, o.Loc, Options{Log: o.Log, TimescaleOff: o.TimescaleOff})
	if err != nil {
		return res, fmt.Errorf("veri kopyalandı ve doğrulandı ama hedef dönüşümü başarısız (panel açılışta yeniden dener): %w", err)
	}
	defer final.Close()
	res.Mode = final.DBMode()
	if !finished {
		var want, got int64
		if err := src.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM heartbeats").Scan(&want); err != nil {
			return res, err
		}
		if err := final.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM heartbeats").Scan(&got); err != nil {
			return res, err
		}
		if want != got {
			return res, fmt.Errorf("dönüşüm sonrası heartbeats satır sayısı tutmuyor: kaynak %d, hedef %d", want, got)
		}
	}
	return res, nil
}

// sqliteWorkingCopy kaynağın tutarlı kopyasını alır ve bu sürümün şemasına yükseltir.
func sqliteWorkingCopy(ctx context.Context, o SQLiteMigrateOptions, tmp string) (*Store, error) {
	path := filepath.Join(tmp, "kopya.db")
	raw, err := sql.Open("sqlite", sqliteDSN(o.From))
	if err != nil {
		return nil, err
	}
	defer raw.Close()
	if _, err := raw.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return nil, fmt.Errorf("kaynağın kopyası alınamadı (boş disk alanı?): %w", err)
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := OpenWith(path, o.Loc, Options{Log: quiet, BackupDir: filepath.Join(tmp, "yedek")})
	if err != nil {
		return nil, fmt.Errorf("kopyanın şeması yükseltilemedi: %w", err)
	}
	return st, nil
}

// migrateCopy kopyalama + doğrulama; true: hedefte taşıma zaten bitmiş.
func migrateCopy(ctx context.Context, o SQLiteMigrateOptions, src, dst *Store, res *SQLiteMigrateResult) (bool, error) {
	if _, err := dst.db.db.ExecContext(ctx, migrateMetaDDL); err != nil {
		return false, err
	}
	var fp string
	var finishedAt int64
	err := dst.db.QueryRowContext(ctx, "SELECT fingerprint, finished_at FROM migrate_meta").Scan(&fp, &finishedAt)
	hasMeta := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if hasMeta && finishedAt > 0 {
		if o.Restart {
			return false, errors.New("bu hedefe taşıma tamamlanmış, panel kullanmış olabilir; --restart reddedildi. Baştan taşımak için boş bir veritabanı verin")
		}
		return true, nil
	}
	// Hedef düz tablo olmalı: Timescale'e çevrilmiş boş kurulumda stats_hourly görünümdür.
	var relkind string
	if err := dst.db.QueryRowContext(ctx,
		"SELECT c.relkind::text FROM pg_class c WHERE c.oid = to_regclass('stats_hourly')").Scan(&relkind); err != nil {
		return false, err
	}
	if relkind != "r" {
		return false, errors.New("hedef veritabanı panelle açılıp TimescaleDB'ye çevrilmiş; taşıma boş veritabanı ister. " +
			"Panel daha önce bu hedefle başlatıldıysa veritabanı birimini silip yeniden oluşturun (içinde veri yoksa), sonra paneli başlatmadan taşıyın")
	}
	tables, skipped, err := planFromSource(ctx, src.db.db)
	if err != nil {
		return false, err
	}
	res.Skipped = skipped
	if err := planForTarget(ctx, dst.db.db, tables); err != nil {
		return false, err
	}
	tables, err = orderByForeignKeys(ctx, dst.db.db, tables)
	if err != nil {
		return false, err
	}
	want := sourceFingerprint(tables)
	switch {
	case hasMeta && o.Restart:
		o.Log.Info("yarım kalan taşıma siliniyor, baştan başlanıyor")
		hasMeta = false
	case hasMeta && fp != want:
		return false, errors.New("kaynak veritabanı önceki denemeden sonra değişmiş (panel açılmış olabilir); yarım kalan taşımaya devam edilemez. Baştan almak için --restart ekleyin")
	case !hasMeta:
		var n int64
		if err := dst.db.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM users) + (SELECT COUNT(*) FROM monitors)").Scan(&n); err != nil {
			return false, err
		}
		if n > 0 {
			return false, fmt.Errorf("hedef veritabanı boş değil (%d kullanıcı/monitör); taşıma var olan veriyi ezmez", n)
		}
	}
	if !hasMeta {
		// Migration'ların eklediği varsayılan satırlar da temizlenir (kaynaktakiler gelecek).
		names := make([]string, 0, len(tables))
		for _, t := range tables {
			names = append(names, pgIdent(t.name))
		}
		stmts := []string{
			"DELETE FROM migrate_meta", "DELETE FROM migrate_progress",
			"TRUNCATE " + strings.Join(names, ", ") + " RESTART IDENTITY CASCADE",
		}
		for _, q := range stmts {
			if _, err := dst.db.db.ExecContext(ctx, q); err != nil {
				return false, err
			}
		}
		if _, err := dst.db.db.ExecContext(ctx,
			"INSERT INTO migrate_meta (id, fingerprint, started_at) VALUES (1, $1, $2)", want, time.Now().Unix()); err != nil {
			return false, err
		}
	} else {
		res.Resumed = true
		fmt.Fprintln(o.Out, "Yarım kalan taşıma bulundu, kaldığı yerden devam ediliyor.")
	}

	pg, err := pgx.Connect(ctx, o.To)
	if err != nil {
		return false, err
	}
	defer pg.Close(context.Background())
	// Yabancı anahtar sırası zaten gözetiliyor; süper kullanıcıda ayrıca kapatılır
	// (ör. döngüsel başvuru ya da SQLite'ta kalmış öksüz satır). Yetki yoksa sorun değil.
	if _, err := pg.Exec(ctx, "SET session_replication_role = replica"); err != nil {
		o.Log.Debug("session_replication_role ayarlanamadı", "err", err)
	}

	var grand int64
	for _, t := range tables {
		grand += t.total
	}
	p := &migProgress{out: o.Out, grand: grand, start: time.Now()}
	for i, t := range tables {
		var copied int64
		var done bool
		err := dst.db.QueryRowContext(ctx, "SELECT copied, done FROM migrate_progress WHERE tbl = $1", t.name).Scan(&copied, &done)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
		p.base += copied
		if done {
			continue
		}
		fmt.Fprintf(o.Out, "[%d/%d] %s (%d satır)\n", i+1, len(tables), t.name, t.total)
		if err := copyTable(ctx, o, src.db.db, pg, t, copied, res, p); err != nil {
			return false, fmt.Errorf("%s: %w", t.name, err)
		}
	}
	if err := resetSequences(ctx, dst.db.db, tables); err != nil {
		return false, fmt.Errorf("sıra sayaçları: %w", err)
	}
	fmt.Fprintln(o.Out, "Satır sayıları doğrulanıyor...")
	var bad []string
	for _, t := range tables {
		var got int64
		if err := dst.db.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+pgIdent(t.name)).Scan(&got); err != nil {
			return false, err
		}
		res.Rows += got
		if got != t.total {
			bad = append(bad, fmt.Sprintf("%s: kaynak %d, hedef %d", t.name, t.total, got))
		}
	}
	res.Tables = len(tables)
	if len(bad) > 0 {
		return false, fmt.Errorf("satır sayıları tutmuyor, taşıma tamamlanmadı: %s", strings.Join(bad, "; "))
	}
	_, err = dst.db.db.ExecContext(ctx, "UPDATE migrate_meta SET finished_at = $1", time.Now().Unix())
	return false, err
}

// planFromSource kopyadaki tablolar, sütunlar, sıralama ve satır sayıları.
func planFromSource(ctx context.Context, db *sql.DB) ([]*migTable, []string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, nil, err
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if n != "schema_version" {
			names = append(names, n)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var out []*migTable
	for _, n := range names {
		t := &migTable{name: n}
		pks := map[int]string{}
		cr, err := db.QueryContext(ctx, `SELECT name, pk, hidden FROM pragma_table_xinfo(?)`, n)
		if err != nil {
			return nil, nil, err
		}
		for cr.Next() {
			var c string
			var pk, hidden int
			if err := cr.Scan(&c, &pk, &hidden); err != nil {
				cr.Close()
				return nil, nil, err
			}
			if hidden != 0 {
				continue // üretilmiş sütun
			}
			t.cols = append(t.cols, migColumn{name: c})
			if pk > 0 {
				pks[pk] = c
			}
		}
		cr.Close()
		if err := cr.Err(); err != nil {
			return nil, nil, err
		}
		// Sıra her çalıştırmada aynı olmalı (devam, satır atlayarak yapılır).
		var withoutRowid int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_list WHERE name = ? AND wr = 1`, n).Scan(&withoutRowid); err != nil {
			return nil, nil, err
		}
		t.order = "rowid"
		if withoutRowid > 0 {
			ord := make([]string, 0, len(pks))
			for i := 1; i <= len(pks); i++ {
				ord = append(ord, pgIdent(pks[i]))
			}
			t.order = strings.Join(ord, ", ")
		}
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+pgIdent(n)).Scan(&t.total); err != nil {
			return nil, nil, err
		}
		out = append(out, t)
	}
	return out, nil, nil
}

// planForTarget tabloları hedefteki sütun türleriyle eşler; hedefte olmayan
// kaynak tablosu/sütunu hata sayılır (sessiz veri kaybı olmasın).
func planForTarget(ctx context.Context, db *sql.DB, tables []*migTable) error {
	rows, err := db.QueryContext(ctx, `SELECT table_name, column_name, data_type FROM information_schema.columns
		WHERE table_schema = current_schema() AND is_generated = 'NEVER'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	types := map[string]map[string]string{}
	for rows.Next() {
		var tn, cn, dt string
		if err := rows.Scan(&tn, &cn, &dt); err != nil {
			return err
		}
		if types[tn] == nil {
			types[tn] = map[string]string{}
		}
		types[tn][cn] = dt
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, t := range tables {
		tt := types[t.name]
		if tt == nil {
			return fmt.Errorf("hedefte tablo yok: %s", t.name)
		}
		for i := range t.cols {
			dt, ok := tt[t.cols[i].name]
			if !ok {
				return fmt.Errorf("hedefte sütun yok: %s.%s", t.name, t.cols[i].name)
			}
			t.cols[i].pgType = dt
		}
	}
	return nil
}

// orderByForeignKeys üst tabloları önce kopyalar (süper kullanıcı olmayan
// hedefte yabancı anahtar kapatılamaz). Döngü varsa kalanlar ad sırasıyla gelir.
func orderByForeignKeys(ctx context.Context, db *sql.DB, tables []*migTable) ([]*migTable, error) {
	rows, err := db.QueryContext(ctx, `SELECT c.conrelid::regclass::text, c.confrelid::regclass::text
		FROM pg_constraint c JOIN pg_class r ON r.oid = c.conrelid
		WHERE c.contype = 'f' AND c.conrelid <> c.confrelid AND r.relnamespace = current_schema()::regnamespace`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deps := map[string][]string{}
	for rows.Next() {
		var child, parent string
		if err := rows.Scan(&child, &parent); err != nil {
			return nil, err
		}
		deps[child] = append(deps[child], parent)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	done := map[string]bool{}
	var out []*migTable
	for len(out) < len(tables) {
		progressed := false
		for _, t := range tables {
			if done[t.name] {
				continue
			}
			ready := true
			for _, p := range deps[t.name] {
				if !done[p] && slices.ContainsFunc(tables, func(x *migTable) bool { return x.name == p }) {
					ready = false
				}
			}
			if ready {
				done[t.name] = true
				out = append(out, t)
				progressed = true
			}
		}
		if !progressed {
			for _, t := range tables {
				if !done[t.name] {
					done[t.name] = true
					out = append(out, t)
					break
				}
			}
		}
	}
	return out, nil
}

func sourceFingerprint(tables []*migTable) string {
	h := sha256.New()
	for _, t := range tables {
		fmt.Fprintf(h, "%s=%d\n", t.name, t.total)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// copyTable tabloyu partiler hâlinde yazar; parti + ilerleme tek işlem.
func copyTable(ctx context.Context, o SQLiteMigrateOptions, src *sql.DB, pg *pgx.Conn, t *migTable, copied int64, res *SQLiteMigrateResult, p *migProgress) error {
	if _, err := pg.Exec(ctx, `INSERT INTO migrate_progress (tbl) VALUES ($1) ON CONFLICT DO NOTHING`, t.name); err != nil {
		return err
	}
	names := make([]string, len(t.cols))
	quoted := make([]string, len(t.cols))
	for i, c := range t.cols {
		names[i] = c.name
		quoted[i] = pgIdent(c.name)
	}
	rows, err := src.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s ORDER BY %s LIMIT -1 OFFSET ?",
		strings.Join(quoted, ", "), pgIdent(t.name), t.order), copied)
	if err != nil {
		return err
	}
	defer rows.Close()

	flush := func(batch [][]any) error {
		tx, err := pg.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(context.Background())
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{t.name}, names, pgx.CopyFromRows(batch)); err != nil {
			return err
		}
		copied += int64(len(batch))
		if _, err := tx.Exec(ctx, `UPDATE migrate_progress SET copied = $1 WHERE tbl = $2`, copied, t.name); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		p.add(int64(len(batch)))
		if o.afterBatch != nil {
			return o.afterBatch(t.name, copied)
		}
		return nil
	}

	batch := make([][]any, 0, o.Batch)
	for rows.Next() {
		vals := make([]any, len(t.cols))
		ptrs := make([]any, len(t.cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		for i := range vals {
			v, changed, err := pgValue(vals[i], t.cols[i].pgType)
			if err != nil {
				return fmt.Errorf("sütun %s: %w", t.cols[i].name, err)
			}
			vals[i] = v
			if changed {
				res.Sanitized++
			}
		}
		batch = append(batch, vals)
		if len(batch) >= o.Batch {
			if err := flush(batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(batch) > 0 {
		if err := flush(batch); err != nil {
			return err
		}
	}
	_, err = pg.Exec(ctx, `UPDATE migrate_progress SET done = TRUE WHERE tbl = $1`, t.name)
	return err
}

// pgValue SQLite'ın esnek türünü hedef sütun türüne çevirir. changed: metin,
// PostgreSQL'in kabul etmediği karakterlerden (NUL, geçersiz UTF-8) temizlendi.
func pgValue(v any, pgType string) (any, bool, error) {
	if v == nil {
		return nil, false, nil
	}
	switch pgType {
	case "bigint", "integer", "smallint":
		switch x := v.(type) {
		case int64:
			return x, false, nil
		case float64:
			return int64(math.Round(x)), false, nil
		case bool:
			if x {
				return int64(1), false, nil
			}
			return int64(0), false, nil
		case string:
			n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
			if err != nil {
				return nil, false, fmt.Errorf("sayı beklenirken %q", x)
			}
			return n, false, nil
		}
	case "boolean":
		switch x := v.(type) {
		case bool:
			return x, false, nil
		case int64:
			return x != 0, false, nil
		case float64:
			return x != 0, false, nil
		case string:
			return x == "1" || strings.EqualFold(x, "true"), false, nil
		}
	case "double precision", "real":
		switch x := v.(type) {
		case float64:
			return x, false, nil
		case int64:
			return float64(x), false, nil
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
			if err != nil {
				return nil, false, fmt.Errorf("sayı beklenirken %q", x)
			}
			return f, false, nil
		}
	case "bytea":
		switch x := v.(type) {
		case []byte:
			return x, false, nil
		case string:
			return []byte(x), false, nil
		}
	case "text", "character varying", "character":
		var s string
		switch x := v.(type) {
		case string:
			s = x
		case []byte:
			s = string(x)
		case int64:
			return strconv.FormatInt(x, 10), false, nil
		case float64:
			return strconv.FormatFloat(x, 'f', -1, 64), false, nil
		case bool:
			return strconv.FormatBool(x), false, nil
		default:
			return nil, false, fmt.Errorf("beklenmeyen tür %T", v)
		}
		clean := strings.ReplaceAll(strings.ToValidUTF8(s, "�"), "\x00", "")
		return clean, clean != s, nil
	}
	return nil, false, fmt.Errorf("desteklenmeyen dönüşüm: %T -> %s", v, pgType)
}

// resetSequences kimlik sayaçlarını taşınan en büyük değere getirir; yoksa
// yeni kayıt, taşınmış bir kimlikle çakışırdı.
func resetSequences(ctx context.Context, db *sql.DB, tables []*migTable) error {
	rows, err := db.QueryContext(ctx, `SELECT table_name, column_name, pg_get_serial_sequence(quote_ident(table_name), column_name)
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND (column_default LIKE 'nextval%' OR is_identity = 'YES')`)
	if err != nil {
		return err
	}
	type seq struct{ tbl, col, name string }
	var seqs []seq
	for rows.Next() {
		var s seq
		var name sql.NullString
		if err := rows.Scan(&s.tbl, &s.col, &name); err != nil {
			rows.Close()
			return err
		}
		if name.Valid {
			s.name = name.String
			seqs = append(seqs, s)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, s := range seqs {
		if !slices.ContainsFunc(tables, func(t *migTable) bool { return t.name == s.tbl }) {
			continue
		}
		q := fmt.Sprintf(`SELECT setval($1, GREATEST(COALESCE(MAX(%s), 0), 1), MAX(%s) IS NOT NULL) FROM %s`,
			pgIdent(s.col), pgIdent(s.col), pgIdent(s.tbl))
		var got int64
		if err := db.QueryRowContext(ctx, q, s.name).Scan(&got); err != nil {
			return err
		}
	}
	return nil
}

func pgIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// migProgress genel ilerleme satırı (en çok 2 sn'de bir).
type migProgress struct {
	out   io.Writer
	grand int64
	base  int64 // önceki çalıştırmada yazılmış
	done  int64
	start time.Time
	last  time.Time
}

func (p *migProgress) add(n int64) {
	p.done += n
	now := time.Now()
	if now.Sub(p.last) < 2*time.Second {
		return
	}
	p.last = now
	total := p.base + p.done
	rate := float64(p.done) / max(now.Sub(p.start).Seconds(), 0.001)
	remain := time.Duration(float64(p.grand-total) / max(rate, 1) * float64(time.Second)).Round(time.Second)
	fmt.Fprintf(p.out, "  %d / %d satır (%%%d), %.0f satır/sn, kalan ~%s\n",
		total, p.grand, total*100/max(p.grand, 1), rate, remain)
}

// CheckSQLiteLeftover panel PostgreSQL ile açılmadan önce çağrılır: veri
// klasöründe dolu bir SQLite varken hedef boşsa (ya da taşıma yarımsa) panel
// boş kurulum ekranıyla açılmasın, eski veriler kaybolmuş sanılmasın.
func CheckSQLiteLeftover(ctx context.Context, dsn, sqlitePath string) error {
	fi, err := os.Stat(sqlitePath)
	if err != nil || fi.Size() == 0 {
		return nil
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var meta, users sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT to_regclass('migrate_meta')::text, to_regclass('users')::text").Scan(&meta, &users); err != nil {
		return nil // veritabanı henüz hazır değil: asıl açılış hatayı söyler
	}
	if meta.Valid {
		var finished int64
		err := db.QueryRowContext(ctx, "SELECT finished_at FROM migrate_meta").Scan(&finished)
		if err == nil && finished == 0 {
			return fmt.Errorf("%w: taşıma yarım kalmış, uptime migrate komutunu yeniden çalıştırın", ErrSQLitePending)
		}
		if err == nil {
			return nil
		}
	}
	var n int64
	if users.Valid {
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n); err != nil {
			return nil
		}
	}
	if n > 0 {
		return nil
	}
	return fmt.Errorf("%w: %s var ama PostgreSQL boş. Paneli başlatmadan önce uptime migrate çalıştırın "+
		"(eski veriyi bilerek bırakıp boş başlamak için SQLITE_IGNORE=1)", ErrSQLitePending, sqlitePath)
}
