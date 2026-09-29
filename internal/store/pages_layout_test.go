package store

import (
	"context"
	"log/slog"
	"reflect"
	"testing"
	"time"
)

// withoutMigrationsFrom from ve sonraki migration'ları geçici olarak kaldırır
// (eski sürümde veritabanı kurmak için); dönen fonksiyon (ve test sonu) geri koyar.
func withoutMigrationsFrom(t *testing.T, from int) func() {
	t.Helper()
	saved := map[int]string{}
	for v, sql := range migrations {
		if v >= from {
			saved[v] = sql
			delete(migrations, v)
		}
	}
	restored := false
	restore := func() {
		if !restored {
			for v, sql := range saved {
				migrations[v] = sql
			}
			restored = true
		}
	}
	t.Cleanup(restore)
	return restore
}

func blockIDs(l PageLayout) []string {
	var out []string
	for _, b := range l.Blocks {
		out = append(out, b.ID)
	}
	return out
}

func TestNormalizeLayout(t *testing.T) {
	def := DefaultLayout(true)
	if def.Style != LayoutList || def.Width != WidthNarrow || !reflect.DeepEqual(blockIDs(def), DefaultBlockOrder) {
		t.Fatalf("varsayılan dizilim: %+v", def)
	}
	for _, b := range def.Blocks {
		if !b.Visible {
			t.Fatalf("varsayılanda tüm bölümler görünür: %+v", def)
		}
	}
	if DefaultLayout(false).Visible(BlockIncidents) {
		t.Fatal("olaylar show_incidents'ı izlemeli")
	}

	// Bilinmeyen değerler varsayılana; bilinmeyen/tekrarlanan bölümler atılır,
	// eksikler görünür olarak sona eklenir; olaylar her zaman showIncidents.
	l := NormalizeLayout(PageLayout{Style: "masonry", Width: "dev", Blocks: []PageBlock{
		{ID: "groups", Visible: true}, {ID: "bilinmeyen", Visible: true}, {ID: "overall", Visible: false},
		{ID: "groups", Visible: false}, {ID: "incidents", Visible: false},
	}}, true)
	if l.Style != LayoutList || l.Width != WidthNarrow {
		t.Errorf("geçersiz değerler varsayılana çekilmeli: %+v", l)
	}
	if want := []string{"groups", "overall", "incidents", "announcements"}; !reflect.DeepEqual(blockIDs(l), want) {
		t.Errorf("sıra %v, %v bekleniyordu", blockIDs(l), want)
	}
	if !l.Visible(BlockGroups) || l.Visible(BlockOverall) || !l.Visible(BlockIncidents) || !l.Visible(BlockAnnouncements) {
		t.Errorf("görünürlük yanlış: %+v", l)
	}

	// Saklama: varsayılan boş metin; olayların görünürlüğü saklanmaz.
	if s := encodeLayout(DefaultLayout(false)); s != "" {
		t.Errorf("varsayılan dizilim boş saklanmalı: %q", s)
	}
	custom := NormalizeLayout(PageLayout{Style: LayoutCompact, Width: WidthWide, Blocks: []PageBlock{
		{ID: "incidents", Visible: false}, {ID: "announcements", Visible: false}}}, false)
	enc := encodeLayout(custom)
	if enc != `{"style":"compact","width":"wide","order":["incidents","announcements","overall","groups"],"hidden":["announcements"]}` {
		t.Errorf("saklanan biçim: %s", enc)
	}
	if got := decodeLayout(enc, false); !reflect.DeepEqual(got, custom) {
		t.Errorf("geri çözme:\n%+v\n%+v", got, custom)
	}
	if got := decodeLayout(enc, true); !got.Visible(BlockIncidents) {
		t.Error("olaylar sütundan (show_incidents) gelmeli")
	}
	for _, bad := range []string{"", "{", `"x"`, `{"style":1}`} {
		if got := decodeLayout(bad, true); !reflect.DeepEqual(got, DefaultLayout(true)) {
			t.Errorf("bozuk %q varsayılan vermeli: %+v", bad, got)
		}
	}
}

func TestPageLayoutStore(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p := StatusPage{Slug: "dizilim", Title: "D", ShowIncidents: true,
		Layout: PageLayout{Style: LayoutGrid, Width: WidthWide, Blocks: []PageBlock{{ID: "groups", Visible: true}, {ID: "overall", Visible: false}}}}
	if err := s.CreatePage(ctx, &p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPage(ctx, p.ID)
	if err != nil || got.Layout.Style != LayoutGrid || got.Layout.Width != WidthWide ||
		!reflect.DeepEqual(blockIDs(got.Layout), []string{"groups", "overall", "announcements", "incidents"}) || got.Layout.Visible(BlockOverall) {
		t.Fatalf("dizilim saklanmadı: %+v %v", got.Layout, err)
	}
	// Olaylar kapatılınca dizilimde de kapalı görünür (tek kaynak).
	got.ShowIncidents = false
	if err := s.UpdatePage(ctx, &got); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetPage(ctx, p.ID)
	if got.Layout.Visible(BlockIncidents) || got.Layout.Style != LayoutGrid {
		t.Fatalf("güncelleme: %+v", got.Layout)
	}
	list, _ := s.ListPages(ctx)
	if len(list) != 1 || !reflect.DeepEqual(list[0].Layout, got.Layout) {
		t.Fatalf("liste: %+v", list)
	}
}

// Migration 17: eski (16) veritabanındaki sayfalar varsayılan dizilimi alır;
// kapalı olay bölümü dizilimde de kapalıdır.
func TestPageLayoutMigration(t *testing.T) {
	ctx := context.Background()
	dbPath := testTarget(t)
	restore := withoutMigrationsFrom(t, 17)

	s, err := Open(dbPath, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.schemaVersion(ctx); v != 16 {
		t.Fatalf("eski sürüm 16 olmalı: %d", v)
	}
	for _, q := range []string{
		`INSERT INTO status_pages (slug, title, description, footer, sections, show_targets, published, created_at, updated_at, show_incidents)
		 VALUES ('eski', 'Eski', '', '', '[]', 0, 1, 1, 1, 1)`,
		`INSERT INTO status_pages (slug, title, description, footer, sections, show_targets, published, created_at, updated_at, show_incidents)
		 VALUES ('olaysiz', 'Olaysız', '', '', '[]', 0, 1, 1, 1, 0)`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	restore()
	s, err = OpenWith(dbPath, time.UTC, Options{Log: slog.New(slog.NewTextHandler(&syncBuf{}, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, _ := s.schemaVersion(ctx); v < 17 {
		t.Fatalf("migration uygulanmadı: %d", v)
	}
	old, err := s.PageBySlug(ctx, "eski")
	if err != nil || !reflect.DeepEqual(old.Layout, DefaultLayout(true)) {
		t.Fatalf("eski sayfa varsayılan dizilim almalı: %+v %v", old.Layout, err)
	}
	noInc, err := s.PageBySlug(ctx, "olaysiz")
	if err != nil || !reflect.DeepEqual(noInc.Layout, DefaultLayout(false)) || noInc.Layout.Visible(BlockIncidents) {
		t.Fatalf("olaysız sayfa: %+v %v", noInc.Layout, err)
	}
	var raw string
	if err := s.db.QueryRowContext(ctx, "SELECT layout FROM status_pages WHERE slug = 'eski'").Scan(&raw); err != nil || raw != "" {
		t.Fatalf("sütun varsayılanı boş olmalı: %q %v", raw, err)
	}
}
