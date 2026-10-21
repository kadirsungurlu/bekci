# Bekci — proje haritası

Keşif yapma: yeri buradan bul, yalnız ilgili dosyanın ilgili kısmını oku. Yapıyı değiştiren iş bu dosyayı günceller;
alttaki otomatik bölüm `sh scripts/harita.sh` ile yenilenir (elle düzenleme).

## Klasör düzeni (repo/)
- `cmd/uptime/` — tek ikili: panel (`main.go`), kontrol noktası/ajan (`probe.go`), Windows hizmeti; `cmd/ajanimza/` ajan imza aracı
- `internal/api/` — HTTP katmanı (stdlib net/http). `server.go`: çekirdek rotalar + ara katmanlar (CSRF, güvenlik başlıkları, dil)
- `internal/store/` — SQL (SQLite varsayılan, PostgreSQL destekli); migration'lar Go içinde; `storetest/` test veritabanı
- `internal/engine/` — denetim döngüsü, olay (incident) açma/kapama (`incidents.go`), bakım, `hub.go` (SSE yayını)
- `internal/notify/` — kanal sağlayıcıları (her tür bir dosya), `dispatcher.go` (kuyruk, tekrar, sessize alma, yükseltme), `email.go` (SMTP)
- `internal/check/` — izleyici türleri (http, tcp, dns, ping, sql, ...); `internal/i18n/` — sunucu tarafı TR/EN metinler
- `internal/servers/` sunucu ajanı metrikleri · `internal/backup/` dışa/içe aktarma (Kuma, UptimeRobot) · `internal/webpush/`
- `internal/apidocs/openapi.yaml` — API belgesi (yeni genel uç eklenirse güncelle)
- `web/` — Svelte 5 + Vite SPA (TS). `src/pages/` sayfalar, `src/components/`, `src/lib/` (api.ts türler+istekler, router, i18n)
- `web/e2e/` — Playwright betikleri (`push.mjs`, `durum.mjs`), `statusgrid.test.mjs` (node --test)
- `site/` — tanıtım sitesi + belgeler (`docs-src/{tr,en}`) · `deploy/compose/` — örnek compose · `Dockerfile`, `Dockerfile.postgres`
- `.github/workflows/imajlar.yml` — CI: go vet/test (sqlite+pg), web check/build, e2e, duman testi, imaj

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
- Test: https://test-uptime.kadir.app (dal `gelistirme`) · Yayın: https://bekci.app (dal `main`), Docker Hub. Yayın kuralı: proje CLAUDE.md.

