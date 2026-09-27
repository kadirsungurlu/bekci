//go:build !linux

package metrics

// Diğer işletim sistemlerinde metrik toplanmaz; ajan nedeni bildirir. Ajan
// ikilisi Linux için derlenir, bu dosya yalnızca derlemenin bozulmaması içindir.
func newSystemSource(func(string) string) (source, string) {
	return nil, "Sunucu metrikleri şimdilik yalnızca Linux'ta toplanabiliyor"
}
