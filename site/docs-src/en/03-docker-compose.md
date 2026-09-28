---
id: compose
title: Install with Docker Compose
nav: Docker Compose
description: Install Bekci in /opt/bekci with a docker-compose.yml file. SQLite or external PostgreSQL; updating is a single command.
section: Install
order: 3
slug: install/docker-compose
---

With Docker Compose every setting lives in one file (`docker-compose.yml`). Starting, stopping and updating are single commands; to change a setting you edit the file and restart. Total time: about 5 minutes.

This guide opens the panel at `http://SERVER-IP:8080`. If you want HTTPS on your own domain, go straight to the [HTTPS install (Caddy)](/en/docs/install/caddy/); that guide uses Compose as well.

## 1. Check Docker and Compose {#check}

```bash
docker compose version
```

> [!CHECK]
> You should see a line such as `Docker Compose version v2.…` (or newer). If you see `docker: 'compose' is not a docker command` or `command not found`, [install Docker with the official script](/en/docs/install/docker/#install-docker); it installs the Compose plugin too.

## 2. Create a folder {#folder}

We'll use `/opt/bekci` for Bekci's files. You can choose another place; what matters is that you run all later `docker compose` commands **from inside this folder**.

```bash
mkdir -p /opt/bekci
cd /opt/bekci
```

## 3. Create docker-compose.yml {#file}

Pick your database. If in doubt, choose **SQLite**: it needs no extra service and is the lightest option ([which database?](/en/docs/#database)).

Create the file with the `nano` text editor:

```bash
nano docker-compose.yml
```

Copy the content below and paste it into the nano window (in most terminals: right-click or <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>V</kbd>):

:::tabs key=db label="Database"
@tab SQLite (recommended)
```yaml title="/opt/bekci/docker-compose.yml" download="/indir/en/docker-compose.yml"
# Bekci — install with Docker Compose (SQLite)
# Guide: https://bekci.app/en/docs/install/docker-compose/
name: bekci
services:
  bekci:
    # To pin a version, replace "latest" with e.g. 1.0.0.
    # For embedded PostgreSQL: kadirsungurlu/bekci:postgres
    image: kadirsungurlu/bekci:latest
    restart: unless-stopped
    stop_grace_period: 30s
    ports:
      - "8080:8080"
    environment:
      BASE_URL: http://⟦203.0.113.10⟧:8080   # the address you open the panel at
      TZ: Europe/Istanbul
    volumes:
      - bekci-data:/data
volumes:
  bekci-data:
```

The only thing to change is the highlighted part: replace `203.0.113.10` with your server's IP address.
@tab External PostgreSQL
This file also starts a PostgreSQL 18 container next to Bekci. If you'll use your own PostgreSQL server instead, delete the `db` service and point `DATABASE_URL` at that server.

```yaml title="/opt/bekci/docker-compose.yml" download="/indir/en/docker-compose.postgres.yml"
# Bekci — install with Docker Compose (external PostgreSQL)
# Guide: https://bekci.app/en/docs/install/docker-compose/
# Change the password in BOTH places the same way (DATABASE_URL and POSTGRES_PASSWORD).
name: bekci
services:
  bekci:
    image: kadirsungurlu/bekci:latest
    restart: unless-stopped
    stop_grace_period: 30s
    ports:
      - "8080:8080"
    environment:
      BASE_URL: http://⟦203.0.113.10⟧:8080   # the address you open the panel at
      TZ: Europe/Istanbul
      DATABASE_URL: postgres://bekci:⟦A-STRONG-PASSWORD⟧@db:5432/bekci?sslmode=disable
    volumes:
      - bekci-data:/data   # backups and temporary files
    depends_on:
      db:
        condition: service_healthy
  db:
    image: postgres:18-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: bekci
      POSTGRES_PASSWORD: ⟦A-STRONG-PASSWORD⟧
      POSTGRES_DB: bekci
    volumes:
      - db-data:/var/lib/postgresql
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U bekci -d bekci"]
      interval: 5s
      retries: 10
volumes:
  bekci-data:
  db-data:
```

Change the highlighted parts: your server's IP address and a strong password, **the same in both places**. Use only letters and digits in the password (e.g. a random 24-character string); characters such as `@`, `:`, `/`, `?` and `#` break the connection URL.
:::

Save and exit: <kbd>Ctrl</kbd>+<kbd>O</kbd>, then <kbd>Enter</kbd> (save), then <kbd>Ctrl</kbd>+<kbd>X</kbd> (exit).

> [!TIP] Creating the file by downloading it
> If you'd rather not paste, download the file directly and then edit only the highlighted parts:
>
> ```bash
> curl -fsSL -o docker-compose.yml https://bekci.app/indir/en/docker-compose.yml
> nano docker-compose.yml
> ```
>
> For external PostgreSQL the address is `https://bekci.app/indir/en/docker-compose.postgres.yml` (the downloaded file must still be named `docker-compose.yml`).

> [!CHECK]
> `docker compose config --quiet` should finish without printing anything. If it prints an error, the indentation was most likely damaged: YAML files are indented with spaces and the number of leading spaces matters. Delete the file (`rm docker-compose.yml`) and create it again.

## 4. Start it {#start}

```bash
docker compose up -d
```

The first time, the image is downloaded from Docker Hub (a few seconds to a minute).

> [!CHECK]
> The command should end with a `Container bekci-bekci-1  Started` line. A few seconds later the **STATUS** column of `docker compose ps` should read `Up … (healthy)`. If you chose PostgreSQL you'll see two rows: `bekci` and `db`.

## 5. Open the panel {#open}

Open `http://⟦SERVER-IP⟧:8080` in your browser and create your admin account on the **Welcome** screen ([details](/en/docs/install/docker/#admin)). If it doesn't open, allow port 8080/TCP in your provider's firewall ([firewall note](/en/docs/install/docker/#firewall)).

> [!WARNING]
> This setup opens the panel over unencrypted `http://`. For permanent use add HTTPS: the [Caddy install](/en/docs/install/caddy/) if the server has no web server yet, otherwise a [reverse proxy](/en/docs/install/reverse-proxy/).

## Logs {#logs}

```bash
cd /opt/bekci
docker compose logs -f bekci      # follow, Ctrl+C to quit
docker compose logs --tail 50 bekci
```

## Updating {#update}

```bash
cd /opt/bekci
docker compose pull
docker compose up -d
```

`pull` downloads the new image and `up -d` recreates the container with it; your data stays in the volume. Back up before updating and learn how to pin versions: [Updates, backups and rollback](/en/docs/updates-backups/).

> [!CHECK]
> The last line of `docker compose logs bekci | grep "Bekci başladı"` should show a different `sürüm=` (version) value than before the update (the short code of the running build, e.g. `2718268`).

## Changing a setting {#change-setting}

After changing something in `docker-compose.yml` (e.g. `BASE_URL`), run:

```bash
docker compose up -d
```

Compose notices the change and recreates the container with the new settings.

## Stopping and removing {#stop}

```bash
docker compose stop     # stop (the container is kept)
docker compose start    # start again
docker compose down     # remove the container; the data stays in the volume
```

> [!CAUTION]
> `docker compose down -v` also deletes the volumes, which means **all your data is permanently gone**. Don't use `-v` unless you want to remove Bekci completely.

Your data lives in a volume named `bekci_bekci-data` (Compose prefixes volume names with the project name). `docker volume ls` lists volumes.
