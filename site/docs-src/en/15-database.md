---
id: database
title: Database, migration and hardware
nav: Database and migration
description: Bekci's default PostgreSQL + TimescaleDB install, upgrading from SQLite and plain PostgreSQL, hardware sizing, backups and rollback.
section: Maintenance and reference
order: 15
slug: database
---

Bekci's default database is **PostgreSQL 18 + TimescaleDB**. Measurements are compressed, charts read precomputed rollups and old data is removed automatically. This page covers new installs, upgrading older installs and choosing hardware.

## Which setup? {#options}

| Setup | Status | Notes |
|---|---|---|
| **PostgreSQL + TimescaleDB** | Default, recommended | Installed with [Docker Compose](/en/docs/install/docker-compose/). The image version is pinned: `timescale/timescaledb:2.31.0-pg18`. |
| **Plain PostgreSQL** (RDS, Cloud SQL, etc.) | Works, not recommended | Without the extension Bekci still runs and shows a warning. It slows down at large scale: no compression, no rollups. |
| **SQLite** | Deprecated | Works in this version with a startup warning. Removed in the next major version. |
| **Embedded PostgreSQL** (`:postgres` image) | Deprecated | Works in this version with a startup warning. Removed in the next major version. |

**Settings → General** shows which mode you are in: with SQLite or plain PostgreSQL a warning strip appears at the top of the page.

## New install {#new-install}

Follow the [Docker Compose](/en/docs/install/docker-compose/) or [HTTPS with Caddy](/en/docs/install/caddy/) guide; both use this setup. Nothing else is needed after creating the admin account: Bekci enables the extension and creates the tables.

`POSTGRES_PASSWORD` must contain letters and digits only; it is written into the connection address, so characters such as `@`, `/` and `:` break it. To generate one:

```bash
openssl rand -hex 24
```

## Upgrading from SQLite {#sqlite}

Migration is done with the `uptime migrate` command. It does **not** change your SQLite file: it takes a copy, upgrades the copy to the new version's schema and transfers it to PostgreSQL from there. Bekci must be stopped during the migration; no checks run in the meantime.

