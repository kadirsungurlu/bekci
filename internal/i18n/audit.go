package i18n

import "strings"

// AuditDetail işlem kaydının ayrıntı metnini istenen dile çevirir.
//
// Ayrıntılar veritabanında Türkçe saklanır ("25 monitör, 2 bildirim",
// "konumlar: Ana sunucu, İstanbul; kural: all", "rol: editor, devre dışı").
// Metin "; " ve ", " ile birleşmiş parçalardan oluşur; her parça auditEN
// kataloğunda ayrı ayrı aranır, kullanıcı verisi (adlar) olduğu gibi kalır.
// Eski kayıtlarda ham saklanan kesinti kuralı ("kural: all") her iki dilde
// okunur metne çevrilir.
func AuditDetail(lang, detail string) string {
	if detail == "" {
		return detail
	}
	en := Or(lang) == EN
	groups := strings.Split(detail, "; ")
	for gi, g := range groups {
		parts := strings.Split(g, ", ")
		for i, p := range parts {
			parts[i] = auditPart(en, p)
		}
		groups[gi] = strings.Join(parts, ", ")
	}
	return strings.Join(groups, "; ")
}

// DownWhenLabel kesinti kuralının (store.DownWhen*) Türkçe adı.
func DownWhenLabel(rule string) string {
	switch rule {
	case "any":
		return "herhangi bir konum çalışmıyorsa"
	case "majority":
		return "konumların çoğu çalışmıyorsa"
	case "all":
		return "tüm konumlar çalışmıyorsa"
	}
	return rule
}

// roleLabels rol kodlarının adları (arayüzdeki ROLE_LABELS ile aynı).
var roleLabels = map[string][2]string{
	"admin":  {"Yönetici", "Admin"},
	"editor": {"Editör", "Editor"},
	"viewer": {"İzleyici", "Viewer"},
}

func auditPart(en bool, p string) string {
	if rule, ok := strings.CutPrefix(p, "kural: "); ok {
		p = "kural: " + DownWhenLabel(rule)
	}
	if role, ok := strings.CutPrefix(p, "rol: "); ok {
		if l, ok := roleLabels[role]; ok {
			if en {
				return "role: " + l[1]
			}
			return "rol: " + l[0]
		}
	}
	if !en {
		return p
	}
	if out, ok := translateAudit(p, 0); ok {
		return out
	}
	return p
}

// translateAudit kalıpta yakalanan parçaları çevirir ("%s (devre dışı)" içindeki
// "cpu %80/5 dk" gibi iç içe parçalar dahil).
func translateAudit(s string, depth int) (string, bool) {
	if out, ok := auditTable.lookup(s); ok {
		return out, true
	}
	if depth > 2 {
		return "", false
	}
	return auditTable.match(s, depth, nil, translateAudit)
}

var auditTable = &table{src: auditEN}

// auditEN işlem kaydı ayrıntılarının İngilizce karşılıkları (biçim: Error ile aynı).
var auditEN = map[string]string{
	// Yedek (dışa/içe aktarma).
	"%d monitör":                           "%d monitors",
	"%d bildirim":                          "%d notifications",
	"%d etiket":                            "%d tags",
	"%d durum sayfası":                     "%d status pages",
	"%d kanal":                             "%d channels",
	"%d örnek":                             "%d samples",
	"atlanan: %d":                          "skipped: %d",
	"kaynak=%s mod=%s eklenen: %d monitör": "source=%s mode=%s added: %d monitors",
	// Kullanıcılar, API anahtarları, giriş.
	"rol: %s":           "role: %s",
	"devre dışı":        "disabled",
	"sahibi: %s":        "owner: %s",
	"2FA kurtarma kodu": "2FA recovery code",
	// Kontrol noktaları ve sunucular.
	"etkinleştirildi":          "enabled",
	"devre dışı bırakıldı":     "disabled",
	"metrik toplama açıldı":    "metrics collection turned on",
	"metrik toplama kapatıldı": "metrics collection turned off",
	"IP kilidi açıldı":         "IP lock turned on",
	"IP kilidi kapatıldı":      "IP lock turned off",
	"IP kilidi sıfırlandı":     "IP lock reset",
	"ad: %s → %s":              "name: %s → %s",
	"%d monitörden çıkarıldı":  "removed from %d monitors",
	"kanal yok":                "no channels",
	"kanallar: %s":             "channels: %s",
	"kural yok":                "no rules",
	"%s/%d dk":                 "%s/%d min",
	"%d dk":                    "%d min",
	"%s %d dk":                 "%s %d min",
	"%s (devre dışı)":          "%s (disabled)",
	"%s (kapalı)":              "%s (disabled)",
	// Monitörler.
	"konumlar: %s":                    "locations: %s",
	"kural: %s":                       "rule: %s",
	"herhangi bir konum çalışmıyorsa": "any location down",
	"konumların çoğu çalışmıyorsa":    "most locations down",
	"tüm konumlar çalışmıyorsa":       "all locations down",
	"kaynak: %s":                      "source: %s",
	"toplu işlem":                     "bulk action",
	"etiket eklendi: %s":              "tag added: %s",
	"etiket kuralı: %s":               "tag rule: %s",
	"etiket kaldırıldı: %s":           "tag removed: %s",
	"kanal eklendi: %s":               "channel added: %s",
	"kanal çıkarıldı: %s":             "channel removed: %s",
	"monitör eklendi: %s":             "monitor added: %s",
	"eski ad: %s":                     "previous name: %s",
	// Değişen alanlar (monitör ve ayar güncellemeleri) ve API anahtarı.
	"değişen: %s":           "changed: %s",
	"değişiklik yok":        "no changes",
	"API anahtarı: %s":      "API key: %s",
	"ad":                    "name",
	"tip":                   "type",
	"açıklama":              "description",
	"kontrol aralığı":       "check interval",
	"tekrar deneme aralığı": "retry interval",
	"alan adı uyarısı":      "domain expiry alert",
	"tekrar deneme sayısı":  "retries",
	"zaman aşımı":           "timeout",
	"hatırlatma sıklığı":    "reminder frequency",
	"ters mod":              "upside-down mode",
	"ayarlar":               "settings",
	"bildirim kanalları":    "notification channels",
	"ham kayıt saklama":     "raw record retention",
	"saatlik özet saklama":  "hourly summary retention",
	"SSL eşikleri":          "SSL thresholds",
	"yedek sayısı":          "backups kept",
	"bildirim dili":         "notification language",
	// Durum sayfaları.
	"alan adı: %s":       "domain: %s",
	"yayında değil":      "not published",
	"şifreli":            "password protected",
	"şifre kaldırıldı":   "password removed",
	"şifre değiştirildi": "password changed",
	"sayfa: %s":          "page: %s",
}
