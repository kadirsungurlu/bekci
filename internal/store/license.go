package store

// Lisans ayarları (docs/LISANS-TASARIM.md "Ayarlar anahtarları"): settings
// tablosunda "lisans.*" anahtarları; migration yok. Zamanlar UTC RFC3339.

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"
)

// Lisans ayar anahtarları.
const (
	LisansPanelID     = "lisans.panel_id"
	LisansKurulum     = "lisans.kurulum"
	LisansEnIleri     = "lisans.en_ileri"
	LisansAnahtar     = "lisans.anahtar"
	LisansKira        = "lisans.kira"
	LisansSonBasari   = "lisans.son_basari"
	LisansSonDeneme   = "lisans.son_deneme"
	LisansSonHata     = "lisans.son_hata"
	LisansUyariSeviye = "lisans.uyari_seviye"
)

// LisansAyar settings tablosundaki lisans satırları.
type LisansAyar struct {
	PanelID     string
	Kurulum     time.Time
	EnIleri     time.Time
	Anahtar     string
	Kira        string
	SonBasari   time.Time
	SonDeneme   time.Time
	SonHata     string
	UyariSeviye string
}

// LisansZaman ayar değeri için zaman metni (sıfır = "").
func LisansZaman(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func lisansZamanOku(v string) time.Time {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(v))
	if err != nil {
		return time.Time{}
	}
	return t
}

// LisansOku tüm lisans ayarlarını okur (olmayan alan boş/sıfır).
func (s *Store) LisansOku(ctx context.Context) (LisansAyar, error) {
	var a LisansAyar
	rows, err := s.db.QueryContext(ctx, "SELECT key, value FROM settings WHERE key LIKE 'lisans.%'")
	if err != nil {
		return a, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return a, err
		}
		switch k {
		case LisansPanelID:
			a.PanelID = v
		case LisansKurulum:
			a.Kurulum = lisansZamanOku(v)
		case LisansEnIleri:
			a.EnIleri = lisansZamanOku(v)
		case LisansAnahtar:
			a.Anahtar = v
		case LisansKira:
			a.Kira = v
		case LisansSonBasari:
			a.SonBasari = lisansZamanOku(v)
		case LisansSonDeneme:
			a.SonDeneme = lisansZamanOku(v)
		case LisansSonHata:
			a.SonHata = v
		case LisansUyariSeviye:
			a.UyariSeviye = v
		}
	}
	return a, rows.Err()
}

// LisansHazirla panel kimliğini (UUID v4) ve kurulum zamanını yoksa oluşturur;
// varsa dokunmaz (ON CONFLICT DO NOTHING: eşzamanlı açılışta da tek değer kalır).
func (s *Store) LisansHazirla(ctx context.Context, simdi time.Time) (LisansAyar, error) {
	id, err := uuidV4()
	if err != nil {
		return LisansAyar{}, err
	}
	for k, v := range map[string]string{LisansPanelID: id, LisansKurulum: LisansZaman(simdi)} {
		if _, err := s.db.ExecContext(ctx,
			"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO NOTHING", k, v); err != nil {
			return LisansAyar{}, err
		}
	}
	return s.LisansOku(ctx)
}

// LisansYaz verilen anahtarları yazar (tek işlem).
func (s *Store) LisansYaz(ctx context.Context, kv map[string]string) error {
	for k := range kv {
		if !strings.HasPrefix(k, "lisans.") {
			return fmt.Errorf("lisans ayarı değil: %s", k)
		}
	}
	return s.SetSettings(ctx, kv)
}

// LisansSil verilen lisans anahtarlarını siler.
func (s *Store) LisansSil(ctx context.Context, keys ...string) error {
	return s.tx(ctx, func(tx *Tx) error {
		for _, k := range keys {
			if !strings.HasPrefix(k, "lisans.") {
				return fmt.Errorf("lisans ayarı değil: %s", k)
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM settings WHERE key = ?", k); err != nil {
				return err
			}
		}
		return nil
	})
}

// LisansEnIleriYaz en ileri görülen zamanı yalnız ileri yazar; yazılan (ya da
// saklı, daha ileri olan) değeri döner.
func (s *Store) LisansEnIleriYaz(ctx context.Context, t time.Time) (time.Time, error) {
	var sonuc time.Time
	err := s.tx(ctx, func(tx *Tx) error {
		var v string
		err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", LisansEnIleri).Scan(&v)
		if err == nil {
			if eski := lisansZamanOku(v); !eski.IsZero() && !t.After(eski) {
				sonuc = eski
				return nil
			}
		}
		sonuc = t.UTC().Truncate(time.Second)
		_, err = tx.ExecContext(ctx,
			"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
			LisansEnIleri, LisansZaman(sonuc))
		return err
	})
	return sonuc, err
}

func uuidV4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
