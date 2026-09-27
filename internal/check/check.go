// Package check monitör tiplerini içerir. Her tip Checker arayüzünü uygular ve
// init() içinde Register ile kendini kaydeder; yeni bir tip eklemek için tek
// dosya yazmak yeterlidir.
package check

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"
)

// CertInfo HTTPS hedefinin sertifika bilgisi.
type CertInfo struct {
	NotAfter time.Time
	Issuer   string
	Subject  string
}

// Result tek kontrolün sonucu. PingMs < 0 "ölçüm yok" demektir.
type Result struct {
	Up      bool
	Pending bool // Up=false iken: sonuç henüz belirsiz (ör. grubun alt monitörü bekliyor); DOWN sayılmaz
	PingMs  int64
	Message string
	Cert    *CertInfo
}

func down(msg string) Result { return Result{Up: false, PingMs: -1, Message: msg} }

type Checker interface {
	// Normalize ayarları doğrular, eksikleri varsayılanlarla doldurur ve
	// saklanacak JSON'u döner. Hatalar kullanıcıya gösterilir (Türkçe).
	Normalize(cfg json.RawMessage) (json.RawMessage, error)
	// Check tek bir kontrol yapar; zaman aşımı ctx ile gelir.
	Check(ctx context.Context, cfg json.RawMessage) Result
	// Target listede gösterilecek kısa hedef açıklaması.
	Target(cfg json.RawMessage) string
}

// TypePush pasif monitör tipi: kontrolü uygulama yapmaz, hedef /api/push'a
// sinyal gönderir. Motor bu tipi özel olarak ele alır.
const TypePush = "push"

var registry = map[string]Checker{}

func Register(name string, c Checker) { registry[name] = c }

func Get(name string) (Checker, bool) {
	c, ok := registry[name]
	return c, ok
}

func Types() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ValidationError kullanıcı girdisindeki hatadır (API'de 400 olarak döner).
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

func invalid(format string, args ...any) error { return ValidationError(fmt.Sprintf(format, args...)) }

// decode boş ayarı {} kabul eder ve bilinmeyen alanları reddeder (yazım hatası yakalanır).
func decode(cfg json.RawMessage, v any) error {
	if len(cfg) == 0 || string(cfg) == "null" {
		cfg = json.RawMessage("{}")
	}
	dec := json.NewDecoder(strings.NewReader(string(cfg)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return invalid("Geçersiz ayar: %v", err)
	}
	return nil
}

func encode(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// describeErr ağ hatalarını anlaşılır Türkçe mesajlara çevirir.
func describeErr(ctx context.Context, err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || os.IsTimeout(err) {
		return "Zaman aşımı"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return "Alan adı bulunamadı: " + dnsErr.Name
		}
		return "DNS hatası: " + dnsErr.Err
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "Bağlantı reddedildi"
	}
	if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return "Hedefe ulaşılamıyor"
	}
	if errors.Is(err, syscall.ECONNRESET) {
		return "Bağlantı karşı taraftan kesildi"
	}
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return "SSL sertifika hatası: " + certErr.Err.Error()
	}
	return err.Error()
}

func msSince(t time.Time) int64 {
	ms := time.Since(t).Milliseconds()
	if ms < 1 {
		ms = 1
	}
	return ms
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
