package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betik"
)

const varsayilanOmur = 90 * 24 * time.Hour

// imzaliBetik panelin POST /api/scripts gövdesi (code alanı hariç).
type imzaliBetik struct {
	Manifest json.RawMessage `json:"manifest"`
	Body     []byte          `json:"body"`
}

func (a *app) imzala(args []string) error {
	fs := newFlags("imzala", a.err)
	keyPath := fs.String("anahtar", "", "yayin anahtarı")
	manPath := fs.String("bildirim", "", "manifest.json")
	bodyPath := fs.String("govde", "", "betik gövdesi")
	outDir := fs.String("cikti", ".", "çıktı dizini")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *manPath == "" || *bodyPath == "" {
		return errors.New("--bildirim ve --govde gerekli")
	}
	k, err := a.anahtarOku(*keyPath, slotYayin)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(*manPath)
	if err != nil {
		return err
	}
	var m betik.Manifest
	if err := betik.DecodeStrict(raw, &m); err != nil {
		return fmt.Errorf("bildirim: %w", err)
	}
	body, err := os.ReadFile(*bodyPath)
	if err != nil {
		return err
	}
	if err := govdeDenetle(body, &m); err != nil {
		return fmt.Errorf("gövde reddedildi: %w", err)
	}
	if err := a.shellcheckCalistir(body); err != nil {
		return fmt.Errorf("gövde reddedildi: %w", err)
	}
	m.SHA256 = betik.Digest(body)
	now := a.now().Unix()
	if m.NotBefore == 0 {
		m.NotBefore = now
	}
	if m.NotAfter == 0 {
		m.NotAfter = m.NotBefore + int64(varsayilanOmur/time.Second)
	}
	if m.Params == nil {
		m.Params = []betik.Param{}
	}
	payload, err := json.Marshal(&m)
	if err != nil {
		return err
	}
	if _, err := betik.ParseManifest(payload); err != nil {
		return fmt.Errorf("bildirim: %w", err)
	}
	priv, err := a.anahtarAc(k)
	if err != nil {
		return err
	}
	defer clear(priv)
	env, err := imzaliUret(priv, betik.KindManifest, payload)
	if err != nil {
		return err
	}
	b, err := json.Marshal(imzaliBetik{Manifest: env, Body: body})
	if err != nil {
		return err
	}
	path := filepath.Join(*outDir, fmt.Sprintf("%s-%d.betik.json", m.ID, m.Version))
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%s\nid: %s\nsurum: %d\nonay: %s\ngovde_sha256: %s\nbitis: %s\n", path, m.ID, m.Version, m.Approval,
		m.SHA256, time.Unix(m.NotAfter, 0).UTC().Format(time.DateOnly))
	return nil
}

func (a *app) shellcheckCalistir(body []byte) error {
	if _, err := exec.LookPath(a.shellcheck); err != nil {
		fmt.Fprintln(a.err, "UYARI: shellcheck yok, gövde yalnız yerleşik denetimden geçti")
		return nil
	}
	c := exec.Command(a.shellcheck, "--norc", "-s", "sh", "-")
	c.Stdin = bytes.NewReader(body)
	var o bytes.Buffer
	c.Stdout, c.Stderr = &o, &o
	if err := c.Run(); err != nil {
		return fmt.Errorf("shellcheck: %w\n%s", err, o.String())
	}
	return nil
}

var (
	paramRefRe = regexp.MustCompile(`BEKCI_P_[A-Za-z0-9_]*`)
	evalRe     = regexp.MustCompile(`(^|[\s;&|(])eval(\s|$)`)
	shCRe      = regexp.MustCompile(`(^|[\s;&|(/])(sh|ash|bash|dash|ksh|zsh)\s+-[A-Za-z]*c(\s|$)`)
)

