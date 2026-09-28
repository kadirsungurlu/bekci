# bekci.app tanıtım sitesi (statik). Derleme: docker build -t bekci-site site
# Çalıştırma: docker run -p 8080:80 bekci-site  → konteyner 80 numaralı portu dinler.
#
# Temel imaj digest ile sabitlenmiştir; güncellemek için:
#   docker buildx imagetools inspect caddy:2-alpine → "Digest" satırı.
FROM caddy:2-alpine@sha256:6aeddd44c3078b0f9a35206472a11420648a79c184603ef95957d0a20044cb2b

COPY Caddyfile /etc/caddy/Caddyfile
COPY index.html 404.html favicon.svg apple-touch-icon.png og.png og-en.png robots.txt sitemap.xml /srv/
COPY en/ /srv/en/
COPY assets/ /srv/assets/
COPY img/ /srv/img/

# /assets ve /img adreslerindeki ?v=dev işaretini dosyanın içerik özetiyle
# değiştir: dosya değişince adres de değişir, uzun önbellek güvenle kullanılır.
# Ardından root olmayan kullanıcı ve Caddy'nin yazabileceği klasörler.
RUN set -eu; \
    cd /srv; \
    for f in $(find assets img -type f) favicon.svg apple-touch-icon.png; do \
      h="$(sha256sum "$f" | cut -c1-10)"; \
      sed -i "s#/$f?v=dev#/$f?v=$h#g" index.html 404.html en/index.html en/404.html; \
    done; \
    if grep -n '?v=dev' index.html 404.html en/index.html en/404.html; then echo "damgalanmamış adres var" >&2; exit 1; fi; \
    caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile; \
    addgroup -S -g 10001 site; \
    adduser -S -D -H -u 10001 -G site site; \
    mkdir -p /data/caddy /config/caddy; \
    chown -R site:site /data /config; \
    chmod -R a+rX /srv

USER site
EXPOSE 80

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1/healthz || exit 1
