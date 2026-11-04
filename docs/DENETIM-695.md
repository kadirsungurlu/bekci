# Genel gerçek kullanım denetimi (695)

Tarih: 2026-11-04. Dal: `gelistirme`. Yer: bu sunucuda yerel konteynerler (imaj `1.7.0-d695`, gelistirme dalından).
Düzenek: varsayılan compose + Caddy, 3 sunucu ajanı (Docker kipi, systemd yeni, systemd 1.6.0), 3 kontrol noktası (2 yeni, 1 1.6.0), 28 monitör, mesh açık, webhook kanalı. Test paneline dağıtım yapılmadı (aşağıda).

## Çalışıyor (gerçekten denendi)

| Senaryo | Sonuç |
|---|---|
| Sıfırdan kurulum | Varsayılan compose + Caddy 19 sn, `db_mode=timescale` |
| 1.6.0 SQLite -> TimescaleDB | 1.275.331 satır, 62 tablo; migrate 143 sn, toplam 163 sn; hizalı 101.047 noktada 0 fark; SQLite dosyası değişmedi; `--dry-run` temiz. Migrate atlanırsa panel "taşınmadı" diye açılmıyor (koruma doğrulandı) |
| 1.6.0 düz PG (compose) -> TS | Kesinti 41 sn; 100.566 noktada 0 fark |
| 1.6.0 düz PG, preload'suz | `db_mode=postgres`, uyarı, 100.886 noktada 0 fark, kesinti 11 sn (güvenli düşüş) |
| 1.6.0 gömülü `-postgres` imajı -> ayrı TS | Döküm + yükleme + dönüşüm kesinti 185 sn (hypertable 156 sn); 100.201 noktada 0 fark |
| PG geri dönüş (döküm + 1.6.0) | 19-20 sn; 100.546 ve 100.201 noktada 0 fark |
| Envanter doğruluğu | Gerçek `docker ps`/`ss`/`systemctl` ile: konteyner 55=55, port 19=19, disk ve sertifika tam; servis 23/25 (Docker kipinde cgroup yedeği `getty@tty1`, `user@0` kaçırıyor); Docker olmayan sunucuda not; eski ajanda boş envanter sorunsuz |
| Disk doldurma | Gerçek XFS loop diski: tahmin 4,2 gün (r2=1), `disk_forecast`, `disk_fast` (%37/60 dk), disk %87 olay + webhook; silince kapandı. Olay #8 (tahmin) artış verisi pencereden çıkana dek açık kalır |
| Servis çökmesi, OOM | `kill -9` -> `service_failed` kritik + `port_closed`; gerçek cgroup OOM -> `oom` olayı; `port_opened` |
| Sertifika | Monitör TLS (5/12/20 gün) bildirimi; envanter port sertifikası 11 gün olayı |
| CLI | sunucular, sunucu, disk (ARTIS/DOLMA gerçek), olaylar, sertifikalar, monitorler, `--json`, hatalı ad; anahtar çıktıda yok |
| Yetki saldırıları | read anahtarı POST/PUT/DELETE/users/settings/audit/export 403, /metrics 401; ajan jetonu panel API 401; yanlış jeton 401; IP kilidi 403; CSRF başlıksız 403; yol aşımı 400; SQL enjeksiyon denemesi zararsız; nokta jetonuyla envanter 403 |
| Canlı topoloji | Nokta durdurma: çevrimdışı (15 sn), mesh olayı + webhook, sayfada kırmızı; başlatınca 4 dk'lık olay kapandı, `mesh_resolved`; uzun ad kırpılıyor |
| Eski ajan + yeni panel | systemd ve kontrol noktası 1.6.0 çalışıyor, hata yok |
| Yeni ajan + 1.6.0 panel | Nokta ölçüm gönderiyor, sunucu 55 konteynerle çevrimiçi, 0 ERROR |
| Ajan otomatik güncelleme | Test imza anahtarıyla 1.6.0 kaynaktan derlenen ajan -> imzalı panel: yerli systemd ve Docker birim düzeni ~3 sn'de geçti, `current`. İmaj içi salt okunur ajan `readonly`, dokunulmuyor |
| Arayüz | 9 ekran x 1920/1440/390 x TR/EN: sayfa düzeyinde yatay taşma 0, sayfa hatası 0, 5xx 0 |

## Denetimde bulunup düzeltilenler (`gelistirme`)
- Kısa ömürlü sertifika (Caddy iç CA, 12 saat) "0 gün kaldı" gürültüsü: ömrü eşiğin iki katından kısa sertifikada alarm yok. Sınandı: 90 gün ömürlü, 5 gün kalan sertifika olay açtı; kısa ömürlüler açmadı.
- Tek başına duran kendinden imzalı sertifika dosyası izlenmiyordu; artık izleniyor. Sınandı.
- Mesh olayı rozeti "çevrimdışı" diyordu, doğru metin geldi. EN topoloji "probe" -> "check location" (diğer ekranlarla aynı terim).

