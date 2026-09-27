#!/bin/sh
# Uptime + gömülü PostgreSQL başlatıcısı (Dockerfile.postgres).
#
# - İlk açılışta /data/postgres altında veritabanını oluşturur.
# - PostgreSQL ağa açılmaz: yalnızca container içindeki Unix soketini dinler.
# - Uygulama ve PostgreSQL root olmayan "postgres" kullanıcısıyla çalışır.
# - SIGTERM gelince önce uygulama, sonra PostgreSQL düzgünce kapatılır.
set -eu

DATA="${DATA_DIR:-/data}"
PGDATA="$DATA/postgres"
SOCK=/run/postgresql

mkdir -p "$PGDATA" "$SOCK" "$DATA/backups"
chown postgres:postgres "$DATA" "$PGDATA" "$SOCK" "$DATA/backups"
chmod 700 "$PGDATA"

if [ ! -s "$PGDATA/PG_VERSION" ]; then
	echo "PostgreSQL veritabanı ilk kez oluşturuluyor…"
	gosu postgres initdb -D "$PGDATA" -U postgres --auth-local=trust --auth-host=reject \
		--encoding=UTF8 --locale=C --no-instructions >/dev/null 2>&1
fi

gosu postgres pg_ctl -D "$PGDATA" -w -t 120 -o "-c listen_addresses='' -c unix_socket_directories=$SOCK -c shared_buffers=64MB -c max_connections=40" start

if ! gosu postgres psql -h "$SOCK" -tAc "SELECT 1 FROM pg_database WHERE datname = 'uptime'" | grep -q 1; then
	gosu postgres createdb -h "$SOCK" uptime
fi

export DATABASE_URL="postgres:///uptime?host=$SOCK&user=postgres&sslmode=disable"

gosu postgres uptime "$@" &
APP=$!
trap 'kill -TERM "$APP" 2>/dev/null' TERM INT

set +e
wait "$APP"
wait "$APP" 2>/dev/null
CODE=$?
set -e

gosu postgres pg_ctl -D "$PGDATA" -m fast -w stop
exit "$CODE"
