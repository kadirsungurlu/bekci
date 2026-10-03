package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

func init() { Register("email", email{}) }

type emailConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"` // starttls | tls | none
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	To       string `json:"to"` // virgülle ayrılmış
}

type email struct{}

func (email) Secrets() []string { return []string{"password"} }

func (email) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c emailConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Host = strings.TrimSpace(c.Host)
	if err := required("SMTP sunucusu", c.Host); err != nil {
		return nil, err
	}
	if c.Security == "" {
		c.Security = "starttls"
	}
	switch c.Security {
	case "starttls", "tls", "none":
	default:
		return nil, invalid("Güvenlik starttls, tls veya none olmalı")
	}
	if c.Port == 0 {
		c.Port = map[string]int{"starttls": 587, "tls": 465, "none": 25}[c.Security]
	}
	if c.Port < 1 || c.Port > 65535 {
		return nil, invalid("Port 1-65535 arasında olmalı")
	}
	if c.Security == "none" && c.Username != "" {
		return nil, invalid("Şifresiz bağlantıda (none) kullanıcı adı/şifre gönderilemez; STARTTLS veya TLS seçin")
	}
	if _, err := mail.ParseAddress(c.From); err != nil {
		return nil, invalid("Gönderen adresi geçersiz")
	}
	if _, err := mail.ParseAddressList(c.To); err != nil || strings.TrimSpace(c.To) == "" {
		return nil, invalid("Alıcı adres(ler)i geçersiz; birden fazlaysa virgülle ayırın")
	}
	return encode(c), nil
}

func (email) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c emailConfig
	json.Unmarshal(raw, &c)
	return sendMail(ctx, c, c.To, func(from, to string) []byte { return buildMail(from, to, ev) })
}

// SendMail bir e-posta kanalının SMTP ayarıyla (cfg) tek alıcıya serbest
// içerikli ileti gönderir (sistem e-postaları: şifre sıfırlama). html boşsa
// yalnızca düz metin gider.
func SendMail(ctx context.Context, cfg json.RawMessage, to, subject, text, html string) error {
	var c emailConfig
	if err := json.Unmarshal(cfg, &c); err != nil {
		return err
	}
	if _, err := mail.ParseAddress(to); err != nil {
		return fmt.Errorf("alıcı adresi geçersiz: %w", err)
	}
	return sendMail(ctx, c, to, func(from, toHdr string) []byte { return buildRawMail(from, toHdr, subject, text, html) })
}

// sendMail SMTP bağlantısını kurar ve body'nin ürettiği iletiyi toList
// (virgülle ayrılmış) alıcılarına gönderir.
func sendMail(ctx context.Context, c emailConfig, toList string, body func(from, to string) []byte) error {
	from, err := mail.ParseAddress(c.From)
	if err != nil {
		return err
	}
	to, err := mail.ParseAddressList(toList)
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	conn, err := (&net.Dialer{Deadline: deadline}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	conn.SetDeadline(deadline)
	tlsCfg := &tls.Config{ServerName: c.Host}
	if c.Security == "tls" {
		conn = tls.Client(conn, tlsCfg)
	}
	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer client.Close()

	if c.Security == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("sunucu STARTTLS desteklemiyor")
		}
		if err := client.StartTLS(tlsCfg); err != nil {
			return err
		}
	}
	if c.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return fmt.Errorf("kimlik doğrulama: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return err
	}
	var toHeader []string
	for _, a := range to {
		if err := client.Rcpt(a.Address); err != nil {
			return fmt.Errorf("alıcı %s: %w", a.Address, err)
		}
		toHeader = append(toHeader, a.String())
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(body(from.String(), strings.Join(toHeader, ", "))); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// messageID spam filtrelerinin beklediği benzersiz Message-ID başlığı.
func messageID(from string) string {
	domain := "uptime.local"
	if a, err := mail.ParseAddress(from); err == nil {
		if _, d, ok := strings.Cut(a.Address, "@"); ok {
			domain = d
		}
	}
	b := make([]byte, 12)
	rand.Read(b)
	return fmt.Sprintf("<%x.%d@%s>", b, time.Now().UnixNano(), domain)
}

func buildMail(from, to string, ev Event) []byte {
	return buildRawMail(from, to, ev.Title(), ev.Text(), ev.HTML())
}

// buildRawMail düz metin + (varsa) HTML gövdeli MIME iletisi.
func buildRawMail(from, to, subject, text, html string) []byte {
	var b bytes.Buffer
	h := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	h("From", from)
	h("To", to)
	h("Subject", mime.QEncoding.Encode("utf-8", subject))
	h("Date", time.Now().Format(time.RFC1123Z)) // gönderim anı; olay zamanı gövdede
	h("Message-ID", messageID(from))
	h("MIME-Version", "1.0")
	// Düz metin + HTML: HTML göstermeyen istemci düz metni okur.
	sep := make([]byte, 12)
	rand.Read(sep)
	boundary := fmt.Sprintf("bekci-%x", sep)
	h("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")
	part := func(ctype, body string) {
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary, ctype)
		qp := quotedprintable.NewWriter(&b)
		qp.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n")))
		qp.Close()
		b.WriteString("\r\n")
	}
	part("text/plain", text)
	if html != "" {
		part("text/html", html)
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes()
}
