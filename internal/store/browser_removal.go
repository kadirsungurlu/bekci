package store

func init() {
	// 9: gerçek tarayıcı (Chrome) monitör tipi kaldırıldı. Ayarlarındaki adres,
	// kelime ve TLS seçeneği HTTP kontrolüyle aynı adları taşıdığından bu
	// monitörler HTTP monitörü olarak çalışmaya devam eder.
	RegisterMigration(9, `UPDATE monitors SET type = 'http' WHERE type = 'browser';`)
}