1. **Take a backup.** A [full backup](/en/docs/updates-backups/#full-backup) is the safest.
2. **Add the `db` service to your `docker-compose.yml`**, set `DATABASE_URL` on the `bekci` service and define `POSTGRES_PASSWORD` in `.env`. Example: [Docker Compose guide](/en/docs/install/docker-compose/). **Leave the `/data` volume of the `bekci` service as it is**; the SQLite file lives there.
3. **Start the database and stop Bekci:**

   ```bash
   docker compose pull
   docker compose stop bekci
   docker compose up -d db
   ```

4. **Try it first.** This command writes nothing; it shows how much data will move and reports anything in the way:

   ```bash
   docker compose run --rm bekci migrate --dry-run
   ```

5. **Migrate:**

   ```bash
   docker compose run --rm bekci migrate
   ```

6. **Start:**

   ```bash
   docker compose up -d
   ```

> [!CHECK]
> The command should end with a `Taşıma tamam: … satır, kip timescale` line (“Migration complete: … rows, mode timescale”). The panel should show your monitors, chart history and incidents. Row counts of every table are compared at the end; on a mismatch the command fails and the migration doesn't count as finished.

**How long does it take?** In our measurement 3.1 million rows moved at about 31,000 rows per second; with the conversion to compressed tables the total was 5 minutes 24 seconds (on a shared server). The conversion step shows no progress and can stay silent for minutes on large data; don't interrupt it.

**If it's interrupted:** run the same command again and it continues where it stopped. With a half-finished migration the panel refuses to start with `SQLite verisi taşınmadı` (“SQLite data not migrated”). This is a safeguard against running on partial data.

**If the target isn't empty:** the command won't start, because it never overwrites existing data. This usually happens when an empty install was opened with `DATABASE_URL` earlier. If you don't want the old data and prefer to start fresh, set `SQLITE_IGNORE=1` in `.env`; the panel then starts and ignores the SQLite file.

### Command options {#migrate-options}

| Option | Description |
|---|---|
| `--from` | SQLite file. Default: `/data/uptime.db` |
| `--to` | Target address. Default: `DATABASE_URL` |
| `--batch` | Rows copied in one transaction |
| `--work-dir` | Folder for the temporary copy. If `/data` lacks space, point to another volume; it needs as much free space as the SQLite file. |
| `--restart` | Discards a half-finished migration and starts over |
| `--dry-run` | Checks without writing |

## Upgrading from plain PostgreSQL {#plain-postgresql}

If Bekci is connected to a PostgreSQL server through `DATABASE_URL`, you don't need to move data: installing the extension on the server is enough. Bekci converts the tables itself at startup.

**The `postgres:18-alpine` container in Compose:** change the image of the `db` service and add `command` so the existing data folder loads the extension:

```yaml
  db:
    image: timescale/timescaledb:2.31.0-pg18
    command: postgres -c shared_preload_libraries=timescaledb -c max_worker_processes=24
```

```bash
docker compose pull
docker compose up -d
```

The major version must match (18). Don't use this route from an older major version or from the Debian-based `postgres:18` image; the collation library difference can corrupt indexes. Use the dump route below instead.

**Dump and restore:** dump the old server, start the new `db` service empty, restore, start Bekci:

```bash
pg_dump --format=custom --no-owner --file=bekci.dump "⟦postgres://user:password@old-server:5432/bekci⟧"
docker compose up -d db
docker compose exec -T db pg_restore -U bekci -d bekci --no-owner < bekci.dump
docker compose up -d
```

**Managed PostgreSQL (RDS, Cloud SQL, etc.):** if the service doesn't offer TimescaleDB, Bekci keeps running in plain mode. It works but isn't recommended; it slows down at large scale.

Existing tables convert to hypertables on the panel's first start. The panel is down meanwhile; the time depends on data size (measured: 1.2 million rows on a shared server, 26 seconds to 2.5 minutes).

## Upgrading from embedded PostgreSQL {#embedded-postgresql}

The `:postgres` image is deprecated. Move the data to a separate `db` service with a dump:

```bash
docker exec ⟦bekci⟧ pg_dump -h /run/postgresql -U postgres --format=custom --no-owner --file=/data/backups/tasima.dump uptime
docker cp ⟦bekci⟧:/data/backups/tasima.dump ./tasima.dump
docker stop ⟦bekci⟧
```

Change your Compose file to the [default setup](/en/docs/install/docker-compose/) (a version number instead of the `postgres` tag), then:

```bash
docker compose up -d db
docker compose exec -T db pg_restore -U bekci -d bekci --no-owner < tasima.dump
docker compose up -d
```

The old `/data/postgres` folder isn't deleted; keep it until you are sure the new setup works.

## Hardware sizing {#hardware}

Assumption: 5 locations, 30-second check interval. The 100 and 1,000 monitor rows are based on measurements; **the 10,000 row is a calculation, not tested at full load.**

| Monitors | RAM | `shared_buffers` | CPU | Disk (raw data 90 days) | Disk (raw data 14 days) |
|---|---|---|---|---|---|
| 100 | 4 GB (minimum 2 GB) | 1 GB | 2 cores | 15 GB | 5 GB |
| 1,000 | 8 GB, comfortably 16 GB | 2-4 GB | 4 cores | 40 GB | 20 GB |
| 10,000 | 32 GB minimum, 64 GB recommended, NVMe | 8-16 GB | 8+ cores | 200 GB | 80 GB |

> [!IMPORTANT]
> In our measurement a single panel wrote at most about 480 results per second; with 5 locations and 30 seconds that equals about 2,900 monitors. Above this limit isn't supported in this version: at larger scale use a longer check interval or fewer locations.

Set `shared_buffers` on the `db` service:

```yaml
  db:
    command: postgres -c shared_preload_libraries=timescaledb -c max_worker_processes=24 -c shared_buffers=⟦2GB⟧
```

On a memory-limited server set `TS_TUNE_MEMORY: 400MB` on the `db` service; on first start the image picks its settings from the machine's total memory. The `timescale` image itself takes about 1.6 GB of disk.

## Backup and restore {#backup}

Bekci writes an `uptime-DATE.dump` file under `/data/backups/` every night. To take one by hand:

```bash
docker compose exec -T db pg_dump -U bekci --format=custom --no-owner bekci > bekci.dump
```

Restoring a TimescaleDB dump needs two extra steps. The target database must be empty:

```bash
docker compose stop bekci
docker compose exec db psql -U bekci -d postgres -c 'DROP DATABASE bekci' -c 'CREATE DATABASE bekci'
docker compose exec db psql -U bekci -d bekci -c 'SELECT timescaledb_pre_restore()'
docker compose exec -T db pg_restore -U bekci -d bekci --no-owner < bekci.dump
docker compose exec db psql -U bekci -d bekci -c 'SELECT timescaledb_post_restore()'
docker compose start bekci
```

In our measurement dumping a database of 3 million rows took 7 seconds and used 532 MB.

## Rollback {#rollback}

**Back to SQLite:** `uptime migrate` doesn't change the SQLite file, so the old install stays intact.

1. `docker compose stop bekci`
2. Delete the `DATABASE_URL` line in `docker-compose.yml`.
3. Set `UPTIME_TAG=⟦1.6.0⟧` in `.env`.
4. `docker compose up -d`

Measurements and incidents written to PostgreSQL after the migration aren't in the SQLite file and are lost on rollback. Don't delete the SQLite file until you are sure everything works.

To migrate again after running on SQLite for a while, empty the target database first (`docker compose down` and delete its volume, or `DROP DATABASE`). Otherwise `uptime migrate` reports "already completed" and doesn't copy the new data.

**Back to plain PostgreSQL:** tables converted to TimescaleDB don't open on a plain PostgreSQL image. Restore the dump you took before upgrading onto the old server.

**Back to the embedded image:** use the old tag and the `/data/postgres` folder; the upgrade doesn't change that folder.
