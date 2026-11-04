#!/bin/bash
# Tek ölçek ölçümü (iş 620): olcum.sh <monitör> <tam_yoğunluk_gün> <toplam_gün> <eski_ham_sn> <parça_sn>
# Çıktı: /tmp/bekci620/sonuc-<N>.jsonl. Veritabanları sonunda silinir.
set -uo pipefail
N=$1; FULL=$2; DAYS=$3; OLDRAW=$4; CHUNK=$5; C=bekci-620-perf
W=/tmp/bekci620; OUT=$W/sonuc-$N.jsonl; : > "$OUT"; HERE=$(dirname "$(readlink -f "$0")")
DB=s$N; OLD=s${N}_old; DSN="postgres://postgres:test@127.0.0.1:55620"
Q() { docker exec -i $C psql -U postgres -d "$1" -qAtX -v ON_ERROR_STOP=1 -c "$2"; }
CG=/sys/fs/cgroup/system.slice/docker-$(docker inspect $C --format '{{.Id}}').scope/cpu.stat
cpu() { awk '/usage_usec/{print $2}' "$CG"; }
J() { echo "$1" | tee -a "$OUT"; }
panel() { # db port off
  ( cd $W; DATABASE_URL="$DSN/$1?sslmode=disable" ADDR=127.0.0.1:$2 DATA_DIR=$W/d$2 DB_TIMESCALE=$3 UPDATE_CHECK=off READ_CACHE_SEC=0 TZ=UTC nohup ./uptime >$W/panel-$2.log 2>&1 & echo $! >$W/panel-$2.pid )
  for i in $(seq 120); do curl -fs 127.0.0.1:$2/healthz >/dev/null && break; /usr/bin/sleep 1; done
}
stop() { kill "$(cat $W/panel-$1.pid)" 2>/dev/null; /usr/bin/sleep 2; }
Q postgres "CREATE DATABASE $DB"; Q postgres "CREATE DATABASE $OLD"
panel $DB 18621 auto; panel $OLD 18622 off; stop 18621; stop 18622
SEEDM="INSERT INTO monitors (id, name, type, config, active, interval_sec, retry_interval_sec, timeout_sec, status, created_at, updated_at)
  SELECT g, 'izleyici-' || g, 'http', '{\"url\":\"http://127.0.0.1:9/\"}', 0, 30, 30, 10, 1, 1, 1 FROM generate_series(1, $N) g;
  SELECT setval(pg_get_serial_sequence('monitors', 'id'), $N);
  INSERT INTO probes (id, name, token_hash, created_at) SELECT g, 'loc-' || g, 'x' || g, 1 FROM generate_series(1, 5) g;
  INSERT INTO monitor_locations SELECT m, p FROM generate_series(1, $N) m CROSS JOIN generate_series(1, 5) p;"
Q $DB "$SEEDM"; Q $OLD "$SEEDM"
Q $DB "SELECT set_chunk_time_interval('heartbeats', CAST($CHUNK AS BIGINT)); SELECT alter_job(job_id, scheduled => false) FROM timescaledb_information.jobs WHERE job_id >= 1000" >/dev/null
c0=$(cpu); t0=$(date +%s)
DB=$DB "$HERE/tohum.sh" "$N" "$FULL" "$DAYS" | tee "$W/tohum-$N.log" | tail -n 4
J "{\"adim\":\"tohum\",\"sn\":$(( $(date +%s) - t0 )),\"cpu_sn\":$(( ($(cpu) - c0) / 1000000 ))}"
# Boyutlar
Q $DB "SELECT json_build_object('adim','boyut',
  'hb_toplam',(SELECT hypertable_size('heartbeats')),
  'sikistirma_once',(SELECT sum(before_compression_total_bytes) FROM chunk_compression_stats('heartbeats')),
  'sikistirma_sonra',(SELECT sum(after_compression_total_bytes) FROM chunk_compression_stats('heartbeats')),
  'sikisan_parca',(SELECT count(*) FROM chunk_compression_stats('heartbeats') WHERE compression_status='Compressed'),
  'cagg_saat',(SELECT hypertable_size(format('%I.%I',materialization_hypertable_schema,materialization_hypertable_name)::regclass) FROM timescaledb_information.continuous_aggregates WHERE view_name='stats_hourly_ts'),
  'cagg_gun',(SELECT hypertable_size(format('%I.%I',materialization_hypertable_schema,materialization_hypertable_name)::regclass) FROM timescaledb_information.continuous_aggregates WHERE view_name='stats_daily_utc'),
  'db',pg_database_size('$DB'))" | tee -a "$OUT"
