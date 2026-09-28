<div align="center">

<img src="web/public/favicon.svg" width="64" height="64" alt="">

# Bekci

**A lightweight, single-image monitoring system for websites, services and servers.**

[![Docker Hub](https://img.shields.io/docker/v/kadirsungurlu/bekci?sort=semver&label=docker&color=0d9488&logo=docker&logoColor=white)](https://hub.docker.com/r/kadirsungurlu/bekci)
[![Go 1.27](https://img.shields.io/badge/Go-1.27-0d9488?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/license-AGPL--3.0-475569)](LICENSE)
[![Platforms](https://img.shields.io/badge/platform-amd64%20%7C%20arm64-475569)](https://hub.docker.com/r/kadirsungurlu/bekci/tags)
[![Database](https://img.shields.io/badge/database-SQLite%20%7C%20PostgreSQL-475569)](#environment-variables)
[![UI](https://img.shields.io/badge/UI-English%20%7C%20Turkish-475569)](#why)

[Türkçe](README.md) · **English**

[bekci.app](https://bekci.app) · Contact: [me@kadir.app](mailto:me@kadir.app)

<img src="docs/ekran/monitorler.png" alt="Monitor list: status, response time, uptime bars for the last 24 hours and an overall summary" width="100%">

</div>

**Bekci** (Turkish for "night watchman") is a monitoring system written in Go with an interface as simple as UptimeRobot's:
uptime checks for websites and services (the Uptime Kuma feature set) and
resource tracking for servers (similar to Beszel). One Docker image, SQLite or
PostgreSQL; designed to stay fast with hundreds of monitors.

> [!NOTE]
> **The UI is available in English and Turkish.** Each user picks their own
> language (Account → Language); status pages and notifications have their own
> language setting. The screenshots below show the Turkish UI; where a menu name
> is mentioned, the Turkish name is given with its English label, e.g.
> **Sunucular** (Servers).

## Why?

- **One image, one container** — UI, API, check engine and the agent binaries
  (Linux amd64/arm64, Windows amd64) ship in the same image; no extra services.
- **Lightweight** — ~15 MB RAM with 10 monitors and 3 servers.
- **SQLite or PostgreSQL** — SQLite by default; optionally an external
  PostgreSQL via `DATABASE_URL`, or the `:postgres` image with embedded
  PostgreSQL 18.
- **Secure agents** — tokens are stored as hashes, IP lock, pinned version
  verified by SHA-256 that never updates itself.
- **Works like an app on your phone (PWA)** — add to home screen, live updates.
- **Turkish UI** — including notifications, date and number formats.
- **Easy migration** — import from an Uptime Kuma backup or an UptimeRobot account.

## Quick start

```bash
docker run -d --name bekci --restart unless-stopped \
  -p 8080:8080 -v bekci-data:/data \
  -e BASE_URL=https://bekci.ornek.com \
  kadirsungurlu/bekci
```

Open `http://server:8080` and create the first admin account. To expose it over
HTTPS, put a reverse proxy (Caddy, Traefik, Nginx) in front of it or use the
[Compose setup](#docker-compose-standalone-server) below. Image:
[Docker Hub `kadirsungurlu/bekci`](https://hub.docker.com/r/kadirsungurlu/bekci)
(linux/amd64 and linux/arm64). Coolify and other ways to install:
[Installation](#installation).

## Screenshots

<table>
  <tr>
    <td width="50%" valign="top">
      <a href="docs/ekran/monitor-detay.png"><img src="docs/ekran/monitor-detay.png" alt="Monitor details"></a>
      <br><sub><b>Monitor details</b> — response time chart, 24 h / 7 / 30 / 90 day uptime, SSL expiry, incidents</sub>
    </td>
    <td width="50%" valign="top">
      <a href="docs/ekran/olay.png"><img src="docs/ekran/olay.png" alt="Incident details"></a>
      <br><sub><b>Incident details</b> — root cause, timeline, notifications sent and the request/response at the time of the outage</sub>
    </td>
  </tr>
  <tr>
    <td width="50%" valign="top">
      <a href="docs/ekran/sunucu-detay.png"><img src="docs/ekran/sunucu-detay.png" alt="Server details"></a>
      <br><sub><b>Server details</b> — CPU, memory, network, disk I/O, load and per-mount usage charts</sub>
    </td>
    <td width="50%" valign="top">
      <a href="docs/ekran/durum-sayfasi.png"><img src="docs/ekran/durum-sayfasi.png" alt="Public status page"></a>
      <br><sub><b>Status page</b> — public, collapsible groups, 90-day history; follows the system theme</sub>
    </td>
  </tr>
  <tr>
    <td colspan="2" valign="top">
      <a href="docs/ekran/sunucular.png"><img src="docs/ekran/sunucular.png" alt="Server list"></a>
      <br><sub><b>Servers</b> — Linux and Windows; CPU, RAM, fullest disk, network, load and alerts at a glance</sub>
    </td>
  </tr>
  <tr>
    <td colspan="2" valign="top">
      <a href="docs/ekran/yeni-monitor.png"><img src="docs/ekran/yeni-monitor.png" alt="New monitor: type picker"></a>
      <br><sub><b>New monitor</b> — HTTP, TCP, Ping, DNS, databases, Docker, Push, groups and more</sub>
    </td>
  </tr>
  <tr>
    <td colspan="2" align="center">
      <a href="docs/ekran/mobil.png"><img src="docs/ekran/mobil.png" alt="Monitor list and server details on a phone"></a>
      <br><sub><b>On the phone</b> — monitor list and server details (can be added to the home screen as a PWA)</sub>
    </td>
  </tr>
</table>

<sub>Screenshots were taken from a demo instance running on fictional data.</sub>

## Contents

- [Why?](#why)
- [Quick start](#quick-start)
- [Screenshots](#screenshots)
- [Features](#features)
- [Installation](#installation)
  - [Coolify](#coolify)
  - [Docker Compose (standalone server)](#docker-compose-standalone-server)
  - [Images and system requirements](#images-and-system-requirements)
  - [Environment variables](#environment-variables)
- [Updates, backups and rollback](#updates-backups-and-rollback)
- [Server agent and check location](#server-agent-and-check-location)
  - [Install commands](#install-commands)
  - [Updating the agent](#updating-the-agent)
  - [Removing the agent](#removing-the-agent)
  - [IP lock and token](#ip-lock-and-token)
  - [Troubleshooting](#troubleshooting)
- [Push monitor](#push-monitor)
- [Password reset](#password-reset)
- [Security notes](#security-notes)
- [Development](#development)

## Features

**Monitors**
- HTTP(S) (method, headers, body, basic auth / OAuth, accepted status codes,
  redirects, mTLS, proxy), keyword and JSON query checks, TCP port, Ping,
  DNS, Push, Docker container, WebSocket, gRPC, databases (PostgreSQL,
  MySQL/MariaDB, MSSQL, MongoDB, Redis), MQTT, SNMP and more — the full list is
  in the "Yeni monitör" (New monitor) form
- SSL certificate expiry warnings, retries, outage reminders, upside-down mode
- Monitor groups, tags, maintenance windows
- Incident history and details (request/response capture at the time of the
  outage — visible to admins only), 24 hour / 7 / 30 / 90 day uptime and
  response time charts
- Remote **check locations**: monitors are checked from multiple locations,
  with an outage rule (any / majority / all)

**Server monitoring**
- CPU, RAM, disk (per mount point), swap, load, temperature, network (Mbit/s),
  Docker containers; Linux and Windows
- Alerts: offline, CPU, RAM, disk, swap, load, temperature, network — using a
  window average and hysteresis (fires when the threshold is reached, clears
  when the value drops slightly below it)

**Notifications**
- WhatsApp (WP API), Telegram, email, Discord, Slack, Teams, Google Chat,
  Mattermost, Rocket.Chat, Webhook, ntfy, Gotify, Pushover, PagerDuty,
  Opsgenie and more; "send sample notification" for every channel

**Status pages**
- Groups (collapsible), target address display (domain only), recent
  incidents (can be hidden), password protection, custom domain, badges

**Users**
- Roles (admin / editor / viewer), **customer accounts** that only see the
  monitors and servers assigned to them, two-factor authentication (2FA), API
  keys, audit log

**Other**
- Live-updating UI (SSE), import/export, automatic nightly backup
- Use it like an app on your phone (PWA): on iPhone, Safari → Share → **Add to
  Home Screen**; on Android, Chrome → **Install app**

## Installation

For a one-command install, see [Quick start](#quick-start) above; below are
Coolify and Compose setups, image tags and environment variables.

### Coolify

- Build Pack: **Dockerfile**, port **8080**, health check `/healthz`
- Persistent storage: **/data** (database and the `backups/` folder). Add it
  in Coolify as a **Volume**. If you want to bind a folder on the host
  (Directory Mount), its owner must be uid 1000 (`chown 1000:1000 <folder>`);
  the application runs as a non-root user.
- On first start, the UI asks you to create an admin account.
- On updates, Coolify starts the new container before stopping the old one.
  The new container waits for the data folder lock (and reports healthy
  meanwhile) and takes over within 1–2 seconds once the old one stops; the two
  copies never run checks at the same time or send duplicate notifications.

### Docker Compose (standalone server)

`deploy/compose/` contains the app + Caddy (automatic HTTPS with Let's Encrypt,
HSTS), ready to go:

```bash
cd deploy/compose
cp .env.example .env        # UPTIME_DOMAIN ve ACME_EMAIL'i yazın
docker compose up -d
```

(The comment in the command means: fill in `UPTIME_DOMAIN` and `ACME_EMAIL`.)

- Details (backups, version pinning, password reset) are in the comment at the
  top of `docker-compose.yml`.
- Moving servers: copy the contents of `/data` from the old server to the new
  volume and point DNS to the new server; since the address stays the same,
  agents reconnect without being reinstalled (the IP lock checks the agent's
  IP, not the main server's).

### Images and system requirements

| Tag (`kadirsungurlu/bekci`) | Contents |
|---|---|
| `:latest` | SQLite (recommended, lightest) |
| `:postgres` | Embedded PostgreSQL 18 (data still under `/data`) |
| `:1.2.3`, `:1.2` / `:1.2.3-postgres`, `:1.2-postgres` | A specific version (for pinning / rollback) |
| `:<short-sha>` / `:postgres-<short-sha>` | A specific commit |

Data cannot be moved between the two images; pick one from the start. Minimum
1 vCPU, 512 MB RAM (1 GB recommended), 10 GB disk. The app uses ~15 MB RAM with
10 monitors and 3 servers; embedded PostgreSQL adds ~200 MB. The image is for
linux/amd64 and linux/arm64; agent binaries for the other platforms (Linux
amd64/arm64, Windows amd64) are included.

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `BASE_URL` | — | External address, e.g. `https://uptime.kadir.app`. Used by notification links and agent install commands |
| `TZ` | `Europe/Istanbul` | Time of daily summaries and the nightly backup |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `MAX_CONCURRENT_CHECKS` | `50` | Maximum number of checks running at the same time |
| `DATA_DIR` | `/data` | Folder for the SQLite database and backups |
| `DATABASE_URL` | — | If set, **PostgreSQL** is used: `postgres://kullanıcı:şifre@sunucu:5432/veritabanı?sslmode=disable` (user:password@host:5432/database). Backups must then be taken on the database side (Coolify backups / `pg_dump`) |
| `ADDR` | `:8080` | Listen address |
| `AGENT_DIR` | `/usr/local/share/uptime/agents` | Agent binaries for other platforms (`uptime-windows-amd64.exe`, `uptime-linux-arm64`); included in the image |
| `PROBE_IMAGE` | — | If set, the check location install command uses this Docker image instead of downloading the binary |
| `UPTIME_LOCK_WAIT` | `600` | Maximum number of seconds a second copy waits for the data folder lock (then exits with an error) |

## Updates, backups and rollback

- **Nightly backup** (SQLite): taken every night under `/data/backups/`, old
  ones are rotated. Copy them off the server as well.
- **Automatic pre-update backup** (SQLite): if a new version changes the
  database schema, a copy
  `/data/backups/pre-migrate-v<eski>-to-v<yeni>-<zaman>.db`
  (`<old>`, `<new>`, `<time>`) is taken first. If the backup cannot be taken
  (e.g. disk full), the update is not applied and the app does not start; it
  starts once space is freed. These files are never deleted automatically.
- **Version pinning / rollback** (Compose): `UPTIME_TAG=1.2.3` in `.env`
  (or `1.2.3-postgres`). If something goes wrong, go back to the old tag; if
  the new version upgraded the database, restore the backup first.
- **PostgreSQL major version upgrade** (`:postgres` image): if the data folder
  was created by another major version, the container does not start and
  writes the steps to the log. In short: go back to the old image →
  `docker exec <c> pg_dump -h /run/postgresql -U postgres --format=custom --file=/data/backups/tasima.dump uptime`
  → rename the `/data/postgres` folder → start the new image →
  `docker exec <c> pg_restore -h /run/postgresql -U postgres --clean --if-exists --no-owner -d uptime /data/backups/tasima.dump`
  → restart.
- With the `:postgres` image, two containers cannot use the same `/data` at
  the same time; if PostgreSQL or the app exits unexpectedly, the container
  exits too and is restarted.

## Server agent and check location

The same `uptime` binary runs in two different roles; they are separate
records in the panel and do not show up on each other's screens:

| | Server agent | Check location |
|---|---|---|
| In the panel | **Sunucular** (Servers) → **Sunucu ekle** (Add server) | **Ayarlar → Kontrol noktaları** (Settings → Check locations) → **Yeni kontrol noktası** (New check location) |
| Role | Sends the server's CPU/RAM/disk/network/Docker metrics | Checks the assigned monitors from its own location and sends the results |
| Install | Docker, Linux (systemd), Windows | Docker |
| Docker container / volume | `uptime-agent` / `uptime-agent-bin` | `uptime-probe` / `uptime-probe-bin` |
| Config file (token) | `/etc/uptime-agent.env` | `/etc/uptime-probe.env` |

The agent uses no database; if it cannot reach the main server, it holds the
results and sends them later. Supported platforms: Linux amd64 and arm64,
Windows amd64.

### Install commands

Always copy the command **from the panel**: the server address, the token and
the SHA-256 digest of the binary are embedded in it.

**Linux (Docker and systemd):** the command spans multiple lines and is pasted
as **root** (if you are not root, change the first line to
`sudo sh <<'UPTIME_KURULUM'`). Its structure:

```bash
sh <<'UPTIME_KURULUM'
set -e
# root kontrolü
install -m 600 /dev/null /etc/uptime-agent.env      # token yalnızca root'a okunur
cat > /etc/uptime-agent.env <<'UPTIME_ENV'
PROBE_SERVER=https://uptime.kadir.app
PROBE_TOKEN=upr_…
UPTIME_ENV
docker run -d --name uptime-agent --restart unless-stopped … --env-file /etc/uptime-agent.env -v uptime-agent-bin:/opt/uptime alpine:3 sh -c '…'
UPTIME_KURULUM
```

(Comments: `# root kontrolü` = root check; `# token yalnızca root'a okunur` =
the token is readable by root only.)

- The token never appears on any process's command line (`ps`); it only lives
  in the config file with 0600 permissions.
- The binary is downloaded for the server's architecture (`uname -m`: x86_64 /
  aarch64) and verified against the **SHA-256** in the command; if it does not
  match, it is not installed.
- Docker containers run with reduced privileges (`--cap-drop ALL`,
  `no-new-privileges`, 256 MB memory, process limit).
- **systemd** install: binary `/usr/local/bin/uptime`, service
  `uptime-agent` (`/etc/systemd/system/uptime-agent.service`), config
  `/etc/uptime-agent.env`. Status: `systemctl status uptime-agent`, logs:
  `journalctl -u uptime-agent`.
- If the main server uses `http://`, `PROBE_ALLOW_INSECURE=1` is added to the
  config file (otherwise the agent refuses the unencrypted connection).

**Windows:** open PowerShell with **Run as administrator** and paste the
command from the Windows tab in the panel.

- Binary `C:\Program Files\Uptime\uptime.exe`, service `uptime-agent`
  (starts automatically, restarts on failure).
- Config (token) `C:\Program Files\Uptime\agent.env`, log `agent.log` in the
  same folder; only SYSTEM and Administrators can access the folder. Startup
  errors are also written to the Windows Event Log (Application, source
  `uptime-agent`).
- On Windows the load average is approximate (processor queue); temperature
  and Docker containers are not collected.

### Updating the agent

The agent does **not update itself** (pinned version): the binary is
downloaded and verified once, and the same binary is used across restarts.
This way, even if the main server is compromised, no new binary lands on your
servers by itself. To update, get the **current** install command from the
panel and run it again:

- **Docker:** remove the old one first, then run the new command:
  ```bash
  docker rm -f uptime-agent; docker volume rm uptime-agent-bin     # sunucu ajanı
  docker rm -f uptime-probe; docker volume rm uptime-probe-bin     # kontrol noktası
  ```
  (first line: server agent; second line: check location)
- **systemd** and **Windows:** run the new command directly (binary and
  service are refreshed).

Since the token is shown only once, you need a **new token** from the panel to
see the command again; the old token becomes invalid.

### Removing the agent

First delete the server / check location in the panel (the token becomes
invalid), then on the server:

**Docker — server agent**
```bash
docker rm -f uptime-agent
docker volume rm uptime-agent-bin
rm -f /etc/uptime-agent.env
```

**Docker — check location**
```bash
docker rm -f uptime-probe
docker volume rm uptime-probe-bin
rm -f /etc/uptime-probe.env
```

**Linux (systemd)**
```bash
systemctl disable --now uptime-agent
rm -f /etc/systemd/system/uptime-agent.service /usr/local/bin/uptime /etc/uptime-agent.env
systemctl daemon-reload
```

**Windows** (Administrator PowerShell)
```powershell
& "$env:ProgramFiles\Uptime\uptime.exe" service uninstall
Remove-Item -Recurse -Force "$env:ProgramFiles\Uptime"
```

`service uninstall` stops and deletes the service and removes the config file
(token) and the Event Log source; the second line deletes the binary and the
logs. A leftover `C:\ProgramData\Uptime\agent.env` from older versions is also
deleted on install and removal.

### IP lock and token

- The agent connects only with its own token (`upr_…`); the token is stored on
  the server as a hash.
- **IP lock** (on by default for new agents): the IP of the first connection
  is pinned — the full address for IPv4, the /64 block for IPv6 (each
  separately; dual-stack servers have no issues). Requests from any other IP
  are rejected (403); even a stolen token cannot be used from another machine.
- If the server's IP changes, **reset the IP lock** in the settings of that
  server / check location in the panel; the next connection pins the new IP.
  The lock can be turned off in the same place.
- If a token leaks, get a **new token** from the panel and run the install
  command again.

### Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `403` in the agent log | IP lock (the server's IP changed → reset the IP lock in the panel) or the agent is disabled in the panel |
| `401` in the agent log | Invalid token (deleted or regenerated) → get a new token from the panel and run the command again |
| `Program özeti uyuşmuyor` (binary digest mismatch) | The command is from an older version (the main server was updated) → get the current command from the panel; on Docker, remove the container and volume first |
| "konteyner içinde /proc bağlanmamış" (/proc not mounted inside the container) instead of metrics | The agent runs in Docker without the host mounts → use the command exactly as given in the panel |
| The agent rejects an `http://` address | Put the main server behind HTTPS or add `PROBE_ALLOW_INSECURE=1` to the config file |

Logs: `docker logs uptime-agent` (or `uptime-probe`),
`journalctl -u uptime-agent`, on Windows `C:\Program Files\Uptime\agent.log`.

## Push monitor

A cron job or script calls this address at the configured interval; if the
call does not arrive, the monitor goes DOWN:

```bash
curl -fsS "https://uptime.kadir.app/api/push/<token>?status=up&msg=tamam&ping=120"
```

Failures can also be reported with `status=down`.

## Password reset

If you cannot log in (same for both images; works while the app is running):

```bash
docker exec -it <container> uptime sifre-sifirla <kullanıcı-adı>
```

With Compose: `docker compose exec -it bekci uptime sifre-sifirla <kullanıcı-adı>`
(`<kullanıcı-adı>` = username). The new password is read from standard input;
all sessions are logged out. To also turn off two-factor authentication, append
`--2fa-kapat`.

## Security notes

- Passwords are stored with bcrypt, agent tokens and API keys as hashes;
  agents have an IP lock and SHA-256 version pinning.
- HSTS is sent on HTTPS requests; the Caddy in Compose sends it too.
- Monitors may also check internal network addresses (on purpose; to monitor
  internal services). Data leakage is closed off instead: outage captures
  (request/response) are visible to admins only, response headers are limited,
  and error messages never show the remote server's response body.
- Stored secret fields (password, token, webhook address) are returned masked
  to the UI; if the target address or a field tied to it changes, the secret
  must be entered again (so an existing password cannot be moved to another
  target).
- Customer accounts only see the monitors and servers assigned to them; access
  to the live stream (SSE) is re-validated regularly.

## Development

> Architecture, data model and design decisions (in Turkish):
> [docs/PLAN.md](docs/PLAN.md).

```bash
# Go testleri (sunucuya Go kurmadan, geçici container'da)
docker run --rm -v "$PWD":/src -w /src golang:1.27 go test -race ./...

# Aynı testler PostgreSQL'e karşı (her test kendi geçici şemasında)
UPTIME_TEST_PG='postgres://postgres:parola@pg:5432/postgres?sslmode=disable' go test -race ./...

# Arayüz
cd web && npm ci && npm run check && npm run build

# İmajlar
docker build -t uptime .
docker build -f Dockerfile.postgres -t uptime:postgres .
```

(Comments, in order: Go tests in a throwaway container without installing Go;
the same tests against PostgreSQL, each test in its own temporary schema; the
web UI; the images.)

CI (`.github/workflows/imajlar.yml`) runs on every push (`gelistirme`, `main`,
`v*` tags) and PR: Go tests (SQLite and PostgreSQL, `-race`), a Windows build
check, `svelte-check`, and smoke tests for both images. Images are published
only from `main` and `v*` tags, once everything passes, to Docker Hub
(`kadirsungurlu/bekci`) and GitHub Container Registry as amd64 + arm64.

To publish a new release:

```bash
git tag v1.2.3 && git push origin v1.2.3
```

## License

[GNU AGPL-3.0](LICENSE). You are free to use, modify and distribute Bekci; if you offer a modified version as a network service, you must share its source code under the same license.
