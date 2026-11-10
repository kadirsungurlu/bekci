package eklentiajan

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Olay kayıt zincirine giren olay. Her çalıştırma girmez (dakikada bir
// gürültü); yalnız aç/kapat, sürüm değişimi, iğne ve günlük özet (B.1).
type Olay struct {
	T       int64          `json:"t"` // unix sn
	Tur     string         `json:"tur"`
	ID      string         `json:"id,omitempty"`
	Surum   int64          `json:"surum,omitempty"`
	SHA256  string         `json:"sha256,omitempty"`
	Ayrinti map[string]any `json:"ayrinti,omitempty"`
	Onceki  string         `json:"onceki"` // önceki satırın sha256'sı (zincir)
}

// Olay türleri.
const (
	OlayAc    = "ac"     // panel eklentiyi açtı (öğe listeye girdi)
	OlayKapat = "kapat"  // panel kapattı (öğe listeden çıktı)
	OlaySurum = "surum"  // çalışan sürüm değişti
	OlayIzin  = "izin"   // yerel iğne kondu (CLI)
	OlayIgneK = "igne_k" // yerel iğne kaldırıldı (CLI)
	OlayOzet  = "ozet"   // günlük özet makbuzu
)

// Kayitci olayı kurcalama belli bir kayda yazar. Açık derlemede yerel zincirli
// JSON satırları; Pro derlemesi betikkayit'e bağlayabilir.
type Kayitci interface {
	Yaz(o Olay) error
}

// DosyaKayit LogDir/kayit.jsonl: her satır önceki satırın özetini taşır, satır
// silinir ya da değiştirilirse zincir kırılır (CLI `liste` denetler).
type DosyaKayit struct {
	Dir string
	mu  sync.Mutex
}

func (k *DosyaKayit) path() string { return filepath.Join(k.Dir, "kayit.jsonl") }

// Yaz olayı zincire ekler.
func (k *DosyaKayit) Yaz(o Olay) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := os.MkdirAll(k.Dir, 0o700); err != nil {
		return err
	}
	last, _, err := k.son()
	if err != nil {
		return err
	}
	if o.T == 0 {
		o.T = time.Now().Unix()
	}
	o.Onceki = last
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(k.path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// son son satırın özetini ve zincirin sağlam olup olmadığını döner.
func (k *DosyaKayit) son() (string, bool, error) {
	f, err := os.Open(k.path())
	if os.IsNotExist(err) {
		return "", true, nil
	}
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	prev, ok := "", true
	for sc.Scan() {
		var o Olay
		if json.Unmarshal(sc.Bytes(), &o) != nil || o.Onceki != prev {
			ok = false
		}
		prev = Digest(sc.Bytes())
	}
	return prev, ok, sc.Err()
}

// Saglam zincir kırılmamış mı.
func (k *DosyaKayit) Saglam() (bool, error) {
	_, ok, err := k.son()
	return ok, err
}
