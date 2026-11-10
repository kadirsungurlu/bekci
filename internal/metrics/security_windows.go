//go:build windows

package metrics

import (
	"context"
	"os"

	"golang.org/x/sys/windows/registry"
)

// Windows'ta bekleyen güncellemeler Windows Update ile (iş 734) gelecek; şimdilik
// yalnız yeniden başlatma gereksinimi kayıt defterinden okunur.
func osSecUpdates(context.Context, secEnv) (*SecUpdates, string) {
	return nil, NoteUnsupported
}

// rebootKeys varlığı yeniden başlatma bekliyor demek olan anahtarlar.
var rebootKeys = []string{
	`SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired`,
	`SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending`,
}

func osSecReboot(context.Context, secEnv) (*SecReboot, string) {
	for _, k := range rebootKeys {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, k, registry.QUERY_VALUE)
		if err == nil {
			key.Close()
			return &SecReboot{Required: true, Reason: RebootRegistry}, ""
		}
	}
	return &SecReboot{}, ""
}

// secExtrasSupported SSH girişleri, oturumlar ve güvenlik duvarı (732) yalnız
// Linux'ta; Windows oturum/Defender/güvenlik duvarı 733'te.
const secExtrasSupported = false

func fileInode(os.FileInfo) uint64 { return 0 }
