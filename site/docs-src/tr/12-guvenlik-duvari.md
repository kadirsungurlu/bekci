---
id: firewall
title: Güvenlik duvarı ve Cloudflare
nav: Güvenlik duvarı ve Cloudflare
description: Sitenizi koruyan Cloudflare veya başka bir güvenlik duvarı Bekci'nin kontrollerini engelliyorsa, IP adreslerini tek tek eklemeden tüm konumlara nasıl izin verilir.
section: Kullanım
order: 12
slug: guvenlik-duvari
---

Siteniz Cloudflare, bir WAF veya bot koruması arkasındaysa Bekci'nin kontrolleri "403 Forbidden" ya da "Just a moment…" sayfasına takılabilir. Monitör o zaman siteniz çalıştığı hâlde **Çalışmıyor** görünür. Birden çok kontrol noktası kullanıyorsanız her birinin IP adresini tek tek eklemek zahmetlidir; bunun yerine Bekci'nin **User-Agent** değerine izin verebilirsiniz.

## Bekci hangi User-Agent ile gelir? {#user-agent}

Web sitesi kontrolleri (HTTP, anahtar kelime, JSON) varsayılan olarak şu User-Agent ile gider:

```text
Mozilla/5.0 (compatible; Bekci/<sürüm>; +https://bekci.app/bot)
```

Sürüm numarası güncellemeyle değişir; kurallarınızda "**Bekci** içeriyor" koşulunu kullanın. Ana sunucu ve tüm kontrol noktaları aynı değeri kullanır: kontrol noktaları bu değeri ana sunucudan alır. (Kontrol noktasının da güncel sürümde olması gerekir; eski ajanlar kendi varsayılanlarıyla gider.)

Değeri **Ayarlar → Genel → Kontrol isteklerinin User-Agent'ı** alanından değiştirebilirsiniz; boş bırakırsanız varsayılan kullanılır. Bir monitörün **Başlıklar (headers)** alanına `User-Agent: …` yazarsanız o monitör için bu değer geçerli olur.

## Cloudflare'de izin verme {#cloudflare}

1. Cloudflare panelinde sitenizi seçin, **Security → WAF → Custom rules** bölümüne girin ve **Create rule** deyin.
2. Kural adı: `Bekci`.
3. **Field:** `User Agent`, **Operator:** `contains`, **Value:** `Bekci`.
4. **Action:** `Skip`. Atlanacakları seçin: *All remaining custom rules*, *Rate limiting rules*, *All managed rules* ve (planınızda varsa) *Super Bot Fight Mode*.
5. Kuralı kaydedip listenin **en üstüne** taşıyın.

İfade düzenleyicisinde (Edit expression) kural şöyle görünür:

```text
(http.user_agent contains "Bekci")
```

Ücretsiz plandaki **Bot Fight Mode** özel kurallarla atlanamaz. Kontroller hâlâ engelleniyorsa **Security → Bots** altından Bot Fight Mode'u kapatmanız gerekebilir.

## Daha güvenli yol: gizli başlık {#gizli-baslik}

User-Agent herkes tarafından taklit edilebilir. Kuralı yalnızca Bekci'nin bilebileceği bir değere bağlamak için:

1. Rastgele bir değer üretin, ör. `openssl rand -hex 16`.
2. Monitörün **Başlıklar (headers)** alanına ekleyin: `X-Bekci-Key: <ürettiğiniz-değer>`.
3. Cloudflare kuralının ifadesini şöyle yazın:

```text
(any(http.request.headers["x-bekci-key"][*] eq "<ürettiğiniz-değer>"))
```

Başlıklar kayıttan sonra panelde gizli tutulur, ama kontrol noktalarına da gönderilir; değeri bir parola gibi saklayın.

## Diğer güvenlik duvarları {#diger}

- **Nginx:** `if ($http_user_agent ~* "Bekci") { ... }` ile sınır kurallarını (ör. `limit_req`) atlayabilirsiniz.
- **IP izin listesi:** Yine de IP adresiyle izin vermek isterseniz kontrol noktalarının adreslerini **Ayarlar → Kontrol noktaları** sayfasında görebilirsiniz.
