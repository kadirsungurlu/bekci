package api

import (
	"fmt"
	"regexp"
	"strings"
)

// Kurulum komutu üreticileri: program bir kez indirilip SHA-256 ile doğrulanır
// ve kalıcı tutulur (sürüm sabit); yeniden başlatmada yeniden indirilmez.
//
// Token hiçbir sürecin argümanlarında (ps, /proc/<pid>/cmdline) görünmez:
// Linux komutları yapıştırılan metni "sh <<'UPTIME_KURULUM'" ile standart
// girdiden çalıştırır; token yalnızca iç içe bir heredoc ile 0600 izinli env
// dosyasına yazılır (cat'in argümanlarında değil, girdisinde). Docker bu
// dosyayı --env-file ile okur; systemd EnvironmentFile ile. Program indirmesi
// token istemez (GET /api/probe/binary herkese açık): kurulumun bütünlüğünü
// komuta gömülü SHA-256 sağlar, programın içinde sır yoktur. Özet yoksa
// (program sunucuda yoksa) doğrulamasız kurulum yapılmaz, komut açık bir
// hatayla durur.

// probeHardenFlags kontrol noktası konteynerinin blast radius'unu küçültür:
// ayrıcalıkları düşürür, yükseltmeyi engeller, bellek ve süreç sınırı koyar.
// CAP_NET_RAW ping kontrolü için gerekir.
const probeHardenFlags = "--cap-drop ALL --cap-add NET_RAW --security-opt no-new-privileges --memory 256m --pids-limit 200 "

// serverHardenFlags sunucu ajanı konteyneri: kontrol yapmadığı için NET_RAW
// gerekmez; host'u okumak için ayrıcalık gerekmediğinden tüm yetenekler düşer.
const serverHardenFlags = "--cap-drop ALL --security-opt no-new-privileges --memory 256m --pids-limit 200 "

// Host'taki env dosyaları (yalnızca root okuyabilir, 0600).
const (
	agentEnvPath = "/etc/uptime-agent.env" // sunucu ajanı (Docker ve systemd)
	probeEnvPath = "/etc/uptime-probe.env" // kontrol noktası (Docker)
)

// installServerRe kurulum komutuna gömülecek sunucu adresi: kabuk kaçışı
// olmaması için şema + güvenli karakterlerle sınırlı (tırnak, boşluk, $, ` yok).
var installServerRe = regexp.MustCompile(`^https?://[A-Za-z0-9._~:/\-]+$`)

// validInstallServer sunucu adresinin kurulum komutuna gömülmeye uygun olup
// olmadığını söyler (kabuk kaçışı olmasın).
func validInstallServer(server string) bool {
	return installServerRe.MatchString(server)
}

// installEnvLines ajanın env dosyası satırları; http adreste
// PROBE_ALLOW_INSECURE eklenir (ajan aksi halde http'yi reddeder).
func installEnvLines(server, token string, extra ...string) []string {
	lines := []string{"PROBE_SERVER=" + server, "PROBE_TOKEN=" + token}
	if strings.HasPrefix(server, "http://") {
		lines = append(lines, "PROBE_ALLOW_INSECURE=1")
	}
	return append(lines, extra...)
}

// rootScript yapıştırılacak Linux komutu: gövde "sh"ın standart girdisinden
// okunur (tırnaklı heredoc: dış kabuk hiçbir şeyi genişletmez), böylece
// içindeki token hiçbir sürecin argümanı olmaz. set -e ilk hatada durdurur;
// ayrı bir sh süreci olduğu için kullanıcının kabuğunu kapatmaz. sudo ile
// çalıştırmak için ilk satır "sudo sh <<'UPTIME_KURULUM'" yapılabilir.
func rootScript(body ...string) string {
	lines := []string{
		"sh <<'UPTIME_KURULUM'",
		"set -e",
		`if [ "$(id -u)" != 0 ]; then echo "Bu komut root olarak çalıştırılmalı (ilk satırı: sudo sh <<'UPTIME_KURULUM')" >&2; exit 1; fi`,
	}
	lines = append(lines, body...)
	return strings.Join(append(lines, "UPTIME_KURULUM"), "\n")
}

// envFileSteps env dosyasını token'ı hiçbir argümana koymadan yazan satırlar:
// dosya önce boş ve 0600 olarak (yeniden) oluşturulur, sonra içerik heredoc
// ile (cat'in girdisinden) yazılır; token hiçbir an başkalarına okunur değildir.
func envFileSteps(path string, lines []string) []string {
	out := []string{
		"install -m 600 /dev/null " + path,
		"cat > " + path + " <<'UPTIME_ENV'",
	}
	out = append(out, lines...)
	return append(out, "UPTIME_ENV")
}

