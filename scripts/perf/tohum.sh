#!/bin/bash
# Sentetik veri (iş 620). Kullanım: tohum.sh <monitör> <tam_yoğunluk_gün> <toplam_gün>
# Tam yoğunluk: 5 konum x 30 sn; daha eski günler saatte 1 sonuç (saatlik özet
# satır sayısı aynı kalır, yalnız ham sayısı düşer). Gün gün yazar, özeti yeniler,
# bir önceki günü sıkıştırır: disk şişmez. Her adım süresi log'a düşer.
set -euo pipefail
N=$1; FULL=$2; DAYS=$3; DB=${DB:-s$N}; C=${C:-bekci-620-perf}
P() { docker exec -i -e PGOPTIONS='-c session_replication_role=replica -c synchronous_commit=off' "$C" psql -U postgres -d "$DB" -qAtX -v ON_ERROR_STOP=1 "$@"; }
now=$(date +%s); today=$((now - now % 86400))
ts() { date +%H:%M:%S; }
ins() { # gün başı, bitiş, parça k
  local d=$1 e=$2 k=$3
  P -c "INSERT INTO heartbeats (monitor_id, time, status, ping_ms, message, location)
    SELECT m, t + m % 30 + l, CASE WHEN r < 0.0005 THEN 0 ELSE 1 END,
      CASE WHEN r < 0.0005 THEN NULL ELSE 15 + (m * 37) % 180 + l * 15 + (random() * random() * 120)::int END,
      CASE WHEN r < 0.0003 THEN 'dial tcp: i/o timeout' WHEN r < 0.0005 THEN 'HTTP 503' WHEN m % 33 = 0 THEN '301 - Moved Permanently' ELSE '200 - OK' END,
      'loc-' || l
    FROM generate_series(1, $N) m CROSS JOIN generate_series(0, 4) l CROSS JOIN generate_series($d, $e, 30) t
    CROSS JOIN LATERAL (SELECT random() + m * 0 + t * 0 + l * 0 AS r) x
    WHERE m % 4 = $k"
}
sparse() {
  local d=$1
  P -c "INSERT INTO heartbeats (monitor_id, time, status, ping_ms, message, location)
    SELECT m, t + m % 3600, 1, 15 + (m * 37) % 180 + (random() * 60)::int, '200 - OK', 'loc-0'
    FROM generate_series(1, $N) m CROSS JOIN generate_series($d, $((d + 86399)), 3600) t"
}
ins_s=0; ref_s=0; cmp_s=0
for ((i = DAYS - 1; i >= 0; i--)); do
  d=$((today - i * 86400)); e=$((d + 86399)); [ $i -eq 0 ] && e=$now
  t0=$(date +%s)
  if [ $i -lt "$FULL" ]; then
    for k in 0 1 2 3; do ins $d $e $k & done; wait
  else
    sparse $d
  fi
  t1=$(date +%s); ins_s=$((ins_s + t1 - t0))
  P -c "CALL refresh_continuous_aggregate('stats_hourly_ts', $d, $((e - e % 3600)))" 
  t2=$(date +%s); ref_s=$((ref_s + t2 - t1))
  P -c "SELECT count(compress_chunk(c, if_not_compressed => true)) FROM show_chunks('heartbeats', older_than => CAST($d AS BIGINT)) c" >/dev/null
  t3=$(date +%s); cmp_s=$((cmp_s + t3 - t2))
  echo "$(ts) gun -$i ekle=$((t1-t0))s ozet=$((t2-t1))s sikistir=$((t3-t2))s disk=$(df -h --output=avail / | tail -1)"
done
t0=$(date +%s)
P -c "CALL refresh_continuous_aggregate('stats_daily_utc', NULL, $((today - 86400)))"
echo "TOPLAM ekle=${ins_s}s ozet=${ref_s}s sikistir=${cmp_s}s gunluk_ozet=$(( $(date +%s) - t0 ))s"
