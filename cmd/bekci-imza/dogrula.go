package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betik"
	"github.com/kadirsungurlu/bekci/internal/betikkayit"
)

// saklamaSn kayıt saklama süresi (D10): checkpoint bundan genç olamaz.
const saklamaSn = 365 * 86400

// zincirBasi --bekle dosyası: önceki denetimde görülen baş (D7 yerel çapa).
type zincirBasi struct {
	Seq  int64  `json:"seq"`
	Hash string `json:"hash"`
	Time int64  `json:"time"`
}

// makbuz zarfı çözülmüş makbuz.
type makbuz struct {
	betik.Receipt
	key, digest string
}

type dogrulama struct {
	a        *app
	x        betikkayit.Export
	keys     map[string]ed25519.PublicKey
	panel    map[string]map[int64]makbuz // anahtar -> seq -> makbuz
	uyari    int
	imzasiz  int
	makbuzSy int
}

func (d *dogrulama) uyar(f string, args ...any) {
	d.uyari++
	fmt.Fprintf(d.a.out, "UYARI: "+f+"\n", args...)
}

func (a *app) dogrula(args []string) error {
	fs := newFlags("dogrula", a.err)
	path := fs.String("kayit", "", "panel dışa aktarımı")
	var logs []string
	fs.Func("ajan-kayit", "ajan kayit.log (birden çok verilebilir)", func(s string) error {
		logs = append(logs, s)
		return nil
	})
	bekle := fs.String("bekle", "", "önceki zincir başı dosyası")
	maxAge := fs.Duration("en-eski-kontrol", 0, "son çapanın en çok yaşı (0: denetlenmez)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("--kayit gerekli")
	}
	b, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	x, err := betikkayit.ParseExport(b)
	if err != nil {
		return fmt.Errorf("dışa aktarım okunamadı: %w", err)
	}
	if err := betikkayit.Verify(x); err != nil {
		return fmt.Errorf("zincir kırık, ilk kırık nokta %w", err)
	}
	d := &dogrulama{a: a, x: x, keys: map[string]ed25519.PublicKey{}, panel: map[string]map[int64]makbuz{}}
	if err := d.checkpointler(); err != nil {
		return err
	}
	if err := d.makbuzlar(); err != nil {
		return err
	}
	for _, p := range logs {
		if err := d.ajanKaydi(p); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	if d.imzasiz > 0 {
		d.uyar("%d makbuzun açık anahtarı dışa aktarımda yok, imzası denetlenemedi", d.imzasiz)
	}
	if len(logs) == 0 {
		d.uyar("ajan kaydı verilmedi: son makbuzların kırpılması görülemez")
	}
	for _, e := range x.Events {
		if e.Kind == "chain_fork" {
			d.uyar("seq %d: panel zincirin geri gittiğini kaydetmiş (chain_fork), yedekten dönüş olabilir: %s", e.Seq, e.Data)
		}
	}
	if *maxAge > 0 {
		if err := d.capaYasi(*maxAge); err != nil {
			return err
		}
	}
	head := d.bas()
	if *bekle != "" {
		if err := d.bekleDenetle(*bekle, head); err != nil {
			return err
		}
	}
	fmt.Fprintf(a.out, "TAMAM: baş seq %d %s, %d olay, %d checkpoint, %d makbuz, %d uyarı\n",
		head.Seq, head.Hash, len(x.Events), len(x.Checkpoints), d.makbuzSy, d.uyari)
	return nil
}

func (d *dogrulama) bas() zincirBasi {
	if n := len(d.x.Events); n > 0 {
		e := d.x.Events[n-1]
		return zincirBasi{e.Seq, e.Hash, e.Time}
	}
	if n := len(d.x.Checkpoints); n > 0 {
		c := d.x.Checkpoints[n-1]
		return zincirBasi{c.Seq, c.Hash, c.Time}
	}
	return zincirBasi{}
}

// checkpointler erken budamayı yakalar (680 §8.6): düşürülen olay, checkpoint
// yazıldığında saklama süresini doldurmuş olmalıdır.
func (d *dogrulama) checkpointler() error {
	var prev int64
	for _, c := range d.x.Checkpoints {
		switch {
		case c.Seq <= prev:
			return fmt.Errorf("checkpoint seq %d: sıra bozuk", c.Seq)
		case !betik.IsDigest(c.Hash):
			return fmt.Errorf("checkpoint seq %d: özet geçersiz", c.Seq)
		case c.CreatedAt-c.Cutoff < saklamaSn, c.CreatedAt-c.Time < saklamaSn:
			return fmt.Errorf("checkpoint seq %d: saklama süresi dolmadan budanmış (yaş %d gün, en az 365)",
				c.Seq, (c.CreatedAt-c.Time)/86400)
		case c.CreatedAt > d.x.ExportedAt:
			return fmt.Errorf("checkpoint seq %d: dışa aktarımdan sonra yazılmış", c.Seq)
		}
		prev = c.Seq
	}
	return nil
}

// makbuzlar zincirdeki makbuzların imzasını ve anahtar başına seq/prev bağını
// denetler. Anahtarlar da zincirden gelir: bağımsız kanıt ajan kaydı ve çapadır.
func (d *dogrulama) makbuzlar() error {
	for _, e := range d.x.Events {
		if e.Kind != "host_key_seen" && e.Kind != "host_key_approved" {
			continue
		}
		var v struct {
			HostKey   string `json:"host_key"`
			PublicKey string `json:"public_key"`
		}
		if json.Unmarshal([]byte(e.Data), &v) != nil {
			return fmt.Errorf("seq %d: ajan anahtarı olayı çözülemedi", e.Seq)
		}
		pub, err := betik.ParsePublicKey(v.PublicKey)
		if err != nil || betik.KeyID(pub) != v.HostKey {
			return fmt.Errorf("seq %d: ajan anahtarı kimliği tutmuyor", e.Seq)
		}
		d.keys[v.HostKey] = pub
	}
	for _, e := range d.x.Events {
		if e.Kind != "receipt" {
			continue
		}
		var v struct {
			Receipt json.RawMessage `json:"receipt"`
		}
		if json.Unmarshal([]byte(e.Data), &v) != nil {
			return fmt.Errorf("seq %d: makbuz olayı çözülemedi", e.Seq)
		}
		m, err := d.zarfCoz(v.Receipt)
		if err != nil {
			return fmt.Errorf("seq %d: %w", e.Seq, err)
		}
		if m.RunID != e.RunID {
			return fmt.Errorf("seq %d: makbuz başka çalıştırmaya ait", e.Seq)
		}
		byKey := d.panel[m.key]
		if byKey == nil {
			byKey = map[int64]makbuz{}
			d.panel[m.key] = byKey
		}
		if o, ok := byKey[m.Seq]; ok && o.digest != m.digest {
			return fmt.Errorf("seq %d: aynı makbuz sırasında (%d) iki farklı makbuz", e.Seq, m.Seq)
		}
		byKey[m.Seq] = *m
		d.makbuzSy++
	}
	for key, byKey := range d.panel {
		seqs := sortedKeys(byKey)
		for i, s := range seqs {
			m := byKey[s]
			if i == 0 {
				continue
			}
			p := seqs[i-1]
			switch {
			case s == p+1 && m.Prev != byKey[p].digest:
				return fmt.Errorf("makbuz zinciri kırık: anahtar %.12s seq %d önceki makbuza bağlanmıyor", key, s)
			case s != p+1:
				d.uyar("anahtar %.12s: makbuz boşluğu %d -> %d (kuyruk düşmüş olabilir, ajan kaydıyla doğrulayın)", key, p, s)
			}
		}
	}
	return nil
}

func sortedKeys(m map[int64]makbuz) []int64 {
	out := make([]int64, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// zarfCoz makbuz zarfını çözer; açık anahtar biliniyorsa imzayı denetler.
func (d *dogrulama) zarfCoz(raw []byte) (*makbuz, error) {
	env, err := betik.ParseEnvelope(raw)
	if err != nil {
		return nil, fmt.Errorf("makbuz zarfı: %w", err)
	}
	if pub, ok := d.keys[env.Key]; !ok {
		// Anahtar olayı yalnız budanmış önekte kalmış olabilir; budama yoksa
		// bilinmeyen anahtar, kendi anahtarıyla imzalanmış sahte makbuzdur.
		if len(d.x.Checkpoints) == 0 {
			return nil, fmt.Errorf("makbuz anahtarı %.12s kayıtta yok", env.Key)
		}
		d.imzasiz++
	} else if !env.Verify(betik.KindReceipt, pub) {
		return nil, errors.New("makbuz imzası geçersiz")
	}
	r, err := betik.ParseReceipt(env.Payload)
	if err != nil {
		return nil, fmt.Errorf("makbuz: %w", err)
	}
	return &makbuz{Receipt: *r, key: env.Key, digest: env.PayloadDigest()}, nil
}

// ajanSatiri kayit.log satırı (internal/betikajan.logLine); yalnız gereken alanlar.
type ajanSatiri struct {
	Kind    string          `json:"kind"`
	RunID   string          `json:"run_id"`
	Receipt json.RawMessage `json:"receipt"`
}

// ajanKaydi kayit.log'u okur (680 §8.5). Dosyanın kendi seq/prev zinciri, panel
// makbuzlarıyla birebir eşleşme ve panelin kaydından kırpılan son makbuzlar.
func (d *dogrulama) ajanKaydi(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	var last *makbuz
	seen := map[string]map[int64]bool{}
	for {
		var l ajanSatiri
		if err := dec.Decode(&l); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if errors.Is(err, io.ErrUnexpectedEOF) { // yazılırken kesilmiş son satır
				d.uyar("%s: son satır yarım", path)
				break
			}
			return fmt.Errorf("bayt %d: satır çözülemedi: %w", dec.InputOffset(), err)
		}
		if l.Kind != betik.EventReceipt {
			continue
		}
		m, err := d.zarfCoz(l.Receipt)
		if err != nil {
			return err
		}
		if m.RunID != l.RunID {
			return fmt.Errorf("makbuz seq %d: satır başka çalıştırmayı gösteriyor", m.Seq)
		}
		switch {
		case last == nil && (m.Seq != 1 || m.Prev != ""):
			return fmt.Errorf("baştan kesilmiş: ilk makbuz seq %d", m.Seq)
		case last != nil && last.key == m.key && (m.Seq != last.Seq+1 || m.Prev != last.digest):
			return fmt.Errorf("ajan zinciri kırık: seq %d, seq %d'ye bağlanmıyor", m.Seq, last.Seq)
		}
		last = m
		if seen[m.key] == nil {
			seen[m.key] = map[int64]bool{}
		}
		seen[m.key][m.Seq] = true
		if err := d.panelleKarsilastir(m); err != nil {
			return err
		}
	}
	// Panelde olup ajanda olmayan makbuz: ajan kaydı kırpılmış ya da sahte makbuz.
	for key, s := range seen {
		for _, seq := range sortedKeys(d.panel[key]) {
			if !s[seq] {
				return fmt.Errorf("makbuz seq %d (anahtar %.12s) panelde var, ajan kaydında yok", seq, key)
			}
		}
	}
	return nil
}

func (d *dogrulama) panelleKarsilastir(m *makbuz) error {
	byKey := d.panel[m.key]
	if p, ok := byKey[m.Seq]; ok {
		if p.digest != m.digest {
			return fmt.Errorf("makbuz seq %d: panel ile ajan kaydı farklı", m.Seq)
		}
		return nil
	}
	if m.EndedAt > d.x.ExportedAt {
		return nil // dışa aktarımdan sonra bitmiş
	}
	if len(d.x.Checkpoints) > 0 && len(d.x.Events) > 0 && m.EndedAt < d.x.Events[0].Time {
		return nil // saklamayla düşmüş önek
	}
	var head int64
	if s := sortedKeys(byKey); len(s) > 0 {
		head = s[len(s)-1]
	}
	if m.Seq > head {
		return fmt.Errorf("makbuz seq %d ajan kaydında var, panelin son makbuzu seq %d: son makbuzlar kırpılmış ya da henüz iletilmemiş", m.Seq, head)
	}
	d.uyar("makbuz seq %d ajan kaydında var, panelde yok (kuyruk düşmüş olabilir)", m.Seq)
	return nil
}

// capaYasi panelin çapa üretmeyi bıraktığını yakalar (680 §8.4).
func (d *dogrulama) capaYasi(maxAge time.Duration) error {
	now := d.a.now()
	for i := len(d.x.Events) - 1; i >= 0; i-- {
		if e := d.x.Events[i]; e.Kind == "anchor" {
			if age := now.Sub(time.Unix(e.Time, 0)); age > maxAge {
				return fmt.Errorf("son çapa seq %d, %s önce: panel susturulmuş olabilir (sınır %s)", e.Seq, age.Round(time.Minute), maxAge)
			}
			return nil
		}
	}
	return errors.New("dışa aktarımda çapa yok")
}

// bekleDenetle önceki başın bu dışa aktarımda aynen durduğunu denetler (geri
// alma ve çatallanma, 680 §8.7), geçerse dosyayı yeni başla günceller.
func (d *dogrulama) bekleDenetle(path string, head zincirBasi) error {
	b, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		fmt.Fprintf(d.a.out, "NOT: %s yok, ilk kayıt yazılıyor\n", path)
		return baslikYaz(path, head)
	case err != nil:
		return err
	}
	var old zincirBasi
	if err := betik.DecodeStrict(b, &old); err != nil {
		return fmt.Errorf("%s bozuk: %w", path, err)
	}
	if !betik.IsDigest(old.Hash) || old.Seq < 1 {
		return fmt.Errorf("%s bozuk", path)
	}
	if err := d.oncekiBas(old, head); err != nil {
		return err
	}
	return baslikYaz(path, head)
}

func (d *dogrulama) oncekiBas(old, head zincirBasi) error {
	if old.Seq > head.Seq {
		return fmt.Errorf("zincir geri alınmış: önceki baş seq %d dışa aktarımda yok, şimdiki baş seq %d", old.Seq, head.Seq)
	}
	for _, e := range d.x.Events {
		if e.Seq == old.Seq {
			if e.Hash != old.Hash {
				return fmt.Errorf("zincir çatallanmış: seq %d önceki denetimdekinden farklı", old.Seq)
			}
			return nil
		}
	}
	for _, c := range d.x.Checkpoints {
		if c.Seq == old.Seq {
			if c.Hash != old.Hash {
				return fmt.Errorf("zincir çatallanmış: checkpoint seq %d önceki denetimdekinden farklı", old.Seq)
			}
			return nil
		}
	}
	if age := d.a.now().Unix() - old.Time; age < saklamaSn {
		return fmt.Errorf("önceki baş seq %d dışa aktarımda yok ve saklama süresi dolmamış (%d gün)", old.Seq, age/86400)
	}
	d.uyar("önceki baş seq %d saklamayla düşmüş, karşılaştırılamadı", old.Seq)
	return nil
}

// baslikYaz atomik yazım: yarım kalan denetim önceki başı bozmaz.
func baslikYaz(path string, h zincirBasi) error {
	if h.Seq == 0 {
		return nil
	}
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".bekle-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(append(b, '\n')); err == nil {
		err = tmp.Sync()
	}
	if e := tmp.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
