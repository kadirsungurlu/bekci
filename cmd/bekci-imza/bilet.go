package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betik"
)

// biletIstek panelin ticket_request alanı (internal/api/scripts.go). params
// imzalı özete giren ham metindir, nesne değil.
type biletIstek struct {
	RunID          string `json:"run_id"`
	HostKey        string `json:"host_key"`
	ScriptID       string `json:"script_id"`
	Version        int64  `json:"version"`
	ManifestSHA256 string `json:"manifest_sha256"`
	ParamsSHA256   string `json:"params_sha256"`
	Params         string `json:"params"`
}

func (a *app) iptal(args []string) error {
	fs := newFlags("iptal", a.err)
	keyPath := fs.String("anahtar", "", "yayin anahtarı")
	id := fs.String("id", "", "betik kimliği")
	below := fs.Int64("alt-surum", 0, "bu sürümün altı çalışmaz")
	if err := fs.Parse(args); err != nil {
		return err
	}
	k, err := a.anahtarOku(*keyPath, slotYayin)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(betik.Revoke{Schema: betik.SchemaVersion, ID: *id, BelowVersion: *below})
	if err != nil {
		return err
	}
	if _, err := betik.ParseRevoke(payload); err != nil {
		return err
	}
	priv, err := a.anahtarAc(k)
	if err != nil {
		return err
	}
	defer clear(priv)
	env, err := imzaliUret(priv, betik.KindRevoke, payload)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%s\n", env)
	return nil
}

func (a *app) bilet(args []string) error {
	fs := newFlags("bilet", a.err)
	keyPath := fs.String("anahtar", "", "onay anahtarı")
	reqPath := fs.String("istek", "", "ticket_request.json")
	sure := fs.Duration("sure", 10*time.Minute, "bilet süresi (en çok 15m)")
	approver := fs.String("onaylayan", "", "onaylayan adı")
	pinPath := fs.String("igneler", "", "anahtar kimliği -> sunucu adı (JSON)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *reqPath == "" {
		return errors.New("--istek gerekli")
	}
	k, err := a.anahtarOku(*keyPath, slotOnay)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(*reqPath)
	if err != nil {
		return err
	}
	var q biletIstek
	if err := betik.DecodeStrict(raw, &q); err != nil {
		return fmt.Errorf("istek: %w", err)
	}
	if got := betik.Digest([]byte(q.Params)); got != q.ParamsSHA256 {
		return fmt.Errorf("params_sha256 tutmuyor: parametrelerden %s çıktı, istekte %s yazıyor; imzalanmadı", got, q.ParamsSHA256)
	}
	params, err := paramSatirlari(q.Params)
	if err != nil {
		return fmt.Errorf("parametreler: %w", err)
	}
	host, err := sunucuAdi(*pinPath, q.HostKey)
	if err != nil {
		return err
	}
	t := betik.Ticket{Schema: betik.SchemaVersion, RunID: q.RunID, HostKey: q.HostKey, ScriptID: q.ScriptID, Version: q.Version,
		ManifestSHA256: q.ManifestSHA256, ParamsSHA256: q.ParamsSHA256, Approver: *approver}
	t.IssuedAt = a.now().Unix()
	t.ExpiresAt = t.IssuedAt + int64(*sure/time.Second)
	if err := t.Validate(); err != nil {
		return err
	}

	// Ekran doğrulanmış alanlardan basılır; stdout yalnız zarf içindir.
	w := a.err
	fmt.Fprintf(w, "\nBetik:      %s\nSürüm:      %d\nSunucu:     %s\n", t.ScriptID, t.Version, host)
	fmt.Fprintln(w, "Parametre:")
	for _, l := range params {
		fmt.Fprintln(w, "  "+l)
	}
	if len(params) == 0 {
		fmt.Fprintln(w, "  (yok)")
	}
	fmt.Fprintf(w, "Çalıştırma: %s\nBildirim:   %s\nOnaylayan:  %q\nSüre:       %s\n\n", t.RunID, t.ManifestSHA256, t.Approver, *sure)
	fmt.Fprint(w, `Onaylamak için "evet" yazın: `)
	ans, err := a.satir()
	if err != nil {
		return err
	}
	if ans != "evet" {
		return errors.New("onaylanmadı, imzalanmadı")
	}
	priv, err := a.anahtarAc(k)
	if err != nil {
		return err
	}
	defer clear(priv)
	// Süre parola sorulduktan sonra başlar.
	t.IssuedAt = a.now().Unix()
	t.ExpiresAt = t.IssuedAt + int64(*sure/time.Second)
	payload, err := json.Marshal(&t)
	if err != nil {
		return err
	}
	if _, err := betik.ParseTicket(payload); err != nil {
		return err
	}
	env, err := imzaliUret(priv, betik.KindTicket, payload)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%s\n", env)
	return nil
}

// paramSatirlari parametre metnini ajanın katılığıyla çözer (yinelenen anahtar
// red: ekrandaki değer ajanın gördüğünden başka olamaz) ve ad sırasıyla basar.
func paramSatirlari(raw string) ([]string, error) {
	var probe map[string]any
	if err := betik.DecodeStrict([]byte(raw), &probe); err != nil {
		return nil, err
	}
	if probe == nil {
		return nil, errors.New("nesne değil")
	}
	d := json.NewDecoder(bytes.NewReader([]byte(raw)))
	d.UseNumber()
	var v map[string]any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(v))
	for name, val := range v {
		switch x := val.(type) {
		case string:
			out = append(out, name+" = "+strconv.Quote(x))
		case json.Number:
			out = append(out, name+" = "+x.String())
		case bool:
			out = append(out, name+" = "+strconv.FormatBool(x))
		default:
			return nil, fmt.Errorf("%q düz değer değil", name)
		}
	}
	slices.Sort(out)
	return out, nil
}

// sunucuAdi host_key'i operatörün yerel dosyasından ada çevirir; panelin
// verdiği ad kullanılmaz.
func sunucuAdi(path, key string) (string, error) {
	unknown := "!!! BİLİNMEYEN SUNUCU !!! anahtar " + key + " yerel iğne dosyasında yok; panel başka bir sunucuyu gösteriyor olabilir"
	if path == "" {
		return unknown, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var pins map[string]string
	if err := json.Unmarshal(b, &pins); err != nil {
		return "", fmt.Errorf("iğne dosyası: %w", err)
	}
	if n, ok := pins[key]; ok {
		return strconv.Quote(n) + " (" + key + ")", nil
	}
	return unknown, nil
}
