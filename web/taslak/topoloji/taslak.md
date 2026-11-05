# Canlı topoloji gruplama taslağı

Bağımsız prototip, kod ana hatta değil. Tohum veri: 100 monitör (9 grup), 10 nokta, 40 sunucu, 3 hatalı monitör, 1 çevrimdışı nokta, 2 uyarılı sunucu.
Çalıştırma: bu klasörde `python3 -m http.server 8765`, sonra `index.html?v=a|b|c&n=9|100|500|1000` (B'de `&g=Ödeme` başlangıç grubu, `&denetle=1` taşma raporu, `&temiz=1` tercihleri siler).
Görüntüler: `projects/bekci/ekran/topoloji-taslak/` (B, Ödeme grubu seçili çekildi).

## Üç varyantta ortak
- **Sorun çizelgesi** (yeni): satır sorun, sütun nokta, kırmızı hata görüyor, koyu temiz, çizgili çevrimdışı; yanında "4/9 nokta, Türkiye'nin tamamı" hükmü. Neresi, kaç tane, hangi noktadan sorularını kartlara inmeden yanıtlar. Bugünkü sayaç kutularının yerini alır; bu ayrı bir karar.
- Sorunlu grup açık, sağlıklı kapalı; tercih `localStorage` içinde grubun o anki durumuyla saklanır, durum değişince geçersiz olur. Elle kapatılan sağlıklı grup bozulunca yine açılır.
- Sorunlu öğe sağlıklıların önünde; mobilde (<=720 px) çizgi yok, sorunlu grupta yalnız sorunlular ve "N sağlıklı öğeyi göster".

## Karşılaştırma
| | A: açılır gruplar | B: akordeon + sahne | C: yoğun |
|---|---|---|---|
| 1920 sayfa boyu (100 monitör) | 1283 px | 1265 px | 1157 px |
| 390 sayfa boyu | 2395 px | 1882 px | 2482 px |
| İlk çizim 500 / 1000 (prototip) | 118 / 147 ms | 73 / 148 ms | 138 / 159 ms |
| Artı | Bugünkü dile en yakın; kartta tür ve gecikme okunur | En temiz çizim; seçili grubun hangi noktalardan geçtiği tek yelpazede; sol liste en kısa | Aynı ekranda en çok bilgi; kapalı grupta her monitör bir hücre, sorunun yeri ve oranı görünür |
| Eksi | Kapalı grupta yalnız oran çubuğu var; sorunlu gruplar uzar, panel ve alt bölüm aşağı kayar | Tüm sistem aynı anda görünmez, başka grupların bağlantısı kaybolur; sahnenin altı boş kalır; TV kipine uymaz; mobilde sahne yok | Kartta gecikme ve tür gizli (yalnız ipucu); uzun adlı grup tek sütuna düşer; panel çevresinde ray yoğunluğu en fazla |

## Öneri: C, çizelge ile birlikte
Hedef "neresi bozuk" sorusunu bir bakışta yanıtlamak ve 1000 monitöre ölçeklenmek. C bunu en kısa sayfada yapıyor: kapalı grup bile tek tek öğe durumunu gösterir, TV kipinde okunur. A'nın ek bilgisi (tür, ms) sağlıklı kartlarda arıza anında gerekmiyor, ipucuna gidebilir. B'nin güçlü yanı olan nokta yelpazesi, çizelgenin kırmızı satırı aynı soruyu daha az yerde yanıtladığı için ayrı bir sahneye değmiyor. Uygulamada istenirse C'de grup üstüne gelince o grubun çizgisi öne alınabilir.

## Uygulamaya geçince gerekenler
- Stash `700 topoloji olcek yarim`: `view=summary` alan şekli (tags, locs, loc_ok, loc_total, customer, üst düzey tags/customers/local_name) alındı, yalnız sahte veride kullanıldı; Go kodu ve kalan kısımlar bırakıldı. Stash yerinde. Yorumdaki `docs/TOPOLOJI-OLCEK.md` stash'te yok.
- API eksiği: summary `locations` alanını atıyor, oysa çizelge için sorunlu monitörde noktaların listesi şart (`locations` ya da `locs_bad`). Noktada bölge/sağlayıcı, sunucuda rol/konum alanı yok; gruplama seçicileri için eklenmeli.
- Çok değerli gruplama: etiket ilk etiket, müşteri ilk müşteri, konum birincil konum. Monitör iki gruba yazılmaz, bu kural seçilirse belgelenmeli.
- "Sorunlular asla gizlenmez": sınırı aşan grupta ("N tane daha") sorunlular hep görünür.
- Ölçümler prototiptendir: sahte veri, başsız Chrome, kapalı grupta kart çizilmez, açık grupta ilk 15 kart. Asıl Svelte bileşeninde 1000 monitör < 300 ms ayrıca ölçülmeli. "Yalnız sorunlular" süzgeci ve TV kipi bu taslakta yok.
- Küçük kurulumda (9 monitör) dikey çizgiler kısa panelin altında boş alanda uzuyor; üst paneller eşit boya getirilmeli.
