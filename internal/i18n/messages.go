package i18n

// messages anahtarlı mesaj kataloğu. Anahtarlar alan.adı biçimindedir
// (notify.*, metric.*, dur.*, badge.*). Her girdi iki dili birlikte taşır;
// TR mevcut Türkçe metnin birebir aynısıdır. Biçim fiilleri (%s, %d …) iki
// dilde aynı sırada ve sayıda olmalıdır (TestCatalog denetler).
// ".one" ile biten anahtarlar TN'nin tekil biçimidir.
var messages = map[string]Msg{
	// Monitör bildirimleri: başlıklar (%s: monitör adı)
	"notify.down.title":        {"🔴 %s çalışmıyor", "🔴 %s is down"},
	"notify.up.title":          {"🟢 %s tekrar çalışıyor", "🟢 %s is up again"},
	"notify.reminder.title":    {"🔴 %s hâlâ çalışmıyor", "🔴 %s is still down"},
	"notify.cert.expired":      {"⚠️ %s: SSL sertifikasının süresi doldu", "⚠️ %s: SSL certificate has expired"},
	"notify.cert.expiring":     {"⚠️ %s: SSL sertifikası %d gün içinde bitiyor", "⚠️ %s: SSL certificate expires in %d days"},
	"notify.cert.expiring.one": {"⚠️ %s: SSL sertifikası %d gün içinde bitiyor", "⚠️ %s: SSL certificate expires in %d day"},
	"notify.test.title":        {"✅ Test bildirimi", "✅ Test notification"},
	"notify.test.body":         {"%s bildirim kanalınız çalışıyor.", "Your %s notification channel is working."},
	"notify.sample.note":       {"(Örnek bildirim — gerçek bir olay değil)", "(Sample notification — not a real event)"},

	// Bildirim metnindeki satır başlıkları
	"notify.field.server":   {"Sunucu", "Server"},
	"notify.field.target":   {"Hedef", "Target"},
	"notify.field.reason":   {"Neden", "Reason"},
	"notify.field.downtime": {"Kesinti süresi", "Downtime"},
	"notify.field.expires":  {"Bitiş", "Expires"},
	"notify.field.issuer":   {"Veren", "Issuer"},
	"notify.field.last_avg": {"Son ortalama", "Last average"},
	"notify.field.info":     {"Ayrıntı", "Info"},
	"notify.field.time":     {"Zaman", "Time"},
	"notify.field.link":     {"Detay", "Details"},

	// Sunucu uyarıları (%s: sunucu adı)
	"notify.server.offline":      {"🔴 %s: sunucudan veri gelmiyor", "🔴 %s: no data from the server"},
	"notify.server.online":       {"🟢 %s: tekrar veri gönderiyor", "🟢 %s: sending data again"},
	"notify.server.alert":        {"🔴 %s: %s %s (%d dk %s, eşik %s)", "🔴 %s: %s %s (%d min %s, threshold %s)"},
	"notify.server.avg":          {"ortalama", "average"},
	"notify.server.avg_per_core": {"ortalama, çekirdek başına", "average, per core"},
	"notify.server.resolved":     {"🟢 %s: %s normale döndü", "🟢 %s: %s back to normal"},
	"notify.server.last_data":    {"Son veri: %s", "Last data: %s"},
	"notify.server.value_gone": {"Değer %d dakikadır gelmiyor (bölüm veya sensör artık raporlanmıyor)",
		"No value for %d minutes (the partition or sensor is no longer reported)"},
	"notify.opsgenie.close_note": {"%s: sorun giderildi", "%s: resolved"},

	// Sunucu olaylarının nedeni (%s: metrik adı, değer, %d: dakika, %s: eşik)
	"incident.server.offline":        {"Sunucudan veri gelmiyor", "No data from the server"},
	"incident.server.alert":          {"%s %s (%d dk ortalama, eşik %s)", "%s %s (%d min average, threshold %s)"},
	"incident.server.alert_per_core": {"%s %s (%d dk ortalama, çekirdek başına, eşik %s)", "%s %s (%d min average, per core, threshold %s)"},

	// Örnek bildirimlerdeki yer tutucu adlar
	"notify.sample.monitor": {"Örnek Site", "Example Site"},
	"notify.sample.target":  {"https://ornek.com", "https://example.com"},
	"notify.sample.server":  {"Örnek Sunucu", "Example Server"},
	"notify.sample.host":    {"sunucu01", "server01"},

	// Sunucu metriklerinin adları
	"metric.cpu":  {"CPU", "CPU"},
	"metric.mem":  {"RAM", "RAM"},
	"metric.swap": {"Swap", "Swap"},
	"metric.disk": {"Disk", "Disk"},
	"metric.load": {"Yük", "Load"},
	"metric.temp": {"Sıcaklık", "Temperature"},
	"metric.net":  {"Ağ", "Network"},

	// Süre birimleri (Duration)
	"dur.sec":  {"%d sn", "%ds"},
	"dur.min":  {"%d dk", "%dm"},
	"dur.hour": {"%d sa", "%dh"},
	"dur.day":  {"%d gün", "%dd"},

	// Rozetler (SVG)
	"badge.status":       {"durum", "status"},
	"badge.paused":       {"durduruldu", "paused"},
	"badge.up":           {"çalışıyor", "up"},
	"badge.down":         {"çalışmıyor", "down"},
	"badge.pending":      {"bekliyor", "pending"},
	"badge.maintenance":  {"bakımda", "maintenance"},
	"badge.unknown":      {"bilinmiyor", "unknown"},
	"badge.uptime":       {"uptime (%s)", "uptime (%s)"},
	"badge.no_data":      {"veri yok", "no data"},
	"badge.ping":         {"yanıt süresi (%s)", "response time (%s)"},
	"badge.cert":         {"sertifika", "certificate"},
	"badge.cert_none":    {"yok", "none"},
	"badge.cert_expired": {"süresi doldu", "expired"},
	"badge.days":         {"%d gün", "%d days"},
	"badge.days.one":     {"%d gün", "%d day"},
	"badge.window.24h":   {"24 saat", "24h"},
	"badge.window.7d":    {"7 gün", "7d"},
	"badge.window.30d":   {"30 gün", "30d"},
	"badge.window.90d":   {"90 gün", "90d"},
}
