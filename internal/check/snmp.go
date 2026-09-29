package check

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

// SNMPConfig bir OID'yi SNMP GET ile okur; isteğe bağlı bir koşulla
// karşılaştırır (belirtilmezse yalnızca yanıt alınması yeterlidir).
type SNMPConfig struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Version   string `json:"version"` // v1, v2c, v3
	Community string `json:"community"`
	OID       string `json:"oid"`
	Condition string `json:"condition"` // "", ==, !=, >, <, contains
	Expected  string `json:"expected"`

	// SNMPv3
	Username     string `json:"username"`
	AuthProtocol string `json:"auth_protocol"` // none, md5, sha, sha224, sha256, sha384, sha512
	AuthPassword string `json:"auth_password"`
	PrivProtocol string `json:"priv_protocol"` // none, des, aes, aes192, aes256
	PrivPassword string `json:"priv_password"`
}

var snmpVersions = map[string]bool{"v1": true, "v2c": true, "v3": true}
var snmpConditions = map[string]bool{"": true, "==": true, "!=": true, ">": true, ">=": true, "<": true, "<=": true, "contains": true}

var snmpAuthProtocols = map[string]gosnmp.SnmpV3AuthProtocol{
	"none": gosnmp.NoAuth, "md5": gosnmp.MD5, "sha": gosnmp.SHA,
	"sha224": gosnmp.SHA224, "sha256": gosnmp.SHA256, "sha384": gosnmp.SHA384, "sha512": gosnmp.SHA512,
}
var snmpPrivProtocols = map[string]gosnmp.SnmpV3PrivProtocol{
	"none": gosnmp.NoPriv, "des": gosnmp.DES, "aes": gosnmp.AES,
	"aes192": gosnmp.AES192, "aes256": gosnmp.AES256,
}

type snmpChecker struct{}

func init() { Register("snmp", snmpChecker{}) }

func (snmpChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c SNMPConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Host = strings.TrimSpace(c.Host)
	if c.Host == "" {
		return nil, invalid("Sunucu adresi gerekli")
	}
	if c.Port == 0 {
		c.Port = 161
	}
	if c.Port < 1 || c.Port > 65535 {
		return nil, invalid("Port 1-65535 arasında olmalı")
	}
	c.Version = strings.ToLower(strings.TrimSpace(c.Version))
	if c.Version == "" {
		c.Version = "v2c"
	}
	if !snmpVersions[c.Version] {
		return nil, invalid("Geçersiz SNMP sürümü: %s", c.Version)
	}
	c.OID = strings.TrimSpace(c.OID)
	if !validOID(c.OID) {
		return nil, invalid("Geçerli bir OID girin (örnek: 1.3.6.1.2.1.1.3.0)")
	}

	if c.Version == "v3" {
		c.Community = ""
		c.Username = strings.TrimSpace(c.Username)
		if c.Username == "" {
			return nil, invalid("SNMPv3 için kullanıcı adı gerekli")
		}
		c.AuthProtocol = strings.ToLower(strings.TrimSpace(c.AuthProtocol))
		if c.AuthProtocol == "" {
			c.AuthProtocol = "none"
		}
		if _, ok := snmpAuthProtocols[c.AuthProtocol]; !ok {
			return nil, invalid("Geçersiz kimlik doğrulama protokolü: %s", c.AuthProtocol)
		}
		c.PrivProtocol = strings.ToLower(strings.TrimSpace(c.PrivProtocol))
		if c.PrivProtocol == "" {
			c.PrivProtocol = "none"
		}
		if _, ok := snmpPrivProtocols[c.PrivProtocol]; !ok {
			return nil, invalid("Geçersiz gizlilik protokolü: %s", c.PrivProtocol)
		}
		if c.AuthProtocol == "none" && c.PrivProtocol != "none" {
			return nil, invalid("Gizlilik protokolü için önce kimlik doğrulama protokolü seçilmeli")
		}
		if c.AuthProtocol != "none" && c.AuthPassword == "" {
			return nil, invalid("Kimlik doğrulama parolası gerekli")
		}
		if c.PrivProtocol != "none" && c.PrivPassword == "" {
			return nil, invalid("Gizlilik parolası gerekli")
		}
	} else {
		c.Username, c.AuthProtocol, c.AuthPassword, c.PrivProtocol, c.PrivPassword = "", "", "", "", ""
		c.Community = strings.TrimSpace(c.Community)
		if c.Community == "" {
			c.Community = "public"
		}
	}

	c.Condition = strings.TrimSpace(c.Condition)
	if !snmpConditions[c.Condition] {
		return nil, invalid("Geçersiz karşılaştırma: %s", c.Condition)
	}
	if c.Condition != "" && strings.TrimSpace(c.Expected) == "" {
		return nil, invalid("Karşılaştırma için beklenen değer gerekli")
	}
	if c.Condition == "" {
		c.Expected = ""
	}
	return encode(c), nil
}

