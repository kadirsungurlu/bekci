---
id: agent
title: Server agent and check locations
nav: Server agent and check locations
description: Install, update and remove the agent that collects your servers' CPU, RAM, disk and network metrics, and the check location that checks monitors from other places.
section: Usage
order: 8
slug: server-agent
---

Bekci's program can run on other servers in two different roles. They are separate records in the panel and don't show up on each other's screens:

| | Server agent | Check location |
|---|---|---|
| What it does | Sends the CPU, RAM, disk, network, temperature and Docker container metrics of the server it runs on, once a minute | Checks the monitors assigned to it from its own location and sends the results |
| In the panel | **Servers → Add server** | **Settings → Check locations → New check location** |
| Install methods | Docker, Linux (systemd), Windows | Docker |
| Supported systems | Linux amd64 and arm64, Windows amd64 | Linux amd64 and arm64 |

The agent uses no database. If it can't reach the main server (your Bekci panel), it holds its results and sends them once the connection is back.

## Before you start {#before}

- **`BASE_URL` must be right.** The install command takes the address the agent connects to from `BASE_URL`. If the panel is at `https://bekci.example.com`, `BASE_URL` must be that too ([how to set it](/en/docs/environment-variables/)).
- **HTTPS is recommended.** By default the agent refuses to connect to an unencrypted (`http://`) address; if the panel uses `http://`, the install command adds that permission (`PROBE_ALLOW_INSECURE=1`) itself, but the token then travels over the network unencrypted.
- **The server you install the agent on must be able to reach your panel's address** (an outgoing HTTPS connection is enough; you don't need to open any incoming port for the agent).

## Install the server agent {#server-agent}

1. In the panel, click **Add server** on the **Servers** page.
2. Enter a **Server name** that will appear in the list and in notifications (e.g. “Web server Frankfurt”) and click **Create**.
3. Choose the **Docker**, **Native (systemd)** or **Windows** tab under **Install method** and copy the command.
4. Run the command on the server you want to watch, as described below.

> [!IMPORTANT] The token is shown only once
> The token inside the command is this server's own key and is not shown again after you close the window. If you lose it, you can get a new command from the server's page (the old token stops working).

:::tabs key=agent-os label="Install method"
@tab Docker
For Linux servers with Docker. The command spans several lines and starts with `sh <<'UPTIME_KURULUM'`. Connect to the terminal as **root** and paste the **whole** command. If you aren't root, change the first line to `sudo sh <<'UPTIME_KURULUM'`.

The command:

- Creates `/etc/uptime-agent.env` readable only by root (mode 600) and writes the token into it. The token never appears on any command line (in `ps` output).
- Starts a container named `uptime-agent`. The container sees the server's disks and `/proc`, `/sys` information **read-only**, all Linux capabilities are dropped and memory is limited to 256 MB.
- Downloads the program once, verifies it against the **SHA-256** digest in the command and keeps it in the `uptime-agent-bin` volume.

> [!CHECK]
> `docker logs uptime-agent` should show these lines:
>
> ```text
> /opt/uptime/uptime.dl: OK
> level=INFO msg="kontrol noktası başladı" sunucu=https://bekci.example.com sürüm=…
> level=INFO msg="sunucu metrikleri gönderiliyor" aralik=60
> ```
>
> The first line means the program passed the SHA-256 check. The next ones mean “agent started” and “sending server metrics every 60 seconds” (the start message reads the same in both roles).
@tab Linux (systemd)
For Linux servers without Docker (the **Native (systemd)** tab). Connect to the terminal as **root** and paste the **whole** command; if you aren't root, change the first line to `sudo sh <<'UPTIME_KURULUM'`.

The command:

- Downloads the program for the server's architecture (`x86_64` or `aarch64`), verifies it with SHA-256 and saves it as `/usr/local/bin/uptime`.
- Writes the token to `/etc/uptime-agent.env`, readable only by root.
- Creates a systemd service named `uptime-agent` (`/etc/systemd/system/uptime-agent.service`), enables it at boot and starts it.

