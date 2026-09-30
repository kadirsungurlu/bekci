package i18n

import "testing"

// QA bulgusu: işlem kaydı ayrıntıları İngilizce arayüzde Türkçe, konum
// kuralı ham kod ("kural: all") olarak görünüyordu.
func TestAuditDetail(t *testing.T) {
	cases := []struct{ in, tr, en string }{
		{"25 monitör, 2 bildirim, 6 etiket, 1 durum sayfası", "25 monitör, 2 bildirim, 6 etiket, 1 durum sayfası",
			"25 monitors, 2 notifications, 6 tags, 1 status pages"},
		{"konumlar: Ana sunucu, İstanbul; kural: all", "konumlar: Ana sunucu, İstanbul; kural: tüm konumlar çalışmıyorsa",
			"locations: Ana sunucu, İstanbul; rule: all locations down"},
		{"konumlar: A; kural: herhangi bir konum çalışmıyorsa", "konumlar: A; kural: herhangi bir konum çalışmıyorsa",
			"locations: A; rule: any location down"},
		{"rol: editor, devre dışı", "rol: Editör, devre dışı", "role: Editor, disabled"},
		{"cpu %80/5 dk, disk / %90/10 dk (devre dışı), offline 5 dk", "cpu %80/5 dk, disk / %90/10 dk (devre dışı), offline 5 dk",
			"cpu %80/5 min, disk / %90/10 min (disabled), offline 5 min"},
		{"/durum/kadir, alan adı: durum.ornek.com, şifreli, şifre değiştirildi", "",
			"/durum/kadir, domain: durum.ornek.com, password protected, password changed"},
		{"etiket eklendi: Kritik", "", "tag added: Kritik"},
		{"http", "http", "http"},
		{"", "", ""},
	}
	for _, c := range cases {
		wantTR := c.tr
		if wantTR == "" {
			wantTR = c.in
		}
		if got := AuditDetail(TR, c.in); got != wantTR {
			t.Errorf("AuditDetail(tr, %q) = %q, %q bekleniyordu", c.in, got, wantTR)
		}
		if got := AuditDetail(EN, c.in); got != c.en {
			t.Errorf("AuditDetail(en, %q) = %q, %q bekleniyordu", c.in, got, c.en)
		}
	}
}
