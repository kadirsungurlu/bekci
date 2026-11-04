# Bekci — proje haritası

Keşif yapma: yeri buradan bul, yalnız ilgili dosyanın ilgili kısmını oku. Yapıyı değiştiren iş bu dosyayı günceller;
alttaki otomatik bölüm `sh scripts/harita.sh` ile yenilenir (elle düzenleme).

## Klasör düzeni (repo/)
- `cmd/uptime/` — tek ikili: panel (`main.go`), kontrol noktası/ajan (`probe.go`), Windows hizmeti; `cmd/ajanimza/` ajan imza aracı
- `internal/api/` — HTTP katmanı (stdlib net/http). `server.go`: çekirdek rotalar + ara katmanlar (CSRF, güvenlik başlıkları, dil)
- `internal/store/` — SQL (PostgreSQL + TimescaleDB varsayılan; düz PG uyumlu; SQLite kullanımdan kalkıyor); migration'lar Go içinde; `storetest/` test veritabanı
- `internal/engine/` — denetim döngüsü, olay (incident) açma/kapama (`incidents.go`), bakım, `hub.go` (SSE yayını)
- `internal/notify/` — kanal sağlayıcıları (her tür bir dosya), `dispatcher.go` (kuyruk, tekrar, sessize alma, yükseltme), `email.go` (SMTP)
- `internal/check/` — izleyici türleri (http, tcp, dns, ping, sql, ...); `internal/i18n/` — sunucu tarafı TR/EN metinler
- `internal/servers/` sunucu ajanı metrikleri · `internal/backup/` dışa/içe aktarma (Kuma, UptimeRobot) · `internal/webpush/`
- `internal/apidocs/openapi.yaml` — API belgesi (yeni genel uç eklenirse güncelle)
- `docs/PERFORMANS-620.md` — ölçüm raporu (ham / Go özet / Timescale, disk, RAM, kaynak önerisi); `scripts/perf/` (tohum.sh, olcum.sh, istek.py) + `internal/store/perf_test.go` (BEKCI_PERF_*)
- `docs/DENETIM-640.md` — Timescale gerçek kullanım denetimi sonuçları + canlı geçiş planı
- `docs/TIMESCALE-TASARIM.md` — Postgres+TimescaleDB ve sunucu bilgi kaynağı tasarımı (600; 610-695 buna uyar): hypertable/cagg/saklama, uyumluluk modları, yükseltme yolları, lisans
- `docs/BETIK-GUVENLIK-680.md` — betik çalıştırma güvenlik tasarımı (tehdit modeli, K_yayın/K_onay/K_ajan, yerel iğne, imza kapsamı, yürütme kafesi, kurcalama belli kayıt, kararlar D1-D15; 690 buna uyar, kod yok)
- `web/` — Svelte 5 + Vite SPA (TS). `src/pages/` sayfalar, `src/components/`, `src/lib/` (api.ts türler+istekler, router, i18n)
- `web/e2e/` — Playwright betikleri (`push.mjs`, `durum.mjs`, `abonelik.mjs` + `smtp-sink.mjs` sahte SMTP; `ozelalan.mjs` abonelik ve `alanadi.mjs` on-demand TLS: gerçek alan adıyla elle, CI'da yok; `guncelleme.mjs` yeni sürüm şeridi, sahte sürüm sunucusu, CI'da; `olaylar.mjs` olay listesi sayfalama, CI'da; `mesh.mjs` bağlantı matrisi, CI'da; `topoloji.mjs` canlı topoloji, CI'da; `envanter.mjs` sunucu envanteri, CI'da; `sunucu-olaylar.mjs` zaman çizelgesi, CI'da; `apikeys.mjs` salt okunur anahtar, CI'da), `statusgrid.test.mjs`, `meshlayout.test.mjs`, `topolayout.test.mjs` (node --test)
- `site/` — tanıtım sitesi + belgeler (`docs-src/{tr,en}`) · `deploy/compose/` — varsayılan compose (TimescaleDB 2.31.0-pg18, Caddy); `site/indir/` indirilebilir compose/env (belgedeki kod bloklarıyla aynı olmalı, `site/docs-build/build.mjs` denetler) · `Dockerfile` (pg_dump dahil), `Dockerfile.postgres` (kullanımdan kalkıyor)
- `.github/workflows/imajlar.yml` — CI: go vet/test (sqlite+pg), web check/build, e2e, duman testi, `duman-veritabani` (`scripts/duman-db.sh`: compose, SQLite->TS, düz PG, eklenti), imaj

