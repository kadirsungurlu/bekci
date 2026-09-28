# bekci.app tanıtım sitesi ve belgeleri (statik). Derleme: docker build -t bekci-site site
# Çalıştırma: docker run -p 8080:80 bekci-site  → konteyner 80 numaralı portu dinler.
#
# Temel imajlar digest ile sabitlenmiştir; güncellemek için:
#   docker buildx imagetools inspect <imaj:etiket> → "Digest" satırı.

# 1) Belgeler: docs-src/{tr,en}/*.md → statik HTML, arama dizini, site haritası.
#    Bağımlılıklar docs-build/package-lock.json ile sabit. Kırık bağlantı, eksik
#    çeviri ya da indirilebilir dosyayla uyuşmayan kod bloğu derlemeyi durdurur.
# node:24-alpine
FROM --platform=$BUILDPLATFORM node:24-alpine@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1 AS docs
WORKDIR /build
COPY docs-build/package.json docs-build/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY docs-build/build.mjs ./
COPY docs-src/ /site/docs-src/
COPY index.html /site/index.html
COPY en/index.html /site/en/index.html
COPY img/ /site/img/
COPY indir/ /site/indir/
COPY assets/ /site/assets/
COPY favicon.svg apple-touch-icon.png /site/
RUN node build.mjs --src /site/docs-src --site /site --out /out

# 2) Sunucu
# caddy:2-alpine
FROM caddy:2-alpine@sha256:6aeddd44c3078b0f9a35206472a11420648a79c184603ef95957d0a20044cb2b

COPY Caddyfile /etc/caddy/Caddyfile
COPY index.html 404.html favicon.svg apple-touch-icon.png og.png og-en.png robots.txt /srv/
COPY en/ /srv/en/
COPY assets/ /srv/assets/
COPY img/ /srv/img/
COPY indir/ /srv/indir/
COPY --from=docs /out/ /srv/

# /assets ve /img adreslerindeki ?v=dev işaretini dosyanın içerik özetiyle
# değiştir: dosya değişince adres de değişir, uzun önbellek güvenle kullanılır.
# Ardından root olmayan kullanıcı ve Caddy'nin yazabileceği klasörler.
RUN set -eu; \
    cd /srv; \
    : > /tmp/damga.sed; \
    for f in $(find assets img -type f) favicon.svg apple-touch-icon.png; do \
      h="$(sha256sum "$f" | cut -c1-10)"; \
      echo "s#/$f?v=dev#/$f?v=$h#g" >> /tmp/damga.sed; \
    done; \
    find . -name '*.html' -exec sed -i -f /tmp/damga.sed {} +; \
    if grep -rn --include='*.html' '?v=dev' .; then echo "damgalanmamış adres var" >&2; exit 1; fi; \
    rm /tmp/damga.sed; \
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
