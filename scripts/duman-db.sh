#!/bin/sh
# Veritabanı duman testi (CI: duman-veritabani). Gerçek imajla dört yol:
#   1) varsayılan compose: TimescaleDB, panel, veri akışı, pg_dump yedeği
#   2) SQLite kurulumu -> boş PostgreSQL'de panel reddeder -> uptime migrate
#   3) eklentisiz düz PostgreSQL: çalışır, uyarı verir
#   4) düz PostgreSQL'e eklentiyi etkinleştirme: aynı veri, dönüşüm
# Kullanım: IMAJ=kadirsungurlu/bekci:duman sh scripts/duman-db.sh
# Oluşturduğu her şey bekci-duman öneklidir ve sonda silinir.
set -eu
IMAJ=${IMAJ:-kadirsungurlu/bekci:duman}
TAG=${IMAJ##*:}
TS=$(sed -n 's/^ *image: \(timescale\/timescaledb:[^ ]*\)$/\1/p' deploy/compose/docker-compose.yml)
PORT=${DUMAN_PORT:-18080}
URL=http://127.0.0.1:$PORT
SIFRE=dumansifre123
TMP=$(mktemp -d)
OV=$TMP/override.yml
printf 'services:\n  bekci:\n    ports: ["127.0.0.1:%s:8080"]\n' "$PORT" >"$OV"
export UPTIME_DOMAIN=duman.local ACME_EMAIL=a@duman.local POSTGRES_PASSWORD=$SIFRE UPTIME_TAG=$TAG
dc() { docker compose -p bekci-duman -f deploy/compose/docker-compose.yml -f "$OV" "$@"; }
hata() {
	echo "HATA: $*" >&2
	docker logs --tail 40 bekci-duman-sqlite 2>/dev/null || true
	docker logs --tail 40 bekci-duman-app 2>/dev/null || true
	dc logs --tail 40 2>/dev/null || true
	exit 1
}
temizle() {
	dc down -v --remove-orphans >/dev/null 2>&1 || true
	docker rm -f bekci-duman-sqlite bekci-duman-app bekci-duman-pg >/dev/null 2>&1 || true
	docker volume rm bekci-duman-pg bekci-duman-appdata >/dev/null 2>&1 || true
	docker network rm bekci-duman-net >/dev/null 2>&1 || true
	rm -rf "$TMP"
}
trap temizle EXIT
bekle() {
	for _ in $(seq 1 120); do
		[ "$(curl -s -o /dev/null -w '%{http_code}' "$URL/healthz" || true)" = 200 ] && return 0
		sleep 1
	done
	hata "panel açılmadı"
}
kip() { curl -s "$URL/healthz?detail" | sed -n 's/.*"db_mode":"\([^"]*\)".*/\1/p'; }
kip_ol() { [ "$(kip)" = "$1" ] || hata "beklenen kip $1, gelen $(kip)"; echo "  kip: $1"; }
post() { curl -s -b "$TMP/c" -c "$TMP/c" -H 'X-Uptime: 1' -H 'Content-Type: application/json' -X POST -d "$2" "$URL$1"; }
kurulum() { post /api/auth/setup '{"username":"duman","password":"duman-sifre-123"}' >/dev/null; }
giris() {
	[ "$(curl -s -o /dev/null -w '%{http_code}' -c "$TMP/c" -H 'X-Uptime: 1' -H 'Content-Type: application/json' \
		-d '{"username":"duman","password":"duman-sifre-123"}' "$URL/api/auth/login")" = 200 ] || hata "giriş olmadı"
}
monitor() { # ad -> push ile bir sonuç yazar
	cevap=$(post /api/monitors "{\"name\":\"$1\",\"type\":\"push\",\"interval\":60,\"config\":{}}")
	belirtec=$(echo "$cevap" | sed -n 's/.*"push_token":"\([^"]*\)".*/\1/p')
	id=$(echo "$cevap" | sed -n 's/^{"id":\([0-9]*\).*/\1/p')
	[ -n "$belirtec" ] || hata "monitör eklenemedi: $cevap"
	curl -s -o /dev/null "$URL/api/push/$belirtec?status=up&msg=ok"
}
var_mi() { # monitör adı listede ve sonuçları duruyor
	curl -s -b "$TMP/c" "$URL/api/monitors" | grep -q "\"$1\"" || hata "monitör yok: $1"
}
sonuc_var() { # kimlik: en az bir kontrol sonucu
	n=$(curl -s -b "$TMP/c" "$URL/api/monitors/$1/beats" | grep -o '"time"' | wc -l)
	[ "$n" -gt 0 ] || hata "monitör $1 için sonuç yok"
	echo "  monitör $1: $n sonuç"
}

echo "== 1) varsayılan compose"
dc up -d --pull never db bekci >/dev/null 2>&1
bekle
kip_ol timescale
kurulum
monitor compose-mon
sleep 2
sonuc_var "$id"
dc exec -T bekci sh -c 'pg_dump --format=custom --no-owner --file /tmp/y.dump --dbname "$DATABASE_URL" && test -s /tmp/y.dump' \
	|| hata "pg_dump başarısız"
echo "  pg_dump tamam"
dc down -v --remove-orphans >/dev/null 2>&1

echo "== 2) SQLite -> uptime migrate"
docker volume create bekci-duman_bekci-data >/dev/null
docker run -d --name bekci-duman-sqlite -p 127.0.0.1:$PORT:8080 -v bekci-duman_bekci-data:/data "$IMAJ" >/dev/null
bekle
kip_ol sqlite
docker logs bekci-duman-sqlite 2>&1 | grep -q 'kullanımdan kalkıyor' || hata "SQLite uyarısı yok"
kurulum
monitor sqlite-mon
eski=$id
sleep 2
docker stop -t 30 bekci-duman-sqlite >/dev/null
docker rm bekci-duman-sqlite >/dev/null
dc up -d --wait --pull never db >/dev/null 2>&1
dc up --pull never --no-start bekci >/dev/null 2>&1
docker start bekci-duman-bekci-1 >/dev/null
red=
for _ in $(seq 1 30); do
	docker logs bekci-duman-bekci-1 2>&1 | grep -q 'uptime migrate' && { red=1; break; }
	sleep 1
done
[ -n "$red" ] || hata "boş PostgreSQL ile panel reddetmedi"
echo "  boş hedefte panel reddetti"
dc stop bekci >/dev/null
dc run --rm -T --no-deps bekci migrate --dry-run | tail -1
dc run --rm -T --no-deps bekci migrate | tail -3
dc up -d --pull never bekci >/dev/null 2>&1
bekle
kip_ol timescale
giris
var_mi sqlite-mon
sonuc_var "$eski"
dc down -v --remove-orphans >/dev/null 2>&1

echo "== 3) eklentisiz düz PostgreSQL"
docker network create bekci-duman-net >/dev/null
docker volume create bekci-duman-pg >/dev/null
docker volume create bekci-duman-appdata >/dev/null
pg() { docker run -d --name bekci-duman-pg --network bekci-duman-net -e POSTGRES_PASSWORD=$SIFRE \
	-v bekci-duman-pg:/var/lib/postgresql "$@" >/dev/null; }
pg_bekle() {
	for _ in $(seq 1 60); do
		docker exec bekci-duman-pg pg_isready -U postgres -h 127.0.0.1 >/dev/null 2>&1 && return 0
		sleep 1
	done
	hata "veritabanı açılmadı"
}
app() { docker run -d --name bekci-duman-app --network bekci-duman-net -p 127.0.0.1:$PORT:8080 -v bekci-duman-appdata:/data \
	-e "DATABASE_URL=postgres://postgres:$SIFRE@bekci-duman-pg:5432/postgres?sslmode=disable" "$IMAJ" >/dev/null; }
pg postgres:18-alpine
pg_bekle
app
bekle
kip_ol postgres
docker logs bekci-duman-app 2>&1 | grep -q 'önerilmez' || hata "düz PostgreSQL uyarısı yok"
kurulum
monitor duz-mon
duz=$id
sleep 2
sonuc_var "$duz"
curl -s -b "$TMP/c" -f -o /dev/null "$URL/api/monitors/$duz/series?range=24h" || hata "grafik verisi alınamadı"

echo "== 4) düz PostgreSQL'e eklentiyi etkinleştirme"
docker stop -t 30 bekci-duman-app >/dev/null
docker rm bekci-duman-app >/dev/null
docker stop -t 30 bekci-duman-pg >/dev/null
docker rm bekci-duman-pg >/dev/null
pg "$TS" postgres -c shared_preload_libraries=timescaledb
pg_bekle
app
bekle
kip_ol timescale
giris
var_mi duz-mon
sonuc_var "$duz"
echo "duman-db: tüm senaryolar geçti"
