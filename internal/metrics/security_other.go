//go:build !linux && !windows

package metrics

import (
	"context"
	"os"
)

// Diğer işletim sistemlerinde güvenlik raporu toplanmaz.
func osSecUpdates(context.Context, secEnv) (*SecUpdates, string) { return nil, NoteUnsupported }

func osSecReboot(context.Context, secEnv) (*SecReboot, string) { return nil, NoteUnsupported }

// secExtrasSupported SSH girişleri, oturumlar ve güvenlik duvarı (732) yalnız
// Linux'ta; Windows oturum/Defender/güvenlik duvarı 733'te.
const secExtrasSupported = false

func fileInode(os.FileInfo) uint64 { return 0 }

// osSecExtras diğer sistemlerde yok.
func osSecExtras(context.Context, secEnv, *Security) bool { return false }