## Denenmedi
- Betik çalıştırma ve betik saldırı denemeleri: **kod yok**. 690, D1-D15 onayına bağlı, yapılmadı.
- Test paneli (`test-uptime.kadir.app`): dağıtılmadı. Coolify `build_pack=dockerfile`, dağıtım = orada derleme (yasak); imaja geçiş Coolify ayar değişikliği ister.
- Gerçek reboot (konteynerde `boot_id` sabit), Windows ajan, Windows otomatik güncelleme, gerçek `AGENT_SIGNING_KEY` ile imza, canlı ajanların güncellenmesi, Docker Hub yayın akışı.
- Canlı verinin kopyasıyla yeni imajda prova bu turda yinelenmedi (640 turunda yapıldı; o tur sonrası kod değişti).
- API anahtarları kartı Fable incelemesine girmedi (görüntü kadrajı üst kısımda kaldı); e2e `apikeys.mjs` CI'da.

## Tasarım incelemesi (Fable, 10 görüntü): yayına engel yok, kozmetikler
Hepsi 2026-11-04'te `gelistirme`de düzeltildi (madde 7'deki mobil şema düğümü dışında: düğüm etkileşimsiz, boyutu değişmedi; bağlantı dokunma alanı 44 px yapıldı). Şema PNG'si (`internal/mesh/render.go`) madde 2'nin işaret kuralını kullanmıyor.

1. Topoloji kartlarında durum yalnız renkle (nokta, sunucu, kanal); sayaç rozetlerinde etiket yok, duraklatılmış sayısı anlaşılmıyor.
2. Mesh şemasında kesişen kenarın işareti tam kesişimde; hangi bağlantıya ait okunmuyor.
3. Ayarlar > Kontrol noktaları: ad altında etiketsiz `upr_...` öneki.
4. EN'de "Main server" / "Bekci panel" ikiliği.
5. Envanter servis durumları (`running`, `dead`, `enabled`) çevrilmemiş.
6. Olay zaman çizelgesinde önem yalnız nokta rengiyle; 390'da nokta hizası. Mesh yorum listesinde sorun cümlesinde yeşil madde.
7. Dil kartı kenar çubuğu seçimini yansıtmıyor; 1440'ta kanal kenarı monitör grubunun köşesinden geçiyor; mobilde şema düğümü ~32 px.

## Canlı geçiş planı (uptime sunucusu, SQLite -> TimescaleDB)
Bugünkü canlı (salt okunur): 1.6.0, SQLite 84 MB + WAL, gece yedeği 78 MB; 1 CPU, 967 MB RAM (434 MB kullanılabilir), swap yok, 4,3 GB boş disk. TS imajı 1,57 GB; DB birimi ~260 MB. Bellek için `TS_TUNE_MEMORY=400MB` zorunlu, 1 GB swap önerilir. Ayrıntılı bellek hesabı: DENETIM-640.

Hazırlık (kesintisiz):
1. Compose + `.env` kopyası, `bekci-data` tgz yedeği, boş disk en az 3 GB. Yoksa eski imajlar (1.3/1.4) silinmesi için ayrıca onay.
2. Yeni Bekci ve `timescale/timescaledb:2.31.0-pg18` önceden çekilir.
3. Compose'a `db`, `TS_TUNE_MEMORY: 400MB`, `shared_preload_libraries=timescaledb`, `max_worker_processes=24`; `.env`'e `POSTGRES_PASSWORD`.
4. `docker compose up -d db`, panel 1.6.0 olarak sürer. `docker compose run --rm bekci migrate --dry-run`.

Geçiş (kesinti tahmini 1-3 dk; 695 provası 1,27 M satırda 163 sn, canlı veri daha küçük ama tek çekirdek):
5. `docker compose stop bekci`; SQLite SHA-256 not edilir.
6. `docker compose run --rm bekci migrate`; satır sayıları ve doğrulama satırı beklenir.
7. `DATABASE_URL` eklenir, `UPTIME_TAG=<yeni>`, `docker compose up -d bekci`.

Doğrulama: `/healthz?detail` `db_mode: timescale`; 5 nokta çevrimiçi; monitör sayısı ve açık olaylar aynı; deneme bildirimi; `job_stats` başarısız iş yok; ertesi gün sıkıştırma + gece yedeği.

Yedek: tgz + değişmeyen SQLite 30 gün; yeni düzende gece `.dump`.

Geri dönüş (2-5 dk): `stop bekci`, `DATABASE_URL` silinir, `UPTIME_TAG=1.6.0`, `up -d bekci`. Geçiş sonrası PostgreSQL'e yazılan veri geri gelmez. Yeniden deneme: DB boşaltılır, adım 5.

Ajanlar: panel geçince 5 ajan otomatik güncellemeyle yeni sürüme geçer (provada ~3 sn, kesintisiz). Yayın imajı gerçek `AGENT_SIGNING_KEY` ile imzalı olmalı; olmazsa ajanlar güncellemeyi reddeder ve eski sürümde sağlıklı çalışır (eski ajan + yeni panel denendi). Önce tek ajanda doğrulama için panelde otomatik güncelleme kapalı başlatılıp tek ajan elle güncellenmesi önerilir; sonra hepsi. Paneli geri dönerseniz güncellenmiş ajan 1.6.0 panelle de çalışır (yeni ajan + eski panel denendi).
