package check

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSNMPNormalize(t *testing.T) {
	c, _ := Get("snmp")
	bad := []string{
		`{}`,
		`{"host":"x"}`,             // oid yok
		`{"host":"x","oid":"abc"}`, // geçersiz oid
		`{"host":"x","oid":"1.3.6.1","version":"v4"}`,   // geçersiz sürüm
		`{"host":"x","oid":"1.3.6.1","version":"v3"}`,   // v3 kullanıcı adı yok
		`{"host":"x","oid":"1.3.6.1","condition":"=="}`, // condition var, expected yok
		`{"host":"x","oid":"1.3.6.1","condition":"???","expected":"1"}`,
		`{"host":"x","oid":"1.3.6.1","bilinmeyen":1}`,
	}
	for _, b := range bad {
		_, err := c.Normalize(json.RawMessage(b))
		var ve ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: doğrulama hatası bekleniyordu, %v geldi", b, err)
		}
	}

	norm, err := c.Normalize(json.RawMessage(`{"host":" 127.0.0.1 ","oid":"1.3.6.1.2.1.1.3.0"}`))
	if err != nil {
		t.Fatal(err)
	}
	var cfg SNMPConfig
	json.Unmarshal(norm, &cfg)
	if cfg.Host != "127.0.0.1" || cfg.Port != 161 || cfg.Version != "v2c" || cfg.Community != "public" {
		t.Errorf("varsayılanlar yanlış: %+v", cfg)
	}

	norm, err = c.Normalize(json.RawMessage(`{"host":"x","oid":"1.3.6.1","version":"v3","username":"kadir","auth_protocol":"sha","auth_password":"p1","priv_protocol":"aes","priv_password":"p2"}`))
	if err != nil {
		t.Fatal(err)
	}
	var v3 SNMPConfig
	json.Unmarshal(norm, &v3)
	if v3.Username != "kadir" || v3.Community != "" {
		t.Errorf("v3 normalize yanlış: %+v", v3)
	}
}

// TestSNMPTimeout yanıt vermeyen bir UDP hedefine karşı zaman aşımını doğrular
// (gerçek bir SNMP ajanı gerektirmez).
func TestSNMPTimeout(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	// Paketleri okuyup hiç yanıt vermeyen bir "kara delik" dinleyici.
	go func() {
		buf := make([]byte, 2048)
		for {
			if _, _, err := pc.ReadFrom(buf); err != nil {
				return
			}
		}
	}()
	host, portStr, _ := net.SplitHostPort(pc.LocalAddr().String())
	port, _ := strconv.Atoi(portStr)

	c, _ := Get("snmp")
	cfg, err := c.Normalize(json.RawMessage(`{"host":"` + host + `","oid":"1.3.6.1.2.1.1.3.0"}`))
	if err != nil {
		t.Fatal(err)
	}
	var sc SNMPConfig
	json.Unmarshal(cfg, &sc)
	sc.Port = port
	cfg2, _ := json.Marshal(sc)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	res := c.Check(ctx, json.RawMessage(cfg2))
	if res.Up || !strings.Contains(res.Message, "Zaman aşımı") {
		t.Errorf("zaman aşımı bekleniyordu: %+v", res)
	}
}

// TestSNMPLive ortam değişkeni verilmişse gerçek bir SNMP ajanına karşı
// çalışır (CI'da atlanır).
func TestSNMPLive(t *testing.T) {
	target := os.Getenv("UPTIME_IT_SNMP_TARGET") // "host:port:community:oid"
	if target == "" {
		t.Skip("UPTIME_IT_SNMP_TARGET ayarlı değil; gerçek SNMP ajanı testi atlanıyor")
	}
	parts := strings.SplitN(target, ":", 4)
	if len(parts) != 4 {
		t.Fatalf("UPTIME_IT_SNMP_TARGET biçimi host:port:community:oid olmalı")
	}
	port, _ := strconv.Atoi(parts[1])
	r := run(t, "snmp", map[string]any{"host": parts[0], "port": port, "community": parts[2], "oid": parts[3]})
	if !r.Up {
		t.Errorf("canlı snmp: %+v", r)
	}
}
