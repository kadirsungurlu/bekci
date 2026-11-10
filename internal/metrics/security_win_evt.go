//go:build windows

package metrics

import (
	"context"
	"errors"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows Security günlüğünden başarısız oturum açma (4625) ve kilitlenen hesap
// (4740) okuma (iş 734, docs/GUVENLIK-EKLENTI-730.md §A.2). wevtapi.dll doğrudan
// çağrılır (PowerShell yok); imleç EventRecordID, durum dosyasında saklanır.
// Okumak için SeSecurityPrivilege gerekir (ajan LocalSystem hizmeti).

const winEventLog = true

var (
	modWevtapi    = windows.NewLazySystemDLL("wevtapi.dll")
	procEvtQuery  = modWevtapi.NewProc("EvtQuery")
	procEvtNext   = modWevtapi.NewProc("EvtNext")
	procEvtRender = modWevtapi.NewProc("EvtRender")
	procEvtClose  = modWevtapi.NewProc("EvtClose")
)

const (
	evtQueryChannelPath      = 0x1
	evtQueryForwardDirection = 0x100
	evtQueryReverseDirection = 0x200
	evtRenderEventXML        = 1
	evtBatch                 = 64
	evtNextTimeoutMs         = 5000
)

type evtHandle uintptr

func evtClose(h evtHandle) {
	if h != 0 {
		procEvtClose.Call(uintptr(h))
	}
}

func evtQuery(query string, flags uint32) (evtHandle, error) {
	if err := procEvtQuery.Find(); err != nil {
		return 0, err
	}
	path, _ := windows.UTF16PtrFromString("Security")
	q, err := windows.UTF16PtrFromString(query)
	if err != nil {
		return 0, err
	}
	r, _, e := procEvtQuery.Call(0, uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(q)), uintptr(evtQueryChannelPath|flags))
	if r == 0 {
		return 0, e
	}
	return evtHandle(r), nil
}

// evtNext sonraki olay tutamaçlarını döndürür; bitince (nil, nil).
func evtNext(rs evtHandle) ([]evtHandle, error) {
	var buf [evtBatch]evtHandle
	var n uint32
	r, _, e := procEvtNext.Call(uintptr(rs), evtBatch, uintptr(unsafe.Pointer(&buf[0])), evtNextTimeoutMs, 0, uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		if errors.Is(e, windows.ERROR_NO_MORE_ITEMS) {
			return nil, nil
		}
		return nil, e
	}
	return buf[:n:n], nil
}

// evtRenderXML olayı XML olarak verir (tampon gerekirse büyütülür).
func evtRenderXML(h evtHandle, buf []uint16) (string, []uint16, error) {
	for range 3 {
		var used, props uint32
		r, _, e := procEvtRender.Call(0, uintptr(h), evtRenderEventXML, uintptr(len(buf)*2),
			uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&props)))
		if r != 0 {
			return windows.UTF16ToString(buf[:used/2]), buf, nil
		}
		if !errors.Is(e, windows.ERROR_INSUFFICIENT_BUFFER) || used/2 <= uint32(len(buf)) {
			return "", buf, e
		}
		buf = make([]uint16, used/2+64)
	}
	return "", buf, windows.ERROR_INSUFFICIENT_BUFFER
}

// evtLatestRecord günlükteki en yeni EventRecordID (0: boş).
func evtLatestRecord() (uint64, error) {
	rs, err := evtQuery("*", evtQueryReverseDirection)
	if err != nil {
		return 0, err
	}
	defer evtClose(rs)
	hs, err := evtNext(rs)
	if err != nil || len(hs) == 0 {
		return 0, err
	}
	defer func() {
		for _, h := range hs {
			evtClose(h)
		}
	}()
	s, _, err := evtRenderXML(hs[0], make([]uint16, 4096))
	if err != nil {
		return 0, err
	}
	ev, _ := parseWinEventRecord(s)
	return ev, nil
}

// readEventLog yeni 4625/4740 olaylarını birikime ekler (c.auth kilidi altında).
// İmleç yoksa ya da günlük temizlenmişse (en yeni kayıt imleçten küçük) son 24 saat.
func (c *Collector) readEventLog(ctx context.Context, now time.Time) (string, string) {
	st := &c.auth
	latest, err := evtLatestRecord()
	if err != nil {
		c.opts.Log.Debug("Security günlüğü okunamadı", "hata", err)
		return "", NoteFailed
	}
	after := st.evtRecord
	if latest < after {
		after = 0
	}
	rs, err := evtQuery(winEvtQuery(after), evtQueryForwardDirection)
	if err != nil {
		c.opts.Log.Debug("Security günlüğü sorgusu", "hata", err)
		return "", NoteFailed
	}
	defer evtClose(rs)
	maxSeen, buf := after, make([]uint16, 8192)
	var events []winEvent
	done := false
loop:
	for ctx.Err() == nil {
		hs, err := evtNext(rs)
		if err != nil {
			c.opts.Log.Debug("Security günlüğü okuma", "hata", err)
			break
		}
		if len(hs) == 0 {
			done = true
			break
		}
		for i, h := range hs {
			var s string
			s, buf, err = evtRenderXML(h, buf)
			evtClose(h)
			if err != nil {
				continue
			}
			if ev, ok := parseWinEvent(s); ok {
				events = append(events, ev)
				maxSeen = max(maxSeen, ev.RecordID)
			}
			if len(events) >= authJournalMax {
				for _, x := range hs[i+1:] {
					evtClose(x)
				}
				break loop
			}
		}
	}
	countWinEvents(st.pending, events)
	if done {
		maxSeen = max(maxSeen, latest) // eşleşmeyen kayıtları bir sonraki turda yeniden tarama
	}
	st.evtRecord = maxSeen
	return AuthSourceEventLog, ""
}
