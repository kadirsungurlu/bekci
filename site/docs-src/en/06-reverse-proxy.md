---
id: proxy
title: Behind a reverse proxy
nav: Behind a reverse proxy
description: If your server already runs Nginx, Traefik, Apache or Caddy, put Bekci behind it. For each one - where the file goes, its full content and the settings live updates need.
section: Install
order: 6
slug: install/reverse-proxy
---

If a web server (a reverse proxy) already uses ports 80 and 443 on your server, you put Bekci behind it: the proxy provides HTTPS and forwards requests to Bekci. If you use Coolify, follow the [Coolify guide](/en/docs/install/coolify/) instead of this page.

Whichever proxy you use, it must do the following for Bekci to work properly:

| Requirement | Why |
|---|---|
| Pass the `Host`, `X-Forwarded-For` and `X-Forwarded-Proto` headers | Bekci learns from `X-Forwarded-Proto` that the request arrived over HTTPS (for the secure session cookie and HSTS). It takes the client's real IP address from `X-Forwarded-For` (for the login attempt limit and the audit log). |
| Pass the `/api/events` response through **without buffering** | The interface's live updates arrive over this long-lived connection (Server-Sent Events). If the proxy buffers the response, the panel doesn't update. |
| Not cut the long connection early | Bekci sends a keep-alive signal on this connection every 25 seconds; the read timeout must be longer. |
| Allow large request bodies | Importing from Uptime Kuma can upload files of up to 200 MB. |

## 1. Run Bekci so it only listens locally {#bekci}

If the proxy is on the same server, publish Bekci's port on `127.0.0.1` only; that way nobody can reach Bekci from outside by going around the proxy. With [Docker Compose](/en/docs/install/docker-compose/):

```yaml title="/opt/bekci/docker-compose.yml"
name: bekci
services:
  bekci:
    image: kadirsungurlu/bekci:latest
    restart: unless-stopped
    stop_grace_period: 30s
    ports:
      - "127.0.0.1:8080:8080"   # reachable from this server only
    environment:
      BASE_URL: https://⟦bekci.example.com⟧
      TZ: Europe/Istanbul
    volumes:
      - bekci-data:/data
volumes:
  bekci-data:
```

```bash
cd /opt/bekci
docker compose up -d
```

`BASE_URL` must be the HTTPS address the panel will be opened at. (If you use Traefik with Docker labels, use the file from the **Traefik** tab below instead.)

> [!CHECK]
> On the server, `curl http://127.0.0.1:8080/healthz` should print `ok`.

## 2. Configure the proxy {#proxy}

Pick the tab for your proxy. Each tab shows where the file goes, its full content and how to apply it.

:::tabs label="Reverse proxy"
@tab Nginx
**Where the file goes:** on Debian and Ubuntu, `/etc/nginx/sites-available/bekci`. RHEL, AlmaLinux, Rocky and the nginx.org packages have no `sites-available` folder; create the file as `/etc/nginx/conf.d/bekci.conf` instead.

```bash
nano /etc/nginx/sites-available/bekci
```

```nginx title="/etc/nginx/sites-available/bekci"
server {
    listen 80;
    listen [::]:80;
    server_name ⟦bekci.example.com⟧;

    # Large files for backup restores and Uptime Kuma imports
    client_max_body_size 200m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Connection "";

        # Live updates (SSE): pass through unbuffered, don't cut the long connection
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 1h;
    }
}
```

Enable the site (Debian/Ubuntu only), test the configuration and reload Nginx:

```bash
ln -s /etc/nginx/sites-available/bekci /etc/nginx/sites-enabled/bekci
nginx -t
systemctl reload nginx
```

`nginx -t` should print two lines: `syntax is ok` and `test is successful`.

**HTTPS:** if you don't have a certificate yet, Certbot is the easiest way. On Ubuntu/Debian: `apt install certbot python3-certbot-nginx`, then `certbot --nginx -d ⟦bekci.example.com⟧`. Certbot adds port 443 and the certificate lines to this file itself; the `location` settings above stay as they are.
@tab Traefik
If you use Traefik with Docker labels, add Bekci to the Docker network Traefik is attached to and describe it with labels. You don't need to publish a port then. Use this file instead of the one from [step 1](#bekci):

```yaml title="/opt/bekci/docker-compose.yml"
name: bekci
services:
  bekci:
    image: kadirsungurlu/bekci:latest
    restart: unless-stopped
    stop_grace_period: 30s
    environment:
      BASE_URL: https://⟦bekci.example.com⟧
      TZ: Europe/Istanbul
    volumes:
      - bekci-data:/data
    networks:
      - ⟦proxy⟧
    labels:
      - traefik.enable=true
      - traefik.http.routers.bekci.rule=Host(`⟦bekci.example.com⟧`)
      - traefik.http.routers.bekci.entrypoints=⟦websecure⟧
      - traefik.http.routers.bekci.tls.certresolver=⟦letsencrypt⟧
      - traefik.http.services.bekci.loadbalancer.server.port=8080
networks:
  ⟦proxy⟧:
    external: true
volumes:
  bekci-data:
```

Replace the highlighted names with the ones from your own Traefik setup:

