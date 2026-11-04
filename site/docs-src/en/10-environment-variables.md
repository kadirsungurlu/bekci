---
id: env
title: Environment variables
nav: Environment variables
description: Every environment variable that configures Bekci and its agents, with default values and how to set them.
section: Maintenance and reference
order: 10
slug: environment-variables
---

Most of Bekci's settings are made in the panel. A few server-level settings are passed as environment variables when the container starts. None of them is required; we only recommend setting `BASE_URL`.

## How to set them {#how}

:::tabs key=install label="Install path"
@tab Docker Compose
Add them under `environment:` in `docker-compose.yml`, then run `docker compose up -d`:

```yaml title="/opt/bekci/docker-compose.yml (excerpt)"
    environment:
      BASE_URL: https://⟦bekci.example.com⟧
      TZ: Europe/Istanbul
      LOG_LEVEL: info
```

In the [Caddy install](/en/docs/install/caddy/) you change `BASE_URL`, `TZ`, `LOG_LEVEL` and `MAX_CONCURRENT_CHECKS` in the `.env` file.
@tab docker run
Add one `-e NAME=value` per variable. Variables are read when the container is created, so you need to delete the container and recreate it with the same volume ([example](/en/docs/install/docker/#base-url)).
@tab Coolify
Add them under the application's **Environment Variables** and redeploy.
:::

> [!CHECK]
> `docker exec ⟦bekci⟧ printenv BASE_URL` should print the value you set (with Compose the container is named `bekci-bekci-1`).

## Bekci (the main application) {#app}

| Variable | Default | Description |
|---|---|---|
| `BASE_URL` | — | The address the panel is reached at from outside, e.g. `https://bekci.example.com`. Links in notifications and server agent install commands use it. If empty, install commands use the address the request came in on. A status page custom domain can't be the same as this address. |
| `TZ` | `Europe/Istanbul` | Time zone: daily summaries and the time of the nightly backup follow it. E.g. `Europe/Berlin`, `UTC`. |
| `LOG_LEVEL` | `info` | Log detail: `debug`, `info`, `warn` or `error`. |
| `MAX_CONCURRENT_CHECKS` | `50` | The maximum number of checks running at the same time. Can be raised for hundreds of monitors. |
| `BEAT_BATCH_MS` | `250` | Check results arriving within this window (milliseconds) are written in a single transaction. Lowers disk writes with many short-interval monitors (5-15 seconds); 0 writes every result separately. |
| `READ_CACHE_SEC` | `15` | TimescaleDB mode only: summary reads that scan all monitors (monitor list, overview, `/metrics`) are shared for this many seconds. The running hour is aggregated from raw data, so the cost grows with the monitor count. `0` disables it. |
| `DATABASE_URL` | — | Database address; required for PostgreSQL + TimescaleDB (the default compose sets it). If unset, the deprecated SQLite is used: `postgres://user:password@host:5432/database?sslmode=disable`. Must start with `postgres://` or `postgresql://`. Bekci then takes no nightly backup; back up on the database side. Ignored by the `postgres` image (it always uses its embedded database). |
| `DB_TIMESCALE` | `auto` | `off`: use the plain PostgreSQL path even if the extension is installed. An existing conversion isn't undone. |
| `SQLITE_IGNORE` | — | `1`: allow starting on an empty PostgreSQL while unmigrated SQLite is in the data folder ([migration](/en/docs/database/#sqlite)). |
| `DATA_DIR` | `/data` | Folder for the SQLite database and backups. No need to change it in Docker; mount the volume at `/data`. |
| `ADDR` | `:8080` | Address and port to listen on inside the container. |
| `TRUSTED_PROXY` | — | Trusted reverse proxy networks as a comma-separated CIDR list (e.g. `172.18.0.0/16,10.0.0.0/8`). When set, the `X-Forwarded-For` header is only trusted on connections from these networks. When empty, all private and local addresses are trusted and a warning is logged at startup; for an install without a proxy, set `127.0.0.1/32` ([details](/en/docs/install/reverse-proxy/#trusted-proxy)). |
| `UPDATE_CHECK` | on | `off` (or `0`, `false`) turns off the new version check: no request leaves the server (offline installs). When on, the panel looks at the releases on GitHub every 6 hours and tells admins at the top of the panel; it can also be turned off in Settings > General. |
| `UPTIME_LOCK_WAIT` | `600` | How many seconds a second copy using the same data folder (SQLite) or the same database (PostgreSQL) waits at most for the first one to stop. When the time is up it exits with an error. Only one copy (replica) runs per database. |
| `AGENT_DIR` | `/usr/local/share/uptime/agents` | Folder with the agent programs for other platforms. Ready in the official image; don't change it. |
| `PROBE_IMAGE` | — | If set, the check location install command uses this Docker image instead of downloading the program. |

## Server agent and check location {#agent}

The install command from the panel sets these for you (in `/etc/uptime-agent.env`, `/etc/uptime-probe.env` or, on Windows, `C:\Program Files\Uptime\agent.env`). Normally you don't need to touch them.

| Variable | Default | Description |
|---|---|---|
| `PROBE_SERVER` | — | The panel's address (`BASE_URL`), e.g. `https://bekci.example.com`. |
| `PROBE_TOKEN` | — | The token given by the panel (starts with `upr_`). |
| `PROBE_ALLOW_INSECURE` | — | `1` allows connecting to an unencrypted `http://` address (not recommended). |
| `MAX_CONCURRENT_CHECKS` | `20` | The maximum number of simultaneous checks on a check location. |
| `METRICS` | `1` | `0` turns off collecting server metrics entirely. |
| `AUTO_UPDATE` | `1` | `0` stops the agent from updating itself to the signed version the panel offers ([updating](/en/docs/server-agent/#update)). |
| `PROBE_STATE_DIR` | program directory | Directory that stores the panel's last known IP addresses (`uptime.dns.json`); if DNS fails the agent connects with that IP ([DNS outage](/en/docs/server-agent/#dns-cache)). |
| `PROBE_OOB_IFACE` | — | Network interface of the second line (OOB), e.g. `eth1`. If empty it is found from the OOB IP entered in the panel; needed when the OOB IP is behind NAT ([OOB](/en/docs/server-agent/#oob)). |
| `MESH_ADDR` | `:7480` | TCP listen address for connections between check locations (mesh); `-` turns listening off and the agent only connects to listening peers itself ([mesh](/en/docs/server-agent/#mesh)). |
| `ADDR` | `:8080` | The agent's health check address; `-` turns it off (install commands use `-`). |
| `HOST_PROC`, `HOST_SYS`, `HOST_ETC`, `HOST_ROOT` | — | Where the server's `/proc`, `/sys`, `/etc` and root directory are mounted when the agent runs in Docker. The Docker install command sets them. |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker API address for container metrics. |

## The .env file of the Caddy install {#caddy-env}

The `/opt/bekci/.env` file of the [Caddy install](/en/docs/install/caddy/) reads these values:

| Variable | Description |
|---|---|
| `UPTIME_DOMAIN` | The panel's domain (without `https://`). `BASE_URL` is built from it. Required. |
| `ACME_EMAIL` | Email for Let's Encrypt notices. Required. |
| `UPTIME_TAG` | Image tag: `latest`, `1.0.0`… (`postgres`, `1.0.0-postgres` are deprecated). Default `latest`. |
| `STATUS_DOMAIN` | No longer needed: status page custom domains get HTTPS automatically. Only used by an older, hand-written `{$STATUS_DOMAIN}` Caddy block ([details](/en/docs/install/caddy/#status-domain-migrate)). |
| `TZ`, `LOG_LEVEL`, `MAX_CONCURRENT_CHECKS` | Same as in the table above. |
