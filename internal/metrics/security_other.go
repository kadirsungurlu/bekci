//go:build !linux && !windows

package metrics

import "context"

// Diğer işletim sistemlerinde güvenlik raporu toplanmaz.
func osSecUpdates(context.Context, secEnv) (*SecUpdates, string) { return nil, NoteUnsupported }

func osSecReboot(context.Context, secEnv) (*SecReboot, string) { return nil, NoteUnsupported }
