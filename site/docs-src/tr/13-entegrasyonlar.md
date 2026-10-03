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

`event` değerleri: `down`, `up`, `reminder`, `cert`, `slow`, `slow_resolved`, `location_down`, `location_up`, `server_alert`, `server_resolved`, `server_reboot`, `probe_offline`, `probe_online`, `acked` (yalnızca bu türü seçen kanallara; `ack: { by, note }` nesnesiyle), `test`. Sunucu uyarılarında ek `server` nesnesi (`metric`, `value`, `threshold`, `minutes`), çok konumlu monitörlerde `locations` dizisi gelir. [Bildirim kuralları](/docs/ilk-adimlar/#bildirim-kurallari) devredeyse `escalated: true` (eskalasyon kanalına N dakikadır süren olay), `delayed: true` (gecikme kuralı ya da sessiz saat sonrası gönderildi) ve `elapsed_seconds` (o ana kadar geçen süre) alanları eklenir.

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

## Olayları CSV olarak dışa aktarma {#csv}

**Olaylar** sayfasındaki **CSV indir** düğmesi, seçili süzgeçteki olayları (en fazla 10.000 satır, en yeniden eskiye) indirir. Aynı dosya API'den de alınabilir; oturum çerezi ya da API anahtarı yeter ve kullanıcı yalnızca görmeye yetkili olduğu olayları alır:

```bash
curl -H "Authorization: Bearer upk_…" "https://⟦bekci.ornek.com⟧/api/incidents?format=csv&kind=monitor" -o olaylar.csv
```

`kind` süzgeci: `monitor` (kesintiler), `partial` (konum kesintileri), `server` (sunucu ve kontrol noktası olayları); boşsa hepsi. Sütunlar:

| Sütun | Anlamı |
|---|---|
| `id` | Olayın kimliği (`/#/incidents/<id>`) |
| `kind` | `monitor`, `degraded` (yavaş yanıt), `partial`, `server_offline`, `server_alert`, `probe_offline` |
| `source` | Monitörün ya da sunucunun / kontrol noktasının adı |
| `started_at`, `resolved_at` | Yerel saatte RFC 3339; süren olayda `resolved_at` boş |
| `duration_seconds` | Süre (saniye); süren olayda şu ana kadar |
| `cause` | Neden (istek dilinde) |

Dosya UTF-8'dir ve Excel'in Türkçe karakterleri doğru açması için BOM ile başlar.

### Son kontroller ve işlem kaydı {#json-listeler}

Aynı kimlik doğrulamayla iki liste daha alınabilir:

- `GET /api/monitors/<id>/beats?limit=50` monitörün en son kontrollerini yeniden eskiye döner (`limit` en fazla 200). Her satırda `time` (unix), `status` (0 çalışmıyor, 1 çalışıyor, 2 tekrar deneniyor, 3 bakım), `ping_ms` (−1: ölçüm yok), `message` (istek dilinde) ve çok konumlu monitörde `location` bulunur.
- `GET /api/audit` (yalnızca yönetici) işlem kaydını döner; süzgeçler: `user` (kullanıcı adı), `action` (`monitor.delete` gibi tam kod ya da `monitor.` gibi alan öneki), `from`/`to` (unix), `q` (hedef, ayrıntı, kullanıcı ve IP'de arama), `before` (sayfalama: bu kimlikten eskiler) ve `limit` (en fazla 500). `GET /api/audit/facets` süzgeç kutuları için kayıtlarda geçen kullanıcı adlarını ve eylem kodlarını verir.

## Prometheus metrikleri {#prometheus}

`GET /metrics` ucu Prometheus metin biçiminde (0.0.4) veri verir. **Ayarlar → API anahtarları** bölümünden bir anahtar alın; `Authorization: Bearer upk_…` başlığıyla ya da Basic kimlikle (kullanıcı `metrics`, şifre anahtar) çağırın. İzleyici yetkisi yeter; kısıtlı bir kullanıcının anahtarı yalnızca ona atanmış monitör ve sunucuları görür.

```yaml title="prometheus.yml (parça)"
scrape_configs:
  - job_name: bekci
    scheme: https
    static_configs: [{ targets: ['⟦bekci.ornek.com⟧'] }]
    basic_auth: { username: metrics, password: upk_… }
```

| Metrik | Etiketler | Anlamı |
|---|---|---|
| `uptime_monitor_status` | `monitor_id`, `monitor_name`, `monitor_type` | 0 çalışmıyor, 1 çalışıyor, 2 bekliyor |
| `uptime_monitor_active` | monitör | 1 etkin, 0 durduruldu |
| `uptime_monitor_response_time_ms` | monitör | Son kontrolün yanıt süresi |
| `uptime_monitor_slow` | monitör | 1 = yanıt süresi eşiği aşılmış (yalnızca eşik tanımlı monitörlerde) |
| `uptime_monitor_cert_days_remaining` | monitör | SSL sertifikasına kalan gün |
| `uptime_monitor_uptime_ratio` | monitör, `window` (24h, 7d, 30d) | Çalışma oranı (0-1) |
| `uptime_server_online` | `server_id`, `server_name` | 1 ajan veri gönderiyor |
| `uptime_server_last_sample_timestamp_seconds` | sunucu | Son örneğin zamanı |
| `uptime_server_cpu_percent`, `uptime_server_memory_percent`, `uptime_server_memory_used_bytes`, `uptime_server_memory_total_bytes`, `uptime_server_swap_percent` | sunucu | Son örnek |
| `uptime_server_load1`, `uptime_server_load5`, `uptime_server_load15`, `uptime_server_load1_per_core` | sunucu | Yük ortalamaları |
| `uptime_server_net_rx_bytes_per_second`, `uptime_server_net_tx_bytes_per_second` | sunucu | Ağ trafiği |
| `uptime_server_disk_read_bytes_per_second`, `uptime_server_disk_write_bytes_per_second` | sunucu | Disk G/Ç |
| `uptime_server_disk_percent` | sunucu, `mount` | Bölüm doluluğu |
| `uptime_server_temperature_celsius` | sunucu | En sıcak sensör |
| `uptime_server_uptime_seconds`, `uptime_server_containers` | sunucu | Açık kalma süresi, konteyner sayısı |
| `uptime_probe_online` | `probe_id`, `probe_name` | 1 kontrol noktası çevrimiçi (yalnızca kısıtsız kullanıcıya) |
| `uptime_incidents_open` | `kind` | Süren olay sayısı (monitor, degraded, partial, server_offline, server_alert, probe_offline) |

Sunucu değerleri ajanın son örneğidir (dakikada bir gelir); konteyner ve sıcaklık sensörü listeleri etiket olarak verilmez, etiket sayısı sunucu sayısıyla sınırlı kalır.

## Durum sayfası RSS akışı {#rss}

Yayındaki her durum sayfasının bir RSS 2.0 akışı vardır; sayfanın alt bilgisindeki **RSS** bağlantısı ve sayfanın `<head>` bölümündeki `alternate` bağlantısı okuyuculara bunu gösterir:

```plaintext
https://⟦bekci.ornek.com⟧/durum/⟦kisa-ad⟧/feed.xml
https://⟦bekci.ornek.com⟧/api/public/pages/⟦kisa-ad⟧/feed.xml
```

Akışta sayfanın olay penceresindeki (varsayılan son 14 gün; düzenleyicide 7/14/30/90) olaylar (sayfada **Olayları göster** açıksa; başlangıç, çözülme ve süre — neden yazılmaz) ve yayına girmiş duyurular (süresi bitmişler dahil, ileri tarihliler hariç) sayfanın dilinde yer alır; süren bir olay çözülünce ayrı bir kayıt olarak eklenir. Elle açılan olaylar başlık ve önemiyle, her olay güncellemesi ("Olay güncellemesi" kategorisinde) ve sayfanın **Planlı bakım** bölümü görünürse süren/yaklaşan bakım pencereleri ("Planlı bakım" kategorisinde) de akışa düşer. Slack, Teams ya da herhangi bir RSS okuyucusu bu adrese abone olabilir.

> [!NOTE]
> Şifre korumalı sayfaların akışı yalnızca şifreyi girmiş tarayıcıya verilir; RSS okuyucularının kullanabileceği bir token yoktur. Bu yüzden şifreli sayfada RSS bağlantısı gösterilmez.
