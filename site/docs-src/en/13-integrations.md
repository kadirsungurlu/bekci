---
id: integrations
title: "Integrations: webhook, Prometheus, RSS and CSV"
nav: Integrations
description: The JSON body of webhook notifications and how to verify their HMAC signature, the Prometheus /metrics endpoint, the status page RSS feed and the CSV export of incidents.
section: Usage
order: 13
slug: integrations
---

Four ways to connect Bekci to your own systems: the generic **webhook** channel (signed), **Prometheus** metrics, the status pages' **RSS** feed and the **CSV** export of incidents.

## Webhook notifications {#webhook}

Choose **Notifications → New channel → Webhook**. On every event a JSON body is sent to your URL with `POST` (or `PUT`). The optional **Headers** field (such as `Authorization: Bearer …`) is added to every request.

```json title="Request body (example)"
{
  "event": "down",
  "title": "🔴 Website is down",
  "text": "🔴 Website is down\nTarget: https://example.com\n…",
  "message": "HTTP 503 Service Unavailable",
  "time": "2026-09-27T10:15:00+03:00",
  "downtime_seconds": 0,
  "monitor": { "id": 1, "name": "Website", "type": "http", "target": "https://example.com", "url": "https://bekci.example.com/#/monitors/1" },
  "incident": { "id": 42, "url": "https://bekci.example.com/#/incidents/42" }
}
```

