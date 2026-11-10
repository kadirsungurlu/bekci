# Pro sayfaları

Açık depoda `sayfalar/` boştur (yalnız `.gitkeep`). bekci-pro derlemesi, web derlemesinden önce kendi
`.svelte` sayfalarını buraya kopyalar; `lib/pro.svelte.ts` bunları `import.meta.glob('../pro/sayfalar/*.svelte')`
ile tembel yükler (klasör boşken derleme hatasız geçer).

Kurallar:
- Dosya adı = `PRO_MENU` öğesinin `id` değeri (`filo-guvenlik.svelte`, `denetimler.svelte`, `raporlar.svelte`,
  `musteriler.svelte`); menü listesi ve özellik bayrağı `lib/pro.svelte.ts` içinde.
- Her sayfa varsayılan dışa aktarımlı bir Svelte bileşenidir ve props ALMAZ; adres `#/pro/<id>`.
- Lisans denetimi sayfanın dışında yapılır: modül var + `proAcik(ozellik)` ise sayfa açılır; Pro derlemesinde
  lisans yoksa sayfa yine gösterilir, üstte "salt okunur" uyarısı çıkar (sunucu değiştiren istekleri 402
  `pro_lisans` ile reddeder). Modül yoksa `pages/ProLocked.svelte` gösterilir.
- Sayfa, açık depodaki `lib/` ve `components/` modüllerini göreli yolla (`../../lib/api`) kullanabilir.
  Metinler TR+EN, emoji yok.
