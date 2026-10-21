#!/bin/sh
# HARITA.md'nin otomatik bölümünü yeniler (rota, migration, sayfa listeleri).
# Kullanım: sh scripts/harita.sh   (repo kökünden)
set -e
cd "$(dirname "$0")/.."
f=HARITA.md
isaret='<!-- OTOMATIK: scripts/harita.sh -->'
tmp=$(mktemp)
awk -v m="$isaret" '{print} $0==m{exit}' "$f" > "$tmp"
{
  echo
  echo "### API rotaları (dosya: yöntem yol)"
  echo '```'
  grep -Hn 'mux\.Handle\(Func\)\?("' internal/api/*.go | grep -v _test |
    sed -E 's#^internal/api/([^:]+):[0-9]+:.*mux\.Handle(Func)?\("([^"]+)".*#\1: \3#' |
    awk -F': ' '{a[$1]=a[$1] (a[$1]?" | ":"") $2} END{for(k in a) print k": "a[k]}' | sort
  echo '```'
  echo
  echo "### Migration'lar (sürüm: dosya)"
  echo '```'
  { grep -n '^	[0-9]*: `' internal/store/migrate.go | sed -E 's/^[0-9]+:	([0-9]+):.*/\1: migrate.go/'
    grep -Hn 'RegisterMigration[A-Za-z]*([0-9]' internal/store/*.go | grep -v _test |
      sed -E 's#^internal/store/([^:]+):[0-9]+:.*RegisterMigration[A-Za-z]*\(([0-9]+).*#\2: \1#'
  } | sort -n -u | awk -F': ' '{a[$1]=a[$1] (a[$1]?", ":"") $2} END{for(k in a) print k": "a[k]}' | sort -n | paste -sd' ' | fold -s -w 140
  echo '```'
  echo
  echo "### Arayüz sayfaları / bileşenler / i18n ad alanları"
  echo '```'
  echo "pages: $(ls web/src/pages | grep svelte | sed 's/.svelte//' | paste -sd' ')"
  echo "pages/settings: $(ls web/src/pages/settings | sed 's/.svelte//' | paste -sd' ')"
  echo "components: $(ls web/src/components | grep svelte | sed 's/.svelte//' | paste -sd' ')" | fold -s -w 140
  echo "i18n: $(ls web/src/lib/i18n/tr | grep -v index | sed 's/.ts//' | paste -sd' ')"
  echo '```'
} >> "$tmp"
mv "$tmp" "$f"
echo "HARITA.md güncellendi ($(wc -l < $f) satır)"