> [!CHECK]
> The command should end with the line `Bekci agent installed and started (status: systemctl status uptime-agent)`.
>
> ```bash
> systemctl status uptime-agent      # you should see "active (running)"
> journalctl -u uptime-agent -n 20   # the last log lines
> ```
@tab Windows
For Windows servers (Windows amd64).

1. Search for PowerShell in the **Start** menu, right-click it and choose **Run as administrator**.
2. Paste the one-line command you copied from the **Windows** tab in the panel and press <kbd>Enter</kbd>.

The command:

- Downloads the program, verifies it with SHA-256 and installs it as `C:\Program Files\Uptime\uptime.exe`.
- Writes the token to `C:\Program Files\Uptime\agent.env`. Only SYSTEM and Administrators can access this folder.
- Installs a service named `uptime-agent` that starts automatically with Windows and restarts itself on failure.

> [!CHECK]
> The command finishes without errors. The `uptime-agent` service shows as **Running** in the **Services** list (services.msc). Log file: `C:\Program Files\Uptime\agent.log`. Startup errors also appear in the Windows **Event Viewer** (Application log, source `uptime-agent`).

On Windows the load average is an approximation (derived from the processor queue); temperature and Docker containers aren't collected.
:::

> [!CHECK]
> Within a minute or two the install window in the panel changes from **Waiting for connection…** to **Connected ✓**. Open the server's page with **Go to server**; the charts start to fill.

## Install a check location {#check-location}

A check location checks your monitors from another city or network too. That way you can tell whether an outage is seen from one place only or from everywhere.

1. Under **Settings → Check locations**, click **New check location**.
2. Give it a short name describing the place (e.g. “Frankfurt”) and save.
3. Run the command shown on the server that will be the check location (it needs Docker), as **root**, pasting the whole command. It creates `/etc/uptime-probe.env` and a container named `uptime-probe`.
4. Close the window with **I've copied it, close**.
5. On a monitor's edit page, select this check location under **Locations** and choose the **Outage rule**: down when any location, the majority of locations or all locations fail.

> [!CHECK]
> Within a few seconds the check location shows as **Online** in the list. A check location that sent results in the last 90 seconds counts as online. `docker logs uptime-probe` should contain a `msg="kontrol noktası başladı"` (“agent started”) line.

## Files the install creates {#files}

| Install | Program | Token (settings file) | Service / container |
|---|---|---|---|
| Server agent, Docker | `uptime-agent-bin` volume | `/etc/uptime-agent.env` | `uptime-agent` container |
| Server agent, systemd | `/usr/local/bin/uptime` | `/etc/uptime-agent.env` | `uptime-agent` service |
| Server agent, Windows | `C:\Program Files\Uptime\uptime.exe` | `C:\Program Files\Uptime\agent.env` | `uptime-agent` service |
| Check location, Docker | `uptime-probe-bin` volume | `/etc/uptime-probe.env` | `uptime-probe` container |

## Updating {#update}

The agent does **not** update itself: the program is downloaded and verified once, and the same program is used on every restart. That way, even if the server running your panel is compromised, no new program lands on your servers by itself. To update your agents after updating Bekci:

1. Get the **current** install command from the panel. Since the token is only shown once, this regenerates it (the old token stops working immediately):
   - Server agent: on the server's page, **Install command** → **Regenerate token and show command**.
   - Check location: in the **Settings → Check locations** list, **Regenerate token** in the row's menu.
2. For Docker installs, remove the old one first:

   ```bash
   docker rm -f uptime-agent; docker volume rm uptime-agent-bin     # server agent
   docker rm -f uptime-probe; docker volume rm uptime-probe-bin     # check location
   ```

3. Run the new command. For systemd and Windows installs there's nothing to remove; the command replaces the program and the service.

