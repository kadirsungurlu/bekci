# Başlangıç betikleri

Betikler burada imzasız durur (D1). Her `<id>/` altında `manifest.json` (bildirim) ve `govde.sh` (gövde) vardır.
İmzalamadan önce gövdeyi okuyun: imza "bunu okudum, çalışabilir" demektir. Tasarım: `docs/BETIK-GUVENLIK-680.md`, biçim: `docs/BETIK-SOZLESME.md`.

| Betik | Grup | Onay | Sınır |
|---|---|---|---|
| `journal-temizle` | `temizlik` | none | 6 saatte 1 |
| `paket-onbellek` | `temizlik` | none | 6 saatte 1 |
| `docker-derleme-temizle` | `docker` | panel | 6 saatte 1 |
| `docker-imaj-temizle` | `docker` | panel | Günde 1 |
| `servis-yeniden` | `sistem` | signed | 10 dakikada 1 |
| `servis-yeniden-web` | `web` | signed | 10 dakikada 1 |
| `sertifika-yenile` | `web` | signed | Günde 1 |

`sshd`, ajan, `docker` ve veritabanı servisleri `servis-yeniden*` listelerine eklenmez; `fail2ban` (güvenlik denetimi, yeniden başlarken yasaklar boşa düşer) ve `rsyslog` (uzak günlük akışı yani tanık kesilir) da bilerek listede yok. Liste, kesintisi tek servisle sınırlı ve onarım için gerçekten gereken servislerden oluşur: `cron`/`crond` (dağıtım adı farkı), `chronyd`. Yeni `none` betiği tek başına ya da diğerleriyle birlikte servis kesmemeli, tanık kayıt silmemeli.

## Araç

Yalnız kendi makinenizde çalıştırın; anahtarlar bu sunucuya ve CI'ya girmez (D2). Araç ağa bağlanmaz.

```sh
go build -o bekci-imza ./cmd/bekci-imza
```

## 1. Anahtarlar (bir kez)

```sh
./bekci-imza anahtar-uret --yuva yayin --cikti yayin.anahtar
./bekci-imza anahtar-uret --yuva onay --cikti onay.anahtar
```

Parola stdin'den okunur. Dosyaları çevrimdışı yedekleyin. Ekrana basılan açık anahtarları panele (Betikler, anahtarlar) ve her sunucunun `/etc/bekci/betik.json` dosyasına ekleyin.

## 2. İmzalama

```sh
./bekci-imza imzala --anahtar yayin.anahtar \
  --bildirim scripts/betikler/journal-temizle/manifest.json \
  --govde scripts/betikler/journal-temizle/govde.sh --cikti imzali/
```

`sha256` ve verilmemişse `not_before` (şimdi), `not_after` (+90 gün, üst sınır 180) araç tarafından yazılır. Yeniden imza için bildirimde `version` artırılır. `shellcheck` varsa hatada durur, yoksa uyarır.

## 3. Yükleme

Çıktı `<id>-<sürüm>.betik.json` dosyasıdır; `POST /api/scripts` gövdesiyle aynıdır, yalnız iki adımlı doğrulama kodu (`code`) eklenir. Panelde Betikler ekranından yükleyin.

`signed` olmayan betikler için her sunucuda yerel izin ve iğne gerekir (D12): araç çıktısındaki `govde_sha256` ile `uptime betik izin` kullanılır, panelden yapılamaz.

## 4. Bilet (`signed` ve tek yöneticili `panel` işleri)

Panelin verdiği `ticket_request` nesnesini dosyaya kaydedin:

```sh
./bekci-imza bilet --anahtar onay.anahtar --istek istek.json --onaylayan kadir --igneler igneler.json
```

`igneler.json` sizin tuttuğunuz `{"<host_key>": "sunucu adı"}` dosyasıdır; panelden gelmez. Ekranda betik, sürüm, parametre ve sunucuyu okuyup `evet` yazın. Bilinmeyen sunucu uyarısını geçiştirmeyin. Zarf stdout'a yazılır, panele yapıştırılır.

## 5. İptal

```sh
./bekci-imza iptal --anahtar yayin.anahtar --id journal-temizle --alt-surum 2
```

## 6. Kayıt doğrulama

Panelden dışa aktarılan kayıt, panel ağına çıkmadan denetlenir; zamanlı işe bağlayın:

```sh
./bekci-imza dogrula --kayit bekci-betik-kayit.json --ajan-kayit kayit.log \
  --bekle zincir-basi.json --en-eski-kontrol 25h
```

`--bekle` önceki zincir başını saklar, geri alma ve çatallanmayı yakalar. `--en-eski-kontrol` son çapanın en çok yaşıdır. Çıkış kodu 0 sağlam, 1 kırık. Bilinçli yedek dönüşünden sonra sebebi kaydedip `--bekle` dosyasını silin.
