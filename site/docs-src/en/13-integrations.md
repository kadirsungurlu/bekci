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

`event` values: `down`, `up`, `reminder`, `cert`, `slow`, `slow_resolved`, `location_down`, `location_up`, `server_alert`, `server_resolved`, `probe_offline`, `probe_online`, `test`. Server alerts add a `server` object (`metric`, `value`, `threshold`, `minutes`); multi-location monitors add a `locations` array.

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
