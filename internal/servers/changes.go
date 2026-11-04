package servers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Zaman çizelgesi olay türleri (store.TimelineEvent.Kind).
const (
	EventReboot             = "reboot"
	EventOOM                = "oom"
	EventServiceFailed      = "service_failed"
	EventServiceRecovered   = "service_recovered"
	EventServiceStopped     = "service_stopped"
	EventContainerStopped   = "container_stopped"
	EventContainerStarted   = "container_started"
	EventContainerRestarted = "container_restarted"
	EventContainerAdded     = "container_added"
	EventContainerRemoved   = "container_removed"
	EventContainerUpdated   = "container_updated"
	EventPortOpened         = "port_opened"
	EventPortClosed         = "port_closed"
	EventAlert              = "alert"
	EventAlertResolved      = "alert_resolved"

	maxInventoryEvents = 50
	// Bunun üstü geçici (ephemeral) istemci portu: çizelgeyi kirletir.
	ephemeralPort = 49152
)

func timelineEvent(id, t int64, kind, subject, level string, data map[string]any) store.TimelineEvent {
	e := store.TimelineEvent{ProbeID: id, Time: t, Kind: kind, Subject: subject, Level: level}
	if len(data) > 0 {
		e.Data, _ = json.Marshal(data)
	}
	return e
}

// timeline olayları yazar; hata izlemeyi durdurmaz.
func (s *Service) timeline(ctx context.Context, list ...store.TimelineEvent) {
	if err := s.store.AddTimelineEvents(ctx, list); err != nil {
		s.log.Error("sunucu olayı yazılamadı", "hata", err)
	}
}

func alertLevel(l string) string {
	if l == store.LevelCritical {
		return store.EventError
	}
	return store.EventWarning
}

func alertData(a *store.ServerAlert, v float64, mount string) map[string]any {
	d := map[string]any{"metric": a.Metric, "value": v}
	if a.Threshold != 0 {
		d["threshold"] = a.Threshold
	}
	if mount != "" {
		d["mount"] = mount
	}
	if a.Level != "" {
		d["level"] = a.Level
	}
	return d
}

func alertSubject(metric, mount string) string {
	if mount != "" {
		return metric + ":" + mount
	}
	return metric
}

// hostEvents ajanın bildirdiği olayları kayda çevirir: tür beyaz listede,
// olmayan zaman (saat kayması, eski kuyruk) panel saatine çekilir.
func hostEvents(id int64, list []metrics.HostEvent, now time.Time) []store.TimelineEvent {
	var out []store.TimelineEvent
	for _, e := range list {
		if e.Kind != metrics.HostEventOOM {
			continue
		}
		t := e.Time
		if t < now.Add(-time.Hour).Unix() || t > now.Add(time.Minute).Unix() {
			t = now.Unix()
		}
		d := map[string]any{"count": max(e.Count, 1)}
		if e.PID > 0 {
			d["pid"] = e.PID
		}
		out = append(out, timelineEvent(id, t, EventOOM, e.Subject, store.EventError, d))
	}
	return out
}

// inventoryChanges iki envanter arasındaki olaylar (saf işlev). prev hiç
// toplanmadıysa olay yoktur: ilk envanter "her şey yeni" demek değildir.
// Notes'ta geçen bölüm bu turda toplanamamıştır, atlanır.
func inventoryChanges(id int64, prev store.ServerInventory, next metrics.Inventory, t int64) []store.TimelineEvent {
	if prev.CollectedAt == 0 {
		return nil
	}
	var out []store.TimelineEvent
	skip := func(section string) bool {
		if _, ok := next.Notes[section]; ok {
			return true
		}
		_, ok := prev.Notes[section]
		return ok
	}
	add := func(kind, subject, level string, data map[string]any) {
		out = append(out, timelineEvent(id, t, kind, subject, level, data))
	}
	if !skip(metrics.InvServices) {
		before := make(map[string]store.InvService, len(prev.Services))
		for _, s := range prev.Services {
			before[s.Name] = s
		}
		for _, n := range next.Services {
			o, ok := before[n.Name]
			if !ok {
				if n.Active == "failed" {
					add(EventServiceFailed, n.Name, store.EventError, map[string]any{"sub": n.Sub})
				}
				continue
			}
			switch {
			case n.Active == "failed" && o.Active != "failed":
				add(EventServiceFailed, n.Name, store.EventError, map[string]any{"sub": n.Sub})
			case o.Active == "failed" && n.Active == "active":
				add(EventServiceRecovered, n.Name, store.EventInfo, nil)
			case o.Sub == "running" && n.Active == "inactive" && n.Enabled == "enabled":
				add(EventServiceStopped, n.Name, store.EventWarning, nil)
			}
		}
	}
	if !skip(metrics.InvContainers) {
		before := make(map[string]store.InvContainer, len(prev.Containers))
		for _, c := range prev.Containers {
			before[c.Name] = c
		}
		seen := make(map[string]bool, len(next.Containers))
		for _, n := range next.Containers {
			seen[n.Name] = true
			o, ok := before[n.Name]
			if !ok {
				add(EventContainerAdded, n.Name, store.EventInfo, map[string]any{"image": n.Image})
				continue
			}
			if o.Image != n.Image {
				add(EventContainerUpdated, n.Name, store.EventInfo, map[string]any{"from": o.Image, "image": n.Image})
			}
			switch {
			case o.State == "running" && n.State != "running":
				add(EventContainerStopped, n.Name, store.EventWarning, map[string]any{"state": n.State})
			case o.State != "running" && n.State == "running":
				add(EventContainerStarted, n.Name, store.EventInfo, nil)
			case n.RestartCount > o.RestartCount:
				add(EventContainerRestarted, n.Name, store.EventWarning, map[string]any{"count": n.RestartCount})
			}
		}
		for _, o := range prev.Containers {
			if !seen[o.Name] {
				add(EventContainerRemoved, o.Name, store.EventInfo, nil)
			}
		}
	}
	if !skip(metrics.InvPorts) {
		key := func(proto string, port int) string { return fmt.Sprintf("%s/%d", proto, port) }
		before, after := map[string]bool{}, map[string]string{}
		for _, p := range prev.Ports {
			if p.Port < ephemeralPort {
				before[key(p.Proto, p.Port)] = true
			}
		}
		for _, p := range next.Ports {
			if p.Port < ephemeralPort {
				after[key(p.Proto, p.Port)] = p.Process
			}
		}
		for _, p := range next.Ports { // envanter sıralı: olay sırası kararlı
			k := key(p.Proto, p.Port)
			if proc, ok := after[k]; ok && !before[k] {
				add(EventPortOpened, k, store.EventInfo, map[string]any{"process": proc})
				delete(after, k)
			}
		}
		for _, p := range prev.Ports {
			k := key(p.Proto, p.Port)
			if _, open := after[k]; before[k] && !open && !portIn(next.Ports, p.Proto, p.Port) {
				add(EventPortClosed, k, store.EventInfo, map[string]any{"process": p.Process})
				delete(before, k)
			}
		}
	}
	if len(out) > maxInventoryEvents {
		out = out[:maxInventoryEvents]
	}
	return out
}

func portIn(list []metrics.InvPort, proto string, port int) bool {
	for _, p := range list {
		if p.Proto == proto && p.Port == port {
			return true
		}
	}
	return false
}
