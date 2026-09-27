package check

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

func TestWebSocket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		if r.URL.Query().Get("echo") == "1" {
			_, data, err := c.Read(ctx)
			if err == nil {
				c.Write(ctx, websocket.MessageText, data)
			}
		} else {
			c.Write(ctx, websocket.MessageText, []byte("merhaba dünya"))
		}
		c.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	if r := run(t, "websocket", map[string]any{"url": wsURL}); !r.Up {
		t.Errorf("bağlantı: %+v", r)
	}
	if r := run(t, "websocket", map[string]any{"url": wsURL, "keyword": "merhaba"}); !r.Up {
		t.Errorf("kelime bulunmalı: %+v", r)
	}
	if r := run(t, "websocket", map[string]any{"url": wsURL, "keyword": "hic-boyle-bir-sey"}); r.Up {
		t.Errorf("kelime bulunmamalı: %+v", r)
	}
	if r := run(t, "websocket", map[string]any{"url": wsURL + "?echo=1", "send": "test-mesaj", "keyword": "test-mesaj"}); !r.Up {
		t.Errorf("gönder/al: %+v", r)
	}

	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		c.Close(websocket.StatusNormalClosure, "")
	}))
	defer tlsSrv.Close()
	wssURL := "wss" + strings.TrimPrefix(tlsSrv.URL, "https")

	if r := run(t, "websocket", map[string]any{"url": wssURL, "ignore_tls": true}); !r.Up || r.Cert == nil {
		t.Errorf("wss sertifika bekleniyordu: %+v", r)
	}
	if r := run(t, "websocket", map[string]any{"url": wssURL}); r.Up {
		t.Errorf("sertifika doğrulama hatası bekleniyordu: %+v", r)
	}

	if r := run(t, "websocket", map[string]any{"url": "ws://127.0.0.1:1"}); r.Up {
		t.Errorf("bağlanamama bekleniyordu: %+v", r)
	}
}

func TestWebSocketNormalize(t *testing.T) {
	c, _ := Get("websocket")
	bad := []string{`{}`, `{"url":"http://x"}`, `{"url":"ws://x","headers":"bozuk"}`, `{"url":"ws://x","bilinmeyen":1}`}
	for _, b := range bad {
		_, err := c.Normalize(json.RawMessage(b))
		var ve ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: doğrulama hatası bekleniyordu, %v geldi", b, err)
		}
	}
}
