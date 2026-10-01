package i18n

// checkEN kontrol sonucu, olay geçmişi ve sunucu ajanı metinlerinin
// İngilizce karşılıkları (bkz. Message). Anahtar, üreten Go kodundaki Türkçe
// metnin (veya fmt biçim dizgesinin) BİREBİR aynısıdır; kod değişirse burası
// da güncellenmelidir (TestProducerCoverage kaynak kodu tarar ve eksik
// girdiyi yakalar; eşleşmeyen mesaj Türkçe gösterilir, hata olmaz).
//
// Kalıplar: %s / %d / %q / %.0f değişken parçadır; "%%" düz % işaretidir.
// Yakalanan %s/%q parçaları da çevrilmeye çalışılır (iç içe mesajlar:
// "Proxy hatası: Zaman aşımı" → "Proxy error: Timeout").
//
// Terimler: çalışıyor/çalışmıyor → up/down, bakım → maintenance, kontrol
// noktası/konum → location, alt monitör → child monitor.
var checkEN = map[string]string{
	// ---------------------------------------------------------------------
	// Ağ hataları (check.describeErr) — tüm tiplerde ortak
	// ---------------------------------------------------------------------
	"Zaman aşımı":                     "Timeout",
	"zaman aşımı":                     "timeout",
	"Alan adı bulunamadı: %s":         "Domain not found: %s",
	"DNS hatası: %s":                  "DNS error: %s",
	"Bağlantı reddedildi":             "Connection refused",
	"Hedefe ulaşılamıyor":             "Target unreachable",
	"Bağlantı karşı taraftan kesildi": "Connection reset by peer",
	"SSL sertifika hatası: %s":        "SSL certificate error: %s",
	"Ayar okunamadı: %s":              "Could not read settings: %s",
	"İstek oluşturulamadı: %s":        "Could not create request: %s",
	"Bağlantı ayarı geçersiz: %s":     "Invalid connection settings: %s",
	"Bağlanılamadı: %s":               "Could not connect: %s",
	"İç hata":                         "Internal error",

	// ---------------------------------------------------------------------
	// HTTP (http.go, http_extras.go, detail.go)
	// ---------------------------------------------------------------------
	"çok fazla yönlendirme":                                 "too many redirects",
	"Çok fazla yönlendirme (en fazla %d)":                   "Too many redirects (max %d)",
	"OAuth2 token alınamadı: %s":                            "Could not get OAuth2 token: %s",
	"yanıtta access_token yok":                              "no access_token in the response",
	"Yanıt gövdesi okunamadı: %s":                           "Could not read response body: %s",
	"Kelime bulundu (olmaması gerekiyordu): %q":             "Keyword found (it should be absent): %q",
	"Kelime bulunamadı: %q":                                 "Keyword not found: %q",
	"Yanıt geçerli bir JSON değil":                          "Response is not valid JSON",
	"JSON alanı yok: %s":                                    "JSON field missing: %s",
	"JSON: %s beklenen değerle eşleşmedi (beklenen: %s %s)": "JSON: %s did not match the expected value (expected: %s %s)",
	"JSON: %s sayısal değil":                                "JSON: %s is not numeric",
	"Kök sertifika (CA) okunamadı":                          "Could not read the root certificate (CA)",
	"İstemci sertifikası yüklenemedi: %s":                   "Could not load the client certificate: %s",
	"Proxy adresi okunamadı":                                "Could not parse the proxy URL",
	"Proxy hatası: %s":                                      "Proxy error: %s",
	"(ikili içerik, %d bayt)":                               "(binary content, %d bytes)",
	"(ikili içerik, %d bayttan büyük)":                      "(binary content, larger than %d bytes)",

	// ---------------------------------------------------------------------
	// DNS
	// ---------------------------------------------------------------------
	"DNS yanıtı: %s":                     "DNS response: %s",
	"%s kaydı bulunamadı":                "No %s record found",
	"Beklenen değer yok (%s); gelen: %s": "Expected value not found (%s); got: %s",

	// ---------------------------------------------------------------------
	// Docker
	// ---------------------------------------------------------------------
	"Endpoint geçersiz: %s": "Invalid endpoint: %s",
	"soket yolu boş":        "socket path is empty",
	"bilinmeyen endpoint biçimi (unix://, tcp:// veya http(s):// olmalı)": "unknown endpoint format (must be unix://, tcp:// or http(s)://)",
	"Konteyner bulunamadı: %s":                  "Container not found: %s",
	"Docker API hatası: HTTP %d":                "Docker API error: HTTP %d",
	"Docker API yanıtı okunamadı: %s":           "Could not read the Docker API response: %s",
	"Konteyner çalışmıyor (durum: %s)":          "Container is not running (state: %s)",
	"Konteyner çalışıyor ama sağlık durumu: %s": "Container is running but its health status is: %s",
	"Çalışıyor (sağlıklı)":                      "Running (healthy)",
	// Konteyner durumları (check.dockerStatusTR)
	"oluşturuldu":          "created",
	"çalışıyor":            "running",
	"duraklatıldı":         "paused",
	"yeniden başlatılıyor": "restarting",
	"kaldırılıyor":         "removing",
	"sona erdi":            "exited",
	"öldü":                 "dead",

	// ---------------------------------------------------------------------
	// Grup
	// ---------------------------------------------------------------------
	"%d monitör":                        "%d monitors",
	"1 monitör":                         "1 monitor",
	"Grup durumu okunamadı":             "Could not read the group status",
	"Alt monitörlerin durumu okunamadı": "Could not read the child monitor statuses",
	"%d/%d alt monitör çalışmıyor: %s":  "%d/%d child monitors down: %s",
	"%d alt monitör bekliyor: %s":       "%d child monitors pending: %s",
	"1 alt monitör bekliyor: %s":        "1 child monitor pending: %s",
	"Değerlendirilecek alt monitör yok": "No child monitors to evaluate",
	"Tüm alt monitörler çalışıyor (%d)": "All child monitors up (%d)",
	// Grup mesajının sonundaki ek: " (2 bakımda, 1 durdurulmuş)"
	"%s (%d bakımda)":                 "%s (%d in maintenance)",
	"%s (%d durdurulmuş)":             "%s (%d paused)",
	"%s (%d bakımda, %d durdurulmuş)": "%s (%d in maintenance, %d paused)",
	"%d bakımda":                      "%d in maintenance",
	"%d durdurulmuş":                  "%d paused",
	"%s ve %d diğer":                  "%s and %d more",
	// İzleyiciye giden ad içermeyen grup mesajları (api.groupMessageFor)
	"Tüm alt monitörler çalışıyor":            "All child monitors are up",
	"Alt monitörlerden en az biri çalışmıyor": "At least one child monitor is down",
	"Alt monitörler kontrol ediliyor":         "Checking child monitors",

	// ---------------------------------------------------------------------
	// gRPC
	// ---------------------------------------------------------------------
	"Bağlantı oluşturulamadı: %s":                                    "Could not create the connection: %s",
	"Sunucuya bağlanılamadı":                                         "Could not connect to the server",
	"Servis bulunamadı (health check tanımlı değil)":                 "Service not found (no health check defined)",
	"Sunucu sağlık kontrolünü desteklemiyor (grpc.health.v1.Health)": "Server does not support health checks (grpc.health.v1.Health)",
	"Yetkilendirme hatası: %s":                                       "Authorization error: %s",
	"gRPC hatası (%s): %s":                                           "gRPC error (%s): %s",
	"Servis durumu: %s":                                              "Service status: %s",

	// ---------------------------------------------------------------------
	// MongoDB, MySQL, PostgreSQL, MSSQL (sql_common.go)
	// ---------------------------------------------------------------------
	"Ping başarılı":                         "Ping successful",
	"Sorgu çalıştırılamadı: %s":             "Could not run the query: %s",
	"Sorgu sonucu okunamadı: %s":            "Could not read the query result: %s",
	"Sorgu sonuç döndürmedi (beklenen: %q)": "Query returned no rows (expected: %q)",
	"Beklenmeyen sonuç: %q (beklenen: %q)":  "Unexpected result: %q (expected: %q)",
	"Sorgu başarılı (%d satır)":             "Query successful (%d rows)",
	"Sorgu başarılı (1 satır)":              "Query successful (1 row)",
	"Giriş başarısız: kullanıcı adı veya şifre yanlış ya da SQL Server kimlik doğrulaması kapalı (%d)": "Login failed: wrong username or password, or SQL Server authentication is disabled (%d)",
	"Veritabanı açılamadı: veritabanı yok ya da kullanıcının erişim izni yok (%d)":                     "Could not open the database: it does not exist or the user has no access (%d)",

	// ---------------------------------------------------------------------
	// MQTT
	// ---------------------------------------------------------------------
	"Broker'a bağlanıldı":                      "Connected to the broker",
	"Abonelik hatası: %s":                      "Subscription error: %s",
	"Abonelik zaman aşımına uğradı":            "Subscription timed out",
	"Mesaj alındı: %s":                         "Message received: %s",
	"%q konusunda mesaj gelmedi (zaman aşımı)": "No message on topic %q (timeout)",

	// ---------------------------------------------------------------------
	// Ping, TCP, TLS sertifikası, WebSocket, Push
	// ---------------------------------------------------------------------
	"Ping gönderilemedi: %s":                 "Could not send ping: %s",
	"Yanıt yok (%d paketin hiçbiri dönmedi)": "No reply (none of the %d packets came back)",
	"%d/%d paket":                            "%d/%d packets",
	"%d/%d paket, %%%.0f kayıp":              "%d/%d packets, %.0f%% loss",
	"Port açık":                              "Port open",
	"Sunucu sertifika sunmadı":               "Server presented no certificate",
	"Sertifika geçerli, bitiş: %s":           "Certificate valid, expires: %s",
	"Mesaj gönderilemedi: %s":                "Could not send the message: %s",
	"Yanıt okunamadı: %s":                    "Could not read the response: %s",
	"Bağlantı kuruldu":                       "Connection established",
	"Belirtilen sürede push sinyali gelmedi": "No push signal received within the expected time",
	"Push alındı":                            "Push received",
	"Push: hata bildirildi":                  "Push: failure reported",

	// ---------------------------------------------------------------------
	// Redis
	// ---------------------------------------------------------------------
	"Anahtar bulunamadı: %s":               "Key not found: %s",
	"Anahtar bulundu: %s":                  "Key found: %s",
	"Beklenmeyen değer: %q (beklenen: %q)": "Unexpected value: %q (expected: %q)",

	// ---------------------------------------------------------------------
	// SMTP
	// ---------------------------------------------------------------------
	"Banner okunamadı: %s":                "Could not read the banner: %s",
	"Banner beklenen metni içermiyor: %s": "Banner does not contain the expected text: %s",
	"EHLO başarısız: %s":                  "EHLO failed: %s",
	"STARTTLS gönderilemedi: %s":          "Could not send STARTTLS: %s",
	"STARTTLS reddedildi: %s":             "STARTTLS rejected: %s",
	"SSL handshake hatası: %s":            "SSL handshake error: %s",
	"STARTTLS sonrası EHLO başarısız: %s": "EHLO after STARTTLS failed: %s",
	"Sunucu yanıt verdi":                  "Server responded",

	// ---------------------------------------------------------------------
	// SNMP
	// ---------------------------------------------------------------------
	"Yanıtta değer yok":                "No value in the response",
	"OID bulunamadı: %s":               "OID not found: %s",
	"OID değeri: %s":                   "OID value: %s",
	"OID değeri: %s (beklenen: %s %s)": "OID value: %s (expected: %s %s)",
	"OID değeri sayısal değil: %s":     "OID value is not numeric: %s",

	// ---------------------------------------------------------------------
	// Motor (engine): bakım, ters mod, çok konumlu kontrol, olay geçmişi
	// ---------------------------------------------------------------------
	"Bakımda":                              "In maintenance",
	"Bakımda (%s)":                         "In maintenance (%s)",
	"Ters mod: hedef erişilebilir (%s)":    "Upside down mode: target is reachable (%s)",
	"Ters mod: hedef erişilemiyor (%s)":    "Upside down mode: target is unreachable (%s)",
	"Ters mod: hedef erişilemiyor":         "Upside down mode: target is unreachable",
	"Ana sunucu":                           "Main server",
	"Kontrol noktalarına ulaşılamıyor":     "Check locations unreachable",
	"%s (ulaşılamayan: %s)":                "%s (unreachable: %s)",
	"Kontrol noktasına ulaşılamıyor":       "Check location unreachable",
	"Kontrol noktalarından sonuç gelmiyor": "No results from check locations",
	"%s (sonuç gelmeyen: %s)":              "%s (no results from: %s)",
	"%d/%d konum çalışıyor — %s":           "%d/%d locations up — %s",
	"Sonuç gelmiyor":                       "No results",
	"Çalışıyor":                            "Up",
	"Çalışmıyor":                           "Down",
	"Çalışmıyor: %s":                       "Down: %s",
	"Bakım penceresi başladı; kontroller sürüyor, bildirim gönderilmiyor": "Maintenance window started; checks continue, no notifications are sent",
	"Bakım penceresi bitti": "Maintenance window ended",
	"Hatırlatma bildirimi":  "Reminder notification",
	// Kısmi kesinti (engine/partial.go)
	"Tüm konumlar çalışıyor":                     "All locations are up",
	"Tam kesintiye dönüştü":                      "Escalated to a full outage",
	"Kısmi kesintiden dönüştü":                   "Escalated from a location outage", // eski kayıtlar
	"Konum kesintisinden dönüştü":                "Escalated from a location outage",
	"Tam kesinti bitti; konum kesintisi sürüyor": "Full outage over; location outage continues",
	"Konum ayarı kaldırıldı; olay kapatıldı":     "Location settings removed; incident closed",
	// Sunucu olayları (store/incident_kinds.go)
	"Sunucuya ulaşılamıyor":   "Server unreachable",
	"Sunucudan veri gelmiyor": "No data from the server",
	"Uyarı kuralı kaldırıldı veya kapatıldı; olay kapatıldı":                    "Alert rule removed or disabled; incident closed",
	"Sunucu devre dışı bırakıldı veya metrik toplama kapatıldı; olay kapatıldı": "Server disabled or metric collection turned off; incident closed",
	// store.AddIncidentEvents
	"Kayıt sınırına ulaşıldı; bu olayın sonraki ayrıntıları kaydedilmiyor": "Record limit reached; further details of this incident are not recorded",
	// notify.Dispatcher (bildirim gönderim kaydı)
	"Bildirim gönderildi":             "Notification sent",
	"Bildirim gönderilemedi":          "Notification could not be sent",
	"Bağlı etkin bildirim kanalı yok": "No active notification channel attached",
	// api.incidentNote (kullanıcı işlemi)
	"Monitör ayarları değiştirildi":                 "Monitor settings changed",
	"Monitör durduruldu; olay kapatıldı":            "Monitor paused; incident closed",
	"Monitörün hedefi değiştirildi; olay kapatıldı": "Monitor target changed; incident closed",

	// ---------------------------------------------------------------------
	// Sunucu ajanı: metrik toplanamama nedeni (metrics paketi, ajanda üretilir)
	// ---------------------------------------------------------------------
	"Ajan konteyner içinde çalışıyor ama sunucunun /proc ve /sys dizinleri bağlanmamış; kurulum komutunu güncelleyin":        "The agent runs in a container but the server's /proc and /sys directories are not mounted; update the install command",
	"HOST_PROC=%s altında /proc bulunamadı; sunucunun kök dizini bağlanmamış (-v /:/host:ro), kurulum komutunu kontrol edin": "No /proc found under HOST_PROC=%s; the server's root directory is not mounted (-v /:/host:ro), check the install command",
	"Sunucu metrikleri şimdilik yalnızca Linux ve Windows'ta toplanabiliyor":                                                 "Server metrics can currently only be collected on Linux and Windows",
	"HOST_ROOT verilmemiş, diskler okunamıyor":                                                                               "HOST_ROOT is not set, disks cannot be read",
	"cpu süreleri boş":      "cpu times are empty",
	"Docker API yanıtı: %d": "Docker API response: %d",
}
