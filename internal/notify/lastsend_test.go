package notify

import (
	"errors"
	"testing"
	"time"
)

func TestLastSends(t *testing.T) {
	var d Dispatcher
	if len(d.LastSends()) != 0 {
		t.Fatal("boş başlamalı")
	}
	d.noteSend(1, time.Unix(100, 0), nil)
	d.noteSend(2, time.Unix(200, 0), errors.New("x"))
	d.noteSend(1, time.Unix(300, 0), errors.New("y"))
	got := d.LastSends()
	if got[1] != (SendStatus{At: 300, OK: false}) || got[2] != (SendStatus{At: 200, OK: false}) || len(got) != 2 {
		t.Fatalf("son gönderimler: %+v", got)
	}
}
