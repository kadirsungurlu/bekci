# Betik çalıştırma: ajan <-> panel sözleşmesi (690)

Durum: hat1 yazdı, hat1 (ajan) ve hat2 (panel) buna uyar. Çelişki çıkarsa bu belge güncellenir, not `.hat1/notlar.md` / `.hat2/notlar.md`.
Güvenlik gerekçesi: `docs/BETIK-GUVENLIK-680.md`. Kod karşılığı: `internal/betik` (saf Go, DB ve ağ yok; ajan, panel ve imza aracı aynı paketi kullanır, biçimi elle yeniden yazmayın).

## 1. Zarf

```text
zarf = {"key":"<anahtar kimliği>","payload":"<base64 std>","sig":"<base64 std>"}
imza = Ed25519(özel, "bekci-script-v1\n" + tür + "\n" + payload_bayt)
tür  = manifest | ticket | revoke | receipt
anahtar kimliği = hex(sha256(ham 32 bayt açık anahtar)), 64 küçük harf
```

- `betik.Sign(priv, kind, payload)`, `betik.ParseEnvelope(raw)`, `Envelope.Verify(kind, pub)`, `betik.KeyID(pub)`.
- Açık anahtar metin biçimi: ham 32 bayt, base64 std.
- `host_key` = ajanın `K_ajan` anahtar kimliği (aynı işlev).
- Önce imza, sonra ayrıştırma. Katı ayrıştırma (`betik.ParseManifest/Ticket/Revoke/Receipt`, `ParseParams`): bilinmeyen alan, yinelenen anahtar, küçük harf olmayan anahtar (`Approval`), sondaki fazla veri, bilinmeyen `schema` = red. Zarf üst sınırı 128 KiB.
- `manifest_sha256` = hex(sha256(manifest zarfının payload baytları)). `params_sha256` = hex(sha256(parametre JSON baytları)).
- Zaman alanları unix saniye (tamsayı). Özetler 64 küçük hex.

## 2. İmzalı nesneler (payload JSON)

**Bildirim** (`manifest`, `K_yayın`):

```json
{"schema":1,"id":"journal-temizle","version":1,"sha256":"<gövde özeti>",
 "interpreter":"sh","run_as":"root","platforms":["linux"],
 "approval":"none","groups":["web"],
 "params":[{"name":"gun","type":"int","min":7,"max":90}],
 "timeout_sec":300,"output_max":65536,
 "min_interval_sec":21600,"max_per_day":4,"jitter_sec":30,
 "not_before":1762000000,"not_after":1777000000}
```

- `id`: `^[a-z0-9][a-z0-9-]{0,63}$`. `version` >= 1. `interpreter` yalnız `sh`. `run_as`: `root` | `nobody`. `platforms` `linux` içermeli. `approval`: `none` | `panel` | `signed`. `groups` boş olamaz, her biri `^[a-z0-9][a-z0-9_-]{0,63}$`.
- Parametre: `{"name","type","values","min","max"}`. `name`: `^[a-z][a-z0-9_]{0,31}$` (ortamda `BEKCI_P_<BÜYÜK AD>`). `type`: `enum` (`values` zorunlu, 1-64 eleman, her biri `name` kalıbında), `int` (`min`, `max` zorunlu), `bool`, `name`. Bildirimdeki **her parametre zorunludur**, varsayılan yok.
- Üst sınırlar (hem `Manifest.Validate` hem ajan): `timeout_sec` 1..3600, `output_max` 1..262144, `jitter_sec` 0..3600, `max_per_day` 1..1440, `min_interval_sec` >= 0, `not_after - not_before` en çok 180 gün.

**Bilet** (`ticket`, `K_onay`): `{"schema":1,"run_id","host_key","script_id","version","manifest_sha256","params_sha256","issued_at","expires_at","approver"}`. `expires_at - issued_at` en çok 900 sn. `approver` 1-128 bayt.

**İptal** (`revoke`, `K_yayın`): `{"schema":1,"id","below_version"}`.

**Makbuz** (`receipt`, `K_ajan`): `{"schema":1,"seq","prev","run_id","script_id","version","manifest_sha256","params_sha256","ticket_sha256","started_at","ended_at","status","exit_code","output_sha256","output_truncated"}`.
`prev` = hex(sha256(önceki makbuzun payload baytları)); ilk makbuzda `""`. `ticket_sha256` = bilet payload özeti, bilet yoksa `""`. `status`: `ok` | `failed` | `timeout` | `cancelled` | `lost`. `exit_code` süreç çıkış kodu; bilinmiyorsa -1. `output_sha256` tam çıktı akışının özeti (kısaltılmamış); başlamadıysa `""`.

## 3. Yetenek başlıkları (ajan -> panel, her istekte)

```text
X-Probe-Scripts: schemas=1;state=on
X-Probe-Scripts: schemas=1;state=off;reason=<kod>
X-Probe-Script-Key: <K_ajan açık anahtarı, base64>     (yalnız state=on)
```

