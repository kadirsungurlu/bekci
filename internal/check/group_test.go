package check

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

func TestGroupNormalize(t *testing.T) {
	g, _ := Get(TypeGroup)
	for _, bad := range []string{`{}`, `{"monitor_ids":[]}`, `{"monitor_ids":[0]}`, `{"monitor_ids":[1],"mode":"hepsi"}`, `{"monitor_ids":[1],"x":1}`} {
		if _, err := g.Normalize(json.RawMessage(bad)); err == nil {
			t.Errorf("%s reddedilmeliydi", bad)
		}
	}
	out, err := g.Normalize(json.RawMessage(`{"monitor_ids":[3,1,3]}`))
	if err != nil || string(out) != `{"monitor_ids":[3,1],"mode":"any_down"}` {
		t.Fatalf("normalize yanlış: %s %v", out, err)
	}
	if g.Target(out) != "2 monitör" {
		t.Errorf("hedef yanlış: %s", g.Target(out))
	}
}

func child(id int64, status int) ChildStatus {
	return ChildStatus{ID: id, Name: fmt.Sprintf("M%d", id), Active: true, Status: status}
}

func TestAggregateGroup(t *testing.T) {
	const (
		up    = store.StatusUp
		down  = store.StatusDown
		pend  = store.StatusPending
		maint = store.StatusMaintenance
	)
	paused := ChildStatus{ID: 9, Name: "M9", Active: false, Status: down}
	cases := []struct {
		name     string
		mode     string
		children []ChildStatus
		up, pend bool
		msg      string
	}{
		{"hepsi UP", GroupAnyDown, []ChildStatus{child(1, up), child(2, up)}, true, false, "Tüm alt monitörler çalışıyor (2)"},
		{"biri DOWN", GroupAnyDown, []ChildStatus{child(1, down), child(2, up), child(3, down), child(4, pend), child(5, up)}, false, false, "2/5 alt monitör çalışmıyor: M1, M3"},
		{"bekleyen", GroupAnyDown, []ChildStatus{child(1, up), child(2, pend)}, false, true, "1 alt monitör bekliyor: M2"},
		{"durdurulmuş ve bakım yok sayılır", GroupAnyDown, []ChildStatus{child(1, up), paused, child(3, maint)}, true, false, "Tüm alt monitörler çalışıyor (1) (1 bakımda, 1 durdurulmuş)"},
		{"boş", GroupAnyDown, []ChildStatus{paused}, true, false, "Değerlendirilecek alt monitör yok (1 durdurulmuş)"},
		{"all_down: biri DOWN", GroupAllDown, []ChildStatus{child(1, down), child(2, up)}, true, false, "1/2 alt monitör çalışmıyor: M1"},
		{"all_down: hepsi DOWN", GroupAllDown, []ChildStatus{child(1, down), child(2, down), paused}, false, false, "2/2 alt monitör çalışmıyor: M1, M2 (1 durdurulmuş)"},
		{"all_down: DOWN + bekleyen", GroupAllDown, []ChildStatus{child(1, down), child(2, pend)}, false, true, "1 alt monitör bekliyor: M2"},
	}
	for _, c := range cases {
		r := AggregateGroup(c.mode, c.children)
		if r.Up != c.up || r.Pending != c.pend || r.Message != c.msg || r.PingMs != -1 {
			t.Errorf("%s: %+v; up=%v pending=%v %q bekleniyordu", c.name, r, c.up, c.pend, c.msg)
		}
	}

	var many []ChildStatus
	for i := range 12 {
		many = append(many, child(int64(i+1), down))
	}
	if r := AggregateGroup(GroupAnyDown, many); !strings.HasSuffix(r.Message, "M10 ve 2 diğer") {
		t.Errorf("uzun liste kısaltılmalı: %q", r.Message)
	}
}

func TestGroupCheckSource(t *testing.T) {
	g, _ := Get(TypeGroup)
	cfg := json.RawMessage(`{"monitor_ids":[1,2,3],"mode":"any_down"}`)
	if r := g.Check(context.Background(), cfg); r.Up || !r.Pending || !strings.Contains(r.Message, "okunamadı") {
		t.Errorf("kaynak yoksa bekliyor: %+v", r)
	}
	ctx := WithStatusSource(context.Background(), func(_ context.Context, ids []int64) (map[int64]ChildStatus, error) {
		return map[int64]ChildStatus{1: child(1, store.StatusUp), 3: child(3, store.StatusDown)}, nil // 2 silinmiş
	})
	if r := g.Check(ctx, cfg); r.Up || r.Message != "1/2 alt monitör çalışmıyor: M3" {
		t.Errorf("silinmiş alt monitör yok sayılmalı: %+v", r)
	}
	ctx = WithStatusSource(context.Background(), func(context.Context, []int64) (map[int64]ChildStatus, error) {
		return nil, errors.New("db")
	})
	if r := g.Check(ctx, cfg); r.Up || !r.Pending {
		t.Errorf("okuma hatası bekliyor sayılmalı: %+v", r)
	}
}