// linuxArchCase uname -m'e göre A (Go mimarisi) ve S (o mimarinin programının
// SHA-256'sı) değişkenlerini atayan case ifadesi. Programı sunucuda olmayan
// mimari ve bilinmeyen mimari açık bir mesajla durur (fail komutuyla);
// özetsiz (doğrulamasız) kurulum yapılmaz. Metin tek tırnak içermez (Docker
// konteynerindeki sh -c '…' içine de girer).
func linuxArchCase(shaAMD64, shaARM64, fail string) string {
	branch := func(pattern, arch, sha string) string {
		if sha == "" {
			return fmt.Sprintf(`%s) echo "Ana sunucuda linux/%s ajan programı yok; programı AGENT_DIR klasörüne uptime-linux-%s adıyla koyun veya imajı AGENT_PLATFORMS=linux/%s ile derleyin" >&2; %s;;`,
				pattern, arch, arch, arch, fail)
		}
		return fmt.Sprintf("%s) A=%s; S=%s;;", pattern, arch, sha)
	}
	return `case "$(uname -m)" in ` + branch("x86_64|amd64", "amd64", shaAMD64) + " " + branch("aarch64|arm64", "arm64", shaARM64) +
		` *) echo "Desteklenmeyen mimari: $(uname -m) (x86_64 veya aarch64 gerekir)" >&2; ` + fail + ";; esac"
}

// linuxSHAs Linux amd64 ve arm64 ajan programlarının SHA-256'ları ("" = yok).
func (s *Server) linuxSHAs() (amd64, arm64 string) {
	return s.agentBinarySHA256("linux", "amd64"), s.agentBinarySHA256("linux", "arm64")
}

// dockerFetchScript alpine konteynerinin giriş betiği: program yoksa
// konteynerin mimarisine uygun olanı token'sız indirir, SHA-256 ile doğrular
// ve kalıcı yola yazar; sonra çalıştırır. Program varsa (yeniden başlatma)
// doğrudan çalıştırır — yeniden indirmez. Hata durumlarında bir saat beklenir:
// yeniden başlatma döngüsünde program her dakika tekrar indirilmesin. Özet
// uyuşmazsa (ör. komut eski, sunucu güncellendi) komut panelden yenilenmelidir.
func dockerFetchScript(binPath, shaAMD64, shaARM64 string) string {
	const fail = "sleep 3600; exit 1"
	return fmt.Sprintf(`sh -c 'set -e; B=%s; if [ ! -x "$B" ]; then `+
		`%s; `+
		`wget -qO "$B.dl" "$PROBE_SERVER/api/probe/binary?os=linux&arch=$A"; `+
		`if ! echo "$S  $B.dl" | sha256sum -c -; then rm -f "$B.dl"; `+
		`echo "Program özeti uyuşmuyor: kurulum komutunu panelden yenileyin" >&2; %s; fi; `+
		`chmod +x "$B.dl"; mv -f "$B.dl" "$B"; fi; exec "$B" probe'`, binPath, linuxArchCase(shaAMD64, shaARM64, fail), fail)
}

// dockerInstallCommand env dosyasını yazıp konteyneri --env-file ile başlatan
// komut. image boşsa program alpine içinde indirilip bir birimde tutulur.
func dockerInstallCommand(envPath string, envLines []string, runPrefix, image, volume, shaAMD64, shaARM64 string) string {
	run := runPrefix + "--env-file " + envPath + " "
	if image != "" {
		// Uygulama imajı bir kayıt deposundan çekilebiliyorsa doğrudan o kullanılır.
		run += image + " probe"
	} else {
		// Program bir kez indirilip kalıcı bir birime yazılır ve SHA-256 ile
		// doğrulanır; yeniden başlatmada tekrar indirilmez (sürüm sabit). Böylece
		// sunucu sonradan ele geçirilse bile filoya kendiliğinden yeni program inmez.
		// Güncelleme: bu komut yeni SHA ile tekrar çalıştırılır (önce birim silinir).
		run += "-v " + volume + ":/opt/uptime alpine:3 " + dockerFetchScript("/opt/uptime/uptime", shaAMD64, shaARM64)
	}
	body := envFileSteps(envPath, envLines)
	return rootScript(append(body, run)...)
}
