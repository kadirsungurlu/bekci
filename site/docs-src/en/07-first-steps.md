---
id: first-steps
title: First steps
nav: First steps
description: Your first ten minutes after installing - admin account, first monitor, notification channel, status page, language settings and adding Bekci to your phone.
section: Usage
order: 7
slug: first-steps
---

Bekci is running and you can open the panel in your browser. This page walks you through what to do in the first ten minutes. On desktop the menu is on the left; on a phone it's at the bottom of the screen, and some sections such as **Settings** are inside the **More** menu.

## 1. Admin account {#admin}

The first time you open the panel, the **Welcome** screen asks you to create the admin account:

- **Username:** 3–32 characters; letters, digits, periods, hyphens and underscores.
- **Password:** at least 8 characters. Type the same in **Password (again)**.

Click **Create account** and you're taken straight into the panel. This screen appears only once, while there are no users yet.

> [!TIP] Turn on two-factor authentication
> With **Settings → My account → Two-factor authentication → Enable** you can also require the code from an authenticator app on your phone (Google Authenticator, Authy, 1Password…) at sign-in. Keep the recovery codes somewhere safe. If you forget your password: [password reset](/en/docs/troubleshooting/#password-reset).

> [!NOTE]
> The panel follows your browser's language on the sign-in screen, and there's a language switch there too. You can change your own interface language any time ([language settings](#language)).

## 2. Add your first monitor {#first-monitor}

1. On the **Monitors** page, click **Add monitor** (or **New** at the top right).
2. Choose **HTTP(S)** as the **Monitor type** (to watch a website or an API).
3. Give it a clear **Name** (e.g. “Company website”) and enter the site's address in **URL** (e.g. `https://example.com`).
4. **Check interval** is how often it's checked: at least 20 seconds, at most 24 hours. 60 seconds is a good start.
5. Click **Add monitor**.

![New monitor form: monitor type picker with web, network, database and system categories](/img/yeni-monitor-1600.webp)

> [!CHECK]
> The monitor appears in the list and the first check runs within a few seconds. If the site is up, it turns green; the response time and status bars update after every check without reloading the page.

> [!TIP] Recent checks
> The **Recent checks** section on a monitor's detail page lists the last 50 checks one by one: time, result, response time, message (e.g. `200 OK`, `timeout`) and, for multi-location monitors, the locations that produced the result. It answers "why did this count as an outage?" faster than the chart and refreshes itself as new results arrive.

> [!TIP] Domain expiry alert
> For HTTP(S), DNS and TLS certificate monitors Bekci also watches the domain registration: it resolves the target's registrable name (e.g. `www.example.co.uk` → `example.co.uk`), queries it over RDAP once a day and shows the result in the **Domain** card on the detail page (days left, expiry date, registrar). When it reaches the thresholds in **Settings → General → Domain alert days** (default 30, 14, 7, 1) the attached channels are notified: 7 days or less is 🔴, more is 🟡; once per threshold. Only the main server queries, and monitors sharing a domain are updated with a single lookup. For TLDs without RDAP (some country codes) the card says "RDAP lookup is not available" and nothing is sent; on a network error the previous information is kept and no false alert is produced. You can turn the alert off per monitor with the **Domain expiry alert** checkbox in the monitor form.

> [!TIP] Slow response alert
> To hear about a site that is up but slow, fill in **Advanced settings → Response-time threshold (ms)** (e.g. 2000). When the average response time of the last N successful checks (**Averaging window**, default 3) exceeds the threshold, the monitor gets a **Slow** badge, a 🟡 “responding slowly” notification goes to its channels and an incident of type “Slow response” opens on the **Incidents** page; it closes with 🟢 once the average drops below 90% of the threshold. The status stays “Up” and uptime is not affected; if the monitor goes down, the slow-response incident closes quietly and a normal outage incident opens.

### Monitor types {#monitor-types}

The **Monitor type** picker offers 18 types in four groups:

| Group | Types | What for |
|---|---|---|
| Web | HTTP(S) | Websites and APIs; headers, body, authentication, keyword and JSON query checks, SSL and domain expiry alerts |
| Network and protocols | TCP Port, Ping, DNS, TLS certificate, SMTP, WebSocket, gRPC, MQTT, SNMP | Servers, ports, domains, mail servers and network devices |
| Database | MySQL / MariaDB, PostgreSQL, Microsoft SQL Server, Redis, MongoDB | Connects to the database and runs a simple query |
| System and signals | Docker container, Push, Group | Container health; cron jobs sending regular signals; combining several monitors into one status (group rules: "any", "all" or "more than N% down") |

The **Push** monitor works the other way round: Bekci doesn't check anything, it waits for your cron job or script to report in at regular intervals. After you save the monitor you get a unique address; your job calls it every time it runs, and if the call doesn't arrive the monitor goes **down**:

```bash
curl -fsS "https://⟦bekci.example.com⟧/api/push/⟦TOKEN⟧?status=up&msg=ok&ping=120"
```

Send `status=down` to report a failure. If your job is sometimes late (e.g. a 30-minute backup that occasionally takes 40), use the **Grace period (sec)** field in the form to allow extra time on top of the expected interval; the monitor only goes down once interval + grace period have passed. If the address leaked, the **Regenerate URL** button in the monitor form creates a new one; the old address stops working immediately.

## 3. Add a notification channel {#notifications}

Add at least one channel so you hear about it when a monitor goes down. There are 24 channels, including WhatsApp, Telegram, email, Slack, Discord, Microsoft Teams, ntfy, PagerDuty and webhooks.

1. On the **Notifications** page, click **New channel**.
2. Pick the channel under **Type** and fill in the details it asks for (e.g. **Bot token** and **Chat ID** for Telegram).
3. Try it with **Send test notification**; the test message should arrive within a few seconds.
4. Tick **Add to new monitors by default** so new monitors get this channel automatically. To add it to your existing monitors as well, tick **Add to all existing monitors**.
5. Click **Save**.

Notifications arrive with a short title: 🔴 outage, 🟢 recovery (locations that are still down are listed), 🟡 location outage, ⚠️ SSL warning, 🟡/🔴 domain expiry; emails contain both HTML and plain text. For a saved channel, **Send sample notifications** sends one sample of every type. On a temporary failure (network, HTTP 5xx/429) delivery is retried after 5 and 20 seconds; the result is written to the incident's timeline.

The **Webhook** channel sends JSON: `event` (`down`, `up`, `reminder`, `location_down`, `location_up`, `cert`, `domain`, `server_alert`, `server_resolved`, `test`), `title`, `text`, `message`, `time`, `downtime_seconds` (on recovery), `cert_days` (on SSL warnings), `domain` and `domain_days` (on domain alerts), `monitor` (`id`, `name`, `type`, `target`, `url`), `incident` (`id`, `url`; on incident-bound notifications), `locations` (failing locations of a multi-location monitor: `name`, `message`) and `server` (server alerts only: `id`, `name`, `metric`, `value`, `threshold`, `minutes`). An example payload is shown in the channel dialog.

### Notification rules {#notification-rules}

The **Rules** section of the channel dialog decides when the channel is notified and about what. Default: all events, any time, no delay (existing channels are unchanged).

| Rule | What it does |
|---|---|
| **Events to receive** | Unchecked types (e.g. recovery, reminder, SSL) are not sent to this channel. 🔴 and 🟢 are selected separately, so you can build a channel that only wants outages. |
| **Quiet hours** | A start-end window in the channel's time zone (`22:00`–`07:00` style windows across midnight are fine). In **Let only critical ones through** mode, 🔴 outage, server alert and check-location notifications and their 🟢 recovery go out immediately; 🟡 slow response, location outage and ⚠️ SSL warnings are deferred to the end of the window. In **Send nothing** mode everything is deferred. A deferred problem notification is sent when the window ends only if the problem is still ongoing (with a "🌙 Quiet hours ended" note); reminders are dropped. |
| **Delay** | "Notify only if it lasts N minutes": short outages (e.g. a two-minute restart) produce no notification. 🔴 goes out once the time has passed if the problem persists (with a "⏳ Delayed notification" note); if it never went out, that incident's 🟢 recovery and reminders are skipped too. |
| **Escalation** | "Also report every incident that has lasted N minutes": even when the channel is not attached to the monitor or server, every outage, server alert and check-location outage open for this many minutes arrives here with an "⏫ Escalation" note, followed by 🟢 when it resolves. Good for the on-call manager's channel. |
| **Notification language** | Per-channel language (overrides the global setting); tests and samples use it too. |

Skipped and deferred notifications are written to the incident's **timeline** with their reason ("the channel does not receive this event type", "quiet hours", "delay rule"…). Deferred notifications wait in the database and survive a restart. **Send test notification** and **Send sample notifications** ignore the rules.

> [!CHECK]
> The test notification reached your channel and the channel shows up in the **Notifications** list. You can also change a monitor's channels on the monitor's edit page.

## 4. Create a status page (optional) {#status-page}

A status page is a public page that shows your customers the state of your services, planned maintenance and past incidents.

1. Under **Status pages**, click **New page**.
2. Enter a **Title** (e.g. “Acme Service Status”) and an **Address (slug)** made of lowercase letters, digits and hyphens (e.g. `acme`).
3. Under **Groups and monitors**, enter a group name (e.g. “Websites”) and pick your monitors with **Add monitor**.
4. Turn on **Published**. You can protect the page with a **Page password** if you want.
5. Click **Create page**.

> [!CHECK]
> Your page opens at `https://⟦bekci.example.com⟧/durum/⟦acme⟧` (`durum` is Turkish for “status”). Open it in a browser where you're not signed in (e.g. a private window) to check.

To publish the page on a separate domain such as `status.example.com`, use the page's **Custom domain** field; the domain's DNS record must point to your server and your proxy must route that name to Bekci ([how to do it with the Caddy install](/en/docs/install/caddy/#status-domain)).

**Windows:** **Uptime windows** picks which percentages each monitor row shows (24 hours, 7, 30, 90 days; several are shown side by side, none means a single window following the bar view). **Incident window** (7/14/30/90 days) sets how much history the "Recent incidents" section and the RSS feed show.

**Manual incidents and updates:** announce a problem that is not detected automatically (e.g. a slowdown at your payment provider) with **Incidents → Open incident**: status page, title, severity (minor/major/critical), affected monitors and a first description. The incident appears on the page with its title, severity and state; **Post update** adds a state (Investigating → Identified → Monitoring → Resolved) and text, each update is a separate entry on the page and in the RSS feed; the **Resolved** state closes the incident. Automatic incidents can receive updates too ("cause found, fixing"); their closure is decided by the monitor. Viewer and customer accounts can only read incidents.

**Planned maintenance block:** active maintenance windows affecting the page's monitors (or all monitors) — ongoing ones and those starting within 7 days — appear in the page's **Planned maintenance** section and in the RSS feed. Its position and visibility are set in **Layout → Sections**; the section is not drawn when there is no window.

**Announcements:** add planned-maintenance or information notes from the **Announcements** card below the page editor. When adding one, **Also add to other pages** drops a copy of the same announcement onto the selected pages or all pages at once; copies are edited separately per page afterwards.

## 5. Language settings {#language}

Bekci's interface is available in English and Turkish. There are three separate language settings:

| What | Where | Affects |
|---|---|---|
| Interface language | **Settings → My account → Language → Interface language** (the sign-in screen has a language switch too) | Only your own account; applies on every device you sign in on |
| Status page language | **Page language** in the status page editor | Visitors of that page (status texts, dates, durations) |
| Notification language | **Settings → General → Notification language** | All notification messages sent to every channel |

## 6. Add it to your phone {#phone}

You can add Bekci to your phone like an app; there's nothing to download from a store:

- **iPhone and iPad:** open the panel in **Safari** → **Share** button → **Add to Home Screen**.
- **Android:** open the panel in **Chrome** → menu at the top right → **Install app** (in some versions **Add to Home screen**).

Opened from the home screen icon, it runs full screen and updates live. On Android the **Install app** option only appears when the panel is opened over HTTPS; it doesn't appear for an address such as `http://SERVER-IP:8080`.

## What next? {#next}

- **Watch your servers:** [install the server agent](/en/docs/server-agent/) for CPU, RAM, disk and network metrics.
- **Check from other locations:** add a [check location](/en/docs/server-agent/#check-location) to check your monitors from another city or network too.
- **Add your team:** create accounts with the **Admin**, **Editor** or **Viewer** role under **Settings → Users**; for your customers you can create accounts that only see the monitors assigned to them.
- **Silence notifications during planned maintenance:** the **Maintenance** section.
- **Move from another service:** import your UptimeRobot account or Uptime Kuma backup under **Settings → Backup / Restore**.
- **Set up backups:** [Updates, backups and rollback](/en/docs/updates-backups/).
- **Connect Prometheus/Grafana:** the `GET /metrics` endpoint exposes monitor status, response time and uptime as well as the server agents' CPU, RAM, disk and network metrics ([metric list](/en/docs/integrations/#prometheus)). Create a key under **Settings → API keys** and call it with the `Authorization: Bearer upk_…` header or Basic auth (user `metrics`, password the key); viewer permission is enough.