## Katmanlar ve kalıplar
- Yön: api -> engine/notify -> store. İşleyiciler `func (s *Server) x(w, r)`; JSON için `writeJSON`/`writeError`, DB hatası `s.dbError`.
- Yeni uç: özellik dosyasında `func init(){ RegisterRoutes(func(s *Server, mux *http.ServeMux){ mux.Handle("POST /api/..", s.admin(s.h)) }) }`.
  Sarmalayıcılar: `s.auth` (oturum), `s.admin` (yönetici), genel uçlar `mux.HandleFunc`. Örnek: `api/password_reset.go`, `api/webpush.go`.
- CSRF: GET/HEAD dışı `/api/` istekleri `X-Uptime: 1` başlığı ister (`server.go csrf`); muafiyet orada (push, API anahtarı, probe).
- Hız sınırı: `api/ratelimit.go` `ipLimiter` (IP başına pencere). İstemci IP: `auth.go clientIP`.
- Yeni migration: özellik dosyasında `func init(){ store.RegisterMigration(N, "SQL") }` (N = son+1, aşağıdaki listeye bak);
  SQLite/PG farkı varsa `RegisterMigrationDialect`. Örnek: `store/webpush.go`, `store/pages_layout.go`.
- Ayarlar: `store/settings.go` `AppSettings` (JSON tek satır); sistem e-postası `SystemMailChannelID` -> `api/password_reset.go systemMailer`.
- Durum sayfası: kayıt `store/pages*.go`, yönetim uçları `api/pages.go`, genel uçlar `api/public*.go` (önbellek + SSE `public_live.go`).
- Olay -> bildirim: `engine/incidents.go openIncident/incidentProgress` -> `notify.Dispatcher.Notify(Event)`.
- Arayüz yeni sayfa: `web/src/pages/X.svelte` + `lib/router.svelte.ts parse()` + `App.svelte`; metin `lib/i18n/{tr,en}/<ad>.ts`
  (en: `satisfies Shape<typeof tr>`; ayrıntı `lib/i18n/README.md`). Genel durum sayfası: `pages/PublicStatus.svelte` (`/durum/<slug>`).
- Kural: EMOJİ YASAK (CI denetler). Metinler TR+EN. Belirteçler DB'de SHA-256 özeti olarak.

## Test ve derleme (yerel; Coolify'da derleme YOK)
- Go: `go vet ./... && go test -count=1 ./internal/<paket>/ 2>&1 | tail -15` ; tam: `go test -race -count=1 ./... 2>&1 | tail -20`
- Web: `cd web && npm run check 2>&1 | tail -5 && npm run build 2>&1 | tail -3` (dist Go ikilisine gömülür)
- Panel çalıştır: `go build -o /tmp/uptime ./cmd/uptime && ADDR=127.0.0.1:8080 DATA_DIR=/tmp/veri /tmp/uptime`
- e2e: host'ta chromium yok -> `docker run --rm --network host -v $PWD/web/e2e:/e2e -w /e2e mcr.microsoft.com/playwright:latest node durum.mjs`
- Haritayı yenile: `sh scripts/harita.sh`

