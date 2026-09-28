---
id: troubleshooting
title: Troubleshooting and FAQ
nav: Troubleshooting and FAQ
description: Password reset, health check, logs, ports in use, live updates not arriving, time zones and other frequently asked questions.
section: Maintenance and reference
order: 11
slug: troubleshooting
---

## Find the container name {#container-name}

The commands on this page use the name of the Bekci container. It depends on how you installed:

| Install | Container name |
|---|---|
| [Docker (one command)](/en/docs/install/docker/) | `bekci` |
| [Docker Compose](/en/docs/install/docker-compose/) and the [Caddy install](/en/docs/install/caddy/) | `bekci-bekci-1` (or `docker compose exec bekci …` from inside `/opt/bekci`) |
| Coolify | The application's random name; the row in `docker ps` that uses the `kadirsungurlu/bekci` image |

```bash
docker ps --filter ancestor=kadirsungurlu/bekci:latest
```

(If you use a fixed version, write that tag instead of `latest`, e.g. `kadirsungurlu/bekci:1.0.0`.)

## I forgot my password {#password-reset}

If you can't sign in, you can reset the password from the server. Run this while Bekci is running, with your own username instead of `admin`:

:::tabs key=install label="Install path"
@tab Docker Compose
```bash
cd /opt/bekci
docker compose exec bekci uptime sifre-sifirla ⟦admin⟧
```
@tab docker run
```bash
docker exec -it bekci uptime sifre-sifirla ⟦admin⟧
```
@tab Coolify
Open the application's **Terminal** section in Coolify and run:

```bash
uptime sifre-sifirla ⟦admin⟧
```

Or, on the server, `docker exec -it ⟦container-name⟧ uptime sifre-sifirla ⟦admin⟧`.
:::

(`sifre-sifirla` is Turkish for “reset password”.) The command asks `Yeni şifre:` (“New password:”); type the new password (at least 8 characters) and press <kbd>Enter</kbd>. The password is visible while you type it, so mind who's watching.

> [!CHECK]
> You'll see `Şifre değiştirildi, tüm oturumlar kapatıldı.` (“Password changed, all sessions signed out.”) and can sign in with the new password. `hata: kullanıcı bulunamadı` (“error: user not found”) means the username is misspelled.

**To turn off two-factor authentication as well** (if you lost both your phone and your recovery codes), add `--2fa-kapat` at the end:

```bash
docker compose exec bekci uptime sifre-sifirla ⟦admin⟧ --2fa-kapat
```

The command works the same way with both images (SQLite and PostgreSQL).

## Is Bekci running? {#health}

The health check address is `/healthz`; it returns `ok` when the database is reachable:

```bash
curl http://localhost:8080/healthz          # from inside the server (if the port is published)
curl https://⟦bekci.example.com⟧/healthz          # from outside
```

The **STATUS** column of `docker ps` also shows the image's own health check: `(healthy)` means all is well, `(unhealthy)` means there's a problem. If the database can't be reached, `/healthz` returns `503` with `veritabanı erişilemiyor` (“database unreachable”).

## Logs {#logs}

```bash
docker logs --tail 100 bekci                 # docker run
cd /opt/bekci && docker compose logs --tail 100 bekci   # Compose
```

On Coolify, look at the application's **Logs**. For more detail set `LOG_LEVEL=debug` and recreate the container ([environment variables](/en/docs/environment-variables/)). The logs are written in Turkish; after a failed start the last line begins with `hata:` (“error:”) and gives the reason.

## The container waits, saying the data is “open in another instance” {#lock}

If you see this in the log:

```text
level=WARN msg="veri klasörü başka bir örnekte açık; o kapanana kadar bekleniyor" kilit=/data/uptime.db.lock en_fazla=10m0s
level=WARN msg="hâlâ bekleniyor: diğer örnek veri klasörünü bırakmadı" gecen=30s
```

(“the data folder is open in another instance; waiting until it closes” / “still waiting: the other instance hasn't released the data folder”), a second Bekci container was started with the same data volume. So that two copies never run checks and send notifications twice, the new copy waits until the old one stops.

- **During an update on Coolify** this is normal: once the old container stops, the new one takes over within a second or two.
- **In any other situation**, find the old container using the same volume and stop it: `docker ps -a`. If the lock isn't released within 10 minutes (`UPTIME_LOCK_WAIT`), the new container exits with the error `aynı veri klasörüyle iki uygulama çalıştırılamaz` (“two instances can't run with the same data folder”).

