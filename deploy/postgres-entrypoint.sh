#!/bin/sh
# Uptime + gömülü PostgreSQL başlatıcısı (Dockerfile.postgres).
#
# - İlk açılışta /data/postgres altında veritabanını oluşturur.
# - PostgreSQL ağa açılmaz: yalnızca container içindeki Unix soketini dinler.
# - Uygulama ve PostgreSQL root olmayan "postgres" kullanıcısıyla çalışır.
# - Aynı /data'yı iki container kullanamaz: /data/.uptime.lock dosyasına
#   özel (exclusive) flock alınır ve container yaşadığı sürece tutulur.
#   flock aynı sunucudaki container'lar arasında da çalışır (aynı dosya).
#   Kilit başkasındaysa (ör. Coolify'ın "rolling update"inde eski container
#   hâlâ çalışırken yenisi açılır) beklenir; süre dolarsa hata ile çıkılır.
# - PostgreSQL ve uygulama birlikte izlenir: biri kapanırsa diğeri de
#   düzgünce kapatılır ve container hata koduyla çıkar (yeniden başlatma
#   politikası devreye girsin diye).
# - SIGTERM/SIGINT (tini iletir) gelince önce uygulama, sonra PostgreSQL
#   düzgünce kapatılır.
set -eu

DATA="${DATA_DIR:-/data}"
PGDATA="$DATA/postgres"
SOCK=/run/postgresql
LOCK="$DATA/.uptime.lock"
# Kilit için en fazla bekleme süresi (saniye).
LOCK_WAIT="${UPTIME_LOCK_WAIT:-600}"
# Kilit beklenirken sağlık kontrolü bu dosyaya bakar (bkz. Dockerfile.postgres).
STANDBY=/run/uptime-bekliyor
DB_URL="postgres:///uptime?host=$SOCK&user=postgres&sslmode=disable"

log() { echo "uptime-entrypoint: $*"; }

rm -f "$STANDBY" # önceki çalışmadan kalmış olabilir
mkdir -p "$DATA" "$SOCK" "$DATA/backups"
chown postgres:postgres "$DATA" "$SOCK" "$DATA/backups"

# 1) Kilit. fd 9 bu kabukta ve alt süreçlerinde (PostgreSQL, uygulama)
#    açık kalır; hepsi kapanınca çekirdek kilidi kendiliğinden bırakır
#    (container çökse bile bayat kilit kalmaz).
exec 9>>"$LOCK"
if ! flock -n 9; then
	log "UYARI: $DATA başka bir Uptime container'ı tarafından kullanılıyor (kilit: $LOCK)."
	log "O container kapanana kadar bekleniyor (en fazla $LOCK_WAIT sn). İki PostgreSQL aynı veri klasörünü açarsa veritabanı bozulur; bu yüzden ikinci kopya başlatılmaz."
	: >"$STANDBY"
	waited=0
	while ! flock -n 9; do
		if [ "$waited" -ge "$LOCK_WAIT" ]; then
			rm -f "$STANDBY"
			log "HATA: $LOCK_WAIT sn beklendi, kilit hâlâ başka bir container'da. Aynı /data birimini kullanan diğer container'ı durdurun. Çıkılıyor."
			exit 1
		fi
		sleep 1
		waited=$((waited + 1))
		if [ $((waited % 30)) -eq 0 ]; then
			log "kilit bekleniyor… ($waited/$LOCK_WAIT sn)"
		fi
	done
	rm -f "$STANDBY"
	log "kilit alındı, devam ediliyor."
fi

# 2) Veritabanı klasörü ve sürüm kontrolü.
mkdir -p "$PGDATA"
chown postgres:postgres "$PGDATA"
chmod 700 "$PGDATA"

SERVER_MAJOR="$(postgres --version | sed -n 's/^[^0-9]*\([0-9][0-9]*\).*/\1/p')"
if [ -z "$SERVER_MAJOR" ]; then
	log "HATA: PostgreSQL sürümü okunamadı ($(postgres --version 2>&1))."
	exit 1
fi

if [ -s "$PGDATA/PG_VERSION" ]; then
	DATA_MAJOR="$(tr -d '[:space:]' <"$PGDATA/PG_VERSION")"
	if [ "$DATA_MAJOR" != "$SERVER_MAJOR" ]; then
		cat <<EOF
================================================================================
HATA: PostgreSQL ana sürümü uyuşmuyor.
  Veri klasörü ($PGDATA): PostgreSQL $DATA_MAJOR
  Bu imajdaki sunucu:      PostgreSQL $SERVER_MAJOR
