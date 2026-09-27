package check

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTTConfig bir MQTT broker'ına bağlanır; bir konu (topic) verilmişse o
// konuya abone olup zaman aşımı içinde bir mesaj bekler.
type MQTTConfig struct {
	BrokerURL string `json:"broker_url"` // tcp://, ssl://, tls://, ws://, wss://
	Username  string `json:"username"`
	Password  string `json:"password"`
	Topic     string `json:"topic"`
	IgnoreTLS bool   `json:"ignore_tls"`

	Keyword string `json:"keyword"` // konu mesajında aranan metin

	JSONPath     string `json:"json_path"`
	JSONOp       string `json:"json_op"`
	JSONExpected string `json:"json_expected"`
}

var mqttSchemes = map[string]bool{"tcp": true, "ssl": true, "tls": true, "ws": true, "wss": true}

type mqttChecker struct{}

func init() { Register("mqtt", mqttChecker{}) }

func (mqttChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c MQTTConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.BrokerURL = strings.TrimSpace(c.BrokerURL)
	u, err := url.Parse(c.BrokerURL)
	if err != nil || !mqttSchemes[u.Scheme] || u.Host == "" {
		return nil, invalid("Geçerli bir broker adresi girin (tcp://, ssl://, ws:// veya wss://)")
	}
	c.Topic = strings.TrimSpace(c.Topic)
	c.JSONPath = strings.TrimSpace(c.JSONPath)
	if c.JSONPath != "" {
		if c.Topic == "" {
			return nil, invalid("JSON kontrolü için bir konu (topic) belirtin")
		}
		if c.JSONOp == "" {
			c.JSONOp = "=="
		}
		if !jsonOps[c.JSONOp] {
			return nil, invalid("Geçersiz JSON karşılaştırması: %s", c.JSONOp)
		}
	} else {
		c.JSONOp, c.JSONExpected = "", ""
	}
	if c.Keyword != "" && c.Topic == "" {
		return nil, invalid("Kelime kontrolü için bir konu (topic) belirtin")
	}
	return encode(c), nil
}

func (mqttChecker) Target(raw json.RawMessage) string {
	var c MQTTConfig
	json.Unmarshal(raw, &c)
	return c.BrokerURL
}

func (mqttChecker) Check(ctx context.Context, raw json.RawMessage) Result {
	var c MQTTConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}
	timeout := 30 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		timeout = time.Until(dl)
	}
	if timeout <= 0 {
		return down("Zaman aşımı")
	}

	opts := mqtt.NewClientOptions().
		AddBroker(c.BrokerURL).
		SetClientID(fmt.Sprintf("uptime-kadir-%d", time.Now().UnixNano())).
		SetConnectTimeout(timeout).
		SetAutoReconnect(false).
		SetCleanSession(true)
	if c.Username != "" {
		opts.SetUsername(c.Username)
	}
	if c.Password != "" {
		opts.SetPassword(c.Password)
	}
	if strings.HasPrefix(c.BrokerURL, "ssl://") || strings.HasPrefix(c.BrokerURL, "tls://") || strings.HasPrefix(c.BrokerURL, "wss://") {
		opts.SetTLSConfig(&tls.Config{InsecureSkipVerify: c.IgnoreTLS})
	}

	start := time.Now()
	client := mqtt.NewClient(opts)
	connectDone := make(chan error, 1)
	go func() {
		token := client.Connect()
		token.Wait()
		connectDone <- token.Error()
	}()
	select {
	case err := <-connectDone:
		if err != nil {
			return down("Bağlanılamadı: " + err.Error())
		}
	case <-ctx.Done():
		return down("Zaman aşımı")
	}
	defer client.Disconnect(250)
	ping := msSince(start)

	if c.Topic == "" {
		return Result{Up: true, PingMs: ping, Message: "Broker'a bağlanıldı"}
	}

	msgCh := make(chan mqtt.Message, 1)
	subToken := client.Subscribe(c.Topic, 0, func(_ mqtt.Client, m mqtt.Message) {
		select {
		case msgCh <- m:
		default:
		}
	})
	subDone := make(chan error, 1)
	go func() {
		subToken.Wait()
		subDone <- subToken.Error()
	}()
	select {
	case err := <-subDone:
		if err != nil {
			return down("Abonelik hatası: " + err.Error())
		}
	case <-ctx.Done():
		return down("Abonelik zaman aşımına uğradı")
	}

	select {
	case msg := <-msgCh:
		payload := msg.Payload()
		ping = msSince(start)
		if c.Keyword != "" && !containsKeyword(payload, c.Keyword, false) {
			return Result{PingMs: ping, Message: fmt.Sprintf("Kelime bulunamadı: %q", c.Keyword)}
		}
		if c.JSONPath != "" {
			ok, msg2 := evalJSON(payload, c.JSONPath, c.JSONOp, c.JSONExpected)
			if !ok {
				return Result{PingMs: ping, Message: msg2}
			}
		}
		return Result{Up: true, PingMs: ping, Message: "Mesaj alındı: " + truncate(string(payload), 120)}
	case <-ctx.Done():
		return down(fmt.Sprintf("%q konusunda mesaj gelmedi (zaman aşımı)", c.Topic))
	}
}
