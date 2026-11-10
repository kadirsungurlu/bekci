# Pro sayfaları

Açık depoda `sayfalar/` ve `ek/` boştur (yalnız `.gitkeep`; `ek/` içeriği `.gitignore` ile yok sayılır). bekci-pro derlemesi, web derlemesinden önce kendi
`.svelte` sayfalarını buraya kopyalar; `lib/pro.svelte.ts` bunları `import.meta.glob('../pro/sayfalar/*.svelte')`
ile tembel yükler (klasör boşken derleme hatasız geçer).

Kurallar:
- Dosya adı = `PRO_MENU` öğesinin `id` değeri (`filo-guvenlik.svelte`, `denetimler.svelte`, `raporlar.svelte`,
  `musteriler.svelte`, `betikler.svelte`); menü listesi ve özellik bayrağı `lib/pro.svelte.ts` içinde.
- Her sayfa varsayılan dışa aktarımlı bir Svelte bileşenidir; adres `#/pro/<id>[/<alt yol>]`. Tek prop'u isteğe
  bağlı `yol: string`tir (alt yol, ör. `#/pro/betikler/runs/<id>` -> `runs/<id>`; yoksa `''`). Alt yol
  `[a-z0-9/_-]` karakterlerinden oluşur; sayfa tanımadığı yolda kendi "bulunamadı" görünümünü gösterir.
- Sayfanın yardımcı bileşen ve modülleri `ek/<ad>/` altına kopyalanır (bekci-pro `web/ek/`); içe aktarmalar
  bu konuma göre görelidir (`../../../lib/api`). Pro metinleri kendi TR/EN sözlüklerindedir.
- Lisans denetimi sayfanın dışında yapılır: modül var + `proAcik(ozellik)` ise sayfa açılır; Pro derlemesinde
  lisans yoksa sayfa yine gösterilir, üstte "salt okunur" uyarısı çıkar (sunucu değiştiren istekleri 402
  `pro_lisans` ile reddeder). Modül yoksa `pages/ProLocked.svelte` gösterilir.
- Sayfa, açık depodaki `lib/` ve `components/` modüllerini göreli yolla (`../../lib/api`) kullanabilir.
  Metinler TR+EN, emoji yok.