- `proxy`: the Docker network the Traefik container is attached to (`docker network ls` lists networks).
- `websecure`: the name of the entrypoint you defined for port 443.
- `letsencrypt`: the name of the certificate resolver (certresolver) in your Traefik configuration.

```bash
cd /opt/bekci
docker compose up -d
```

Traefik doesn't buffer responses and adds the `X-Forwarded-*` headers itself; live updates need no extra settings.
@tab Apache
Enable the required modules (Debian/Ubuntu):

```bash
a2enmod proxy proxy_http headers
```

**Where the file goes:** on Debian and Ubuntu, `/etc/apache2/sites-available/bekci.conf`. On the RHEL family, `/etc/httpd/conf.d/bekci.conf`.

```bash
nano /etc/apache2/sites-available/bekci.conf
```

```apache title="/etc/apache2/sites-available/bekci.conf"
<VirtualHost *:80>
    ServerName ⟦bekci.example.com⟧

    ProxyPreserveHost On
    ProxyRequests Off
    RequestHeader set X-Forwarded-Proto expr=%{REQUEST_SCHEME}

    # Live updates (SSE): pass through immediately, don't cut the long connection
    ProxyPass        / http://127.0.0.1:8080/ flushpackets=on timeout=3600
    ProxyPassReverse / http://127.0.0.1:8080/
</VirtualHost>
```

Enable the site, test the configuration and reload:

```bash
a2ensite bekci
apachectl configtest
systemctl reload apache2
```

The last line of `apachectl configtest` should be `Syntax OK`.

**HTTPS:** on Ubuntu/Debian `apt install certbot python3-certbot-apache`, then `certbot --apache -d ⟦bekci.example.com⟧`. Certbot creates a copy of this site for port 443; there `X-Forwarded-Proto` automatically becomes `https`.
@tab Caddy
If Caddy is installed on your server as a package, its configuration file is `/etc/caddy/Caddyfile`. Add this block at the end of the file:

```bash
nano /etc/caddy/Caddyfile
```

```caddyfile title="/etc/caddy/Caddyfile (add at the end)"
⟦bekci.example.com⟧ {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8080 {
		# Live updates (SSE): pass through without buffering
		flush_interval -1
	}
}
```

Validate the configuration and reload Caddy:

```bash
caddy validate --config /etc/caddy/Caddyfile
systemctl reload caddy
```

The output of `caddy validate` should end with `Valid configuration`. Caddy obtains the certificate itself; there is nothing else to do.

If your Caddy runs in Docker, add Bekci to the same Docker network as Caddy and write `bekci:8080` instead of `127.0.0.1:8080` (the files in the [Caddy install](/en/docs/install/caddy/) are a complete example of this).
:::

## 3. Verify {#verify}

1. Open `https://⟦bekci.example.com⟧` in your browser and sign in (if it's the first time, create your admin account on the **Welcome** screen).
2. Test live updates: add a monitor with a short check interval (e.g. 20 seconds) and stay on the monitor list. After every check, the monitor's status bars and response time should update without reloading the page.

> [!CHECK]
> You can sign in and there's no **Offline** notice at the top of the page. If that notice keeps showing, the live stream is getting stuck in the proxy; see [below](#no-live-updates).

## Trusted proxy (TRUSTED_PROXY) {#trusted-proxy}

Bekci only believes the client address in the `X-Forwarded-For` header if the connection delivering the request comes from a trusted address. By default all private (`10.x`, `172.16–31.x`, `192.168.x`) and local (`127.x`) addresses are trusted; if the proxy runs on the same server or in the same Docker network, no setting is needed.

To be stricter, list the trusted networks in the `TRUSTED_PROXY` environment variable, separated by commas, e.g. `TRUSTED_PROXY=172.17.0.1/32`. When it's set, only headers from these networks are trusted.

## Common problems {#problems}

### Live updates don't arrive, “Offline” notice {#no-live-updates}

The proxy buffers the `/api/events` response or cuts the connection early. Check these lines in the configurations above: `proxy_buffering off` and `proxy_read_timeout` for Nginx, `flushpackets=on` for Apache, `flush_interval -1` for Caddy. If there's another layer in between, such as Cloudflare, it mustn't buffer the connection either.

### 502 Bad Gateway {#502}

The proxy can't reach Bekci. Is Bekci running (`docker compose ps`), is the port right (`curl http://127.0.0.1:8080/healthz`)? With Traefik, Bekci and Traefik must be on the same Docker network.

### “413 Request Entity Too Large” when importing {#413}

The proxy's request body limit is too low. In Nginx add `client_max_body_size 200m;`.

### I sign in but get signed out right away, or an endless redirect {#redirects}

The `X-Forwarded-Proto` header isn't passed or is wrong. If the proxy terminates HTTPS, the header must be `https`. If you use Cloudflare, set the SSL/TLS mode to **Full (strict)**; **Flexible** mode causes a redirect loop.

### The address in notifications and agent commands is wrong {#wrong-address}

`BASE_URL` isn't set or is wrong. Set it to the `https://` address the panel is opened at and recreate the container (`docker compose up -d`).
