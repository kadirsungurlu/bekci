//go:build !linux && !windows

package metrics

// Diğer işletim sistemlerinde metrik toplanmaz; ajan nedeni bildirir. Ajan
// ikilisi Linux ve Windows için derlenir, bu dosya yalnızca derlemenin
// bozulmaması içindir.
func newSystemSource(func(string) string) (source, string) {
	return nil, "Sunucu metrikleri şimdilik yalnızca Linux ve Windows'ta toplanabiliyor"
}
