package check

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// TypeGroup alt monitörlerin durumunu birleştiren monitör tipi. Kendisi ağa
// çıkmaz; motorun context ile verdiği StatusSource'tan alt monitörlerin
// güncel durumunu okur.
const TypeGroup = "group"

// Grup modları.
const (
	GroupAnyDown = "any_down" // herhangi bir alt monitör DOWN ise grup DOWN (varsayılan)
	GroupAllDown = "all_down" // yalnızca tüm alt monitörler DOWN ise grup DOWN
)

const maxGroupChildren = 1000

type GroupConfig struct {
	MonitorIDs []int64 `json:"monitor_ids"`
	Mode       string  `json:"mode"`
}

// ChildStatus bir alt monitörün grup için gereken bilgisi.
type ChildStatus struct {
	ID     int64
	Name   string
	Active bool
	Status int
}

// StatusSource verilen monitörlerin güncel durumunu döner (olmayanlar atlanır).
type StatusSource func(ctx context.Context, ids []int64) (map[int64]ChildStatus, error)

type statusSourceKey struct{}

// WithStatusSource grup kontrolünün alt monitörleri okuyacağı kaynağı ctx'e ekler.
func WithStatusSource(ctx context.Context, src StatusSource) context.Context {
	return context.WithValue(ctx, statusSourceKey{}, src)
}

type groupChecker struct{}

func init() { Register(TypeGroup, groupChecker{}) }

// GroupConfigOf kaydedilmiş (normalize edilmiş) grup ayarını çözer.
func GroupConfigOf(raw json.RawMessage) GroupConfig {
	var c GroupConfig
	json.Unmarshal(raw, &c)
	if c.Mode == "" {
		c.Mode = GroupAnyDown
	}
	return c
}

func (groupChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c GroupConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	switch c.Mode {
	case "":
		c.Mode = GroupAnyDown
	case GroupAnyDown, GroupAllDown:
	default:
		return nil, invalid("Grup modu any_down veya all_down olmalı")
	}
	ids := make([]int64, 0, len(c.MonitorIDs))
	seen := map[int64]bool{}
	for _, id := range c.MonitorIDs {
		if id <= 0 {
			return nil, invalid("Geçersiz alt monitör")
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, invalid("En az bir alt monitör seçin")
	}
	if len(ids) > maxGroupChildren {
		return nil, invalid("Bir grupta en fazla %d alt monitör olabilir", maxGroupChildren)
	}
	c.MonitorIDs = ids
	return encode(c), nil
}

func (groupChecker) Target(raw json.RawMessage) string {
	return fmt.Sprintf("%d monitör", len(GroupConfigOf(raw).MonitorIDs))
}

func (groupChecker) Check(ctx context.Context, raw json.RawMessage) Result {
	// Okuma hatası grubun DOWN olduğu anlamına gelmez: sahte alarm olmasın diye
	// sonuç "bekliyor" sayılır (onaylı durum değişmez).
	src, _ := ctx.Value(statusSourceKey{}).(StatusSource)
	if src == nil {
		return Result{Pending: true, PingMs: -1, Message: "Grup durumu okunamadı"}
	}
	cfg := GroupConfigOf(raw)
	statuses, err := src(ctx, cfg.MonitorIDs)
	if err != nil {
		return Result{Pending: true, PingMs: -1, Message: "Alt monitörlerin durumu okunamadı"}
	}
	children := make([]ChildStatus, 0, len(cfg.MonitorIDs))
	for _, id := range cfg.MonitorIDs {
		if c, ok := statuses[id]; ok { // silinmiş alt monitörler yok sayılır
			children = append(children, c)
		}
	}
	return AggregateGroup(cfg.Mode, children)
}

// AggregateGroup alt monitör durumlarından grup sonucunu hesaplar.
// Durdurulmuş ve bakımdaki alt monitörler hesaba katılmaz.
func AggregateGroup(mode string, children []ChildStatus) Result {
	var downNames, pendingNames []string
	var up, paused, maint int
	for _, c := range children {
		switch {
		case !c.Active:
			paused++
		case c.Status == store.StatusMaintenance:
			maint++
		case c.Status == store.StatusDown:
			downNames = append(downNames, c.Name)
		case c.Status == store.StatusUp:
			up++
		default:
			pendingNames = append(pendingNames, c.Name)
		}
	}
	counted := len(downNames) + len(pendingNames) + up
	var extra []string
	if maint > 0 {
		extra = append(extra, fmt.Sprintf("%d bakımda", maint))
	}
	if paused > 0 {
		extra = append(extra, fmt.Sprintf("%d durdurulmuş", paused))
	}
	suffix := ""
	if len(extra) > 0 {
		suffix = " (" + strings.Join(extra, ", ") + ")"
	}

	isDown := len(downNames) > 0
	isPending := len(pendingNames) > 0
	if mode == GroupAllDown {
		isDown = counted > 0 && len(downNames) == counted
		isPending = !isDown && up == 0 && len(pendingNames) > 0
	}
	switch {
	case isDown:
		return Result{PingMs: -1, Message: fmt.Sprintf("%d/%d alt monitör çalışmıyor: %s%s",
			len(downNames), counted, nameList(downNames), suffix)}
	case isPending:
		return Result{Pending: true, PingMs: -1, Message: fmt.Sprintf("%d alt monitör bekliyor: %s%s",
			len(pendingNames), nameList(pendingNames), suffix)}
	case counted == 0:
		return Result{Up: true, PingMs: -1, Message: "Değerlendirilecek alt monitör yok" + suffix}
	case len(downNames) > 0: // all_down: bazıları DOWN ama hepsi değil
		return Result{Up: true, PingMs: -1, Message: fmt.Sprintf("%d/%d alt monitör çalışmıyor: %s%s",
			len(downNames), counted, nameList(downNames), suffix)}
	}
	return Result{Up: true, PingMs: -1, Message: fmt.Sprintf("Tüm alt monitörler çalışıyor (%d)%s", up, suffix)}
}

// nameList en fazla 10 adı virgülle birleştirir.
func nameList(names []string) string {
	const max = 10
	if len(names) <= max {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:max], ", ") + fmt.Sprintf(" ve %d diğer", len(names)-max)
}
