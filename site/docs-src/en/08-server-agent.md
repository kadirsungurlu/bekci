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
> Within a minute or two the install window in the panel changes from **Waiting for connection…** to **Connected**. Open the server's page with **Go to server**; the charts start to fill.

## Alert rules {#alert-rules}

The **Alert rules** section on a server's page decides when a notification is sent. A new server gets default rules: offline after 3 min without data, CPU and RAM 10-min average at 90%, fullest disk at 85%, and reboot. Each rule can be enabled or disabled on its own; a server without rules never sends alerts.

**Warning and critical thresholds.** CPU, RAM, swap, load, temperature, network and disk rules take an optional **warning** threshold (it must be below the critical one). When the value crosses the warning threshold a *warning* is sent, when it crosses the critical threshold a *critical* alert; when the level drops you get a "downgraded from critical to warning" note, and when the value falls below the threshold a resolved notification. The incident stays a single record; the **Incidents** page shows level changes on its timeline. In the server's **Notification channels** section you can pick a level per channel: *all*, *warnings only* or *critical only* — for example Slack gets everything while your phone only rings for critical. The resolved notification goes to the channels that received the incident's highest level.

**Container alarm.** The *Container* metric alerts when a Docker container stops running. Leave the name empty and the alarm fires when **any** container previously seen on the server stops; type a name (names seen on the server are suggested) and only that container is watched — the alarm also opens when the name no longer appears at all (the container was removed). The duration is how many minutes the container must be not running; a container that restarts within seconds doesn't trigger it. On servers without Docker data (Windows, an agent without the Docker socket mounted) the rule stays silent and never produces a false alarm. Stopped containers are listed in the server page's container table with a **stopped** badge; the agent shipped with this version reports stopped containers too; older agents only report running ones, and the rule then fires when the name disappears (see [updating](#update)).

**Reboot detection.** The *Reboot* rule sends "*Name* was rebooted" when the server's boot time moves forward (by at least 60 seconds) and adds a row with the new boot time to the **Events** list on the server page. This is an instant notification: it doesn't open an incident and there is no resolved message. If you want the record but not the notification, disable the rule — reboots are still written to the list.

### Server maintenance {#server-maintenance}

Before a planned reboot or upgrade open a window on the **Maintenance windows** page and pick the servers (or *All servers*) in the **Affected servers** section; selecting monitors in the same window is optional. While the window is active no new alerts open on the selected servers and no offline or reboot notifications are sent; the server list and detail page show a **Maintenance** badge. An alert opened earlier can still resolve during maintenance (the notification is sent). When the window ends monitoring resumes where it left off; metrics still above a threshold open an alert after the normal duration.

## Install a check location {#check-location}

A check location checks your monitors from another city or network too. That way you can tell whether an outage is seen from one place only or from everywhere.

1. Under **Settings → Check locations**, click **New check location**.
2. Give it a short name describing the place (e.g. “Frankfurt”) and save.
3. Run the command shown on the server that will be the check location (it needs Docker), as **root**, pasting the whole command. It creates `/etc/uptime-probe.env` and a container named `uptime-probe`.
4. Close the window with **I've copied it, close**.
5. On a monitor's edit page, select this check location under **Locations** and choose the **Outage rule**: any location, the majority of locations or **Partial reachability split** (an outage only when all locations fail). With **Also notify on partial reachability outages** enabled, a separate notification is sent when a location goes down or becomes unreachable while the monitor stays up, and again when it recovers; uptime is not affected. On a new monitor, selecting a second location switches the rule to **Partial reachability split** and turns this notification on; you can change both before saving. Details: [Partial reachability outage](#partial-reachability).

> [!CHECK]
> Within a few seconds the check location shows as **Online** in the list. The check location sends the panel a heartbeat every **panel interval** (5 s by default); when **loss threshold** (3 by default) heartbeats in a row are missed, i.e. within 15 seconds by default, it counts as offline. A location whose connection drops (stopped, crashed) shows as offline immediately. Older agents that send no heartbeat keep the old rule (a request in the last 90 seconds). `docker logs uptime-probe` should contain a `msg="kontrol noktası başladı"` (“agent started”) line.

### Connections between check locations (mesh) {#mesh}

Check locations also connect to each other and measure the path between them: each pair keeps a single persistent TCP connection and both directions ping over it every **mesh ping interval** (5 s by default). When loss threshold replies in a row are missed, that direction counts as down. Measurements are reported to the panel in batches every panel interval. Connections are signed with a per-pair key generated by the panel (HMAC-SHA256); frames carry no secrets, so no TLS is used. Regenerating a check location's token or disabling it also rotates its keys.

- **Port:** The agent listens on `7480/TCP` by default (`MESH_ADDR`, `-` turns listening off). Open it in the firewall so other check locations can reach it.
- **Behind NAT:** A location that cannot listen (not reachable from outside) still connects to the listening peers itself and is measured. Tell the panel by entering `-` in the **Mesh address** field of the check location's edit dialog; a pair where neither end listens counts as “not measurable”, not as down.
- **Mesh address:** When empty, peers connect to port 7480 of the IP the check location uses to reach the panel. Enter `host:port` if a different address or port is needed.
- **Settings:** **Settings > Check locations** has the mesh ping interval (1-60 s), the loss threshold (1-10 misses in a row) and the panel interval (1-60 s). Threshold times interval must be at least 3 seconds (a single lost packet must not raise an alarm). Each check location can override these in its edit dialog; with **Use global value** selected the global setting applies.

**Verdict and notification.** When at least two check locations send heartbeats, the panel interprets all links together (including the main server) and sends a single mesh notification instead of separate "offline" ones:

- **Network partition:** the locations split into at least two groups that cannot reach each other.
- **General reachability problem:** one location is unreachable on at least two links while the others reach each other (the problem is on that location).
- **Path problem:** two locations cannot reach each other but are reachable through other locations.
- **One-way link:** one direction works, the reverse is down.
- **Insufficient observation:** a link is down but there are not enough measurements to interpret it.

The verdict waits one panel interval and is issued only if it persists (a momentary blip sends nothing). The notification includes the message owner (main server), the state of every link and the interpretation; it goes to the channels of the affected locations that have **notify when offline** selected. The incident is visible on the **Incidents** page to administrators only; when the links recover it closes and a recovery notification is sent.

**Topology diagram.** **Settings > Check locations** shows a live diagram above the connection matrix: the main server on top, check locations around it. A solid line with a check mark means two-way reachability, a cross means the link is down, an arrow shows the only working direction (one-way link), and a dashed grey line means the link cannot be measured. Hover over or keyboard-focus a link to see both directions' state, round-trip time (RTT), consecutive losses and the last successful measurement. The mesh incident detail page draws the same diagram with the state at the time of the incident. Telegram notifications include the diagram as a PNG (if sending the image fails, the text is still sent), and e-mail notifications include it as an inline image; plain-text clients read the text unchanged.

### Partial reachability outage {#partial-reachability}

When the target is reachable from some locations but not from others (for example, its firewall blocks a country), it is not a full outage. With the **Partial reachability split** rule:

- If every location fails, the monitor goes **Down** and the usual outage notification is sent.
- If only some fail, the monitor stays **Up** and uptime is not affected; a “Partial reachability outage” incident opens on the **Incidents** page. If notifications are on, the title reads “Partial reachability outage: <monitor>” and each location gets a “Successful” or “Failed” line.
- A partial reachability decision needs at least one location with a healthy connection reporting “up”. If no check location can be reached, the decision rests only on the main server's result; the notification and the incident detail say so.
- If, during a partial reachability outage, the connection to the location reporting “up” drops, the outage is held for 3 more check intervals; if the connection does not come back, the monitor goes **Down**.

The main server's location name (default “Main server”) can be changed under **Settings → General**; notifications and incidents show that name.

Existing monitors keep their rule. To move a few monitors to the new rule, pick it on each edit page; for many monitors use the API (`PUT /api/monitors/{id}/locations` per monitor with `down_when: "all"` and `notify_partial: true`; also send the selected locations as `include_local` and `probe_ids`). See [Integrations](/en/docs/integrations/) for API keys and sample requests.

### Offline notification {#check-location-notification}

To be told when a check location goes down, open **Edit** from the row menu, enable **Notify when offline** and pick the notification channels. When loss threshold heartbeats in a row are missed (3 x 5 s = 15 seconds by default), a “*Name*: check location unreachable” notification is sent and an incident of type “Check location offline” is opened on the **Incidents** page; when it reconnects, a “… back online” notification closes the incident with the downtime. A restart shorter than that does not notify; raise the threshold or the interval for more tolerance. If you disable the check location yourself, nothing is sent and an open incident is closed quietly. These notifications are independent of the monitors' own channels: they go to the channels selected for the check location.

### Second line (OOB) {#oob}

If the check location's server has a second network connection (OOB, management line, backup line), the agent moves its panel connection to that line when it can't reach the panel over the primary line. The check location then doesn't show as “offline” when the primary line fails; the panel knows which line it is on and notifies you.

Only the agent's **connection to the panel** (job list, results, heartbeat, metrics, updates) switches lines. The checks the agent runs (HTTP, ping, DNS etc.) and the mesh connections always follow the operating system's routing.

**Enter the OOB IP.** In the **Settings → Check locations** list, open **Edit** from the row's menu and in the **OOB (second line)** section enter the second line's IP address in **OOB IP address**, and optionally a name shown in notifications in **Line name** (default “OOB”, at most 40 characters). Clearing the IP turns the feature off. Loopback (`127.0.0.1`, `::1`), multicast and `0.0.0.0` style addresses are rejected.

**IP lock.** When the [IP lock](#ip-lock) is on, the panel accepts requests from the OOB IP in addition to the locked primary IP. A request from the OOB IP never changes the lock; the lock is pinned only by the first request over the primary line. The OOB IP is compared as a full address (not a /64 block for IPv6).

**Finding the interface.** The agent looks for the OOB IP given by the panel among the server's network interfaces and binds the panel connection to that interface (source address = OOB IP). If the OOB IP lives on a NAT device in front of the server (a public IP) rather than on the server, no interface is found: the panel shows an **OOB interface not found** warning and the agent doesn't switch to OOB. In that case add the interface name to the agent's settings file (`/etc/uptime-probe.env`), e.g. `PROBE_OOB_IFACE=eth1`; that interface's address of the same family (IPv4/IPv6) as the panel address is used as the source. Restart the agent; in Docker recreate the container (`docker restart` doesn't re-read the settings file).

**Linux.** The connection is bound to the interface with `SO_BINDTODEVICE` and to the OOB IP. This needs the `CAP_NET_RAW` capability (or root); the Docker install command already grants it (`--cap-add NET_RAW`). Without it only the source address is bound, and the policy routing rule below is **required** for packets to leave through the OOB interface. Even with the capability, set up the same rule if the OOB interface has no gateway of its own (the default route is only on the primary line):

```bash
# separate routing table for the OOB interface (e.g. eth1, gateway 10.20.0.1, OOB IP 10.20.0.15)
echo "100 oob" >> /etc/iproute2/rt_tables
ip route add default via 10.20.0.1 dev eth1 table oob
ip rule add from 10.20.0.15 table oob
# check
ip rule
ip route show table oob
curl --interface eth1 -sI https://bekci.example.com/healthz
```

These commands are lost on reboot. To make them permanent, add them to your distribution's network configuration (netplan: `routing-policy` and `routes` with `table: 100`; NetworkManager: `ipv4.routing-rules` and `ipv4.routes ... table=100`; ifupdown: `post-up` lines on the interface).

**Docker.** On the default bridge network the container doesn't see the server's OOB interface. Run a check location that uses OOB on the host network: replace `-p 7480:7480` in the install command with `--network host` (no port publishing is needed on the host network; mesh listens on port 7480 directly). With Compose, add `network_mode: host` to the service and remove its `ports:` lines.

**Windows.** The connection is bound to the OOB interface with `IP_UNICAST_IF`; no extra privileges are needed. The OOB interface must have a gateway (otherwise Windows can't send the packets). For NAT, add the interface name (`PROBE_OOB_IFACE=Ethernet 2`) to `C:\Program Files\Uptime\agent.env`.

**Switching.** The agent switches to OOB when both conditions hold:

- Loss threshold (Settings → Check locations, default 3) requests in a row to the panel over the primary line failed with a connection error (could not connect, timeout). `4xx`/`5xx` responses from the panel don't count; if the panel answers, the line is fine.
- The panel's `GET /healthz` answered successfully over the OOB line.

If mesh is on and the agent still reaches at least one peer over the primary line, it doesn't switch: the problem is on the panel's side, not the line. Open connections are closed on the switch and re-established over OOB. With default settings the switch happens about loss threshold x panel interval (3 x 5 s = 15 s) after the primary line fails. The log shows `ana hattan Main'e ulaşılamıyor; OOB hattına geçildi` (“panel unreachable over the primary line; switched to the OOB line”).

**Switching back.** After at least **5 minutes** on OOB, the agent returns to the primary line once it gets **3 successful** `/healthz` responses in a row over it (log: `ana hatta dönüldü`, “back on the primary line”). If the OOB line fails too, there is no wait: a single successful `/healthz` over the primary line is enough.

**Notifications.** On the switch “*Name* cannot connect over the primary line, now connected via *OOB name*” is sent, on the return “*Name* is back on the primary line” (with the time spent on the second line). They go to the check location's **Notify when offline** channels ([Offline notification](#check-location-notification)); for a server agent, to the server's notification channels. The badge in the check locations list shows the active line (**Primary line** or “Via *OOB name*”) and when it switched.

**Check results while on OOB.** While on OOB the agent's checks still leave over the primary (broken) path, so that location's results don't count towards the outage decision and the location is treated as “unknown” (shown as a note on the location card). Even if a location on OOB reports “Failed”, no partial reachability outage or outage is opened.

The agent reports the active line on every request with the `X-Probe-Uplink` header (`primary` or `oob`) and the interface state with `X-Probe-OOB-Iface` (`ok` or `missing`); a reverse proxy in front of the panel must not strip these headers.

## Files the install creates {#files}

| Install | Program | Token (settings file) | Service / container |
|---|---|---|---|
| Server agent, Docker | `uptime-agent-bin` volume | `/etc/uptime-agent.env` | `uptime-agent` container |
| Server agent, systemd | `/usr/local/bin/uptime` | `/etc/uptime-agent.env` | `uptime-agent` service |
| Server agent, Windows | `C:\Program Files\Uptime\uptime.exe` | `C:\Program Files\Uptime\agent.env` | `uptime-agent` service |
| Check location, Docker | `uptime-probe-bin` volume | `/etc/uptime-probe.env` | `uptime-probe` container |

### Staying connected during a DNS outage {#dns-cache}

When the agent resolves the panel's domain, it writes the IP addresses to `uptime.dns.json` in the program directory (readable by the owner only; change the directory with `PROBE_STATE_DIR`). The name is resolved again every hour. If the DNS server does not answer or the name is not found, the agent keeps connecting with the **last known IP**; there is no time limit, and the agent uses the IP from the file even if it restarts meanwhile. Once DNS works again the new IP is picked up immediately.

- This only applies to the agent's connection to the panel; the checks the agent runs (HTTP, DNS, etc.) resolve names as usual.
- The connection is still verified against the panel's domain (TLS certificate and SNI); if a different server now sits at the old IP, the connection is refused.
- If the program directory is not writable, the cache is kept in memory only (it is empty after the agent restarts).

## Server inventory {#inventory}

Every five minutes the agent collects the server's inventory and sends it to the panel when something changes, and at least once an hour. It appears on the **Inventory** tab of the server page, with search and filters:

- **Services:** systemd units, their state and whether they start at boot.
- **Containers:** name, image, state, health and restart count. Opening a row shows CPU and memory charts for the last 24 hours.
- **Ports:** listening TCP/UDP ports and their process. Ephemeral UDP ports are left out.
- **Certificates:** local files (`/etc/letsencrypt/live`, `/etc/ssl/private`, web server folders, the Caddy Docker volume) and certificates read from listening ports with a TLS handshake.
- **Disk partitions:** mount point, device, file system and size. Opening a row shows a usage chart.

Environment variables and private keys are **not collected**. Passwords, tokens and connection string passwords in command lines are masked on the agent before sending, and again by the panel when saving.

An incident opens and the server's notification channels are alerted 14, 7 and 1 days before a certificate expires. Change the thresholds under **Settings > General > SSL alert days**; leave the field empty to turn alerts off. The incident closes by itself when the certificate is renewed. To leave a certificate out of alerts, use **Ignore** on the Inventory tab.

| Variable | Default | Purpose |
|---|---|---|
| `INVENTORY` | `1` | `0` turns the inventory off on this agent. |
| `PROBE_CERT_DIRS` | empty | Extra certificate folders (separated by `:`). |
| `PROBE_TLS_SCAN` | `on` | `off` stops connecting to listening ports. |
| `PROBE_TLS_SNI` | empty | Domain names to try when scanning ports (comma separated). |

On Windows only disk partitions are collected for now; services and ports are not supported. An agent running in Docker sees services and ports when the host file system is mounted (`HOST_ROOT`, `HOST_PROC`, `HOST_SYS`). Older agents do not send an inventory; the tab then asks you to update the agent.

## Server events and disk forecast {#events}

The **Events** tab on the server page lists, in time order: reboots, processes killed for lack of memory (OOM), failed or recovered services, stopped, restarted or updated containers, opened or closed ports, and alerts starting and ending. Records are kept for one year.

For OOM the agent watches the `/proc/vmstat` counter and reads the process name from `/dev/kmsg`. If the kernel log cannot be read the event is still stored, without a process name. Service, container and port events come from the inventory round (every 5 minutes).

**Disk forecast:** the panel derives each partition's growth rate from the last 7 days of hourly usage. The Disks table on the server page shows days left and daily growth; the Servers list flags partitions with under 30 days left. A forecast needs at least 12 hours of data, until then it reads "Collecting data". If usage is not growing it reads "Stable".

Two alert rules are on by default for every server; change the thresholds in the server's Settings dialog:

- **Disk full forecast:** opens an alert and notifies when a partition will be full within 7 days. It closes once the time left exceeds 1.5 times the threshold. Irregular growth (R² under 0.5) does not open a new alert.
- **Rapid disk growth:** alerts when a partition grows by 5 points or more within 60 minutes. It closes when the rise falls under 60% of the threshold.

Older panels ignore the `events` field; older agents send no events, but inventory-based events and the forecast still work.

## Updating {#update}

Agents installed with Bekci 1.4.0 or later **update themselves**: when you upgrade Bekci, server agents and check locations switch to the same version within seconds. An agent installs the program from the panel only if the **project signature** verifies. The signature's public key is built into the agent, and the private key is only used by the official release build. So even if the server running your panel is compromised, no unsigned or modified program lands on your servers, and agents can't be downgraded.

How it works:

- When an agent sees the panel's version, it downloads the signed program. It verifies the checksum and the signature, replaces the running program and restarts. Docker, systemd or the Windows service manager starts it again with the new program.
- If the new version can't reach the panel within three attempts, the agent goes back to the previous program and tells the panel.
- The status shows on the server page in the **Agent version** row and in the **Version** column of the check locations list: **Up to date**, **Update available**, **Updating…**, **Update failed** or **Old agent**.

Settings:

- **Settings → General → Update agents automatically**: the global setting, on by default.
- Per agent: **Automatic updates** in the server's **Settings** dialog or the check location's **Edit** dialog. The choices are the global setting, on or off.
- While it's off, **Update now** updates an agent once. **Update all** on the Servers page and in the check locations list updates every agent that can be updated.
- To turn updates off on the agent itself, add `AUTO_UPDATE=0` to the agent's settings file (`/etc/uptime-agent.env`, `/etc/uptime-probe.env` or `agent.env`) and restart the agent.

> [!NOTE]
> Only official release images (such as `kadirsungurlu/bekci:1.5.0`) contain signed agents. A panel built from source or from a commit doesn't offer updates to agents, and the agent row says **Unsigned build** or **Unversioned build**. Agents that run from the app image with `PROBE_IMAGE` can't update themselves either (**Manual updates**); update the image for those.

### Updating old agents once {#old-agents}

Agents installed before 1.4.0 can't update themselves. They show as **Old agent** in the list. Reinstall them by hand once, and later versions arrive on their own:

1. Get the **current** install command from the panel. Since the token is only shown once, this step regenerates it and the old token stops working immediately:
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

If you entered an [OOB IP](#oob) for a check location, requests from that address are accepted too; the lock doesn't change.

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
| “Check location unreachable” in an incident or notification | No results arrive from that location (agent stopped, network down). The location is left out of the outage rule; a location whose last result was “down” keeps counting as down until that result goes stale (an agent restart doesn't close and reopen the incident). Check the agent and its access to the panel. |
| “… server unreachable” notification, “Server unreachable” in the incident list | No data from the server agent for 3 minutes (offline alert). Check that the agent container/service is running and can reach the panel; once data arrives, a “… sending data again” notification is sent and the incident closes. |
