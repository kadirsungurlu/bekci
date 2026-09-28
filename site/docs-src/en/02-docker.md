---
id: docker
title: Install with one Docker command
nav: Docker (one command)
description: Start Bekci with a single docker run command, open it in your browser and create your admin account. The quickest way to try it.
section: Install
order: 2
slug: install/docker
---

By the end of this guide Bekci will be running on your server and you'll be able to open the panel at `http://SERVER-IP:8080`. Total time: about 3 minutes.

> [!TIP]
> This path is ideal for trying Bekci out. For a permanent install we recommend [Docker Compose](/en/docs/install/docker-compose/) (settings live in a file and updates are easy) or the [HTTPS install](/en/docs/install/caddy/) on your own domain.

## 1. Install Docker {#install-docker}

First check whether Docker is already on your server:

```bash
docker --version
```

If you see a version number (e.g. `Docker version 29.x`), skip this step. If you see `command not found`, install Docker with the official install script:

```bash
curl -fsSL https://get.docker.com -o get-docker.sh
sh get-docker.sh
```

This script is maintained by Docker itself ([get.docker.com](https://get.docker.com)); on common distributions such as Ubuntu, Debian, Fedora and CentOS/RHEL it installs Docker Engine together with the Compose plugin. If you prefer your distribution's package repository, follow [Docker's install docs](https://docs.docker.com/engine/install/).

> [!CHECK]
> This command should print a short “Hello from Docker!” message:
>
> ```bash
> docker run --rm hello-world
> ```

## 2. Start Bekci {#start}

Copy the whole command below and run it on your server:

```bash
docker run -d --name bekci --restart unless-stopped \
  -p 8080:8080 \
  -v bekci-data:/data \
  kadirsungurlu/bekci:latest
```

What each part does:

| Part | Meaning |
|---|---|
| `-d` | Run the container in the background; it keeps running when you close the terminal. |
| `--name bekci` | The container's name. You'll use it in later commands. |
| `--restart unless-stopped` | Start again automatically after a server reboot or a crash. |
| `-p 8080:8080` | Forward the server's port 8080 to Bekci. |
| `-v bekci-data:/data` | Store the data in a Docker volume named `bekci-data`; it survives even if you delete the container. |
| `kadirsungurlu/bekci:latest` | The Bekci image on Docker Hub (amd64 and arm64). |

> [!CHECK]
> After about 10 seconds, run `docker ps`. In the `bekci` row the **STATUS** column should end in **(healthy)**, e.g. `Up 12 seconds (healthy)`. If it says `(health: starting)`, wait a few more seconds.
>
> The health check should also return `ok` from inside the server:
>
> ```bash
> curl http://localhost:8080/healthz
> ```

## 3. Open the panel and create the admin account {#admin}

1. Open `http://⟦SERVER-IP⟧:8080` in your browser. Your hosting provider's dashboard shows the server's IP address; the first address printed by `hostname -I` on the server is usually the same.
2. On the **Welcome** screen enter a **username** (3–32 characters: letters, digits, periods, hyphens, underscores) and a **password** of at least 8 characters, and type the password again in **Password (again)**.
3. Click **Create account**.

> [!CHECK]
> The monitor list opens with the text “Add your first monitor”. Continue with [First steps](/en/docs/first-steps/).

> [!WARNING] This address is not encrypted
> On a panel opened over `http://` your password travels over the network unencrypted. That's fine for a trial; for permanent use put the panel behind HTTPS on a domain: [HTTPS install (Caddy)](/en/docs/install/caddy/) or [behind a reverse proxy](/en/docs/install/reverse-proxy/).

## 4. Check the firewall {#firewall}

If the panel doesn't open, port 8080 is most likely closed.

- **Your hosting provider's firewall** (Hetzner, DigitalOcean, AWS “security group”, etc.): allow incoming connections on 8080/TCP.
- **ufw:** Docker adds firewall rules for the ports it publishes, so a port published with `-p 8080:8080` is reachable from outside regardless of your ufw rules. If you want the opposite, i.e. the panel reachable only from the server itself, change `-p 8080:8080` to `-p 127.0.0.1:8080:8080`.

> [!TIP] Trying it without opening a port
> To try the panel without exposing it to the internet, run the command with `-p 127.0.0.1:8080:8080` and open an SSH tunnel from your own computer:
>
> ```bash
> ssh -L 8080:localhost:8080 ⟦root@SERVER-IP⟧
> ```
>
> While the tunnel is open, visit `http://localhost:8080` on your own computer.

## 5. Set BASE_URL {#base-url}

Bekci needs to know the address the panel is reached at, for links in notifications and for server agent install commands. You provide it with the `BASE_URL` environment variable. Environment variables are set when the container is created, so you need to recreate the container; your data lives in the volume and is kept:

```bash
docker stop bekci && docker rm bekci
docker run -d --name bekci --restart unless-stopped \
  -p 8080:8080 \
  -v bekci-data:/data \
  -e BASE_URL=http://⟦SERVER-IP⟧:8080 \
  -e TZ=Europe/Istanbul \
  kadirsungurlu/bekci:latest
```

`TZ` is the time zone (used for daily summaries and the time of the nightly backup), e.g. `Europe/Berlin` or `UTC`. All options: [Environment variables](/en/docs/environment-variables/).

> [!CHECK]
> `docker exec bekci printenv BASE_URL` should print the address you set, and you can still sign in with the same account.

## Where is your data? {#data}

All data (the database and the automatic backups) lives in the `bekci-data` Docker volume, which is the `/data` folder inside the container:

```bash
docker exec bekci ls -la /data
```

Deleting the container (`docker rm bekci`) does not delete the data. Deleting the volume **permanently deletes all data**; only do that when removing Bekci for good. For backups see [Updates, backups and rollback](/en/docs/updates-backups/).

## Everyday commands {#everyday}

```bash
docker stop bekci          # stop
docker start bekci         # start
docker restart bekci       # restart
docker logs -f bekci       # follow the logs (Ctrl+C to quit)
docker logs --tail 50 bekci
```

The last line of a healthy startup looks like this (“Bekci başladı” means “Bekci started”):

```text
level=INFO msg="Bekci başladı" sürüm=… adres=:8080 veri=/data saat_dilimi=Europe/Istanbul
```

### Updating {#update}

Pull the new image and recreate the container with the same command:

```bash
docker pull kadirsungurlu/bekci:latest
docker stop bekci && docker rm bekci
```

Then run the `docker run` command from [step 5](#base-url) again. To make updates a single command, we recommend [Docker Compose](/en/docs/install/docker-compose/). Don't forget to back up first: [Updates, backups and rollback](/en/docs/updates-backups/).

## Common problems {#problems}

### “port is already allocated” or “address already in use” {#port-taken}

Another program is using port 8080. If it's another container, `docker ps --filter publish=8080` shows which one. **On servers running Coolify, Coolify's proxy uses port 8080 too.** Pick another port, e.g. `-p 8081:8080`, and open the panel at `http://SERVER-IP:8081`. If you use Coolify, the [Coolify guide](/en/docs/install/coolify/) is a better fit.

### The browser says the connection timed out {#timeout}

Port 8080 may be closed in your provider's firewall; see [step 4](#firewall).

### The container keeps restarting {#restarting}

The line starting with `hata:` (“error:”) at the end of `docker logs bekci` tells you why. See [Troubleshooting](/en/docs/troubleshooting/) for fixes.

## Removing Bekci {#remove}

```bash
docker stop bekci && docker rm bekci
docker volume rm bekci-data    # CAREFUL: permanently deletes all data
```