Ana sürümler arasında veri klasörü doğrudan açılamaz. Veriyi taşımak için:

  1. Bu container'ı durdurun ve ESKİ imaja (PostgreSQL $DATA_MAJOR içeren sürüm,
     ör. ghcr.io/kadirsa1105/uptime-kadir-app:postgres-<eski-sha>) geri dönün.
  2. Eski imaj çalışırken yedek alın:
       docker exec <container> pg_dump -h /run/postgresql -U postgres \\
         --format=custom --file=/data/backups/tasima.dump uptime
     ve /data/backups/tasima.dump dosyasını sunucu dışına da kopyalayın.
  3. Container'ı durdurun; /data/postgres klasörünü silmek yerine yeniden
     adlandırın (ör. /data/postgres-$DATA_MAJOR-eski).
  4. Yeni imajı başlatın: boş bir PostgreSQL $SERVER_MAJOR veritabanı oluşur.
  5. Yedeği geri yükleyin:
       docker exec <container> pg_restore -h /run/postgresql -U postgres \\
         --clean --if-exists --no-owner -d uptime /data/backups/tasima.dump
     ve container'ı yeniden başlatın.
  6. Her şey yolundaysa eski klasörü (/data/postgres-$DATA_MAJOR-eski) silin.
================================================================================
EOF
		exit 1
	fi
else
	log "PostgreSQL $SERVER_MAJOR veritabanı ilk kez oluşturuluyor ($PGDATA)…"
	if ! su-exec postgres initdb -D "$PGDATA" -U postgres --auth-local=trust --auth-host=reject \
		--encoding=UTF8 --locale=C --no-instructions; then
		log "HATA: initdb başarısız oldu (ayrıntılar yukarıda). $PGDATA klasörünü ve disk alanını kontrol edin."
		exit 1
	fi
fi

# 3) PostgreSQL'i bu kabuğun çocuğu olarak başlat (ölürse fark edelim).
su-exec postgres postgres -D "$PGDATA" \
	-c listen_addresses='' \
	-c unix_socket_directories="$SOCK" \
	-c shared_buffers=64MB \
	-c max_connections=40 &
PG=$!
APP=
STOPPING=

stop_pg() {
	if kill -0 "$PG" 2>/dev/null; then
		kill -INT "$PG" 2>/dev/null || true # "fast" kapanış
		wait "$PG" 2>/dev/null || true
	fi
}

# shellcheck disable=SC2329 # trap ile çağrılır
on_signal() {
	STOPPING=1
	if [ -n "$APP" ]; then
		kill -TERM "$APP" 2>/dev/null || true
	else
		stop_pg
		exit 0
	fi
}
trap on_signal TERM INT

# Hazır olana kadar bekle (en fazla 120 sn; 0,25 sn aralıkla).
i=0
until su-exec postgres pg_isready -q -h "$SOCK" -d postgres; do
	if ! kill -0 "$PG" 2>/dev/null; then
		log "HATA: PostgreSQL başlatılamadı (ayrıntılar yukarıda)."
		exit 1
	fi
	i=$((i + 1))
	if [ "$i" -ge 480 ]; then
		log "HATA: PostgreSQL 120 sn içinde hazır olmadı."
		stop_pg
		exit 1
	fi
	sleep 0.25
done

if ! su-exec postgres psql -h "$SOCK" -XtAc "SELECT 1 FROM pg_database WHERE datname = 'uptime'" | grep -q 1; then
	su-exec postgres createdb -h "$SOCK" uptime
fi

# DATABASE_URL imajda da tanımlı (docker exec ile çalışan komutlar için);
# bu imaj her zaman gömülü veritabanını kullanır.
if [ "${DATABASE_URL:-$DB_URL}" != "$DB_URL" ]; then
	log "UYARI: DATABASE_URL yok sayıldı; bu imaj gömülü PostgreSQL'i kullanır. Dış PostgreSQL için varsayılan (SQLite) imajı DATABASE_URL ile çalıştırın."
fi
export DATABASE_URL="$DB_URL"

# 4) Uygulama.
su-exec postgres uptime "$@" &
APP=$!

# 5) İzleme: ikisi de çalıştığı sürece bekle. "sleep & wait" sinyallerin
#    hemen işlenmesini sağlar; wait biten çocukları toplar, kill -0 da
#    bitenleri görür.
while kill -0 "$APP" 2>/dev/null && kill -0 "$PG" 2>/dev/null; do
	sleep 1 &
	wait $! 2>/dev/null || true
done

if ! kill -0 "$APP" 2>/dev/null; then
	set +e
	wait "$APP"
	CODE=$?
	set -e
	if [ -z "$STOPPING" ]; then
		log "HATA: uygulama beklenmedik şekilde kapandı (çıkış kodu $CODE); PostgreSQL kapatılıyor."
		[ "$CODE" -eq 0 ] && CODE=1
	fi
	stop_pg
	exit "$CODE"
fi

log "HATA: PostgreSQL beklenmedik şekilde kapandı; uygulama durduruluyor ve container kapanıyor."
kill -TERM "$APP" 2>/dev/null || true
wait "$APP" 2>/dev/null || true
exit 1
