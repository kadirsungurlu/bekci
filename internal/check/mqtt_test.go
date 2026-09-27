package check

import (
	"encoding/json"
	"errors"
	"net"
	"testing"

	mqttserver "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

// startTestBroker gömülü bir MQTT broker'ı geçici bir TCP portunda başlatır.
func startTestBroker(t *testing.T) (brokerURL string, srv *mqttserver.Server) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	srv = mqttserver.New(&mqttserver.Options{InlineClient: true})
	if err := srv.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatal(err)
	}
	tcp := listeners.NewTCP(listeners.Config{ID: "t1", Address: addr})
	if err := srv.AddListener(tcp); err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Close() })
	return "tcp://" + addr, srv
}

func TestMQTT(t *testing.T) {
	brokerURL, srv := startTestBroker(t)

	if r := run(t, "mqtt", map[string]any{"broker_url": brokerURL}); !r.Up {
		t.Errorf("bağlantı: %+v", r)
	}

	// Kalıcı (retained) mesaj: abone olunduğu anda hemen teslim edilir.
	if err := srv.Publish("durum", []byte(`{"ok":true}`), true, 0); err != nil {
		t.Fatal(err)
	}
	if r := run(t, "mqtt", map[string]any{"broker_url": brokerURL, "topic": "durum", "keyword": "ok"}); !r.Up {
		t.Errorf("kelime bulunmalı: %+v", r)
	}
	if r := run(t, "mqtt", map[string]any{"broker_url": brokerURL, "topic": "durum", "keyword": "yok-boyle-bir-sey"}); r.Up {
		t.Errorf("kelime bulunmamalı: %+v", r)
	}
	if r := run(t, "mqtt", map[string]any{"broker_url": brokerURL, "topic": "durum", "json_path": "ok", "json_expected": "true"}); !r.Up {
		t.Errorf("json kontrolü: %+v", r)
	}
	if r := run(t, "mqtt", map[string]any{"broker_url": brokerURL, "topic": "bos-konu"}); r.Up {
		t.Errorf("mesaj gelmemesi bekleniyordu: %+v", r)
	}

	if r := run(t, "mqtt", map[string]any{"broker_url": "tcp://127.0.0.1:1"}); r.Up {
		t.Errorf("bağlanamama bekleniyordu: %+v", r)
	}
}

func TestMQTTNormalize(t *testing.T) {
	c, _ := Get("mqtt")
	bad := []string{
		`{}`,
		`{"broker_url":"http://x"}`,
		`{"broker_url":"tcp://x","json_path":"a"}`, // topic yok
		`{"broker_url":"tcp://x","keyword":"a"}`,   // topic yok
		`{"broker_url":"tcp://x","topic":"t","json_path":"a","json_op":"???"}`,
		`{"broker_url":"tcp://x","bilinmeyen":1}`,
	}
	for _, b := range bad {
		_, err := c.Normalize(json.RawMessage(b))
		var ve ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: doğrulama hatası bekleniyordu, %v geldi", b, err)
		}
	}
	norm, err := c.Normalize(json.RawMessage(`{"broker_url":" tcp://kadir.app:1883 "}`))
	if err != nil {
		t.Fatal(err)
	}
	var cfg MQTTConfig
	json.Unmarshal(norm, &cfg)
	if cfg.BrokerURL != "tcp://kadir.app:1883" {
		t.Errorf("normalize yanlış: %+v", cfg)
	}
}
