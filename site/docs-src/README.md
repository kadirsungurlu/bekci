# bekci.app belgeleri

Kaynaklar: `tr/*.md` (Türkçe, `/docs/…`) ve `en/*.md` (İngilizce, `/en/docs/…`).
Site imajı derlenirken `site/docs-build/build.mjs` bunları statik HTML'e çevirir
(`site/Dockerfile` içindeki ilk aşama). Ayrıca arama dizinlerini
(`/assets/docs-search-{tr,en}.json`) ve `sitemap.xml`'i üretir.

## Sayfa eklemek ya da düzenlemek

1. `tr/` altında bir dosya oluşturun (ör. `12-yeni-sayfa.md`) ve aynı `id` ile
   `en/` altında eşini yazın. Eşi olmayan sayfa derlemeyi durdurur.
2. Dosyanın başına ön bilgi yazın:

   ```yaml
   ---
   id: yeni-sayfa            # TR ve EN eşini bağlar (hreflang, dil düğmesi)
   title: Sayfa başlığı      # <h1> ve <title>
   nav: Kısa ad              # kenar menüsünde görünen ad (isteğe bağlı)
   description: Tek cümlelik özet (meta description ve başlık altı).
   section: Kurulum          # kenar menüsündeki grup
   order: 12                 # menü ve önceki/sonraki sırası
   slug: kurulum/yeni-sayfa  # adres: /docs/kurulum/yeni-sayfa/ ("" = /docs/)
   ---
   ```

3. İçerikte `#` (h1) kullanmayın; başlıklar `##` ile başlar. `## Başlık {#kimlik}`
   ile sabit bir çapa verebilirsiniz.

## Markdown uzantıları

- Uyarı kutuları: `> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`,
  `[!CAUTION]`, `[!CHECK]` (“Ne görmelisiniz?”). Aynı satıra yazılan metin kutunun
  başlığı olur: `> [!WARNING] Özel başlık`.
- Kod başlığı ve indirme düğmesi: ` ```yaml title="/opt/bekci/docker-compose.yml" download="/indir/docker-compose.yml" `.
  `download` verilirse sayfadaki içerik o dosyayla birebir aynı olmalıdır
  (yer tutucu işaretleri hariç); değilse derleme durur.
- Yer tutucu: `⟦bekci.ornek.com⟧` kod içinde ve satır içi kodda vurgulanır;
  kopyalanan metinde işaretler yoktur.
- Sekmeler (içlerinde başlık olmaz):

  ```
  :::tabs key=db label="Veritabanı"
  @tab SQLite
  …
  @tab PostgreSQL {#postgresql}
  …
  :::
  ```

  Aynı `key`'e sahip sekmeler sayfalar arasında birlikte değişir ve seçim
  hatırlanır. `{#kimlik}` sekmeye doğrudan bağlantı verir (`…/#postgresql`).
- Görseller yalnızca `/img/` altından: `![açıklama](/img/yeni-monitor-1600.webp)`.

## Yerelde derleme

```bash
cd site/docs-build && npm ci
node build.mjs --src ../docs-src --site .. --out /tmp/bekci-docs
```

Kırık iç bağlantı ya da çapa, yinelenen kimlik, eksik çeviri veya bozuk yer
tutucu hata verir (çıkış kodu 1).