`reason`: `missing` (betik.json yok), `disabled`, `config`, `state` (durum/anahtar bozuk), `platform` (Docker, Windows, OpenRC, systemd < 254), `insecure` (http panel ya da PROBE_ALLOW_INSECURE).
Başlık yok = eski ajan: panel `script_jobs` göndermez, arayüz "desteklenmiyor" der. `state=off` = panel iş üretmez, arayüz nedeni gösterir.
Panel `host_key`'i `X-Probe-Script-Key`'den hesaplar (`betik.KeyID`); bilinen anahtardan farklıysa otomatik kabul etmez (§7, D14). Çözülemeyen başlıkta `state=off` say. `schemas` virgüllü liste olabilir.

## 4. İş iletimi (panel -> ajan)

`GET /api/probe/jobs` yanıtına tek yeni alan (omitempty; eski ajan yok sayar):

```json
"script_jobs": {
  "runs": [{
    "run_id": "<32 küçük hex>",
    "manifest": {zarf},
    "body": "<gövde, base64 std>",
    "params": "<parametre JSON baytları, base64 std; parametre yoksa {} kodlanır>",
    "ticket": {zarf}
  }],
  "revokes": [{zarf}],
  "cancel": ["<run_id>"]
}
```

- `ticket` yalnız bilet varsa; varsa `approval` ne olursa olsun ajan doğrular (D3: `panel` sınıfı tek yöneticide bilet akışına düşer). `params` alanı hep gönderilir, bilet `params_sha256` tam bu baytları bağlar.
- Parametre JSON'u düz nesne: `{"gun":30}`, `{"servis":"caddy"}`, `{"zorla":true}`. Her yanıtta en çok 16 `runs`, 64 `revokes`; fazlası ajanda yok sayılır.
- Panel bir `run_id`'yi `accepted`/`rejected`/`receipt` gelene dek her yoklamada yeniden gönderir (uzun yoklamayı uyandırmak panelin işidir). Ajan çalışan işi sessizce yok sayar, biten işe `rejected/run_id_replay` döner. `accepted` görülmüş işte panel bu reddi **yok sayar**; görülmemişse (ajan yeniden başladı, `accepted` kayboldu) işi kabul edilmiş sayar, reddetmez: yeniden göndermeyi keser, makbuzu bekler. Panel iptal ettiği işe geç `accepted` gelirse durdurmayı `cancel` listesiyle yeniden yollar.
- `cancel`: çalışan birim durdurulur, makbuz `cancelled`. Bilinmeyen `run_id` yok sayılır.

## 5. Olay iletimi (ajan -> panel)

`POST /api/probe/script-result` (gövde 2 MiB'den küçük), istek başına tek olay:

```json
{"kind":"accepted","run_id":"...","time":1762000000}
{"kind":"rejected","run_id":"...","code":"sig_invalid","time":1762000000}
{"kind":"receipt","run_id":"...","time":1762000000,"receipt":{zarf},"output":"<base64, kısaltılmış çıktı>"}
{"kind":"notice","code":"clock_forward","time":1762000000}
```

- `accepted`: yerel kayıt atıldı (adım 7), jitter ve çalıştırma başlıyor. Makbuz `run_id` ile eşleşir.
- `rejected`: iş çalışmadı. `code` yalnız aşağıdaki sabit listeden; ayrıntı panele gitmez. `run_id` biçimi bozuksa `run_id` boş gelir.
- `receipt`: panel `Envelope.Verify("receipt", pub)` + `ParseReceipt`, `run_id` eşleşmesi, `seq`/`prev` sürekliliği. `output` ajanın beyanıdır; kısaltılmışsa baş + son ve aradaki `\n[... N bayt kısaltıldı ...]\n` işareti içerir, `output_sha256` buna değil tam akışa aittir.
- `notice`: bilgi (`clock_forward`, `queue_dropped`).
- Yanıt: `2xx` kabul; `409`/`410`/`422` kalıcı red, ajan kuyruktan siler (`kayit.log`'da kalır); diğer (`404/405` eski panel, `5xx`, ağ) ajan sınırlı disk kuyruğunda tutar ve yeniden dener. Aynı makbuz iki kez gelebilir: panel `run_id + seq` ile tekilleştirir, ikincisine `2xx` döner.

## 6. Red kodları

`betik.Code*` sabitleri, tek kaynak `internal/betik/kodlar.go`. Arayüz TR+EN metni bu kodlardan üretir:

`disabled, config_invalid, state_invalid, insecure_panel, platform, clock_back, envelope_invalid, sig_invalid, schema_unknown, payload_invalid, expired, not_yet, revoked, version_rollback, version_jump, body_too_large, body_mismatch, group_denied, not_allowed, pin_mismatch, ticket_missing, ticket_sig_invalid, ticket_invalid, ticket_host, ticket_mismatch, ticket_expired, params_invalid, run_id_format, run_id_replay, rate_interval, rate_day, rate_hour, busy, state_write`

## 7. Panelin bilmesi gereken ajan davranışları

- Etkin onay ajanda hesaplanır: en yüksek(imzalı `approval`, yerel `min_approval`, iğne yoksa `signed`). Panel yerel `allow` listesini görmez; `ticket_missing` reddi "bu sunucuda bilet gerekiyor" demektir.
- Sıra: panel `https` değilse ya da `PROBE_ALLOW_INSECURE` açıksa özellik kapalıdır; yerel test için TLS gerekir.
- Eski panel: `script-result` 404 alır, makbuz kuyrukta bekler. Eski ajan: `script-result` hiç gelmez.
