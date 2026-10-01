---
id: integrations
title: Entegrasyonlar: webhook, Prometheus, RSS ve CSV
nav: Entegrasyonlar
description: Webhook bildirimlerinin JSON gövdesi ve HMAC imzasını doğrulama, Prometheus /metrics ucu, durum sayfası RSS akışı ve olayların CSV dışa aktarımı.
section: Kullanım
order: 13
slug: entegrasyonlar
---

Bekci'yi kendi sistemlerinize bağlamanın dört yolu: genel **webhook** kanalı (imzalı), **Prometheus** metrikleri, durum sayfalarının **RSS** akışı ve olayların **CSV** dışa aktarımı.

## Webhook bildirimi {#webhook}

**Bildirimler → Yeni kanal → Webhook** seçin. Her olayda verdiğiniz adrese `POST` (ya da `PUT`) ile JSON gövde gönderilir. İsteğe bağlı **Başlıklar** alanı (`Authorization: Bearer …` gibi) her isteğe eklenir.

```json title="Gönderilen gövde (örnek)"
{
  "event": "down",
  "title": "🔴 Web sitesi çalışmıyor",
  "text": "🔴 Web sitesi çalışmıyor\nHedef: https://ornek.com\n…",
  "message": "HTTP 503 Service Unavailable",
  "time": "2026-09-27T10:15:00+03:00",
  "downtime_seconds": 0,
  "monitor": { "id": 1, "name": "Web sitesi", "type": "http", "target": "https://ornek.com", "url": "https://bekci.ornek.com/#/monitors/1" },
  "incident": { "id": 42, "url": "https://bekci.ornek.com/#/incidents/42" }
}
```

`event` değerleri: `down`, `up`, `reminder`, `cert`, `slow`, `slow_resolved`, `location_down`, `location_up`, `server_alert`, `server_resolved`, `probe_offline`, `probe_online`, `test`. Sunucu uyarılarında ek `server` nesnesi (`metric`, `value`, `threshold`, `minutes`), çok konumlu monitörlerde `locations` dizisi gelir.

### HMAC imzası {#webhook-imza}

Alıcınızın isteğin gerçekten Bekci'den geldiğini doğrulaması için kanal ayarındaki **İmza anahtarı (HMAC)** alanına uzun, rastgele bir değer yazın (ör. `openssl rand -hex 32`). O andan sonra her istek şu başlığı taşır:

```plaintext
X-Bekci-Signature: t=1759000000,v1=5f3b1c…e2a9
```

- `t`: isteğin gönderildiği an (Unix saniye).
- `v1`: `HMAC-SHA256(anahtar, t + "." + ham gövde)` değerinin onaltılık (hex) hali.

Doğrulamak için başlığı ayırın, aynı hesabı **ham gövde** üzerinde yapın (JSON'u yeniden serileştirmeyin) ve sabit zamanlı karşılaştırın. Eski istekleri (ör. 5 dakikadan eski `t`) reddederek tekrar saldırılarını engelleyin.

```javascript title="Node.js (Express) doğrulama"
import crypto from 'node:crypto';
import express from 'express';

const SECRET = process.env.BEKCI_SECRET;
const app = express();

app.post('/bekci', express.raw({ type: 'application/json' }), (req, res) => {
  const sig = Object.fromEntries((req.get('X-Bekci-Signature') ?? '').split(',').map((p) => p.split('=')));
  const t = Number(sig.t);
  if (!t || Math.abs(Date.now() / 1000 - t) > 300) return res.status(400).end('eski istek');
  const want = crypto.createHmac('sha256', SECRET).update(`${t}.`).update(req.body).digest('hex');
  if (!sig.v1 || !crypto.timingSafeEqual(Buffer.from(want), Buffer.from(sig.v1))) return res.status(401).end('imza yanlış');
  const ev = JSON.parse(req.body);
  console.log(ev.event, ev.monitor?.name);
  res.end('ok');
});
app.listen(3000);
```

```python title="Python (Flask) doğrulama"
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
> İmza anahtarı diğer gizli alanlar gibi saklanır: kaydettikten sonra arayüzde maskeli görünür, yalnızca değiştirmek için yeniden yazılır. Kanalın **Örnek bildirim gönder** düğmesi imzalı istek de gönderir; alıcınızı onunla deneyin.