## Port already in use {#port}

A `port is already allocated` or `address already in use` error means another program uses the port you chose:

```bash
docker ps --filter publish=⟦8080⟧        # the container using the port
ss -ltnp 'sport = :⟦8080⟧'              # the program using the port
```

- **8080:** on servers with Coolify, Coolify's proxy uses 8080. Pick another port (`-p 8081:8080`) or follow the [Coolify guide](/en/docs/install/coolify/).
- **80 / 443:** a web server already runs on the server. Instead of the Caddy install, follow the [reverse proxy](/en/docs/install/reverse-proxy/) or [Coolify](/en/docs/install/coolify/) guide.

## The panel opens but live updates don't arrive {#live}

If the **Offline** notice keeps showing at the top of the page, or monitor states only change when you reload, the reverse proxy in front of Bekci is buffering the live stream (`/api/events`). The setting your proxy needs is on the [reverse proxy page](/en/docs/install/reverse-proxy/#no-live-updates).

## “Too many failed attempts” {#too-many-attempts}

After 5 failed sign-in attempts from the same IP address within 15 minutes, that address has to wait 15 minutes. Try again when the time is up, or [reset the password](#password-reset). If Bekci sits behind a reverse proxy and all users get blocked at once, the proxy isn't passing the `X-Forwarded-For` header ([reverse proxy settings](/en/docs/install/reverse-proxy/)).

## Times look wrong {#time}

The interface currently always shows times in Türkiye time (`Europe/Istanbul`). Daily summaries, the nightly backup time and the log lines follow the server's `TZ` environment variable (default `Europe/Istanbul`). For another time zone set e.g. `TZ=Europe/Berlin` and recreate the container. The `Bekci başladı` (“Bekci started”) log line shows the time zone in use (`saat_dilimi=`).

## Links in notifications or the address in agent commands are wrong {#base-url}

`BASE_URL` isn't set or is wrong. Set it to the address the panel is opened at from outside (`https://…`) and recreate the container ([environment variables](/en/docs/environment-variables/)).

## The disk is full {#disk}

When the disk is full, Bekci can't write new measurements; with SQLite the automatic backup before a database-upgrading update can't be taken either, and Bekci won't start. Free up space: old pre-update backups (`/data/backups/pre-migrate-…db`) are never deleted automatically, and unused Docker images take up space too. You can shorten how long raw check history is kept under **Settings → General**.

## PostgreSQL version mismatch {#postgresql-version}

If the `postgres` image refuses to start with `HATA: PostgreSQL ana sürümü uyuşmuyor.` (“ERROR: PostgreSQL major version mismatch.”), follow the [PostgreSQL major version upgrade](/en/docs/updates-backups/#postgresql-upgrade) steps.

## Frequently asked questions {#faq}

### Is Bekci free? {#free}

Yes. Bekci is open source and released under the [AGPL-3.0](https://github.com/kadirsungurlu/bekci/blob/main/LICENSE) license; no license fees, no subscription, no paid tiers. The source code is [on GitHub](https://github.com/kadirsungurlu/bekci).

### Does it need an internet connection? {#internet}

The image is downloaded from Docker Hub during install and updates. After that Bekci only connects out for the checks and notifications you define; it can also watch targets on your internal network.

### Can I run two copies of Bekci at the same time? {#two-copies}

Not with the same data; the second copy [waits](#lock) until the first one stops. To check from different locations, use a [check location](/en/docs/server-agent/#check-location).

### Can I move from SQLite to PostgreSQL? {#sqlite-postgresql}

The database can't be moved directly. You can carry monitors, notification channels, tags and status pages over to a new install with the JSON backup under **Settings → Backup / Restore**; users and check history aren't included ([details](/en/docs/updates-backups/#json-backup)).

### How much disk does it use? {#disk-usage}

It depends on the number of monitors and the check interval. Each individual check is kept for 14 days by default, then condensed into hourly and daily summaries; you can change these periods under **Settings → General**. Nightly backups take a few times the database size as well.

### I found a bug or want a feature {#report}

[Open an issue on GitHub](https://github.com/kadirsungurlu/bekci/issues) or write to [me@kadir.app](mailto:me@kadir.app).