// govdeDenetle imzalanmadan önceki yerleşik denetim (680 §6.1): boyut, tırnaksız
// parametre, eval, sh -c "$...", heredoc, bildirimde olmayan parametre. Kötü
// niyete değil yanlışa karşıdır; gövde operatörün okuduğu güvenilen koddur.
func govdeDenetle(body []byte, m *betik.Manifest) error {
	if len(body) == 0 || len(body) > betik.MaxBody {
		return fmt.Errorf("boş ya da %d baytın üstünde", betik.MaxBody)
	}
	if bytes.IndexByte(body, 0) >= 0 {
		return errors.New("NUL bayt var")
	}
	known := map[string]bool{}
	for _, p := range m.Params {
		known["BEKCI_P_"+strings.ToUpper(p.Name)] = true
	}
	for i, l := range strings.Split(string(body), "\n") {
		for _, r := range paramRefRe.FindAllString(l, -1) {
			if !known[r] {
				return fmt.Errorf("satır %d: %s bildirimde yok", i+1, r)
			}
		}
	}
	mask, err := kabukTara(string(body))
	if err != nil {
		return err
	}
	raw := strings.Split(string(body), "\n")
	for i, l := range strings.Split(mask, "\n") {
		switch {
		case strings.Contains(l, "<<"):
			return fmt.Errorf("satır %d: heredoc denetlenemez, kullanmayın", i+1)
		case evalRe.MatchString(l):
			return fmt.Errorf("satır %d: eval yasak", i+1)
		case shCRe.MatchString(l) && strings.Contains(raw[i], "$"):
			return fmt.Errorf("satır %d: sh -c ile değişken yasak", i+1)
		}
	}
	return nil
}

// kabukTara gövdeyi tırnak ve komut ikamesi bağlamıyla yürür. Çift tırnak
// dışındaki her $BEKCI_P_ kullanımını reddeder; tek tırnak içindeki de reddedilir
// (genelde sh -c'ye gider). Dönen metinde yorum ve tırnak içi boşluktur, satır
// numaraları korunur.
func kabukTara(s string) (string, error) {
	const (
		dq   = 'd' // çift tırnak
		sub  = 'm' // $( ... )
		par  = 'p' // ( ... )
		tick = 'b' // ` ... `
	)
	var mask strings.Builder
	stack := []byte{'c'}
	line := 1
	top := func() byte { return stack[len(stack)-1] }
	wordStart := func(i int) bool { return i == 0 || strings.IndexByte(" \t\n;&|()", s[i-1]) >= 0 }
	blank := func(c byte) {
		if c == '\n' {
			line++
			mask.WriteByte('\n')
		} else {
			mask.WriteByte(' ')
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		inDQ := top() == dq
		if c == '$' && (strings.HasPrefix(s[i:], "$BEKCI_P_") || strings.HasPrefix(s[i:], "${BEKCI_P_")) && !inDQ {
			return "", fmt.Errorf("satır %d: tırnaksız parametre", line)
		}
		switch {
		case c == '\\' && i+1 < len(s):
			i++
			if inDQ {
				blank(' ')
				blank(s[i])
			} else if s[i] == '\n' {
				blank('\n')
			} else {
				mask.WriteByte(s[i]) // \eval yine eval'dir
			}
		case inDQ && c == '"':
			stack = stack[:len(stack)-1]
			blank(' ')
		case c == '$' && i+1 < len(s) && s[i+1] == '(':
			stack = append(stack, sub)
			i++
			blank(' ')
			blank(' ')
		case c == '`':
			if top() == tick {
				stack = stack[:len(stack)-1]
			} else {
				stack = append(stack, tick)
			}
			blank(' ')
		case inDQ:
			blank(c)
		case c == '"':
			stack = append(stack, dq)
			blank(' ')
		case c == '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return "", fmt.Errorf("satır %d: tek tırnak kapanmamış", line)
			}
			q := s[i+1 : i+1+end]
			if strings.Contains(q, "$BEKCI_P_") || strings.Contains(q, "${BEKCI_P_") {
				return "", fmt.Errorf("satır %d: tek tırnak içinde parametre", line)
			}
			for j := 0; j < len(q)+2; j++ {
				blank(s[i+j])
			}
			i += end + 1
		case c == '#' && wordStart(i):
			for ; i < len(s) && s[i] != '\n'; i++ {
				blank(' ')
			}
			i--
		case c == '(':
			stack = append(stack, par)
			mask.WriteByte(c)
		case c == ')':
			if t := top(); t == sub || t == par {
				stack = stack[:len(stack)-1]
			}
			mask.WriteByte(c) // case deseni kapatan ) da buraya düşer
		case c == '\n':
			blank(c)
		default:
			mask.WriteByte(c)
		}
	}
	if len(stack) != 1 {
		return "", errors.New("tırnak ya da parantez kapanmamış")
	}
	return mask.String(), nil
}
