package metrics

import "time"

// callWithTimeout fn'i ayrı bir goroutine'de çalıştırır ve en fazla d kadar
// bekler. fn süresinde dönerse sonucunu ve timedOut=false döner; süre dolarsa
// sıfır değer ve timedOut=true döner, fn ise arka planda çalışmaya devam eder.
//
// Kanal tamponlu (kapasite 1) olduğu için fn eninde sonunda dönerse sonucunu
// yazıp biter: zaman aşımından sonra kimse okumasa bile goroutine gönderimde
// süresiz bloklanmaz. Her çağrı en fazla bir goroutine başlatır; goroutine
// yalnızca fn (ör. bir syscall) çekirdekte gerçekten askıda kaldığı sürece
// yaşar, bu da kaçınılmaz olan durumdur.
func callWithTimeout[T any](d time.Duration, fn func() T) (v T, timedOut bool) {
	ch := make(chan T, 1)
	go func() { ch <- fn() }()
	select {
	case v = <-ch:
		return v, false
	case <-time.After(d):
		var zero T
		return zero, true
	}
}