## Nerede ne var
| Özellik | Dosyalar |
|---|---|
| Kimlik, oturum, 2FA, OIDC | api/auth.go, sessions.go, twofactor.go, oidc.go; store/users.go, sessions.go |
| Şifre sıfırlama, sistem e-postası | api/password_reset.go; notify/email.go, email_html.go |
| İzleyiciler | api/monitors.go; store/monitors.go; check/*; engine/runner.go |
| Olaylar | engine/incidents.go; api/incidents*.go; store/incident*.go |
| Bildirim kanalları | api/notifications.go; notify/*; store/notifications.go, notification_rules.go |
| Durum sayfaları (yönetim) | api/pages.go; store/pages*.go; web pages/StatusPages, StatusPageEditor, PagePreviewPanel |
| Durum sayfası (genel) | api/public.go, public_feed.go, public_incidents.go, public_live.go; web pages/PublicStatus.svelte, lib/statusgrid.ts |
| Durum sayfası e-posta aboneliği | api/subscribers.go (uçlar, belirteçler), subscribers_notify.go (birleştirme, hız sınırı, e-posta); store/subscribers.go; notify Dispatcher.SetObserver, SendMailHeaders |
| Bakım / duyurular | api/maintenance.go; store/maintenance.go; internal/maintenance; web pages/Maintenance*, Announcements |
| Sunucu ajanları | api/servers.go, metrics*.go; internal/servers; store/servers*.go |
| Yedek / içe aktarma | api/backup.go, importer.go; internal/backup |
| Ayarlar | api/misc.go (settings); store/settings.go; web pages/Settings + settings/* |
| İşlem kaydı | store/audit.go; i18n/audit.go; web settings/Audit.svelte |

<!-- OTOMATIK: scripts/harita.sh -->

### API rotaları (dosya: yöntem yol)
```
agentupdate.go: POST /api/probes/{id}/update | POST /api/probes/update-all | POST /api/probe/update
apikeys.go: GET /api/api-keys | POST /api/api-keys | DELETE /api/api-keys/{id}
backup.go: GET /api/export | POST /api/export | POST /api/import | POST /api/import/uptime-kuma | POST /api/import/uptimerobot
badges.go: GET /api/badge/{id}/{kind}
incidents.go: GET /api/incidents/{id} | GET /api/servers/{id}/incidents
incidents_ack.go: POST /api/incidents/{id}/ack | DELETE /api/incidents/{id}/ack | POST /api/incidents/{id}/snooze | DELETE /api/incidents/{id}/snooze
incidents_manual.go: POST /api/incidents | PUT /api/incidents/{id} | DELETE /api/incidents/{id} | POST /api/incidents/{id}/updates | DELETE /api/incidents/{id}/updates/{uid}
maintenance.go: GET /api/maintenance | POST /api/maintenance | GET /api/maintenance/{id} | PUT /api/maintenance/{id} | DELETE /api/maintenance/{id} | POST /api/maintenance/{id}/pause | POST /api/maintenance/{id}/resume | POST /api/maintenance/{id}/end
metrics.go: GET /metrics
oidc.go: GET /api/settings/oidc | PUT /api/settings/oidc | POST /api/settings/oidc/test | GET /api/auth/oidc/start | GET /api/auth/oidc/callback
openapi.go: GET /api/openapi.json
pages.go: GET /api/status-pages | POST /api/status-pages | GET /api/status-pages/{id} | PUT /api/status-pages/{id} | DELETE /api/status-pages/{id} | GET /api/status-pages/{id}/preview | POST /api/status-pages/preview-data | GET /api/status-pages/{id}/logo | PUT /api/status-pages/{id}/logo | DELETE /api/status-pages/{id}/logo | GET /api/status-pages/{id}/announcements | POST /api/status-pages/{id}/announcements | PUT /api/announcements/{id} | DELETE /api/announcements/{id} | GET /api/public/resolve | GET /api/public/pages/{slug} | POST /api/public/pages/{slug}/unlock | GET /api/public/pages/{slug}/logo | GET /api/public/pages/{slug}/events
password_reset.go: POST /api/auth/email | POST /api/auth/forgot | POST /api/auth/reset
probes.go: GET /api/probes | POST /api/probes | PUT /api/probes/{id} | DELETE /api/probes/{id} | POST /api/probes/{id}/token | GET /api/monitors/{id}/locations | PUT /api/monitors/{id}/locations | GET /api/probe/jobs | GET /api/probe/binary | POST /api/probe/results
public_feed.go: GET /api/public/pages/{slug}/feed.xml | GET /durum/{slug}/feed.xml
quick.go: POST /api/monitors/{id}/clone | POST /api/monitors/{id}/reset-stats | POST /api/monitors/{id}/push-token | PUT /api/monitors/{id}/notifications | POST /api/monitors/bulk | POST /api/status-pages/{id}/monitors
server.go: GET /healthz | GET /api/push/{token} | POST /api/push/{token} | GET /api/auth/state | POST /api/auth/setup | POST /api/auth/login | POST /api/auth/logout | POST /api/auth/password | PUT /api/auth/preferences | GET /api/summary | GET /api/monitors | POST /api/monitors | GET /api/monitors/{id} | PUT /api/monitors/{id} | DELETE /api/monitors/{id} | POST /api/monitors/{id}/pause | POST /api/monitors/{id}/resume | GET /api/monitors/{id}/series | GET /api/monitors/{id}/beats | GET /api/monitors/{id}/incidents | GET /api/incidents | GET /api/notifications | POST /api/notifications | PUT /api/notifications/{id} | DELETE /api/notifications/{id} | POST /api/notifications/test | POST /api/notifications/{id}/samples | GET /api/settings | PUT /api/settings | GET /api/events | GET /api/users | POST /api/users | PUT /api/users/{id} | DELETE /api/users/{id} | POST /api/users/{id}/password | GET /api/audit | GET /api/audit/facets | /api/ | /
servers.go: GET /api/servers | POST /api/servers | GET /api/servers/{id} | GET /api/servers/{id}/stats | GET /api/servers/{id}/events | PUT /api/servers/{id}/alerts | PUT /api/servers/{id}/notifications | POST /api/probe/metrics
sessions.go: GET /api/auth/sessions | DELETE /api/auth/sessions/{id} | DELETE /api/auth/sessions
subscribers.go: GET /api/status-pages/{id}/subscribers | GET /api/status-pages/{id}/subscribers.csv | DELETE /api/status-pages/{id}/subscribers/{sid} | POST /api/public/pages/{slug}/subscribe | POST /api/public/subscriptions/confirm | POST /api/public/subscriptions/unsubscribe
tags.go: GET /api/tags | POST /api/tags | PUT /api/tags/{id} | DELETE /api/tags/{id} | PUT /api/monitors/{id}/tags
twofactor.go: GET /api/auth/2fa | POST /api/auth/2fa/setup | POST /api/auth/2fa/enable | POST /api/auth/2fa/disable | POST /api/auth/2fa/recovery-codes | POST /api/auth/login/2fa | POST /api/users/{id}/2fa/reset
webpush.go: GET /api/webpush/vapid | GET /api/webpush/subscriptions | POST /api/webpush/subscriptions | DELETE /api/webpush/subscriptions/{id} | POST /api/webpush/test
```

### Migration'lar (sürüm: dosya)
```
1: migrate.go 2: migrate.go 3: maintenance.go 4: tags.go 5: apikeys.go 6: twofactor.go 7: probes.go 8: pages_range.go 9: browser_removal.go 
10: servers.go 11: incident_events.go 12: user_servers.go 13: pages_incidents.go 14: pages_incidents.go 15: probes_iplock.go 16: i18n.go 
17: pages_layout.go 18: incident_kinds.go 19: probes.go 20: maintenance.go 21: probes.go 22: monitors.go 23: notification_rules.go 24: 
pages_period.go 25: incident_manual.go 26: servers_ext.go 27: beats_recent.go 28: domain.go 29: incident_ack.go 30: tag_rules.go 31: 
auth_ext.go 32: webpush.go 33: auth_ext.go 34: sessions.go 35: probes_update.go 36: subscribers.go
```

### Arayüz sayfaları / bileşenler / i18n ad alanları
```
pages: Announcements Auth ForcePassword IncidentDetail Incidents MaintenanceForm MaintenanceList MonitorDetail MonitorForm MonitorList More Notifications PublicStatus ServerDetail ServerList Settings StatusPageEditor StatusPages
pages/settings: Account ApiKeys Audit Backup General Login Probes Tags TwoFactor UserForm Users
components: AgentAutoUpdate AgentVersion AlertRulesEditor BadgeBuilder BulkEditModal ConfigFields ConfirmDialog ConnectionDetails 
ContainerTable CopyButton DateTimeField FilePick Icon ImportResult IncidentAck IncidentEditModal IncidentTable IncidentUpdates LangSwitch 
LineChart Modal MonitorMenu MonitorPicker MonitorRow NotificationForm PagePreviewPanel PageThumb PingChart PushDevices QuickMaintModal 
QuickNotifyModal QuickPageModal QuickTagsModal RecoveryCodes RowMenu ServerPicker ServerSettingsModal ServerSetupModal Sessions Sparkline 
StatusIcon StatusView TagChip TagDialog TagRulePicker ThemeToggle Toasts Tooltip TypeBadge UptimeBars UsageBar
i18n: account ale.ts apiKeys audit auth backup common incide.ts maintenance monitorTypes monitors nav notifications notifyTypes overview pages probes pub roles servers settings status tags users
```
