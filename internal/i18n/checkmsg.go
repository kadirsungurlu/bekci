package i18n

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

var trDateRe = regexp.MustCompile(`^\d{2}\.\d{2}\.\d{4}$`)

// Message kontrol sonucu ve olay geçmişi metnini istenen dile çevirir.
//
// Kontrol mesajları (heartbeat mesajı, monitörün son mesajı, olay nedeni,
// işlem geçmişi kayıtları, konum mesajları, sunucu ajanı notu) veritabanında
// Türkçe saklanır; API bunları okuma anında isteğin diline çevirir. Kaynak
// eşleme checkEN'dir (checkmsg_en.go); biçim Error ile aynıdır (düz metin +
// fmt kalıpları, yakalanan parçalar da çevrilir).
//
// Kontrol mesajları iç içe kurulur; bunlar da parça parça çevrilir:
//   - sarmalayıcılar: "Bakımda (…)", "Ters mod: hedef erişilebilir (…)",
//     "Proxy hatası: …", "… (sonuç gelmeyen: A, B)", "… (2 bakımda)";
//   - çok konumlu birleşik mesaj: "Ana sunucu: Zaman aşımı; İstanbul: …";
//   - ham ağ hatası metni (zaten İngilizce: "dial tcp …: connection refused")
//     ve kullanıcı verisi (anahtar kelime, konum adı) olduğu gibi kalır.
//
// Bilinmeyen metin değişmeden döner. Sonuçlar sınırlı bir önbellekte tutulur
// (aynı mesajlar yüzlerce monitörde tekrarlanır): tekrar eden çağrı tek map
// aramasıdır.
func Message(lang, msg string) string {
	if msg == "" || Or(lang) != EN {
		return msg
	}
	if en, ok := msgCache.get(msg); ok {
		return en
	}
	en, ok := translateMessage(msg, 0)
	if !ok {
		en = msg
	}
	msgCache.put(msg, en)
	return en
}

// HasMessage Türkçe biçim dizgesi (veya düz metin) checkEN'de birebir anahtar
// olarak var mı? (Üretici mesajların kapsam testleri için.)
func HasMessage(tr string) bool {
	_, ok := checkEN[tr]
	return ok
}

// maxMessageDepth iç içe çeviri derinliği sınırı.
const maxMessageDepth = 6

var msgTable = &table{src: checkEN, anchoredFirst: true}

var (
	anchoredOnly = true
	looseOnly    = false
)

// translateMessage sırası: düz metin; başı sabit kalıplar (dış sarmalayıcılar:
// "Bakımda (%s)", "%d/%d konum çalışıyor — %s"); "; " ile birleşik mesaj;
// "Konum adı: mesaj"; başı serbest kalıplar ("%s (sonuç gelmeyen: %s)",
// "%s kaydı bulunamadı"); alt düzeyde "a, b" ad listesi. Konum ayrımı başı
// serbest kalıplardan önce yapılır: "Ana sunucu: A kaydı bulunamadı" içinde
// "%s kaydı bulunamadı" konum adını yutmasın.
func translateMessage(msg string, depth int) (string, bool) {
	if en, ok := msgTable.lookup(msg); ok {
		return en, true
	}
	if depth > maxMessageDepth {
		return "", false
	}
	if en, ok := msgTable.match(msg, depth, &anchoredOnly, translateMessage); ok {
		return en, true
	}
	if strings.Contains(msg, "; ") {
		parts := strings.Split(msg, "; ")
		changed := false
		for i, p := range parts {
			if en, ok := translateMessage(p, depth+1); ok {
				parts[i], changed = en, true
			}
		}
		if changed {
			return strings.Join(parts, "; "), true
		}
	}
	// "Konum adı: mesaj" (çok konumlu monitörde konum başına sonuç).
	if head, tail, ok := strings.Cut(msg, ": "); ok && head != "" {
		h, hok := msgTable.lookup(head)
		t, tok := translateMessage(tail, depth+1)
		if hok || tok {
			if !hok {
				h = head
			}
			if !tok {
				t = tail
			}
			return h + ": " + t, true
		}
	}
	if en, ok := msgTable.match(msg, depth, &looseOnly, translateMessage); ok {
		return en, true
	}
	// Yakalanan tarih: "24.11.2026" → arayüzün İngilizce biçimi "Nov 24, 2026"
	// (sertifika bitişi; arayüzdeki tarih kartlarıyla aynı görünsün).
	if depth > 0 && trDateRe.MatchString(msg) {
		if d, err := time.Parse("02.01.2006", msg); err == nil {
			return d.Format("Jan 2, 2006"), true
		}
	}
	// Yakalanan ad listesi: "Ana sunucu, İstanbul" → yalnızca birebir girdiler.
	if depth > 0 && strings.Contains(msg, ", ") {
		parts := strings.Split(msg, ", ")
		changed := false
		for i, p := range parts {
			if en, ok := msgTable.lookup(p); ok {
				parts[i], changed = en, true
			}
		}
		if changed {
			return strings.Join(parts, ", "), true
		}
	}
	return "", false
}

// msgCache çevrilmiş mesajların sınırlı önbelleği. Dolunca boşaltılır
// (mesajlar kullanıcı verisi içerebildiği için sınırsız büyümesin).
var msgCache = &boundedCache{max: 8192}

type boundedCache struct {
	mu  sync.RWMutex
	m   map[string]string
	max int
}

func (c *boundedCache) get(k string) (string, bool) {
	c.mu.RLock()
	v, ok := c.m[k]
	c.mu.RUnlock()
	return v, ok
}

func (c *boundedCache) put(k, v string) {
	if len(k) > 1024 {
		return // uzun, büyük olasılıkla tekil metinler önbelleği şişirmesin
	}
	c.mu.Lock()
	if c.m == nil || len(c.m) >= c.max {
		c.m = make(map[string]string, 256)
	}
	c.m[k] = v
	c.mu.Unlock()
}
