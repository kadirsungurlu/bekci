---
id: caddy
title: HTTPS install (Caddy)
nav: HTTPS install (Caddy)
description: Install Bekci on an empty server with your own domain and an automatic Let's Encrypt certificate. Exactly which file goes where, step by step.
section: Install
order: 4
slug: install/caddy
---

By the end of this guide Bekci will run on your own domain, such as `https://bekci.example.com`, with a valid HTTPS certificate. The [Caddy](https://caddyserver.com) web server obtains the certificate from Let's Encrypt and renews it before it expires; you don't have to do anything. Total time: about 10 minutes (plus waiting for DNS to propagate).

> [!CAUTION] This guide is for an empty server
> Caddy uses the server's ports **80 and 443**. If Coolify, Nginx, Apache or another web server already runs on your server, **don't follow** this guide: go to [Coolify](/en/docs/install/coolify/) or [behind a reverse proxy](/en/docs/install/reverse-proxy/) instead. If you're not sure, do the [check in step 3](#ports).

## What you need {#needs}

- A Linux server with a public IPv4 address ([requirements](/en/docs/#requirements)).
- A domain or subdomain, e.g. `bekci.example.com`, and access to its DNS settings (at your domain registrar or Cloudflare).
- An email address for certificate notices.

## The files you'll end up with {#file-layout}

Your server will have one folder with three files in it. You'll only edit one of them (`.env`):

```text title="Folder layout"
/opt/bekci/
├── docker-compose.yml   ← Bekci + Caddy definition    (use as is)
├── Caddyfile            ← Caddy's configuration       (use as is)
└── .env                 ← your domain and email       (the only file you edit)
```

The name `.env` starts with a dot, so `ls` doesn't show it; `ls -la` does.

## 1. Add the DNS record {#dns}

In your domain's DNS settings, create a new **A** record:

| Field | Value |
|---|---|
| Type | `A` |
| Name / Host | `bekci` — for `bekci.example.com`. For the domain itself (`example.com`) use `@` |
| Value / IPv4 address | Your server's public IPv4 address, e.g. `203.0.113.10` |
| TTL | Auto or 300 |

> [!IMPORTANT] If you use Cloudflare
> Add the record as **DNS only** (grey cloud), with the orange cloud (**Proxied**) turned off. Once the certificate has been issued you can turn the proxy on if you like; see [below](#cloudflare).

Your hosting provider's dashboard shows the server's public IP address; the first address printed by `hostname -I` on the server is usually the same. If you use IPv6 you can also add an **AAAA** record with the same name; if you do, make sure the server is really reachable over IPv6.

> [!CHECK]
> DNS changes can take a few minutes to propagate. On the server, this command should print your server's IP address:
>
> ```bash
> getent ahostsv4 ⟦bekci.example.com⟧ | head -n 1
> ```
>
> If it prints nothing or another address, wait a few minutes and try again. With the Cloudflare proxy on you'll see Cloudflare's addresses here.

## 2. Install Docker {#docker}

If Docker isn't installed, follow [the first step of the Docker guide](/en/docs/install/docker/#install-docker) (the official script installs the Compose plugin too).

> [!CHECK]
> `docker compose version` should print a version number.

## 3. Check ports 80 and 443 {#ports}

First make sure the ports are free:

```bash
ss -ltnp 'sport = :80'
ss -ltnp 'sport = :443'
```

> [!CHECK]
> Both commands should print only the header line (`State  Recv-Q  Send-Q …`). If there's a line below it, another program is using that port: a part such as `users:(("nginx",…))` tells you which one. In that case see [the problems section](#port-taken).

Then make sure the ports are reachable from outside:

- **Your hosting provider's firewall** (Hetzner, DigitalOcean, AWS “security group”, etc.): allow incoming **80/TCP**, **443/TCP** and optionally **443/UDP** (HTTP/3).
- If you use **ufw** (`ufw status` says “Status: active”), add the rules; don't forget SSH:

```bash
ufw allow OpenSSH
ufw allow 80/tcp
ufw allow 443/tcp
ufw allow 443/udp
```

> [!NOTE]
> Docker adds firewall rules for the ports it publishes itself, so if ufw is off you don't need to turn it on for this install. What really matters is that your provider's firewall allows these ports.

## 4. Create the folder {#folder}

```bash
mkdir -p /opt/bekci
cd /opt/bekci
```

Run all later commands from inside this folder. If you close the terminal and reconnect, type `cd /opt/bekci` first.

## 5. Create the three files {#files}

You can create the files in one of two ways. Downloading is quicker and rules out copy-paste mistakes.

:::tabs key=create label="How to create the files"
@tab Download (recommended)
```bash
cd /opt/bekci
curl -fsSL -o docker-compose.yml https://bekci.app/indir/en/caddy/docker-compose.yml
curl -fsSL -o Caddyfile https://bekci.app/indir/en/caddy/Caddyfile
curl -fsSL -o .env https://bekci.app/indir/en/caddy/env.example
ls -la
```

The third command saves the example settings file under the name `.env`; the leading dot matters.
@tab Create by hand
For each file, open an empty file with `nano`, paste the content below, save with <kbd>Ctrl</kbd>+<kbd>O</kbd> and <kbd>Enter</kbd>, and exit with <kbd>Ctrl</kbd>+<kbd>X</kbd>.

**docker-compose.yml** — `nano /opt/bekci/docker-compose.yml`

```yaml title="/opt/bekci/docker-compose.yml" download="/indir/en/caddy/docker-compose.yml"
# Bekci + Caddy: install with automatic HTTPS (Let's Encrypt).
# Step-by-step guide: https://bekci.app/en/docs/install/caddy/
#
# You don't need to change this file: the domain, email and version are read
# from the .env file. All three files live in the same folder (e.g. /opt/bekci):
#   docker-compose.yml   this file
#   Caddyfile            Caddy configuration
#   .env                 your domain and email
#
# Start:     docker compose up -d
# Logs:      docker compose logs -f bekci      (for Caddy: caddy)
# Update:    docker compose pull && docker compose up -d
# Backups, version pinning, rollback: https://bekci.app/en/docs/updates-backups/

name: bekci

services:
  bekci:
    # UPTIME_TAG: latest (SQLite, recommended) | postgres (embedded PostgreSQL)
    # or a fixed version: 1.0.0 / 1.0.0-postgres. Data can't be moved between
    # the two kinds: pick one from the start.
    image: kadirsungurlu/bekci:${UPTIME_TAG:-latest}
    restart: unless-stopped
    # Give the app (and embedded PostgreSQL) time to shut down cleanly.
    stop_grace_period: 30s
    environment:
      BASE_URL: https://${UPTIME_DOMAIN:?UPTIME_DOMAIN must be set in the .env file}
      TZ: ${TZ:-Europe/Istanbul}
      LOG_LEVEL: ${LOG_LEVEL:-info}
      MAX_CONCURRENT_CHECKS: ${MAX_CONCURRENT_CHECKS:-50}
    volumes:
      - bekci-data:/data
    # Not published directly; only reachable through Caddy.
    expose:
      - "8080"
    logging:
      driver: json-file
      options:
        max-size: 10m
        max-file: "3"

  caddy:
    # caddy:2-alpine (pinned by digest)
    image: caddy:2-alpine@sha256:6aeddd44c3078b0f9a35206472a11420648a79c184603ef95957d0a20044cb2b
    restart: unless-stopped
    depends_on:
      bekci:
        condition: service_healthy
    ports:
      - "80:80"
      - "443:443"
      - "443:443/udp" # HTTP/3
    environment:
      UPTIME_DOMAIN: ${UPTIME_DOMAIN}
      ACME_EMAIL: ${ACME_EMAIL:?ACME_EMAIL must be set in the .env file}
      STATUS_DOMAIN: ${STATUS_DOMAIN:-}
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy-data:/data
      - caddy-config:/config
    logging:
      driver: json-file
      options:
        max-size: 10m
        max-file: "3"

volumes:
  bekci-data:
  caddy-data:
  caddy-config:
```

**Caddyfile** — `nano /opt/bekci/Caddyfile` (the file name is exactly `Caddyfile`: no extension, capital C)

```caddyfile title="/opt/bekci/Caddyfile" download="/indir/en/caddy/Caddyfile"
# Caddy: obtains the HTTPS certificate automatically and forwards to Bekci.
# The domains come from the .env file; you don't need to change this file.
# Guide: https://bekci.app/en/docs/install/caddy/

{
	email {$ACME_EMAIL}
}

{$UPTIME_DOMAIN} {
	encode zstd gzip
	# Browsers should only use HTTPS for this domain for one year.
	# ">" means: if the app sends the same header, keep only this one.
	header >Strict-Transport-Security "max-age=31536000"
	reverse_proxy bekci:8080 {
		# Pass the live stream (Server-Sent Events) through without buffering.
		flush_interval -1
	}
}

# Optional: a separate domain for the status page (e.g. status.example.com).
# Set STATUS_DOMAIN in .env and remove the # signs at the start of the block
# below; then enter the same name in the status page's "Custom domain" field
# in the panel. That address only serves the status page, never the admin panel.
#
# {$STATUS_DOMAIN} {
# 	encode zstd gzip
# 	header >Strict-Transport-Security "max-age=31536000"
# 	reverse_proxy bekci:8080
# }
```

**.env** — `nano /opt/bekci/.env` (you'll edit its content in the next step)

```ini title="/opt/bekci/.env" download="/indir/en/caddy/env.example"
# Bekci + Caddy settings. On the server this file must be named ".env" and sit
# in the same folder as docker-compose.yml (e.g. /opt/bekci/.env).
# Guide: https://bekci.app/en/docs/install/caddy/

# The panel's domain (without http:// and /). Its DNS A record must point here.
UPTIME_DOMAIN=⟦bekci.example.com⟧

# Your email address for Let's Encrypt certificate notices.
ACME_EMAIL=⟦admin@example.com⟧

# latest: SQLite (recommended) | postgres: embedded PostgreSQL
# To pin a version: 1.0.0 (SQLite) or 1.0.0-postgres.
UPTIME_TAG=latest

# Time zone for daily summaries and the nightly backup.
TZ=Europe/Istanbul
LOG_LEVEL=info

# Maximum number of checks at the same time (raise it for hundreds of monitors).
MAX_CONCURRENT_CHECKS=50

# Optional: a separate domain for the status page (also enable the block in the Caddyfile).
# STATUS_DOMAIN=status.example.com
```
:::

> [!CHECK]
> `ls -la /opt/bekci` should list all three files: `.env`, `Caddyfile` and `docker-compose.yml`.

## 6. Put your domain in .env {#env}

```bash
nano /opt/bekci/.env
```

Change just two lines; the rest can stay as they are:

```ini title="/opt/bekci/.env (the lines you change)"
UPTIME_DOMAIN=⟦bekci.example.com⟧
ACME_EMAIL=⟦you@example.com⟧
```

- `UPTIME_DOMAIN`: the domain you created the DNS record for in step 1. Don't add `https://` in front or `/` at the end.
- `ACME_EMAIL`: your email address, where Let's Encrypt sends certificate notices.

The other lines: `UPTIME_TAG` is the Bekci version to run ([version pinning](/en/docs/updates-backups/#pinning)), `TZ` is the time zone and `MAX_CONCURRENT_CHECKS` the maximum number of checks at the same time ([all variables](/en/docs/environment-variables/)). Set `TZ` to your own zone, e.g. `Europe/Berlin` or `UTC`.

Save and exit (<kbd>Ctrl</kbd>+<kbd>O</kbd>, <kbd>Enter</kbd>, <kbd>Ctrl</kbd>+<kbd>X</kbd>).

> [!CHECK]
> ```bash
> cd /opt/bekci
> docker compose config --quiet
> ```
>
> The command should finish without printing anything. If you see `required variable UPTIME_DOMAIN is missing a value`, the `.env` file was saved under a wrong name (e.g. `env.example` or `.env.txt`) or sits in another folder.

## 7. Start it {#start}

```bash
cd /opt/bekci
docker compose up -d
```

The first time, the images are downloaded. Caddy waits until Bekci is healthy, then starts and requests the certificate.

> [!CHECK]
> A few seconds later `docker compose ps` should list two rows: `bekci` with **STATUS** `Up … (healthy)` and `caddy` with `Up …`. The **PORTS** column of the `caddy` row shows `0.0.0.0:80->80/tcp` and `0.0.0.0:443->443/tcp`.

## 8. Verify the certificate {#certificate}

Caddy's log tells you whether it obtained the certificate:

```bash
docker compose logs caddy | grep -i "certificate obtained"
```

> [!CHECK]
> You should see a line containing `"msg":"certificate obtained successfully","identifier":"bekci.example.com"`. In your browser, `https://⟦bekci.example.com⟧` should open with the padlock icon in the address bar. You can also try from the server:
>
> ```bash
> curl -sI https://⟦bekci.example.com⟧/healthz | head -n 1
> ```
>
> The output should be `HTTP/2 200`.

If the line isn't there, wait a minute and check again. If it still isn't there, the `"level":"error"` lines in `docker compose logs caddy` tell you why; see [common problems](#problems).

## 9. Create the admin account {#admin}

Open `https://⟦bekci.example.com⟧`. On the **Welcome** screen enter a username and a password of at least 8 characters, then click **Create account**.

> [!CHECK]
> The monitor list opens. The install is done; add your first monitor with [First steps](/en/docs/first-steps/).

In this setup `BASE_URL` is built from the domain in `.env` automatically (`https://` + `UPTIME_DOMAIN`); you don't need to set it separately.

## Common problems {#problems}

### Port 80 or 443 is in use {#port-taken}

`docker compose up -d` stops with one of these errors:

```text
Bind for 0.0.0.0:80 failed: port is already allocated
```

```text
failed to bind host port 0.0.0.0:80/tcp: address already in use
```

The first means **another container** uses the port; `docker ps --filter publish=80` shows which one (on servers running Coolify it's `coolify-proxy`). The second means a program running directly on the server (usually Nginx or Apache) uses it; `ss -ltnp 'sport = :80'` shows which one.

- If you use that web server, put Bekci behind it instead of installing Caddy: [behind a reverse proxy](/en/docs/install/reverse-proxy/). With Coolify: [Coolify](/en/docs/install/coolify/).
- If you don't use it, stop and disable it, e.g. `systemctl disable --now nginx` (for Apache: `apache2`), then run `docker compose up -d` again.

### DNS hasn't propagated yet or is wrong {#dns-problem}

Caddy's log shows errors about obtaining the certificate and the browser shows a security warning. If the `getent` command from [step 1](#dns) doesn't print your server's IP, DNS isn't ready yet. Caddy retries on its own; once DNS points to the right address you can speed things up with:

```bash
docker compose restart caddy
```

### I use Cloudflare {#cloudflare}

- For the first install the record must be **DNS only** (grey cloud); otherwise Let's Encrypt's validation can get stuck at Cloudflare.
- If you turn the proxy (orange cloud) on after the certificate has been issued, set Cloudflare's **SSL/TLS** mode to **Full (strict)**. **Flexible** mode causes an endless redirect loop (`ERR_TOO_MANY_REDIRECTS`).
- Live updates work behind Cloudflare too: Bekci sends a keep-alive signal every 25 seconds to keep the connection open.
- If certificate renewal causes trouble later, temporarily switch the record back to **DNS only**.

### The browser says “not secure” {#not-secure}

The certificate hasn't been issued yet. Do the check from [step 8](#certificate). After many failed attempts Let's Encrypt refuses new requests for a while, so once you've fixed DNS and the ports, watch the log instead of running `docker compose up -d` over and over: `docker compose logs -f caddy`.

### A change in .env had no effect {#env-change}

After editing `.env`, run `docker compose up -d`; Compose recreates the changed containers. If you edited the `Caddyfile`, you also need `docker compose restart caddy`.

## A separate domain for the status page (optional) {#status-domain}

You can publish your public status page at a separate address such as `status.example.com`. That address only serves the status page, never the admin panel.

1. Add an **A** record named `status` pointing to the same server (like [step 1](#dns)).
2. In `.env`, remove the `#` at the start of the last line and fill in the domain: `STATUS_DOMAIN=⟦status.example.com⟧`
3. In the `Caddyfile`, remove the `#` signs at the start of the lines of the last block. The block should look like this:

   ```caddyfile title="/opt/bekci/Caddyfile (end of the file)"
   {$STATUS_DOMAIN} {
   	encode zstd gzip
   	header >Strict-Transport-Security "max-age=31536000"
   	reverse_proxy bekci:8080
   }
   ```

4. Apply it:

   ```bash
   cd /opt/bekci
   docker compose up -d --force-recreate caddy
   ```

5. In the panel, open your page under **Status pages**, enter `status.example.com` in the **Custom domain** field and save.

> [!CHECK]
> `https://⟦status.example.com⟧` should open your status page.

## Next steps {#next}

- [First steps](/en/docs/first-steps/): your first monitor, notification channel and status page.
- [Updates, backups and rollback](/en/docs/updates-backups/): updating is as simple as `cd /opt/bekci && docker compose pull && docker compose up -d`, but learn how to back up first.