# Eski yol: Go özet tabloları aynı satırlarla, ham yalnız son kısım
cols="monitor_id, bucket, up, down, ping_sum, ping_count, ping_min, ping_max"
hist=$(seq -f 'h%02g' 0 16 | paste -sd, | sed 's/,/, /g')
now=$(date +%s); mid=$((N / 2 + 1))
page=$(python3 -c "print(','.join(str(1+i*$N//50) for i in range(min(50,$N))))")
docker exec -e PGOPTIONS='-c session_replication_role=replica -c synchronous_commit=off' $C bash -c "psql -U postgres -d $DB -qAtX -c \"COPY (SELECT $cols, $hist FROM stats_hourly_ts) TO STDOUT\" | psql -U postgres -d $OLD -q -c \"COPY stats_hourly ($cols, $hist) FROM STDIN\"
  psql -U postgres -d $DB -qAtX -c \"COPY (SELECT $cols FROM stats_daily_utc) TO STDOUT\" | psql -U postgres -d $OLD -q -c \"COPY stats_daily ($cols) FROM STDIN\"
  psql -U postgres -d $DB -qAtX -c \"COPY (SELECT monitor_id, time, status, ping_ms, message, location FROM heartbeats WHERE time >= $((now - OLDRAW)) OR (time >= $((now - 86400)) AND monitor_id IN ($mid,$page))) TO STDOUT\" | psql -U postgres -d $OLD -q -c \"COPY heartbeats (monitor_id, time, status, ping_ms, message, location) FROM STDIN\""
Q $OLD "ANALYZE"; Q $DB "ANALYZE monitors"
Q $OLD "SELECT json_build_object('adim','eski_boyut','hb_satir',(SELECT count(*) FROM heartbeats),'hb',pg_total_relation_size('heartbeats'),'hourly',pg_total_relation_size('stats_hourly'),'daily',pg_total_relation_size('stats_daily'))" | tee -a "$OUT"
J "{\"adim\":\"eski_doldu\",\"t\":$(date +%s)}"
# Sorgu karşılaştırması
cd $W
for mode in yeni go; do
  d=$DB; [ $mode = go ] && d=$OLD
  BEKCI_PERF_DSN="$DSN/$d?sslmode=disable" BEKCI_PERF_MODE=$mode BEKCI_PERF_CGROUP=$CG ./perf.test -test.run 'TestPerfQueries' -test.timeout 30m 2>&1 | grep '^{' >> "$OUT"
done
if [ "$FULL" -ge "$DAYS" ]; then
  BEKCI_PERF_DSN="$DSN/$DB?sslmode=disable" BEKCI_PERF_MODE=ham BEKCI_PERF_REPS=3 BEKCI_PERF_CGROUP=$CG ./perf.test -test.run 'TestPerfQueries' -test.timeout 45m 2>&1 | grep '^{' >> "$OUT"
fi
# Panel istekleri
panel $DB 18621 auto; panel $OLD 18622 off
python3 "$HERE/istek.py" yeni 18621 $N >> "$OUT"; python3 "$HERE/istek.py" go 18622 $N >> "$OUT"
for p in 18621 18622; do J "{\"adim\":\"panel_rss\",\"port\":$p,\"kb\":$(awk '/VmRSS/{print $2}' /proc/$(cat $W/panel-$p.pid)/status)}"; done
# Sıcak veri: sayfa karışımından sonra paylaşımlı bellekteki bloklar
Q $DB "CREATE EXTENSION IF NOT EXISTS pg_buffercache"
Q $DB "SELECT json_build_object('adim','sicak',
  'ham_acik_mb',sum(CASE WHEN c.relname LIKE '\_hyper\_1\_%' THEN 1 ELSE 0 END)*8/1024,
  'ham_sikisik_mb',sum(CASE WHEN c.relname LIKE 'compress\_hyper%' OR c.relname LIKE '%compress%' THEN 1 ELSE 0 END)*8/1024,
  'toplam_mb',count(*)*8/1024)
  FROM pg_buffercache b JOIN pg_class c ON pg_relation_filenode(c.oid) = b.relfilenode WHERE b.reldatabase = (SELECT oid FROM pg_database WHERE datname = current_database())" | tee -a "$OUT"
