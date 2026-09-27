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
	from, err := mail.ParseAddress(c.From)
	if err != nil {
		return err
	}
	to, err := mail.ParseAddressList(c.To)
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
	if _, err := w.Write(buildMail(from.String(), strings.Join(toHeader, ", "), ev)); err != nil {
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
	var b bytes.Buffer
	h := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	h("From", from)
	h("To", to)
	h("Subject", mime.QEncoding.Encode("utf-8", ev.Title()))
	h("Date", ev.Time.Format(time.RFC1123Z))
	h("Message-ID", messageID(from))
	h("MIME-Version", "1.0")
	h("Content-Type", "text/plain; charset=utf-8")
	h("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	qp.Write([]byte(strings.ReplaceAll(ev.Text(), "\n", "\r\n")))
	qp.Close()
	return b.Bytes()
}
