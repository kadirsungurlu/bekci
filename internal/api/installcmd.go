package api

import (
	"fmt"
	"regexp"
	"strings"
)

// Kurulum komutu üreticileri: program bir kez indirilip SHA-256 ile doğrulanır
// ve kalıcı tutulur (sürüm sabit); yeniden başlatmada yeniden indirilmez.

// probeHardenFlags kontrol noktası konteynerinin blast radius'unu küçültür:
// ayrıcalıkları düşürür, yükseltmeyi engeller, bellek ve süreç sınırı koyar.
// CAP_NET_RAW ping kontrolü için gerekir.
const probeHardenFlags = "--cap-drop ALL --cap-add NET_RAW --security-opt no-new-privileges --memory 256m --pids-limit 200 "

// serverHardenFlags sunucu ajanı konteyneri: kontrol yapmadığı için NET_RAW
// gerekmez; host'u okumak için ayrıcalık gerekmediğinden tüm yetenekler düşer.
const serverHardenFlags = "--cap-drop ALL --security-opt no-new-privileges --memory 256m --pids-limit 200 "

// installServerRe kurulum komutuna gömülecek sunucu adresi: kabuk kaçışı
// olmaması için şema + güvenli karakterlerle sınırlı (tırnak, boşluk, $, ` yok).
var installServerRe = regexp.MustCompile(`^https?://[A-Za-z0-9._~:/\-]+$`)

// installEnv docker için ortam bayrakları; http adreste PROBE_ALLOW_INSECURE
// eklenir (ajan aksi halde http'yi reddeder). server güvenli değilse boş bırakır.
func installEnv(server, token string) string {
	env := fmt.Sprintf("-e PROBE_SERVER=%s -e PROBE_TOKEN=%s", server, token)
	if strings.HasPrefix(server, "http://") {
		env += " -e PROBE_ALLOW_INSECURE=1"
	}
	return env + " "
}

// dockerFetchScript alpine konteynerinin giriş betiği: program yoksa indirir,
// SHA-256 ile doğrular ve kalıcı yola yazar; sonra çalıştırır. Program varsa
// (yeniden başlatma) doğrudan çalıştırır — yeniden indirmez.
func dockerFetchScript(binPath, sha string) string {
	verify := ""
	if sha != "" {
		// Özet uyuşmazsa (ör. komut eski, sunucu güncellendi) indirilen dosya
		// silinir ve bir saat beklenir: yeniden başlatma döngüsünde program her
		// dakika tekrar indirilmesin. Komut panelden yenilenmelidir.
		verify = fmt.Sprintf(`if ! echo "%s  $B.dl" | sha256sum -c -; then rm -f "$B.dl"; `+
			`echo "Program özeti uyuşmuyor: kurulum komutunu panelden yenileyin" >&2; sleep 3600; exit 1; fi; `, sha)
	}
	return fmt.Sprintf(`sh -c 'set -e; B=%s; if [ ! -x "$B" ]; then `+
		`wget -qO "$B.dl" --header "Authorization: Bearer $PROBE_TOKEN" "$PROBE_SERVER/api/probe/binary"; `+
		`%s`+
		`chmod +x "$B.dl"; mv -f "$B.dl" "$B"; fi; exec "$B" probe'`, binPath, verify)
}

// validInstallServer sunucu adresinin kurulum komutuna gömülmeye uygun olup
// olmadığını söyler (kabuk kaçışı olmasın).
func validInstallServer(server string) bool {
	return installServerRe.MatchString(server)
}