## Removing {#remove}

First delete the server or check location in the panel (its token stops working). Then, on the server where the agent runs:

:::tabs label="Install to remove"
@tab Docker — server agent
```bash
docker rm -f uptime-agent
docker volume rm uptime-agent-bin
rm -f /etc/uptime-agent.env
```
@tab Docker — check location
```bash
docker rm -f uptime-probe
docker volume rm uptime-probe-bin
rm -f /etc/uptime-probe.env
```
@tab Linux (systemd)
```bash
systemctl disable --now uptime-agent
rm -f /etc/systemd/system/uptime-agent.service /usr/local/bin/uptime /etc/uptime-agent.env
systemctl daemon-reload
```
@tab Windows
In a PowerShell opened as administrator:

```powershell
& "$env:ProgramFiles\Uptime\uptime.exe" service uninstall
Remove-Item -Recurse -Force "$env:ProgramFiles\Uptime"
```

The first line stops and deletes the service and removes the settings file (token) and the Event Log source; the second line deletes the program and its logs.
:::

## IP lock {#ip-lock}

For new agents **Lock to IP** is on by default: the IP address of the agent's first connection is recorded (the full address for IPv4, the /64 block for IPv6), and requests from any other address are refused afterwards. Even if the token is stolen, it can't be used from another machine.

If the server's IP address changes (e.g. you moved the server), reset the lock; the next connection records the new address:

- **Server agent:** on the server's page, **Settings** → **Reset lock**.
- **Check location:** in the **Settings → Check locations** list, **Edit** in the row's menu → **Reset lock**.

You can turn the lock off entirely in the same place (the **Lock to IP** switch). If you suspect the token leaked, regenerate it and run the install command again (the [update](#update) steps).

## Troubleshooting {#troubleshooting}

Logs: on Docker `docker logs uptime-agent` (for a check location `uptime-probe`), on systemd `journalctl -u uptime-agent`, on Windows `C:\Program Files\Uptime\agent.log`. The agent's log messages are in Turkish; the table gives their meaning.

| What you see in the log or the panel | Cause and fix |
|---|---|
| `ana sunucu isteği reddetti: ajan devre dışı veya başka bir IP'ye kilitli` (status 403) — “the main server refused: the agent is disabled or locked to another IP” | The server's IP changed or the agent is disabled in the panel. [Reset the IP lock](#ip-lock) or enable the agent again. |
| `ana sunucu token'ı reddetti` (status 401) — “the main server rejected the token” | The token is invalid (the server was deleted or the token regenerated). Get a new command from the panel and run it. |
| `Program özeti uyuşmuyor: kurulum komutunu panelden yenileyin` — “program checksum mismatch: get a fresh install command from the panel” | The command is from an older version; Bekci has been updated. Get the current command from the panel; on Docker remove the container and the volume first ([updating](#update)). |
| “The agent can’t read host metrics” in the panel | The agent runs in Docker without the server's `/proc` and `/sys` mounts. Use the command from the panel exactly as given, in full. |
| `PROBE_SERVER https olmalı; şifrelenmemiş http için PROBE_ALLOW_INSECURE=1 gerekir` — “PROBE_SERVER must be https; unencrypted http needs PROBE_ALLOW_INSECURE=1” | The panel is at an `http://` address. Put the panel behind HTTPS or add `PROBE_ALLOW_INSECURE=1` to the settings file. |
| `ana sunucuya ulaşılamıyor, tekrar denenecek` — “can't reach the main server, will retry” | The agent can't reach the panel's address. Check `BASE_URL`, DNS and outgoing connections from the agent's server (e.g. `curl -sI https://bekci.example.com/healthz`). |
| “Docker wasn’t found or the agent can’t access the Docker socket” in the panel | Container data isn't collected; that's normal if there's no Docker. If there is, make sure the `/var/run/docker.sock` mount from the command is in place. |
