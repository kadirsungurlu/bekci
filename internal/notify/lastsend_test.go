package notify

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
	"github.com/kadirsungurlu/bekci/internal/store/storetest"
)

func TestLastSendsPersist(t *testing.T) {
	ctx := context.Background()
	st := storetest.Open(t, time.UTC)
	d := NewDispatcher(st, slog.Default())
	ids := make([]int64, 2)
	for i := range ids {
		n := store.Notification{Name: "k", Type: "webhook", Active: true, Config: json.RawMessage(`{"url":"http://127.0.0.1:1"}`)}
		if err := st.CreateNotification(ctx, &n, false); err != nil {
			t.Fatal(err)
		}
		ids[i] = n.ID
	}
	if got, _ := d.LastSends(ctx); len(got) != 0 {
		t.Fatal("boş başlamalı")
	}
	d.noteSend(ids[0], time.Unix(100, 0), nil)
	d.noteSend(ids[1], time.Unix(200, 0), errors.New("x"))
	d.noteSend(ids[0], time.Unix(300, 0), nil)
	// Yeni dispatcher (yeniden başlatma) aynı sonucu görür.
	got, err := NewDispatcher(st, slog.Default()).LastSends(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got[ids[0]] != (SendStatus{At: 300, OK: true}) || got[ids[1]] != (SendStatus{At: 200, OK: false}) || len(got) != 2 {
		t.Fatalf("son gönderimler: %+v", got)
	}
}
