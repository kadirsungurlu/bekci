---
id: coolify
title: Install on Coolify
nav: Coolify
description: Add Bekci to Coolify v4 as a Docker image and let Coolify handle the domain, the HTTPS certificate and updates.
section: Install
order: 5
slug: install/coolify
---

If your server runs [Coolify](https://coolify.io), adding Bekci as a **Docker Image** resource is the easiest path. Coolify's own proxy (Traefik) takes care of the domain, the HTTPS certificate and routing. Total time: about 5 minutes.

> [!NOTE]
> This guide covers Coolify v4. Coolify's interface changes a little between versions; if a button or field is named differently for you, look for the option that does the same thing. Places with two different spellings are pointed out below.

## Before you start {#before}

- You need a project and a server (destination) in Coolify.
- The **A** record of the domain you'll use for Bekci (e.g. `bekci.example.com`) must point to the Coolify server's IP address ([how to add a DNS record](/en/docs/install/caddy/#dns)).

## 1. Create a new resource {#resource}

1. In Coolify, open your project and environment (e.g. **production**).
2. Click **New resource** (in some versions **+ New** or **+ Add Resource**).
3. Choose **Docker Image** from the list.
4. Enter the image: `kadirsungurlu/bekci` in **Image name** and `latest` in **Tag**. (If there's only one field: `kadirsungurlu/bekci:latest`.) To pin a specific version, use `1.0.0` instead of `latest` ([version tags](/en/docs/updates-backups/#tags)).
5. Pick the server if asked and save with **Create application** (or **Save**).

> [!CHECK]
> The application's configuration page opens. On the left you'll see sections such as **General**, **Environment Variables**, **Persistent Storage** and **Healthcheck**.

## 2. Set the port {#port}

In **General**, enter `8080` in **Ports Exposes** (remove any other value). Bekci listens on port 8080 inside the container, and Coolify's proxy forwards traffic to it. Leave **Port mappings** empty.

## 3. Add the domain {#domain}

Enter your domain including `https://`: `https://⟦bekci.example.com⟧`

- In newer Coolify versions this happens on a separate **Domains** page (**Add domain** button).
- In older versions it goes into the **Domains** field under **General**.

With `https://` in front, Coolify obtains the Let's Encrypt certificate itself.

## 4. Add persistent storage {#storage}

> [!CAUTION] Don't skip this step
> Without persistent storage all data (monitors, history, users) stays inside the container and is **deleted on every redeploy**.

1. Open **Persistent Storage**.
2. Add a new **volume** (**Add volume mount**; in older versions **+ Add** → **Volume**).
3. **Name**: `bekci-data` (any name you like), **Destination Path**: `/data`.
4. Save.

If you'd rather mount a folder on the server (**directory mount**) instead of a volume, that folder must be owned by uid 1000: `chown 1000:1000 ⟦/path/to/folder⟧`. Bekci runs as a non-root user and can't write to a folder owned by someone else.

## 5. Add environment variables {#env}

Under **Environment Variables** add:

```ini title="Environment Variables"
BASE_URL=https://⟦bekci.example.com⟧
TZ=Europe/Istanbul
```

`BASE_URL` must match the domain from step 3: links in notifications and server agent install commands use it. Set `TZ` to your time zone (e.g. `Europe/Berlin`, `UTC`). Other options: [Environment variables](/en/docs/environment-variables/).

## 6. Leave the health check alone {#healthcheck}

Leave Coolify's health check under **Healthcheck** **disabled** (the default). The Bekci image has its own health check, designed to work correctly during updates.

> [!WARNING] Why keep it disabled?
> During an update Coolify starts the new container before stopping the old one. The new Bekci container waits for the old one to stop, so that two copies never use the same data at once; during that wait the image's own health check reports “healthy”. If you enable Coolify's health check, it replaces the image's, the waiting container looks “unhealthy” and Coolify rolls the update back.

## 7. Deploy {#deploy}

Click **Deploy** at the top right. Coolify pulls the image and starts the container.

> [!CHECK]
> The deployment log finishes successfully and the application's status becomes **Running**. The application's **Logs** show this line (“Bekci started”):
>
> ```text
> level=INFO msg="Bekci başladı" sürüm=… adres=:8080 veri=/data saat_dilimi=Europe/Istanbul
> ```
>
> Open `https://⟦bekci.example.com⟧` and create your admin account on the **Welcome** screen. Getting the certificate can take about a minute the first time.

## With PostgreSQL {#postgresql}

The default SQLite is enough for most installs ([which database?](/en/docs/#database)). If you want PostgreSQL, there are two ways:

:::tabs key=coolify-db label="PostgreSQL option"
@tab Embedded PostgreSQL
The easiest: in step 1 use the tag `postgres` instead of `latest` (for a fixed version `1.0.0-postgres`). PostgreSQL 18 runs inside the same container; the database still lives in the `/data` volume and nightly backups go to `/data/backups`. All other steps are the same. It uses roughly 200 MB of extra memory.
@tab PostgreSQL in Coolify
1. In the same project create a database with **New resource** → **PostgreSQL** and start it.
2. On the database's page, copy the internal connection URL (shown with a name such as **Postgres URL (internal)**). It looks like `postgres://user:password@…:5432/…`.
3. Add it to the Bekci application's **Environment Variables**:

   ```ini title="Environment Variables"
   DATABASE_URL=⟦the-internal-url-you-copied⟧
   ```

   The value looks like `postgres://postgres:…@…:5432/postgres`.

4. Keep the tag `latest` (the SQLite image); with `DATABASE_URL` set, this image connects to PostgreSQL. Redeploy with **Deploy**.

Bekci and the database must be on the same Coolify server (the internal URL only works there). In this setup Bekci does **not** take nightly backups; schedule them under **Backups** on the database's page.
:::

> [!CHECK]
> The log should contain a line `msg=veritabanı tür="PostgreSQL …"` (“database type”; the password is masked as `xxxxx`).

## Updating {#update}

- **With the `latest` tag:** click **Redeploy** (or **Deploy**) on the application's page; Coolify pulls the newest image and replaces the container.
- **With a fixed version (e.g. `1.0.0`):** change the tag to the new version and deploy.

During an update it's **normal** to see this line in the new container's log for a while (“the data folder is open in another instance; waiting until it closes”). As soon as the old container stops, the new one takes over within a second or two, so two copies never run checks or send notifications twice:

```text
level=WARN msg="veri klasörü başka bir örnekte açık; o kapanana kadar bekleniyor" kilit=/data/uptime.db.lock en_fazla=10m0s
```

Don't forget to back up before updating: [Updates, backups and rollback](/en/docs/updates-backups/).

## Common problems {#problems}

### The page shows “no available server” or 404 {#no-available-server}

Coolify's proxy can't reach Bekci. Check that **Ports Exposes** is `8080` and that the domain was entered with `https://`, then redeploy.

### The setup screen appears after every deployment {#data-lost}

Persistent storage is missing or its destination is wrong. Make sure the destination under **Persistent Storage** is exactly `/data` ([step 4](#storage)).

### The update is rolled back as “unhealthy” {#unhealthy}

Coolify's health check is enabled; disable it as in [step 6](#healthcheck) and redeploy.

### The container stops with “permission denied” {#permission}

The log shows a line like this (“could not open the database: could not open the lock file”):

```text
hata: veritabanı açılamadı: kilit dosyası açılamadı: open /data/uptime.db.lock: permission denied
```

You mounted a folder from the server that isn't owned by uid 1000: fix it with `chown -R 1000:1000 ⟦/path/to/folder⟧` and redeploy.
