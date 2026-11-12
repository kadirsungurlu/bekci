# Örnek eklentiler

Bekci sunucu ajanının belirli aralıklarla çalıştırdığı imzalı küçük `sh` betikleri. Ayrıntılı belge: https://bekci.app/docs/eklentiler/

| Klasör | Ne yapar | Çıktı | Çalışan kullanıcı |
|---|---|---|---|
| `mdadm/` | `/proc/mdstat`'tan yazılım RAID dizilerini okur: etkin/toplam disk, bozuk dizi sayısı, eşitleme yüzdesi | Nagios | nobody |
| `smart/` | `smartctl` ile disk sağlığı, sıcaklık, yeniden atanmış/bekleyen sektör, NVMe aşınma ve ortam hataları | JSON | root (smartmontools gerekir) |

Her klasörde `manifest.json` ve `govde.sh` vardır. Manifestteki `sha256` boş, `not_before`/`not_after` 0 bırakılır; imzalama aracı doldurur (90 gün geçerli).

## Akış

1. İmza anahtarı üretin (kendi makinenizde, bir kez; özel anahtarı gizli tutun):

   ```sh
   uptime betik eklenti anahtar-uret ~/bekci-eklenti.key
   ```

2. İmzalayın. Zarf ekrana, gövde sha256 hata akışına yazılır:

   ```sh
   uptime betik eklenti imzala ~/bekci-eklenti.key mdadm/manifest.json mdadm/govde.sh > mdadm.zarf.json
   ```

3. Ajanın çalıştığı sunucuda root ile açık anahtarı ekleyin (bir kez) ve gövdeye izin verin:

   ```sh
   uptime betik eklenti anahtar <açık anahtar base64>
   uptime betik eklenti izin mdadm <gövde sha256>
   ```

4. Panelde **Sunucular > sunucu > Denetimler > Eklenti ekle**: `mdadm.zarf.json` ve `govde.sh` dosyalarını seçin, aralığı belirleyip kaydedin.

Betiği değiştirdiğinizde `version` değerini artırın, yeniden imzalayın, yeni sha256 ile `izin` verin ve panelde yeni sürümü yükleyin.

## Deneme

```sh
MDSTAT=/tmp/mdstat-ornek sh mdadm/govde.sh; echo "dönüş kodu: $?"
sudo sh smart/govde.sh
```
