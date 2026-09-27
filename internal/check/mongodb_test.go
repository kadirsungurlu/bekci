package check

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMongoDBNormalize(t *testing.T) {
	c, _ := Get("mongodb")
	bad := []string{
		`{}`,
		`{"uri":""}`,
		`{"uri":"http://x"}`,
		`{"uri":"mongodb://x","bilinmeyen":1}`,
	}
	for _, b := range bad {
		if _, err := c.Normalize(json.RawMessage(b)); err == nil {
			t.Errorf("%s: doğrulama hatası bekleniyordu", b)
		} else {
			var ve ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("%s: ValidationError bekleniyordu, %v geldi", b, err)
			}
		}
	}
	norm, err := c.Normalize(json.RawMessage(`{"uri":"mongodb://kadir:gizli@db.local:27017"}`))
	if err != nil {
		t.Fatal(err)
	}
	target := c.Target(norm)
	if strings.Contains(target, "gizli") || strings.Contains(target, "kadir") {
		t.Errorf("kimlik bilgisi hedefe sızdı: %s", target)
	}
	if !strings.Contains(target, "db.local:27017") {
		t.Errorf("hedef host içermiyor: %s", target)
	}

	// mongodb+srv de kabul edilir.
	if _, err := c.Normalize(json.RawMessage(`{"uri":"mongodb+srv://cluster.example/"}`)); err != nil {
		t.Errorf("mongodb+srv kabul edilmeliydi: %v", err)
	}
}

func TestMongoDBConnRefused(t *testing.T) {
	c, _ := Get("mongodb")
	norm, err := c.Normalize(json.RawMessage(`{"uri":"mongodb://127.0.0.1:1"}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r := c.Check(ctx, norm)
	if r.Up {
		t.Errorf("bağlanamama bekleniyordu: %+v", r)
	}
}

// TestMongoDBLive gerçek bir MongoDB sunucusuna karşı çalışır.
// UPTIME_IT_MONGODB tam bir mongodb:// URI'si olarak beklenir.
func TestMongoDBLive(t *testing.T) {
	uri := os.Getenv("UPTIME_IT_MONGODB")
	if uri == "" {
		t.Skip("UPTIME_IT_MONGODB ayarlı değil, canlı test atlanıyor")
	}
	c, _ := Get("mongodb")
	norm, err := c.Normalize(json.RawMessage(`{"uri":"` + uri + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := c.Check(ctx, norm)
	if !r.Up || r.Message != "Ping başarılı" {
		t.Errorf("canlı MongoDB kontrolü başarısız: %+v", r)
	}
}
