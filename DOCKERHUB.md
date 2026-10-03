<p align="center">
  <img src="https://raw.githubusercontent.com/kadirsungurlu/bekci/main/site/apple-touch-icon.png" width="88" alt="Bekci">
</p>

<h1 align="center">Bekci</h1>

<p align="center"><b>Self-hosted uptime &amp; server monitoring — one image, ~15 MB RAM, English &amp; Turkish UI.</b><br>
<a href="https://bekci.app">Website</a> · <a href="https://bekci.app/en/docs/">Docs</a> · <a href="https://github.com/kadirsungurlu/bekci">GitHub</a> · <a href="https://github.com/kadirsungurlu/bekci/releases">Releases</a></p>

![Bekci monitor list](https://raw.githubusercontent.com/kadirsungurlu/bekci/main/docs/ekran/monitorler.png)

## Quick start

```bash
docker run -d --name bekci --restart unless-stopped \
  -p 8080:8080 -v bekci-data:/data \
  -e BASE_URL=https://status.example.com \
  kadirsungurlu/bekci:1
```

Open `http://SERVER:8080` and create the first admin account. For HTTPS with Caddy, Docker Compose, Coolify or an existing reverse proxy see the **[installation guides](https://bekci.app/en/docs/)**.

## Tags

| Tag | Contents |
|---|---|
| `latest`, `1`, `1.3`, `1.3.0` (exact version, e.g.) | SQLite (recommended, lightest) |
| `postgres`, `1-postgres`, `1.3-postgres`, `1.3.0-postgres` | Embedded PostgreSQL 18 |

- `latest` / `postgres` always point to the latest **released** version; development builds are not pushed to Docker Hub (they live on `ghcr.io/kadirsungurlu/bekci:<short-sha>` only).
- Platforms: `linux/amd64`, `linux/arm64`.
- Pin a minor version (`1.3`) in production to receive patch fixes only.
- Every image ships with an SBOM and build provenance attestations.

## What it does

- **Uptime checks:** HTTP(S) with keyword/JSON, TCP, ping, DNS, push, Docker, PostgreSQL/MySQL/MSSQL/MongoDB/Redis, MQTT, SNMP, gRPC, WebSocket, TLS certificate.
- **Multi-location checks:** lightweight agents check from other servers; location outages (some locations down while the monitor is up) are recorded separately.
- **Server monitoring:** CPU, RAM, per-mount disk, swap, load, temperature, network and Docker containers on Linux and Windows, with alert rules.
- **Notification channels (24 + Web Push):** Telegram, WhatsApp, e-mail (HTML), Slack, Discord, Teams, ntfy, PagerDuty, Opsgenie, signed webhooks and more; per-channel rules (event filter, quiet hours, delay, escalation, language), tag-based binding, slow-response and domain-expiry alerts.
- **Status pages:** several layouts, custom domain, password protection, per-page language, manual incidents with updates, planned-maintenance block and RSS feed.
- **Incidents:** timeline plus connection diagnostics, acknowledge/snooze, CSV export, maintenance windows, roles, customer accounts, 2FA, OpenID Connect sign-in, API keys and an OpenAPI document.
- **Other:** dark/light theme; installable as a PWA on phones; encrypted backups; import from Uptime Kuma and UptimeRobot; Prometheus `/metrics`.

## Data & configuration

| | |
|---|---|
| Data volume | `/data` (database + automatic backups) |
| Port | `8080` |
| Health check | built in (`/healthz`) |
| Main settings | `BASE_URL`, `TZ`, `DATABASE_URL` (external PostgreSQL), `MAX_CONCURRENT_CHECKS` |

The full list is in the [environment variables](https://bekci.app/en/docs/environment-variables/) docs.

Updates: `docker compose pull && docker compose up -d`. The database is backed up automatically before a schema upgrade.

## License

[AGPL-3.0](https://github.com/kadirsungurlu/bekci/blob/main/LICENSE) · Made in Türkiye · Contact: me@kadir.app

---

🇹🇷 **Türkçe:** Bekci, web siteleri, servisler ve sunucular için kendi sunucunuzda çalışan, tek imajlık izleme sistemidir. Türkçe belgeler: **https://bekci.app/docs/**
