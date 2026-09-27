package store

func init() {
	// 13: herkese açık durum sayfasında "Son 14 günün olayları" bölümü
	// gösterilsin mi? Mevcut sayfalar eskisi gibi gösterir.
	RegisterMigration(13, `ALTER TABLE status_pages ADD COLUMN show_incidents INTEGER NOT NULL DEFAULT 1;`)
}

func init() {
	// 14: herkese açık sayfada gruplar (bölümler) ziyaretçi tarafından açılıp
	// kapatılabilsin mi? Varsayılan kapalı: mevcut sayfalar değişmez.
	RegisterMigration(14, `ALTER TABLE status_pages ADD COLUMN collapsible INTEGER NOT NULL DEFAULT 0;`)
}
