#!/bin/sh
# Bekci eklentisi: Linux yazılım RAID (md) durumu.
# /proc/mdstat okunur; root gerekmez, ağ gerekmez.
# Çıktı Nagios biçimindedir: dönüş kodu 0 ok, 1 uyarı, 2 kritik, 3 bilinmiyor.
#   Bozuk dizi (eksik ya da arızalı disk, etkin olmayan dizi) = kritik.
#   Eşitleme / yeniden kurma / yeniden şekillendirme sürüyorsa = uyarı.
#   Hiç dizi yoksa ya da dosya okunamıyorsa = bilinmiyor.
# Metrikler (dizi başına): <ad>.active, <ad>.total, <ad>.sync_pct; toplam: degraded.
# Deneme için yol MDSTAT ortam değişkeniyle değiştirilebilir.

MDSTAT=${MDSTAT:-/proc/mdstat}

if [ ! -r "$MDSTAT" ]; then
	echo "RAID UNKNOWN: $MDSTAT okunamadı"
	exit 3
fi

awk '
function flush() {
	if (name == "") return
	n++
	nm[n] = name; lv[n] = level; act[n] = a; tot[n] = t; st[n] = state
	op[n] = oper; pct[n] = p; fl[n] = failed
	name = ""
}
/^md[0-9a-zA-Z_]* *:/ {
	flush()
	name = $1; state = $3; level = ""; a = -1; t = -1; oper = ""; p = -1; failed = 0
	for (i = 4; i <= NF; i++) {
		if ($i ~ /^\(/) continue
		if (level == "" && $i !~ /\[/) { level = $i; continue }
		if ($i ~ /\(F\)/) failed++
	}
	if (state == "inactive") level = ""
	next
}
name != "" && /blocks/ {
	if (match($0, /\[[0-9]+\/[0-9]+\]/)) {
		s = substr($0, RSTART + 1, RLENGTH - 2)
		split(s, x, "/"); t = x[1] + 0; a = x[2] + 0
	}
	next
}
name != "" && /resync=(DELAYED|PENDING)/ { oper = "resync"; p = 0; next }
name != "" && /(resync|recovery|reshape|check) *=/ {
	if (match($0, /(resync|recovery|reshape|check) *= *[0-9.]+%/)) {
		s = substr($0, RSTART, RLENGTH)
		oper = s; sub(/ *=.*/, "", oper)
		sub(/.*= */, "", s); sub(/%/, "", s); p = s + 0
	}
	next
}
END {
	flush()
	if (n == 0) { print "RAID UNKNOWN: md dizisi yok"; exit 3 }
	code = 0; bad = 0; msg = ""; perf = ""
	for (i = 1; i <= n; i++) {
		d = (st[i] != "active") || fl[i] > 0 || (tot[i] > 0 && act[i] < tot[i])
		w = (op[i] == "resync" || op[i] == "recovery" || op[i] == "reshape")
		if (d) { bad++; code = 2 } else if (w && code < 1) code = 1
		e = nm[i]
		if (lv[i] != "") e = e " " lv[i]
		if (st[i] != "active") e = e " " st[i]
		if (tot[i] > 0) e = e " " act[i] "/" tot[i]
		if (fl[i] > 0) e = e " arızalı " fl[i]
		if (d) e = e " BOZUK"
		if (op[i] != "") e = e " " op[i] " %" pct[i]
		msg = msg (i > 1 ? ", " : "") e
		if (act[i] >= 0) perf = perf " " nm[i] ".active=" act[i] " " nm[i] ".total=" tot[i]
		if (st[i] == "active") perf = perf " " nm[i] ".sync_pct=" (pct[i] >= 0 ? pct[i] : 100) "%;;;0;100"
	}
	lbl[0] = "OK"; lbl[1] = "WARNING"; lbl[2] = "CRITICAL"
	head = "RAID " lbl[code] ": " n " dizi, " bad " bozuk; "
	text = head msg
	if (length(text) > 480) text = substr(text, 1, 477) "..."
	print text " | degraded=" bad ";;0;0" perf
	exit code
}
' "$MDSTAT"
