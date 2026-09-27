package store

func init() {
	// 13: herkese açık durum sayfasında "Son 14 günün olayları" bölümü
	// gösterilsin mi? Mevcut sayfalar eskisi gibi gösterir.
	RegisterMigration(13, `ALTER TABLE status_pages ADD COLUMN show_incidents INTEGER NOT NULL DEFAULT 1;`)
}
