#!/bin/sh
# Bekci eklentisi: disk sağlığı özeti (SMART).
# smartctl (smartmontools paketi) gerekir ve diskleri okumak için root ister
# (manifestte run_as root). Ağ gerekmez.
# Diskler `smartctl --scan` ile bulunur, her biri için `smartctl -H -A` okunur.
# Çıktı Bekci JSON şemasındadır:
#   {"status":"ok|warning|critical|unknown","message":"...","metrics":{"sda.temp_c":{"value":34,"unit":"C"}}}
# Durum: sağlık testi başarısız ya da sıcaklık >= 70 = kritik; yeniden atanmış /
# bekleyen sektör, NVMe ortam hatası, aşınma >= 90% ya da sıcaklık >= 60 = uyarı;
# smartctl yoksa, disk bulunamazsa ya da hiçbir disk okunamazsa = bilinmiyor.

LC_ALL=C
export LC_ALL
PATH=/usr/sbin:/usr/bin:/sbin:/bin:$PATH

if ! command -v smartctl >/dev/null 2>&1; then
	printf '{"status":"unknown","message":"smartctl bulunamadı (smartmontools kurulu değil)","metrics":{}}\n'
	exit 0
fi

scan=$(smartctl --scan 2>/dev/null | awk '$1 ~ /^\// && $2 == "-d" { print $1, $3 }')
if [ -z "$scan" ]; then
	printf '{"status":"unknown","message":"smartctl disk bulamadı","metrics":{}}\n'
	exit 0
fi

# Her disk için "#DISK ad aygıt tür" başlığı ve ardından smartctl çıktısı.
echo "$scan" | while read -r dev typ; do
	name=${dev##*/}
	case $typ in
	*,*) t=${typ%%,*}; name="${t##*+}${typ##*,}" ;;
	esac
	echo "#DISK $name $dev $typ"
	smartctl -H -A -d "$typ" "$dev" 2>&1
	echo "#RC $?"
done | awk '
function num(s) { gsub(/[^0-9.]/, "", s); return s + 0 }
function clean(s) { gsub(/[^a-zA-Z0-9_.-]/, "_", s); return substr(s, 1, 40) }
function add(k, v, u) {
	if (nm >= 50) return
	nm++
	metrics = metrics (metrics == "" ? "" : ",") "\"" k "\":{\"value\":" v ",\"unit\":\"" u "\"}"
}
function lvl(l) { if (l > worst) worst = l }
function note(s) { notes = notes (notes == "" ? "" : ", ") s }
function finish() {
	if (d == "") return
	# Sağlık ve sıcaklık yoksa aygıt açılamadı (root değil ya da desteklenmiyor).
	if (health == "" && temp == "") { note(d " okunamadı"); unread++; d = ""; return }
	readok++
	if (health == "FAILED") { lvl(2); note(d " sağlık testi BAŞARISIZ") }
	if (health != "") add(d ".health", (health == "FAILED" ? 0 : 1), "")
	if (temp != "") {
		add(d ".temp_c", temp, "C")
		if (temp >= 70) { lvl(2); note(d " sıcaklık " temp " C") }
		else if (temp >= 60) { lvl(1); note(d " sıcaklık " temp " C") }
	}
	if (realloc != "") { add(d ".reallocated", realloc, ""); if (realloc > 0) { lvl(1); note(d " yeniden atanmış sektör " realloc) } }
	if (pending != "") { add(d ".pending", pending, ""); if (pending > 0) { lvl(1); note(d " bekleyen sektör " pending) } }
	if (used != "") {
		add(d ".used_pct", used, "%")
		if (used >= 90) { lvl(1); note(d " aşınma %" used) }
	}
	if (media != "") { add(d ".media_errors", media, ""); if (media > 0) { lvl(1); note(d " ortam hatası " media) } }
	d = ""
}
/^#DISK / { finish(); d = clean($2); health = ""; temp = ""; realloc = ""; pending = ""; used = ""; media = ""; next }
/^#RC / { next }
d == "" { next }
/overall-health self-assessment test result:/ { health = ($NF == "PASSED") ? "PASSED" : "FAILED"; next }
/^SMART Health Status:/ { health = ($4 == "OK") ? "PASSED" : "FAILED"; next }
# ATA öznitelik tablosu: ID# AD BAYRAK DEĞER EN_KÖTÜ EŞİK TÜR GÜNCELLEME HATA_ZAMANI HAM_DEĞER
$1 ~ /^[0-9]+$/ && NF >= 10 {
	if ($2 == "Reallocated_Sector_Ct") realloc = num($10)
	else if ($2 == "Current_Pending_Sector") pending = num($10)
	else if ($2 == "Temperature_Celsius" || ($2 == "Airflow_Temperature_Cel" && temp == "")) temp = num($10)
	next
}
/^Current Drive Temperature:/ { temp = num($4); next }
/^Elements in grown defect list:/ { realloc = num($NF); next }
/^Temperature:/ && /Celsius/ { temp = num($2); next }
/^Percentage Used:/ { used = num($3); next }
/^Media and Data Integrity Errors:/ { media = num($NF); next }
END {
	finish()
	st[0] = "ok"; st[1] = "warning"; st[2] = "critical"
	if (readok == 0) { status = "unknown"; msg = "hiçbir disk okunamadı" (notes != "" ? ": " notes : "") }
	else {
		status = st[worst + 0]
		msg = readok " disk okundu" (unread > 0 ? ", " unread " okunamadı" : "")
		if (notes != "") msg = msg "; " notes
		else msg = msg ", sorun yok"
	}
	gsub(/["\\]/, "", msg)
	if (length(msg) > 500) msg = substr(msg, 1, 497) "..."
	printf "{\"status\":\"%s\",\"message\":\"%s\",\"metrics\":{%s}}\n", status, msg, metrics
}
'
