# Uptime — Coolify: Build Pack = Dockerfile, Port 8080, kalıcı depolama /data

# 1) Arayüz
FROM node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# 2) Uygulama (arayüz ikilinin içine gömülür)
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
ARG SOURCE_COMMIT=dev
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=$(echo ${SOURCE_COMMIT} | cut -c1-7)" \
      -o /out/uptime ./cmd/uptime
# Başka platformların ajan programları (aynı sürüm): Windows sunucular
# panelden indirir (GET /api/probe/binary?os=windows&arch=amd64). Her biri
# imaja ~38 MB ekler; örn. ARM sunucular için:
#   --build-arg AGENT_PLATFORMS="windows/amd64 linux/arm64"
ARG AGENT_PLATFORMS="windows/amd64"
RUN mkdir -p /out/agents && for p in ${AGENT_PLATFORMS}; do \
      os=${p%/*}; arch=${p#*/}; ext=; [ "$os" = windows ] && ext=.exe; \
      CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
        -ldflags "-s -w -X main.version=$(echo ${SOURCE_COMMIT} | cut -c1-7)" \
        -o /out/agents/uptime-$os-$arch$ext ./cmd/uptime || exit 1; \
    done

# 3) Çalışma imajı
FROM alpine:3
RUN apk add --no-cache ca-certificates \
    && adduser -D -H -u 1000 uptime \
    && mkdir -p /data \
    && chown uptime:uptime /data
COPY --from=build /out/uptime /usr/local/bin/uptime
COPY --from=build /out/agents/ /usr/local/share/uptime/agents/

# Ping yetkisiz (UDP tabanlı ICMP) çalışır; Docker'ın varsayılan
# net.ipv4.ping_group_range ayarı buna izin verir, root gerekmez.
USER uptime
ENV ADDR=:8080 \
    DATA_DIR=/data \
    TZ=Europe/Istanbul
VOLUME ["/data"]
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["uptime"]
