package check

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"
)

type DNSConfig struct {
	Host       string `json:"host"`        // sorgulanacak ad
	Server     string `json:"server"`      // DNS sunucusu
	Port       int    `json:"port"`        // DNS sunucu portu
	RecordType string `json:"record_type"` // A, AAAA, CNAME, MX, NS, TXT, SOA, SRV, CAA, PTR
	Expected   string `json:"expected"`    // boş değilse cevaplardan biri bunu içermeli
}

var dnsTypes = map[string]uint16{
	"A": dns.TypeA, "AAAA": dns.TypeAAAA, "CNAME": dns.TypeCNAME, "MX": dns.TypeMX,
	"NS": dns.TypeNS, "TXT": dns.TypeTXT, "SOA": dns.TypeSOA, "SRV": dns.TypeSRV,
	"CAA": dns.TypeCAA, "PTR": dns.TypePTR,
}

type dnsChecker struct{}

func init() { Register("dns", dnsChecker{}) }

func (dnsChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c DNSConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Host = strings.TrimSuffix(strings.TrimSpace(c.Host), ".")
	if c.Host == "" {
		return nil, invalid("Sorgulanacak alan adı gerekli")
	}
	c.Server = strings.TrimSpace(c.Server)
	if c.Server == "" {
		c.Server = "1.1.1.1"
	}
	if c.Port == 0 {
		c.Port = 53
	}
	if c.Port < 1 || c.Port > 65535 {
		return nil, invalid("Port 1-65535 arasında olmalı")
	}
	c.RecordType = strings.ToUpper(strings.TrimSpace(c.RecordType))
	if c.RecordType == "" {
		c.RecordType = "A"
	}
	if _, ok := dnsTypes[c.RecordType]; !ok {
		return nil, invalid("Desteklenmeyen kayıt tipi: %s", c.RecordType)
	}
	c.Expected = strings.TrimSpace(c.Expected)
	return encode(c), nil
}

func (dnsChecker) Target(raw json.RawMessage) string {
	var c DNSConfig
	json.Unmarshal(raw, &c)
	return fmt.Sprintf("%s (%s @ %s)", c.Host, c.RecordType, c.Server)
}

func (dnsChecker) Check(ctx context.Context, raw json.RawMessage) (res Result) {
	var c DNSConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}
	qtype := dnsTypes[c.RecordType]
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(c.Host), qtype)
	msg.RecursionDesired = true
	addr := net.JoinHostPort(c.Server, strconv.Itoa(c.Port))
	// Tanı: sorgu ve yanıt; sunucu yanıt vermezse sunucuya tek paketlik ping.
	dg := newDiag("dns", raw, addr).network(c.Server, c.Port, false)
	dd := &DiagDNS{Server: addr, Type: c.RecordType, Query: c.Host, Transport: "udp"}
	dg.dns = dd
	defer dg.attach(ctx, &res)

	// Kütüphanenin varsayılan 2 saniyelik süresi yerine monitörün zaman aşımı.
	timeout := 30 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		timeout = time.Until(dl)
	}
	start := time.Now()
	in, rtt, err := (&dns.Client{Net: "udp", Timeout: timeout}).ExchangeContext(ctx, msg, addr)
	if err == nil && in.Truncated {
		dd.Transport = "tcp"
		in, rtt, err = (&dns.Client{Net: "tcp", Timeout: timeout}).ExchangeContext(ctx, msg, addr)
	}
	dd.ElapsedMs = time.Since(start).Milliseconds()
	if err != nil {
		dg.fail(ctx, PhaseQuery, err)
		if dg.class == ClassTimeout {
			dd.Rcode = "timeout"
		}
		return down(describeErr(ctx, err))
	}
	dd.Rcode = dns.RcodeToString[in.Rcode]
	for _, rr := range in.Answer {
		if len(dd.Answers) < diagMaxAnswers {
			dd.Answers = append(dd.Answers, dns.TypeToString[rr.Header().Rrtype]+" "+rrValue(rr))
		}
	}
	if in.Rcode != dns.RcodeSuccess {
		dg.failClass(PhaseResponse, ClassRcode)
		return down("DNS yanıtı: " + dns.RcodeToString[in.Rcode])
	}

	var values []string
	for _, rr := range in.Answer {
		if rr.Header().Rrtype != qtype {
			continue // ör. A sorgusunda gelen CNAME zinciri
		}
		values = append(values, rrValue(rr))
	}
	if len(values) == 0 {
		dg.failClass(PhaseResponse, ClassNoRecord)
		return down(fmt.Sprintf("%s kaydı bulunamadı", c.RecordType))
	}
	joined := truncate(strings.Join(values, ", "), 200)
	if c.Expected != "" {
		found := false
		for _, v := range values {
			if strings.Contains(strings.ToLower(v), strings.ToLower(c.Expected)) {
				found = true
				break
			}
		}
		if !found {
			dg.failClass(PhaseResponse, ClassMismatch)
			return down(fmt.Sprintf("Beklenen değer yok (%s); gelen: %s", c.Expected, joined))
		}
	}
	ms := rtt.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	return Result{Up: true, PingMs: ms, Message: joined}
}

func rrValue(rr dns.RR) string {
	switch r := rr.(type) {
	case *dns.A:
		return r.A.String()
	case *dns.AAAA:
		return r.AAAA.String()
	case *dns.CNAME:
		return strings.TrimSuffix(r.Target, ".")
	case *dns.MX:
		return fmt.Sprintf("%d %s", r.Preference, strings.TrimSuffix(r.Mx, "."))
	case *dns.NS:
		return strings.TrimSuffix(r.Ns, ".")
	case *dns.TXT:
		return strings.Join(r.Txt, "")
	case *dns.PTR:
		return strings.TrimSuffix(r.Ptr, ".")
	case *dns.SRV:
		return fmt.Sprintf("%d %d %d %s", r.Priority, r.Weight, r.Port, strings.TrimSuffix(r.Target, "."))
	case *dns.CAA:
		return fmt.Sprintf("%d %s %q", r.Flag, r.Tag, r.Value)
	case *dns.SOA:
		return fmt.Sprintf("%s %s %d", strings.TrimSuffix(r.Ns, "."), strings.TrimSuffix(r.Mbox, "."), r.Serial)
	}
	// Başlık kısmını at, sadece veri kalsın.
	parts := strings.SplitN(rr.String(), "\t", 5)
	return parts[len(parts)-1]
}

// DNSConfigOf kayıtlı ayarı çözer (alan adı bitiş sorgusu için).
func DNSConfigOf(raw json.RawMessage) DNSConfig {
	var c DNSConfig
	json.Unmarshal(raw, &c)
	return c
}