Q $DB "SELECT json_build_object('adim','sicak_parca','en_yeni_parca_mb',(SELECT round(pg_total_relation_size(format('%I.%I',chunk_schema,chunk_name)::regclass)/1048576.0) FROM timescaledb_information.chunks WHERE hypertable_name='heartbeats' AND NOT is_compressed ORDER BY range_end_integer DESC LIMIT 1),
  'acik_parca',(SELECT count(*) FROM timescaledb_information.chunks WHERE hypertable_name='heartbeats' AND NOT is_compressed))" | tee -a "$OUT"
J "{\"adim\":\"pg_bellek\",\"mb\":$(docker stats --no-stream $C --format '{{.MemUsage}}' | awk '{v=$1; g=(v~/GiB/); sub(/[A-Za-z]+/,"",v); printf "%d", g?v*1024:v}')}"
stop 18621; stop 18622
# Yazma hızı: hedef oran ve azami
R=$((N * 5 / 30))
for mode in yeni go; do
  d=$DB; [ $mode = go ] && d=$OLD
  for rate in $R 0; do
    BEKCI_PERF_DSN="$DSN/$d?sslmode=disable" BEKCI_PERF_MODE=$mode BEKCI_PERF_RATE=$rate BEKCI_PERF_BATCH=250 BEKCI_PERF_SEC=30 BEKCI_PERF_CGROUP=$CG ./perf.test -test.run 'TestPerfWrite' -test.timeout 5m 2>&1 | grep '^{' >> "$OUT"
  done
done
# Arka plan işleri: 15 dakikalık yeni veriden sonra özet yenileme; bir parçayı yeniden sıkıştırma
for k in 0 1 2 3; do
Q $DB "INSERT INTO heartbeats (monitor_id, time, status, ping_ms, message, location)
  SELECT m, t + m % 30 + l, 1, 15 + (m * 37) % 180, '200 - OK', 'loc-' || l FROM generate_series(1, $N) m CROSS JOIN generate_series(0, 4) l
  CROSS JOIN generate_series($(( $(date +%s) - 900 )), $(date +%s), 30) t WHERE m % 4 = $k" &
done; wait
job() { Q $DB "SELECT job_id FROM timescaledb_information.jobs WHERE proc_name = 'policy_refresh_continuous_aggregate' AND hypertable_name = (SELECT materialization_hypertable_name FROM timescaledb_information.continuous_aggregates WHERE view_name = '$1')"; }
for v in stats_hourly_ts stats_daily_utc; do
  c0=$(cpu); s=$(date +%s%3N); Q $DB "CALL run_job($(job $v))"
  J "{\"adim\":\"is_$v\",\"ms\":$(( $(date +%s%3N) - s )),\"cpu_ms\":$(( ($(cpu) - c0) / 1000 ))}"
done
chunk=$(Q $DB "SELECT format('%I.%I', chunk_schema, chunk_name) FROM timescaledb_information.chunks WHERE hypertable_name = 'heartbeats' AND is_compressed ORDER BY range_end_integer DESC LIMIT 1")
Q $DB "SELECT decompress_chunk('$chunk')" >/dev/null
rows=$(Q $DB "SELECT count(*) FROM $chunk")
c0=$(cpu); s=$(date +%s%3N); Q $DB "SELECT compress_chunk('$chunk')" >/dev/null
J "{\"adim\":\"is_sikistir\",\"satir\":$rows,\"ms\":$(( $(date +%s%3N) - s )),\"cpu_ms\":$(( ($(cpu) - c0) / 1000 ))}"
Q postgres "DROP DATABASE $DB WITH (FORCE)"; Q postgres "DROP DATABASE $OLD WITH (FORCE)"
J "{\"adim\":\"bitti\"}"
