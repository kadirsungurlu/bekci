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
| `DATABASE_URL` | — | If set, **PostgreSQL** is used instead of SQLite: `postgres://user:password@host:5432/database?sslmode=disable`. Must start with `postgres://` or `postgresql://`. Bekci then takes no nightly backup; back up on the database side. Ignored by the `postgres` image (it always uses its embedded database). |
| `DATA_DIR` | `/data` | Folder for the SQLite database and backups. No need to change it in Docker; mount the volume at `/data`. |
| `ADDR` | `:8080` | Address and port to listen on inside the container. |
| `TRUSTED_PROXY` | — | Trusted reverse proxy networks as a comma-separated CIDR list (e.g. `172.17.0.1/32,10.0.0.0/8`). When set, the `X-Forwarded-For` header is only trusted on connections from these networks. When empty, all private and local addresses are trusted and a warning is logged at startup; for an install without a proxy, set `127.0.0.1/32` ([details](/en/docs/install/reverse-proxy/#trusted-proxy)). |
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
| `ADDR` | `:8080` | The agent's health check address; `-` turns it off (install commands use `-`). |
| `HOST_PROC`, `HOST_SYS`, `HOST_ETC`, `HOST_ROOT` | — | Where the server's `/proc`, `/sys`, `/etc` and root directory are mounted when the agent runs in Docker. The Docker install command sets them. |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker API address for container metrics. |

## The .env file of the Caddy install {#caddy-env}

The `/opt/bekci/.env` file of the [Caddy install](/en/docs/install/caddy/) reads these values:

| Variable | Description |
|---|---|
| `UPTIME_DOMAIN` | The panel's domain (without `https://`). `BASE_URL` is built from it. Required. |
| `ACME_EMAIL` | Email for Let's Encrypt notices. Required. |
| `UPTIME_TAG` | Image tag: `latest`, `postgres`, `1.0.0`, `1.0.0-postgres`… Default `latest`. |
| `STATUS_DOMAIN` | Optional: a separate domain for the status page ([details](/en/docs/install/caddy/#status-domain)). |
| `TZ`, `LOG_LEVEL`, `MAX_CONCURRENT_CHECKS` | Same as in the table above. |
