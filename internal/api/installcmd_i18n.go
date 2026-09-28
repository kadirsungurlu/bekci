package api

import (
	"regexp"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/brand"
)

// Kurulum komutlarındaki kullanıcıya dönük mesajlar Türkçe üretilir; İngilizce
// arayüzde localizeScript bunları çevirir. Komutun kendisi (yollar, bayraklar,
// SHA, token) değişmez; yalnızca echo/throw metinleri.

var installMsgsEN = []struct{ tr, en string }{
	{`Bu komut root olarak çalıştırılmalı (ilk satırı: sudo sh <<'UPTIME_KURULUM')`,
		`This command must be run as root (or change the first line to: sudo sh <<'UPTIME_KURULUM')`},
	{`Program özeti (SHA-256) uyuşmuyor: kurulum komutunu panelden yenileyin`,
		`Program checksum (SHA-256) mismatch: get a fresh install command from the panel`},
	{`Program özeti uyuşmuyor: kurulum komutunu panelden yenileyin`,
		`Program checksum mismatch: get a fresh install command from the panel`},
	{brand.Name + ` ajanı kuruldu ve başlatıldı (durum: systemctl status uptime-agent)`,
		brand.Name + ` agent installed and started (status: systemctl status uptime-agent)`},
	{`Ana sunucuda Windows (amd64) ajan programı yok; programı AGENT_DIR klasörüne uptime-windows-amd64.exe adıyla koyun (resmi Docker imajı içerir) ve komutu panelden yenileyin`,
		`The main server has no Windows (amd64) agent program; put it in the AGENT_DIR folder as uptime-windows-amd64.exe (the official Docker image includes it) and get a fresh command from the panel`},
	{`Program bütünlük doğrulaması başarısız (SHA-256 uyuşmuyor)`,
		`Program integrity check failed (SHA-256 mismatch)`},
	{`Kurulum tamamlanamadı (çıkış kodu $c)`, `Installation failed (exit code $c)`},
	{`Desteklenmeyen mimari: $(uname -m) (x86_64 veya aarch64 gerekir)`,
		`Unsupported architecture: $(uname -m) (x86_64 or aarch64 required)`},
}

var noLinuxAgentRe = regexp.MustCompile(`Ana sunucuda linux/([a-z0-9]+) ajan programı yok; programı AGENT_DIR klasörüne uptime-linux-([a-z0-9]+) adıyla koyun veya imajı AGENT_PLATFORMS=linux/([a-z0-9]+) ile derleyin`)

// localizeScript kurulum komutunun mesajlarını lang diline çevirir (tr: aynen).
func localizeScript(lang, script string) string {
	if lang != "en" || script == "" {
		return script
	}
	for _, m := range installMsgsEN {
		script = strings.ReplaceAll(script, m.tr, m.en)
	}
	return noLinuxAgentRe.ReplaceAllString(script,
		`The main server has no linux/$1 agent program; put it in the AGENT_DIR folder as uptime-linux-$2 or build the image with AGENT_PLATFORMS=linux/$3`)
}
