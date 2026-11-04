package store

import (
	"context"
	"encoding/json"
	"testing"
)

func TestTimelineEvents(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p := &Probe{Name: "ev", Kind: ProbeKindServer}
	err := s.CreateProbe(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	ev := []TimelineEvent{
		{ProbeID: p.ID, Time: 100, Kind: "oom", Subject: "java", Level: EventError, Data: json.RawMessage(`{"count":2}`)},
		{ProbeID: p.ID, Time: 100, Kind: "oom", Subject: "java", Level: EventError}, // ajanın tekrarı
		{ProbeID: p.ID, Time: 200, Kind: "reboot", Level: EventWarning},
		{ProbeID: p.ID, Time: 300, Kind: "port_opened", Subject: "tcp/443", Level: EventInfo},
	}
	if err := s.AddTimelineEvents(ctx, ev); err != nil {
		t.Fatal(err)
	}
	all, err := s.TimelineEvents(ctx, p.ID, 0, nil, 10)
	if err != nil || len(all) != 3 || all[0].Time != 300 || string(all[2].Data) != `{"count":2}` {
		t.Fatalf("%+v %v", all, err)
	}
	page, _ := s.TimelineEvents(ctx, p.ID, 300, nil, 1)
	if len(page) != 1 || page[0].Kind != "reboot" {
		t.Fatalf("%+v", page)
	}
	only, _ := s.TimelineEvents(ctx, p.ID, 0, []string{"oom", "reboot"}, 10)
	if len(only) != 2 {
		t.Fatalf("%+v", only)
	}
	if err := s.PruneTimelineEvents(ctx, 150+serverEventsKeepSec); err != nil {
		t.Fatal(err)
	}
	if left, _ := s.TimelineEvents(ctx, p.ID, 0, nil, 10); !s.tsFull() && len(left) != 2 {
		t.Fatalf("%+v", left)
	}
}
