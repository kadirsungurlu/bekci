---
id: updates
title: Updates, backups and rollback
nav: Updates, backups, rollback
description: Update Bekci safely, pin its version, find the automatic backups, copy backups off the server, restore them and go back to an older version if needed.
section: Maintenance and reference
order: 9
slug: updates-backups
---

## Version tags {#tags}

Bekci images are published for amd64 and arm64 on Docker Hub as [`kadirsungurlu/bekci`](https://hub.docker.com/r/kadirsungurlu/bekci/tags) (the same images are on GitHub Container Registry as `ghcr.io/kadirsungurlu/bekci`):

| Tag | Content | When |
|---|---|---|
| `latest` | Newest build (latest state of the main branch), SQLite | If you always want the newest |
| `postgres` | Newest build, embedded PostgreSQL 18 | The newest with embedded PostgreSQL |
| `1.0.0`, `1.0` | A specific version, SQLite | To pin the version and to roll back (recommended) |
| `1.0.0-postgres`, `1.0-postgres` | A specific version, embedded PostgreSQL | The same, for PostgreSQL |
| `2718268`, `postgres-2718268` | A specific build (short commit code) | To go back to a build you saw in the log |

Two-part tags such as `1.0` follow the latest patch of that series (e.g. once `1.0.1` is out, `1.0` points to it).

> [!IMPORTANT]
> **Don't** switch between the SQLite (`latest`, `1.0.0`) and PostgreSQL (`postgres`, `1.0.0-postgres`) tags: data isn't carried over between them.

## Before you update {#before}

1. **Take a backup.** The [full backup](#full-backup) below is the safest. With SQLite, Bekci also takes its own backup before updates that change the database structure ([automatic backups](#automatic-backups)).
2. **Read the release notes:** [releases on GitHub](https://github.com/kadirsungurlu/bekci/releases).

## Updating {#update}

:::tabs key=install label="Install path"
@tab Docker Compose
```bash
cd /opt/bekci
docker compose pull
docker compose up -d
```

The commands are the same for the [Caddy install](/en/docs/install/caddy/).
@tab docker run
```bash
docker pull kadirsungurlu/bekci:latest
docker stop bekci && docker rm bekci
```

Then start the container again with your original `docker run` command (with the same `-v bekci-data:/data` and `-e` values; see the [Docker guide](/en/docs/install/docker/#base-url)).
@tab Coolify
Click **Redeploy** (or **Deploy**) on the application's page. If you use a fixed version, change the tag to the new version first ([Coolify guide](/en/docs/install/coolify/#update)).
:::

> [!CHECK]
> The log should show a new start line (`docker compose logs bekci | grep "Bekci başladı"`). The `sürüm=` (version) field is the short code of the running build (e.g. `2718268`); it should have changed after the update:
>
> ```text
> level=INFO msg="Bekci başladı" sürüm=… adres=:8080 veri=/data
> ```
>
> If the database structure changed, you'll also see `msg="migration öncesi yedek alındı"` (“pre-migration backup taken”) and `msg="migration uygulandı"` (“migration applied”) lines just above it.

## Pinning the version {#pinning}

The `latest` tag brings the newest version with every `pull`. If you want to decide when to update, use a specific version tag:

- **Docker Compose:** in `docker-compose.yml`, change `image: kadirsungurlu/bekci:latest` to `image: kadirsungurlu/bekci:⟦1.0.0⟧`.
- **Caddy install:** set `UPTIME_TAG=⟦1.0.0⟧` in `/opt/bekci/.env` (for embedded PostgreSQL `1.0.0-postgres`).
- **Coolify:** enter `1.0.0` in the **Tag** field.

To move to a new version, change the tag and run `docker compose up -d` (redeploy on Coolify).

## Automatic backups {#automatic-backups}

Bekci takes two kinds of backups on its own. Both live inside the data volume, in `/data/backups/`.

| Backup | File name | When |
|---|---|---|
| Nightly backup (SQLite) | `uptime-2026-09-28.db` | Once a day, after 03:00 (in the `TZ` time zone); the last 7 are kept by default. If Bekci starts after 03:00, that day's backup is taken within a few minutes |
| Nightly backup (embedded PostgreSQL) | `uptime-2026-09-28.dump` | The same, in `pg_dump` format |
| Pre-update backup (SQLite) | `pre-migrate-v16-to-v17-20260928-030000.db` | Right before a new version changes the database structure |

- Change how many nightly backups are kept under **Settings → General → Nightly backups to keep** (0 turns nightly backups off).
- If the pre-update backup can't be taken (e.g. the disk is full), the update isn't applied and Bekci doesn't start; it starts once there's space again. These files are never deleted automatically; you can delete old ones by hand.
- With **external PostgreSQL** (`DATABASE_URL`) Bekci takes **no** backups; back up on the database side ([below](#external-postgresql)).

To list the backups:

```bash
docker exec bekci ls -la /data/backups
```

In a Compose install the container is named `bekci-bekci-1`; or, from inside `/opt/bekci`, use `docker compose exec bekci ls -la /data/backups`.

> [!WARNING] Copy backups off the server too
> Automatic backups sit on the same disk: if the server or the disk is lost, so are the backups. Copy them somewhere else regularly.

## Copying backups off the server {#off-server}

Copy the backup folder from the container to the server:

```bash
cd /opt/bekci
docker compose cp bekci:/data/backups ./bekci-backups
```

(For a `docker run` install: `docker cp bekci:/data/backups ./bekci-backups`.)

Then download it **from your own computer**:

```bash
scp -r ⟦root@SERVER-IP⟧:/opt/bekci/bekci-backups ./
```

We recommend automating these steps with a cron job or your backup tool (restic, rsync, your provider's backup service…).

### Full backup (the whole data volume) {#full-backup}

To capture the whole data volume, database included, in a single file, stop Bekci briefly (a database file copied while it's open can be inconsistent):

```bash
cd /opt/bekci
docker compose stop bekci
docker run --rm -v bekci_bekci-data:/data -v "$PWD":/yedek alpine \
  tar czf /yedek/bekci-data-$(date +%F).tgz -C /data .
docker compose start bekci
```

The volume is named `bekci_bekci-data` in a Compose install and `bekci-data` in a `docker run` install (`docker volume ls` shows it). The command creates a file such as `/opt/bekci/bekci-data-2026-09-28.tgz`.

## Restoring {#restore}

:::tabs key=restore label="Backup type"
@tab From an SQLite backup
To restore a nightly or pre-update backup:

```bash
cd /opt/bekci
docker compose exec bekci ls /data/backups          # pick the file name
docker compose stop bekci
docker run --rm -v bekci_bekci-data:/data alpine sh -c \
  'cd /data && cp backups/⟦uptime-2026-09-28.db⟧ uptime.db && rm -f uptime.db-wal uptime.db-shm && chown 1000:1000 uptime.db'
docker compose start bekci
```

The `uptime.db-wal` and `uptime.db-shm` files belong to the old database and must be deleted. For a `docker run` install the volume is named `bekci-data` and the stop and start commands are `docker stop bekci` and `docker start bekci`.
@tab From a full backup (.tgz)
```bash
cd /opt/bekci
docker compose stop bekci
docker run --rm -v bekci_bekci-data:/data -v "$PWD":/yedek alpine sh -c \
  'find /data -mindepth 1 -delete && tar xzf /yedek/⟦bekci-data-2026-09-28.tgz⟧ -C /data'
docker compose start bekci
```

This deletes the current content of the volume entirely and replaces it with the backup.
@tab Embedded PostgreSQL
With the `postgres` image the nightly backups are `uptime-…dump` files. While the container is running:

```bash
docker exec ⟦bekci⟧ pg_restore -h /run/postgresql -U postgres --clean --if-exists --no-owner -d uptime /data/backups/⟦uptime-2026-09-28.dump⟧
docker restart ⟦bekci⟧
```
@tab External PostgreSQL {#external-postgresql}
Bekci doesn't back up an external database; you take the backup with `pg_dump`. For the PostgreSQL setup from the [Docker Compose guide](/en/docs/install/docker-compose/), back up with:

```bash
cd /opt/bekci
docker compose exec -T db pg_dump -U bekci --format=custom bekci > bekci-$(date +%F).dump
```

Restore with:

```bash
cd /opt/bekci
docker compose stop bekci
docker compose exec -T db pg_restore -U bekci --clean --if-exists --no-owner -d bekci < ⟦bekci-2026-09-28.dump⟧
docker compose start bekci
```
:::

> [!CHECK]
> After restoring, sign in to the panel: your monitors should look the way they did when the backup was taken.

### JSON backup of your configuration {#json-backup}

**Settings → Backup / Restore → Download backup** exports monitors, notification channels, tags, status pages and settings into a single JSON file. Users, check history and the audit log aren't included. You can import this file into another Bekci install (SQLite or PostgreSQL, it doesn't matter) with **Restore** in the same section. The file contains passwords and tokens in plain text; keep it somewhere safe.

## Going back to an older version {#rollback}

If a new version causes trouble, you can go back to the tag of the old one:

1. Find the tag to go back to: published versions (e.g. `1.0.0`) are listed [on Docker Hub](https://hub.docker.com/r/kadirsungurlu/bekci/tags). The old `sürüm=` (version) value from the log (e.g. `0ae7b6d`) works as a tag too: `kadirsungurlu/bekci:0ae7b6d`.
2. **If the new version upgraded the database** (the log has a `migration uygulandı` line), the old version may not be able to open the new database. First [restore](#restore) the pre-update backup (`pre-migrate-…db`). Changes made after that backup are lost.
3. Switch the tag back to the old version and start it, e.g. `image: kadirsungurlu/bekci:⟦1.0.0⟧` and `docker compose up -d`.

## PostgreSQL major version upgrade {#postgresql-upgrade}

This section only applies to the `postgres` image with embedded PostgreSQL. If Bekci ever moves to a new major PostgreSQL version (e.g. from 18 to 19), the data folder can't be opened directly. The container then won't start and writes a step-by-step explanation to its log under the heading `HATA: PostgreSQL ana sürümü uyuşmuyor.` (“ERROR: PostgreSQL major version mismatch.”). In short:

1. Stop the container and go back to the **old** image (the tag that contains the old PostgreSQL version).
2. While the old image is running, take a backup and copy the file off the server too:

   ```bash
   docker exec ⟦bekci⟧ pg_dump -h /run/postgresql -U postgres --format=custom --file=/data/backups/tasima.dump uptime
   ```

3. Stop the container and rename the `/data/postgres` folder instead of deleting it (e.g. `/data/postgres-18-old`).
4. Start the new image; an empty database is created.
5. Restore the backup and restart the container:

   ```bash
   docker exec ⟦bekci⟧ pg_restore -h /run/postgresql -U postgres --clean --if-exists --no-owner -d uptime /data/backups/tasima.dump
   docker restart ⟦bekci⟧
   ```

6. Once everything works, delete the old folder.

## Moving to another server {#moving}

1. Take a [full backup](#full-backup) on the old server and copy the file to the new server.
2. Install Bekci the same way on the new server, then stop it and [restore](#restore) the backup.
3. Point the domain's DNS record to the new server.

Because the address stays the same, server agents reconnect without being reinstalled: the IP lock looks at the agent's IP address, not the panel's.