func (snmpChecker) Target(raw json.RawMessage) string {
	var c SNMPConfig
	json.Unmarshal(raw, &c)
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) + " " + c.OID
}

func (snmpChecker) Check(ctx context.Context, raw json.RawMessage) (res Result) {
	var c SNMPConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}
	// SNMP UDP üzerindedir: ağ tanısı yalnızca çözümleme ve ping.
	dg := newDiag("snmp", raw, net.JoinHostPort(c.Host, strconv.Itoa(c.Port))).network(c.Host, c.Port, false)
	defer dg.attach(ctx, &res)

	timeout := 10 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		timeout = time.Until(dl)
	}
	if timeout <= 0 {
		dg.failClass(PhaseConnect, ClassTimeout)
		return down("Zaman aşımı")
	}

	g := &gosnmp.GoSNMP{
		Target:  c.Host,
		Port:    uint16(c.Port),
		Timeout: timeout,
		Retries: 1,
		Context: ctx,
	}
	switch c.Version {
	case "v1":
		g.Version = gosnmp.Version1
		g.Community = c.Community
	case "v2c":
		g.Version = gosnmp.Version2c
		g.Community = c.Community
	case "v3":
		g.Version = gosnmp.Version3
		g.SecurityModel = gosnmp.UserSecurityModel
		authProto := snmpAuthProtocols[c.AuthProtocol]
		privProto := snmpPrivProtocols[c.PrivProtocol]
		msgFlags := gosnmp.NoAuthNoPriv
		switch {
		case privProto != gosnmp.NoPriv:
			msgFlags = gosnmp.AuthPriv
		case authProto != gosnmp.NoAuth:
			msgFlags = gosnmp.AuthNoPriv
		}
		g.MsgFlags = msgFlags
		g.SecurityParameters = &gosnmp.UsmSecurityParameters{
			UserName:                 c.Username,
			AuthenticationProtocol:   authProto,
			AuthenticationPassphrase: c.AuthPassword,
			PrivacyProtocol:          privProto,
			PrivacyPassphrase:        c.PrivPassword,
		}
	}

	start := time.Now()
	if err := g.Connect(); err != nil {
		dg.fail(ctx, PhaseConnect, err)
		return down("Bağlanılamadı: " + describeErr(ctx, err))
	}
	defer g.Conn.Close()

	result, err := g.Get([]string{c.OID})
	if err != nil {
		dg.fail(ctx, PhaseQuery, err)
		return down(describeErr(ctx, err))
	}
	ping := msSince(start)
	if len(result.Variables) == 0 {
		dg.failClass(PhaseResponse, ClassProtocol)
		return down("Yanıtta değer yok")
	}
	v := result.Variables[0]
	if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance {
		dg.failClass(PhaseResponse, ClassNotFound)
		return down("OID bulunamadı: " + c.OID)
	}
	val := snmpValueString(v)

	if c.Condition == "" {
		return Result{Up: true, PingMs: ping, Message: "OID değeri: " + truncate(val, 120)}
	}
	ok, msg := compareSNMP(val, c.Condition, c.Expected)
	if !ok {
		dg.failClass(PhaseResponse, ClassMismatch)
		return Result{PingMs: ping, Message: msg}
	}
	return Result{Up: true, PingMs: ping, Message: msg}
}

func snmpValueString(pdu gosnmp.SnmpPDU) string {
	switch val := pdu.Value.(type) {
	case []byte:
		return string(val)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", val)
	}
}

func compareSNMP(got, op, expected string) (bool, string) {
	fail := fmt.Sprintf("OID değeri: %s (beklenen: %s %s)", truncate(got, 80), op, expected)
	gotNum, errA := strconv.ParseFloat(strings.TrimSpace(got), 64)
	expNum, errB := strconv.ParseFloat(strings.TrimSpace(expected), 64)
	numeric := errA == nil && errB == nil

	var ok bool
	switch op {
	case "==":
		ok = got == expected || (numeric && gotNum == expNum)
	case "!=":
		ok = got != expected && !(numeric && gotNum == expNum)
	case "contains":
		ok = strings.Contains(got, expected)
	case ">", ">=", "<", "<=":
		if !numeric {
			return false, fmt.Sprintf("OID değeri sayısal değil: %s", truncate(got, 80))
		}
		switch op {
		case ">":
			ok = gotNum > expNum
		case ">=":
			ok = gotNum >= expNum
		case "<":
			ok = gotNum < expNum
		case "<=":
			ok = gotNum <= expNum
		}
	}
	if !ok {
		return false, fail
	}
	return true, "OID değeri: " + truncate(got, 120)
}

// validOID "1.3.6.1.2.1.1.3.0" gibi noktalarla ayrılmış sayısal bir OID mi
// kontrol eder (başında opsiyonel nokta kabul edilir).
func validOID(s string) bool {
	s = strings.TrimPrefix(s, ".")
	if s == "" {
		return false
	}
	for _, part := range strings.Split(s, ".") {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
