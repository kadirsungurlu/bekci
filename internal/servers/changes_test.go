package servers

import (
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func kindMap(l []store.TimelineEvent) map[string]string {
	out := map[string]string{}
	for _, e := range l {
		out[e.Kind] = e.Subject
	}
	return out
}

func TestInventoryChanges(t *testing.T) {
	prev := store.ServerInventory{
		CollectedAt: 1,
		Services: []store.InvService{
			{Name: "a.service", Active: "active", Sub: "running", Enabled: "enabled"},
			{Name: "b.service", Active: "failed", Sub: "failed"},
			{Name: "c.service", Active: "active", Sub: "running", Enabled: "enabled"},
			{Name: "d.service", Active: "active", Sub: "running", Enabled: "disabled"},
		},
		Containers: []store.InvContainer{
			{Name: "web", Image: "x:1", State: "running", RestartCount: 1},
			{Name: "db", Image: "y:1", State: "running"},
			{Name: "old", Image: "z", State: "running"},
			{Name: "job", Image: "j", State: "exited"},
		},
		Ports: []store.InvPort{{Proto: "tcp", Port: 80}, {Proto: "tcp", Port: 60000}},
	}
	next := metrics.Inventory{
		Services: []metrics.InvService{
			{Name: "a.service", Active: "failed", Sub: "failed", Enabled: "enabled"},
			{Name: "b.service", Active: "active", Sub: "running"},
			{Name: "c.service", Active: "inactive", Sub: "dead", Enabled: "enabled"},
			{Name: "d.service", Active: "inactive", Sub: "dead", Enabled: "disabled"},
		},
		Containers: []metrics.InvContainer{
			{Name: "web", Image: "x:2", State: "running", RestartCount: 2},
			{Name: "db", Image: "y:1", State: "exited"},
			{Name: "new", Image: "n", State: "running"},
			{Name: "job", Image: "j", State: "running"},
		},
		Ports: []metrics.InvPort{{Proto: "tcp", Port: 443, Process: "caddy"}, {Proto: "tcp", Port: 61000}},
	}
	got := kindMap(inventoryChanges(1, prev, next, 100))
	want := map[string]string{
		EventServiceFailed: "a.service", EventServiceRecovered: "b.service", EventServiceStopped: "c.service",
		EventContainerUpdated: "web", EventContainerRestarted: "web", EventContainerStopped: "db",
		EventContainerAdded: "new", EventContainerRemoved: "old", EventContainerStarted: "job",
		EventPortOpened: "tcp/443", EventPortClosed: "tcp/80",
	}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q", k, got[k])
		}
	}
}

func TestInventoryChangesFirstAndNotes(t *testing.T) {
	next := metrics.Inventory{Containers: []metrics.InvContainer{{Name: "x", State: "running"}}}
	if l := inventoryChanges(1, store.ServerInventory{}, next, 1); len(l) != 0 {
		t.Fatalf("ilk envanter: %+v", l)
	}
	prev := store.ServerInventory{CollectedAt: 1, Containers: []store.InvContainer{{Name: "y", State: "running"}}}
	next.Notes = map[string]string{metrics.InvContainers: "error"}
	if l := inventoryChanges(1, prev, next, 1); len(l) != 0 {
		t.Fatalf("toplanamayan bölüm: %+v", l)
	}
}

func TestHostEvents(t *testing.T) {
	now := time.Unix(100000, 0)
	l := hostEvents(1, []metrics.HostEvent{
		{Time: now.Unix() - 30, Kind: "oom", Subject: "java", PID: 3, Count: 2},
		{Time: 5, Kind: "oom", Subject: "eski"},
		{Time: now.Unix(), Kind: "bilinmeyen"},
	}, now)
	if len(l) != 2 || l[0].Time != now.Unix()-30 || l[1].Time != now.Unix() {
		t.Fatalf("%+v", l)
	}
}