`event` values: `down`, `up`, `reminder`, `cert`, `slow`, `slow_resolved`, `location_down`, `location_up`, `server_alert`, `server_resolved`, `server_reboot`, `probe_offline`, `probe_online`, `acked` (only to channels that select this event; carries an `ack: { by, note }` object), `test`. Server alerts add a `server` object (`metric`, `value`, `threshold`, `minutes`); multi-location monitors add a `locations` array. When [notification rules](/en/docs/first-steps/#notification-rules) apply, `escalated: true` (sent to an escalation channel for an incident open for N minutes), `delayed: true` (sent after the delay rule or quiet hours) and `elapsed_seconds` (time elapsed so far) are added.

### HMAC signature {#webhook-signature}

So that your receiver can verify a request really came from Bekci, put a long random value (e.g. `openssl rand -hex 32`) into the channel's **Signing secret (HMAC)** field. From then on every request carries this header:

```plaintext
X-Bekci-Signature: t=1759000000,v1=5f3b1c…e2a9
```

- `t`: when the request was sent (Unix seconds).
- `v1`: hex-encoded `HMAC-SHA256(secret, t + "." + raw body)`.

To verify, split the header, compute the same HMAC over the **raw body** (do not re-serialize the JSON) and compare in constant time. Reject old requests (e.g. `t` older than 5 minutes) to prevent replay attacks.

```javascript title="Node.js (Express) verification"
import crypto from 'node:crypto';
import express from 'express';

const SECRET = process.env.BEKCI_SECRET;
const app = express();

app.post('/bekci', express.raw({ type: 'application/json' }), (req, res) => {
  const sig = Object.fromEntries((req.get('X-Bekci-Signature') ?? '').split(',').map((p) => p.split('=')));
  const t = Number(sig.t);
  if (!t || Math.abs(Date.now() / 1000 - t) > 300) return res.status(400).end('stale request');
  const want = crypto.createHmac('sha256', SECRET).update(`${t}.`).update(req.body).digest('hex');
  if (!sig.v1 || !crypto.timingSafeEqual(Buffer.from(want), Buffer.from(sig.v1))) return res.status(401).end('bad signature');
  const ev = JSON.parse(req.body);
  console.log(ev.event, ev.monitor?.name);
  res.end('ok');
});
app.listen(3000);
```

```python title="Python (Flask) verification"
import hmac, hashlib, time, os
from flask import Flask, request, abort

SECRET = os.environ["BEKCI_SECRET"].encode()
app = Flask(__name__)

@app.post("/bekci")
def bekci():
    parts = dict(p.split("=", 1) for p in request.headers.get("X-Bekci-Signature", "").split(",") if "=" in p)
    t = int(parts.get("t", 0))
    if not t or abs(time.time() - t) > 300:
        abort(400)
    want = hmac.new(SECRET, f"{t}.".encode() + request.get_data(), hashlib.sha256).hexdigest()
    if not hmac.compare_digest(want, parts.get("v1", "")):
        abort(401)
    ev = request.get_json()
    print(ev["event"], ev["monitor"]["name"])
    return "ok"
```

> [!TIP]
> The signing secret is stored like the other secret fields: after saving it is shown masked and only re-entered to change it. The channel's **Send sample notifications** button sends signed requests too; use it to test your receiver.

## Exporting incidents as CSV {#csv}

The **Download CSV** button on the **Incidents** page downloads the incidents of the selected filter (up to 10,000 rows, newest first). The same file is available from the API; a session cookie or an API key is enough, and a user only receives the incidents they are allowed to see:

```bash
curl -H "Authorization: Bearer upk_…" "https://⟦bekci.example.com⟧/api/incidents?format=csv&kind=monitor" -o incidents.csv
```

The `kind` filter: `monitor` (outages), `partial` (location outages), `server` (server and check-location incidents); empty means all. Columns:

| Column | Meaning |
|---|---|
| `id` | Incident id (`/#/incidents/<id>`) |
| `kind` | `monitor`, `degraded` (slow response), `partial`, `server_offline`, `server_alert`, `probe_offline` |
| `source` | Name of the monitor or of the server / check location |
| `started_at`, `resolved_at` | RFC 3339 in local time; empty `resolved_at` while ongoing |
| `duration_seconds` | Duration in seconds; for an ongoing incident, so far |
| `cause` | Cause (in the request language) |

The file is UTF-8 and starts with a BOM so that Excel opens Turkish characters correctly.

### Recent checks and the audit log {#json-lists}

Two more lists are available with the same authentication:

- `GET /api/monitors/<id>/beats?limit=50` returns the monitor's most recent checks, newest first (`limit` at most 200). Each row has `time` (unix), `status` (0 down, 1 up, 2 retrying, 3 maintenance), `ping_ms` (−1: no measurement), `message` (in the request language) and, for multi-location monitors, `location`.
- `GET /api/audit` (admins only) returns the audit log; filters: `user` (username), `action` (a full code such as `monitor.delete` or an area prefix such as `monitor.`), `from`/`to` (unix), `q` (search in target, details, user and IP), `before` (paging: entries older than this id) and `limit` (at most 500). `GET /api/audit/facets` lists the usernames and action codes present in the log for filter boxes.

## Prometheus metrics {#prometheus}

`GET /metrics` serves data in the Prometheus text format (0.0.4). Create a key under **Settings → API keys** and call the endpoint with the `Authorization: Bearer upk_…` header or Basic auth (user `metrics`, password the key). Viewer permission is enough; a restricted user's key only sees the monitors and servers assigned to them.

```yaml title="prometheus.yml (excerpt)"
scrape_configs:
  - job_name: bekci
    scheme: https
    static_configs: [{ targets: ['⟦bekci.example.com⟧'] }]
    basic_auth: { username: metrics, password: upk_… }
```

| Metric | Labels | Meaning |
|---|---|---|
| `uptime_monitor_status` | `monitor_id`, `monitor_name`, `monitor_type` | 0 down, 1 up, 2 pending |
| `uptime_monitor_active` | monitor | 1 active, 0 paused |
| `uptime_monitor_response_time_ms` | monitor | Response time of the last check |
| `uptime_monitor_slow` | monitor | 1 = response-time threshold exceeded (only for monitors with a threshold) |
| `uptime_monitor_cert_days_remaining` | monitor | Days until the SSL certificate expires |
| `uptime_monitor_uptime_ratio` | monitor, `window` (24h, 7d, 30d) | Uptime ratio (0-1) |
| `uptime_server_online` | `server_id`, `server_name` | 1 the agent is sending data |
| `uptime_server_last_sample_timestamp_seconds` | server | Time of the last sample |
| `uptime_server_cpu_percent`, `uptime_server_memory_percent`, `uptime_server_memory_used_bytes`, `uptime_server_memory_total_bytes`, `uptime_server_swap_percent` | server | Last sample |
| `uptime_server_load1`, `uptime_server_load5`, `uptime_server_load15`, `uptime_server_load1_per_core` | server | Load averages |
| `uptime_server_net_rx_bytes_per_second`, `uptime_server_net_tx_bytes_per_second` | server | Network traffic |
| `uptime_server_disk_read_bytes_per_second`, `uptime_server_disk_write_bytes_per_second` | server | Disk I/O |
| `uptime_server_disk_percent` | server, `mount` | Partition usage |
| `uptime_server_temperature_celsius` | server | Hottest sensor |
| `uptime_server_uptime_seconds`, `uptime_server_containers` | server | Uptime, container count |
| `uptime_probe_online` | `probe_id`, `probe_name` | 1 check location online (unrestricted users only) |
| `uptime_incidents_open` | `kind` | Ongoing incidents (monitor, degraded, partial, server_offline, server_alert, probe_offline) |

Server values are the agent's last sample (sent once a minute); container and temperature-sensor lists are not exposed as labels, so cardinality stays bounded by the number of servers.

## Status page RSS feed {#rss}

Every published status page has an RSS 2.0 feed; the **RSS** link in the page footer and the `alternate` link in the page's `<head>` point readers to it:

```plaintext
https://⟦bekci.example.com⟧/durum/⟦slug⟧/feed.xml
https://⟦bekci.example.com⟧/api/public/pages/⟦slug⟧/feed.xml
```

The feed contains the incidents within the page's incident window (last 14 days by default; 7/14/30/90 in the editor) (if **Show incidents** is on for the page; start, resolution and duration — never the cause) and published announcements (expired ones included, future-dated ones excluded) in the page's language; when an ongoing incident is resolved, a separate entry is added. Manually opened incidents (with title and severity), every incident update (category "Incident update") and, if the page's **Planned maintenance** section is visible, ongoing/upcoming maintenance windows (category "Planned maintenance") are in the feed as well. Slack, Teams or any RSS reader can subscribe to the address.

> [!NOTE]
> The feed of a password-protected page is only served to a browser that has entered the password; there is no token for RSS readers. That is why the RSS link is not shown on protected pages.
