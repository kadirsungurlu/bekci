//go:build windows

package metrics

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// Windows Update sorgusu (iş 734). COM nesneleri kilitli bir iş parçacığında
// kullanılır; sorgu wuTimeout ile sınırlıdır, askıda kalan varken yenisi başlamaz.

const (
	wuTimeout      = 3 * time.Minute
	wuHistoryLimit = 200
	wuCriteria     = "IsInstalled=0 and IsHidden=0"
)

var (
	wuBusy    atomic.Int32
	errWUBusy = errors.New("windows update: önceki sorgu sürüyor")
)

func wuQuery(ctx context.Context) (*SecUpdates, *SecWU, error) {
	if !wuBusy.CompareAndSwap(0, 1) {
		return nil, nil, errWUBusy
	}
	type res struct {
		u   *SecUpdates
		w   *SecWU
		err error
	}
	ch := make(chan res, 1)
	go func() {
		defer wuBusy.Store(0)
		u, w, err := wuQueryCOM()
		ch <- res{u, w, err}
	}()
	ctx, cancel := context.WithTimeout(ctx, wuTimeout)
	defer cancel()
	select {
	case r := <-ch:
		return r.u, r.w, r.err
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

// oleProp özelliği okur; hata ya da boşsa nil.
func oleProp(d *ole.IDispatch, name string, args ...any) *ole.VARIANT {
	v, err := oleutil.GetProperty(d, name, args...)
	if err != nil {
		return nil
	}
	return v
}

func oleString(d *ole.IDispatch, name string) string {
	v := oleProp(d, name)
	if v == nil {
		return ""
	}
	defer v.Clear()
	s, _ := v.Value().(string)
	return s
}

func oleInt(d *ole.IDispatch, name string) int {
	v := oleProp(d, name)
	if v == nil {
		return 0
	}
	defer v.Clear()
	switch x := v.Value().(type) {
	case int32:
		return int(x)
	case int64:
		return int(x)
	case uint32:
		return int(x)
	case int16:
		return int(x)
	case uint8:
		return int(x)
	}
	return 0
}

// oleEach koleksiyonun (Count + Item) her öğesi için fn'i çağırır.
func oleEach(coll *ole.IDispatch, limit int, fn func(*ole.IDispatch)) {
	n := min(oleInt(coll, "Count"), limit)
	for i := range n {
		v := oleProp(coll, "Item", i)
		if v == nil {
			continue
		}
		if d := v.ToIDispatch(); d != nil {
			fn(d)
		}
		v.Clear()
	}
}

// oleStrings dize koleksiyonu (KBArticleIDs).
func oleStrings(coll *ole.IDispatch, limit int) []string {
	var out []string
	n := min(oleInt(coll, "Count"), limit)
	for i := range n {
		v := oleProp(coll, "Item", i)
		if v == nil {
			continue
		}
		if s, ok := v.Value().(string); ok {
			out = append(out, s)
		}
		v.Clear()
	}
	return out
}

func oleChild(d *ole.IDispatch, name string) (*ole.IDispatch, *ole.VARIANT) {
	v := oleProp(d, name)
	if v == nil {
		return nil, nil
	}
	return v.ToIDispatch(), v
}

func wuQueryCOM() (*SecUpdates, *SecWU, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		var oe *ole.OleError
		if !errors.As(err, &oe) || oe.Code() != 1 { // S_FALSE: zaten başlatılmış
			return nil, nil, err
		}
	}
	defer ole.CoUninitialize()
	unk, err := oleutil.CreateObject("Microsoft.Update.Session")
	if err != nil {
		return nil, nil, err
	}
	defer unk.Release()
	sess, err := unk.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return nil, nil, err
	}
	defer sess.Release()
	sv, err := oleutil.CallMethod(sess, "CreateUpdateSearcher")
	if err != nil {
		return nil, nil, err
	}
	defer sv.Clear()
	searcher := sv.ToIDispatch()
	if _, err := oleutil.PutProperty(searcher, "Online", false); err != nil {
		return nil, nil, err
	}
	rv, err := oleutil.CallMethod(searcher, "Search", wuCriteria)
	if err != nil {
		return nil, nil, err
	}
	defer rv.Clear()
	var list []wuUpdate
	if ups, uv := oleChild(rv.ToIDispatch(), "Updates"); ups != nil {
		oleEach(ups, 2000, func(item *ole.IDispatch) {
			x := wuUpdate{Title: oleString(item, "Title"), Severity: oleString(item, "MsrcSeverity")}
			if kb, v := oleChild(item, "KBArticleIDs"); kb != nil {
				x.KBs = oleStrings(kb, 8)
				v.Clear()
			}
			if cats, v := oleChild(item, "Categories"); cats != nil {
				oleEach(cats, 16, func(c *ole.IDispatch) {
					x.Categories = append(x.Categories, oleString(c, "CategoryID"))
				})
				v.Clear()
			}
			list = append(list, x)
		})
		uv.Clear()
	}
	upd := wuPackages(list)
	upd.IndexTime = wuLastSearch()
	var hist []wuHistory
	n := 0
	if cv, err := oleutil.CallMethod(searcher, "GetTotalHistoryCount"); err == nil {
		if x, ok := cv.Value().(int32); ok {
			n = int(x)
		}
		cv.Clear()
	}
	if n > 0 {
		if hv, err := oleutil.CallMethod(searcher, "QueryHistory", 0, min(n, wuHistoryLimit)); err == nil {
			oleEach(hv.ToIDispatch(), wuHistoryLimit, func(e *ole.IDispatch) {
				h := wuHistory{Title: oleString(e, "Title"), Code: oleInt(e, "ResultCode"), Operation: oleInt(e, "Operation")}
				if v := oleProp(e, "Date"); v != nil {
					h.Date, _ = v.Value().(time.Time)
					v.Clear()
				}
				hist = append(hist, h)
			})
			hv.Clear()
		}
	}
	return upd, wuFailures(hist, time.Now()), nil
}

// wuLastSearch WU hizmetinin son başarılı taraması (Microsoft.Update.AutoUpdate;
// unix sn, 0 bilinmiyor). Paket dizininin tazeliği olarak gösterilir.
func wuLastSearch() int64 {
	unk, err := oleutil.CreateObject("Microsoft.Update.AutoUpdate")
	if err != nil {
		return 0
	}
	defer unk.Release()
	au, err := unk.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return 0
	}
	defer au.Release()
	r, v := oleChild(au, "Results")
	if r == nil {
		return 0
	}
	defer v.Clear()
	d := oleProp(r, "LastSearchSuccessDate")
	if d == nil {
		return 0
	}
	defer d.Clear()
	if t, ok := d.Value().(time.Time); ok && t.Year() > 2000 {
		return t.Unix()
	}
	return 0
}