## Ortamlar
- Test: Coolify Bekci Test (dal `gelistirme`; adres proje CLAUDE.md'de) · Yayın: https://bekci.app (dal `main`), Docker Hub. Yayın kuralı: proje CLAUDE.md.

## Nerede ne var
| Özellik | Dosyalar |
|---|---|
| Veritabanı kipi (TimescaleDB) | store/timescale.go (ensureTimescale: migration kilidi altında algılama, DBMode timescale/timescale-apache/postgres/sqlite, DB_TIMESCALE=off; heartbeats+mesh_samples hypertable, sıkıştırma, stats_hourly_ts + stats_daily_utc sürekli özet, okuma adı stats_hourly = görünüm (özet + stats_hourly_legacy, monitors.stats_reset_at); gecikme histogramı h00..h16 PingBounds; migration 43); saklama cleanup.go (drop_chunks); test timescale_test.go (var olan veriyle dönüşüm); /healthz?detail db_mode; CI işi test-timescale |
| Kimlik, oturum, 2FA, OIDC | api/auth.go, sessions.go, twofactor.go, oidc.go; store/users.go, sessions.go |
| Şifre sıfırlama, sistem e-postası | api/password_reset.go; notify/email.go, email_html.go |
| İzleyiciler | api/monitors.go (aralık sınırı store.MinIntervalSec=5); store/monitors.go, beats.go + beats_batch.go (grup commit, BEAT_BATCH_MS); check/*; engine/runner.go |
| Olaylar | engine/incidents.go; api/incidents*.go, misc.go listIncidents (offset + X-Total-Count); store/incident*.go; web pages/Incidents, components/Pager (ortak sayfa gezgini) |
| Bildirim kanalları | api/notifications.go; notify/*; store/notifications.go, notification_rules.go |
| Durum sayfaları (yönetim) | api/pages.go; store/pages*.go; web pages/StatusPages, StatusPageEditor, PagePreviewPanel |
| E-posta aboneliği (durum sayfası) | api/subscribers.go, subscribers_notify.go; store/subscribers.go; web components/SubscribePanel, PageSubscribers |
| Özel alan adı, otomatik HTTPS | api/domains.go (GET /api/tls/ask: Caddy on-demand TLS onayı; alan adı durum denetimi); web components/DomainStatus; deploy/compose/Caddyfile; belge site/docs-src/*/04-caddy.md |
| Timescale okuma yolları | store/timescale_read.go (tsConvertDaily: stats_daily -> stats_daily_legacy, timescale_meta; dailySQL yerel gün saatlikten; tsWindowParts uzun pencere ekseni; PingPercentile; tsChunkPlan/tsTuneChunks; tsCached READ_CACHE_SEC); eşitlik testi timescale_read_test.go |
| Yeni sürüm bildirimi | api/updatecheck.go (GitHub yayın+etiket, semver, 6 sa; UPDATE_CHECK=off, UPDATE_CHECK_URL); store/updates.go (kapatma, migration 38); web components/UpdateBanner, settings/General |
| Durum sayfası (genel) | api/public.go, public_feed.go, public_incidents.go, public_live.go; web pages/PublicStatus.svelte, lib/statusgrid.ts |
| Durum sayfası e-posta aboneliği | api/subscribers.go (uçlar, belirteçler), subscribers_notify.go (birleştirme, hız sınırı, e-posta); store/subscribers.go; notify Dispatcher.SetObserver, SendMailHeaders |
| Bakım / duyurular | api/maintenance.go; store/maintenance.go; internal/maintenance; web pages/Maintenance*, Announcements |
| Sunucu ajanları | api/servers.go, metrics*.go; internal/servers; store/servers*.go |
| Sunucu envanteri | metrics/inventory.go (tipler, Sanitize, Hash, MaskSecrets), inventory_collect.go (Docker, yerel + port sertifikaları), inventory_linux.go (systemd/cgroup, /proc/net); probe/inventory.go (panel inventory_interval verirse toplar, 300 sn, değişince ya da 12 turda bir POST /api/probe/inventory; alan yoksa hiç çalışmaz); store/inventory.go (migration 45, SaveInventory, inv_cert_alerts); servers/inventory.go (IngestInventory, sertifika eşikleri, saatlik CheckCerts; olay kind=server_alert metric=cert, alert_id = -inv_cert_alerts.id); api/inventory.go; ayar server_cert_days; web components/ServerInventory, ServerDetail sayfa sekmesi; e2e envanter.mjs (CI); belge 08-sunucu-ajani#envanter |
| Yedek / içe aktarma | api/backup.go, importer.go; internal/backup |
| Ayarlar | api/misc.go (settings); store/settings.go; web pages/Settings + settings/* |
| Ajan (probe) istemcisi | internal/probe/probe.go (yoklama, sonuç gönderimi), dnscache.go (Main adı için son bilinen IP, PROBE_STATE_DIR/uptime.dns.json); cmd/uptime/probe.go env |
| Çok konum, kısmi erişim kesintisi | engine/locations.go (aggregateDecision, linkHealthy, noVote, meshLost), partial.go, localname_test.go; ana sunucu konum adı AppSettings.LocalLocationName (engine.LocalLocationName); olay ayrıntısı mesh_lost (api/incidents.go, web IncidentDetail); varsayılan all+notify_partial yalnız yeni monitörde (MonitorForm) |
| İşlem kaydı | store/audit.go; i18n/audit.go; web settings/Audit.svelte |
| Mesh (probe arası TCP ping, kalp atışı) | store/mesh.go (ayar/ezme, probe_links, probe_pair_keys, migration 40); api/probes_mesh.go (POST /api/probe/mesh, GET /api/probe/mesh/peers), mesh_settings.go; engine/probes.go (ProbeOnline: eşik x panel aralığı, eski ajan 90 sn); probe/meshtcp.go (çerçeve+HMAC), heartbeat.go, meshtypes.go (tel sözleşmesi); cmd/uptime/probe_mesh.go (MESH_ADDR :7480); web settings/Probes |
| Mesh karar + bildirim | mesh/verdict.go (saf karar: bölünme, düğüm, yol, tek yönlü, yetersiz gözlem), mesh/text.go (TR/EN metin, Report); mesh/render.go (şema PNG, RenderPNG; Inter alt kümesi mesh/font/, altın PNG testdata/topo*.png); engine/mesh.go (evalMesh: ısınma, 1 panel aralığı erteleme, tek olay; probe_offline tekil bildirimi bastırır); store/mesh_incident.go (olay türü mesh, yalnız yönetici, IncidentFilter.NoMesh); notify/mesh.go (KindMesh/KindMeshResolved, kanal = noktaların çevrimdışı kanalları); api/incidents_mesh.go; api/mesh_view.go (GET /api/mesh matris, engine.MeshSnapshot); i18n/mesh.go; web IncidentDetail mesh kartı, components/MeshMatrix (Ayarlar > Kontrol noktaları, SSE "mesh") |
| OOB ikinci hat (ajan hat yedeği) | store/probes_oob.go (migration 41: oob_ip, oob_name, uplink, uplink_since, oob_iface_missing); api/probe_guard.go (IP kilidi OOB yuvası), probes_oob.go (X-Probe-Uplink / X-Probe-OOB-Iface başlıkları), jobs + kalp atışı yanıtında oob; engine/uplink.go (SetProbeUplink, bildirim, OOB konumu noVote); notify KindUplinkFailover/Restored; probe/bind*.go (arayüz bulma, SO_BINDTODEVICE / IP_UNICAST_IF, PROBE_OOB_IFACE), probe/uplink.go (geçiş/dönüş durum makinesi); web components/UplinkBadge, OobFields; belge 08-sunucu-ajani#oob |
| Canlı topoloji sayfası | api/topology.go (GET /api/topology: tek istekte panel özeti, monitör x konum son sonucu, noktalar + panel RTT/hat/eş, mesh, sunucu son metrik, kanal son gönderim, açık olaylar; rol/müşteri süzgeci), mesh_view.go meshView; engine/probes.go ProbeLate (kalp atışı gecikiyor, erken uyarı); engine/rate.go (saniyedeki sonuç), notify/lastsend.go + store/channel_sends.go (kanal son gönderim, DB migration 42); ajan panel RTT'si kalp atışında (probe/heartbeat.go main_rtt_ms -> probe_links src=nokta dst=0 rtt_ms); web pages/Topology.svelte, lib/topology.svelte.ts (SSE tetikli + 5 sn yedek yoklama, gizli sekmede durur); geniş ekran components/TopoCanvas.svelte (HTML kartlar + SVG bağlantılar, ipucu klavye/Escape) ve yerleşim lib/topolayout.ts (genişliğe göre ızgara, 'N tane daha', mesh tek sıra <=10 nokta yoksa matrise düşer; test e2e/topolayout.test.mjs), kurallar lib/topoview.ts (durum sınıfı, sorunlular önce, kenar türü, ipucu satırları); dar ekran (<640 px) components/TopoList.svelte; TopoMatrix (bağlantı matrisi), TopoDecision (açık olay: neden, mesh bulguları, olay ayrıntısındaki notify kayıtları); i18n topology; e2e topoloji.mjs (CI) |
| Sunucu olayları, disk tahmini | metrics/oom*.go (ajan: vmstat + /dev/kmsg, Sample.Events); store/server_events.go (migration 46, tablo server_events, 365 gün), server_series.go (sütunlu okuma); servers/changes.go (olay üretimi: envanter farkı, reboot, uyarı), forecast.go (regresyon, DiskForecasts, evaluateDiskTrend: disk_forecast, disk_fast); api/servers.go GET /api/servers/{id}/timeline; web components/ServerTimeline, ServerDetail Olaylar sekmesi + tahmin sütunu, ServerList rozeti; e2e sunucu-olaylar.mjs (CI); belge 08-sunucu-ajani#olaylar |
| Salt okunur erişim, CLI | store/apikeys.go (migration 47, scope full/read); api/apikeys.go userForAPIKey (izin listesi Usable sonrası, /metrics ve rozet dahil), api/apikeys_read.go (readScopeAllows; GET /api/certs, GET /api/server-events; read anahtarı hep viewer + tüm sunucular); auth.go role() 403 code=read_only_key; web settings/ApiKeys (Erişim seçimi, varsayılan read), e2e apikeys.mjs (CI); belge 13-entegrasyonlar#salt-okunur; CLI repo dışında: /root/claude-proje/bin/bekci + bekci.py, adres/anahtar /root/.secrets/bekci-okuma.env (SECRETS_DOSYA, bin/secrets.sh), test için BEKCI_OKUMA_DOSYA=<yol> |
| SQLite -> PostgreSQL taşıma | `uptime migrate`: cmd/uptime/migrate.go, store/sqlite_migrate.go (kopya, partiler, devam, doğrulama, CheckSQLiteLeftover); belge site/docs-src/{tr/15-veritabani,en/15-database}.md; Ayarlar uyarısı settings/General (/healthz?detail db_mode) |
| Mesh topoloji şeması | web components/MeshDiagram.svelte (elle SVG, lib/meshlayout.ts yerleşimi, ipucu + klavye odağı; MeshMatrix kartında ve IncidentDetail mesh kartında); GET /api/mesh links (yönlü: rtt_ms, loss_streak, last_ok_at; api/mesh_view.go meshLinkViews); olay kaydı nodes + ab/ba (api/incidents_mesh.go); i18n probes.topo; Tooltip focusin |

<!-- OTOMATIK: scripts/harita.sh -->

### API rotaları (dosya: yöntem yol)
```
agentupdate.go: POST /api/probes/{id}/update | POST /api/probes/update-all | POST /api/probe/update
apikeys.go: GET /api/api-keys | POST /api/api-keys | DELETE /api/api-keys/{id}
apikeys_read.go: GET /api/certs | GET /api/server-events
backup.go: GET /api/export | POST /api/export | POST /api/import | POST /api/import/uptime-kuma | POST /api/import/uptimerobot
badges.go: GET /api/badge/{id}/{kind}
domains.go: GET /api/tls/ask | GET /api/status-pages/{id}/domain
incidents.go: GET /api/incidents/{id} | GET /api/servers/{id}/incidents
incidents_ack.go: POST /api/incidents/{id}/ack | DELETE /api/incidents/{id}/ack | POST /api/incidents/{id}/snooze | DELETE /api/incidents/{id}/snooze
incidents_manual.go: POST /api/incidents | PUT /api/incidents/{id} | DELETE /api/incidents/{id} | POST /api/incidents/{id}/updates | DELETE /api/incidents/{id}/updates/{uid}
inventory.go: GET /api/servers/{id}/inventory | PUT /api/servers/{id}/inventory/certs/{fingerprint} | POST /api/probe/inventory
maintenance.go: GET /api/maintenance | POST /api/maintenance | GET /api/maintenance/{id} | PUT /api/maintenance/{id} | DELETE /api/maintenance/{id} | POST /api/maintenance/{id}/pause | POST /api/maintenance/{id}/resume | POST /api/maintenance/{id}/end
mesh_view.go: GET /api/mesh
metrics.go: GET /metrics
oidc.go: GET /api/settings/oidc | PUT /api/settings/oidc | POST /api/settings/oidc/test | GET /api/auth/oidc/start | GET /api/auth/oidc/callback
openapi.go: GET /api/openapi.json
pages.go: GET /api/status-pages | POST /api/status-pages | GET /api/status-pages/{id} | PUT /api/status-pages/{id} | DELETE /api/status-pages/{id} | GET /api/status-pages/{id}/preview | POST /api/status-pages/preview-data | GET /api/status-pages/{id}/logo | PUT /api/status-pages/{id}/logo | DELETE /api/status-pages/{id}/logo | GET /api/status-pages/{id}/announcements | POST /api/status-pages/{id}/announcements | PUT /api/announcements/{id} | DELETE /api/announcements/{id} | GET /api/public/resolve | GET /api/public/pages/{slug} | POST /api/public/pages/{slug}/unlock | GET /api/public/pages/{slug}/logo | GET /api/public/pages/{slug}/events
password_reset.go: POST /api/auth/email | POST /api/auth/forgot | POST /api/auth/reset
probes.go: GET /api/probes | POST /api/probes | PUT /api/probes/{id} | DELETE /api/probes/{id} | POST /api/probes/{id}/token | GET /api/monitors/{id}/locations | PUT /api/monitors/{id}/locations | GET /api/probe/jobs | GET /api/probe/binary | POST /api/probe/results
probes_mesh.go: POST /api/probe/mesh | GET /api/probe/mesh/peers
public_feed.go: GET /api/public/pages/{slug}/feed.xml | GET /durum/{slug}/feed.xml
quick.go: POST /api/monitors/{id}/clone | POST /api/monitors/{id}/reset-stats | POST /api/monitors/{id}/push-token | PUT /api/monitors/{id}/notifications | POST /api/monitors/bulk | POST /api/status-pages/{id}/monitors
server.go: GET /healthz | GET /api/push/{token} | POST /api/push/{token} | GET /api/auth/state | POST /api/auth/setup | POST /api/auth/login | POST /api/auth/logout | POST /api/auth/password | PUT /api/auth/preferences | GET /api/summary | GET /api/monitors | POST /api/monitors | GET /api/monitors/{id} | PUT /api/monitors/{id} | DELETE /api/monitors/{id} | POST /api/monitors/{id}/pause | POST /api/monitors/{id}/resume | GET /api/monitors/{id}/series | GET /api/monitors/{id}/beats | GET /api/monitors/{id}/incidents | GET /api/incidents | GET /api/notifications | POST /api/notifications | PUT /api/notifications/{id} | DELETE /api/notifications/{id} | POST /api/notifications/test | POST /api/notifications/{id}/samples | GET /api/settings | PUT /api/settings | GET /api/events | GET /api/users | POST /api/users | PUT /api/users/{id} | DELETE /api/users/{id} | POST /api/users/{id}/password | GET /api/audit | GET /api/audit/facets | /api/ | /
servers.go: GET /api/servers | POST /api/servers | GET /api/servers/{id} | GET /api/servers/{id}/stats | GET /api/servers/{id}/timeline | GET /api/servers/{id}/events | PUT /api/servers/{id}/alerts | PUT /api/servers/{id}/notifications | POST /api/probe/metrics
sessions.go: GET /api/auth/sessions | DELETE /api/auth/sessions/{id} | DELETE /api/auth/sessions
subscribers.go: GET /api/status-pages/{id}/subscribers | GET /api/status-pages/{id}/subscribers.csv | DELETE /api/status-pages/{id}/subscribers/{sid} | POST /api/public/pages/{slug}/subscribe | POST /api/public/subscriptions/confirm | POST /api/public/subscriptions/unsubscribe
tags.go: GET /api/tags | POST /api/tags | PUT /api/tags/{id} | DELETE /api/tags/{id} | PUT /api/monitors/{id}/tags
topology.go: GET /api/topology
twofactor.go: GET /api/auth/2fa | POST /api/auth/2fa/setup | POST /api/auth/2fa/enable | POST /api/auth/2fa/disable | POST /api/auth/2fa/recovery-codes | POST /api/auth/login/2fa | POST /api/users/{id}/2fa/reset
updatecheck.go: GET /api/update | POST /api/update/check | POST /api/update/dismiss
webpush.go: GET /api/webpush/vapid | GET /api/webpush/subscriptions | POST /api/webpush/subscriptions | DELETE /api/webpush/subscriptions/{id} | POST /api/webpush/test
```

### Migration'lar (sürüm: dosya)
```
1: migrate.go 2: migrate.go 3: maintenance.go 4: tags.go 5: apikeys.go 6: twofactor.go 7: probes.go 8: pages_range.go 9: browser_removal.go 
10: servers.go 11: incident_events.go 12: user_servers.go 13: pages_incidents.go 14: pages_incidents.go 15: probes_iplock.go 16: i18n.go 
17: pages_layout.go 18: incident_kinds.go 19: probes.go 20: maintenance.go 21: probes.go 22: monitors.go 23: notification_rules.go 24: 
pages_period.go 25: incident_manual.go 26: servers_ext.go 27: beats_recent.go 28: domain.go 29: incident_ack.go 30: tag_rules.go 31: 
auth_ext.go 32: webpush.go 33: auth_ext.go 34: sessions.go 35: probes_update.go 36: subscribers.go 37: subscribers.go 38: updates.go 39: 
incident_kinds.go 40: mesh.go 41: probes_oob.go 42: channel_sends.go 43: timescale.go 44: server_metrics.go 45: inventory.go 46: 
server_events.go 47: apikeys.go
```

### Arayüz sayfaları / bileşenler / i18n ad alanları
```
pages: Announcements Auth ForcePassword IncidentDetail Incidents MaintenanceForm MaintenanceList MonitorDetail MonitorForm MonitorList More Notifications PublicStatus ServerDetail ServerList Settings StatusPageEditor StatusPages Topology
pages/settings: Account ApiKeys Audit Backup General Login Probes Tags TwoFactor UserForm Users
components: AgentAutoUpdate AgentVersion AlertRulesEditor BadgeBuilder BulkEditModal ConfigFields ConfirmDialog ConnectionDetails 
ContainerTable CopyButton DateTimeField DomainStatus FilePick Icon ImportResult IncidentAck IncidentEditModal IncidentTable IncidentUpdates 
LangSwitch LineChart MeshDiagram MeshMatrix Modal MonitorMenu MonitorPicker MonitorRow NotificationForm OobFields PagePreviewPanel 
PageSubscribers PageThumb Pager PingChart PushDevices QuickMaintModal QuickNotifyModal QuickPageModal QuickTagsModal RecoveryCodes RowMenu 
ServerInventory ServerPicker ServerSettingsModal ServerSetupModal ServerTimeline Sessions Sparkline StatusIcon StatusView SubscribePanel 
TagChip TagDialog TagRulePicker ThemeToggle Toasts Tooltip TopoCanvas TopoDecision TopoList TopoMatrix TypeBadge UpdateBanner UplinkBadge 
UptimeBars UsageBar
i18n: account ale.ts apiKeys audit auth backup common incide.ts maintenance monitorTypes monitors nav notifications notifyTypes overview pages probes pub roles servers settings status tags topology users
```
