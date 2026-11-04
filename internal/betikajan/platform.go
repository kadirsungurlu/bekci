package betikajan

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	minSystemd         = 254 // --expand-environment
	privatePIDsSystemd = 257
)

// Platform ajanın bulunduğu ortam (D5: yalnız Linux + systemd >= 254 + yerli ajan).
type Platform struct {
	OK          bool
	Detail      string // kapalıysa neden (yalnız yerel kayıt)
	PrivatePIDs bool
}

// DetectPlatform ortamı algılar. Docker ajanı, Windows ve OpenRC reddedilir.
func DetectPlatform() Platform {
	if runtime.GOOS != "linux" {
		return Platform{Detail: "yalnız Linux"}
	}
	if os.Getenv("HOST_PROC") != "" || exists("/.dockerenv") || exists("/run/.containerenv") {
		return Platform{Detail: "konteyner içinde çalışıyor"}
	}
	if !exists("/run/systemd/system") {
		return Platform{Detail: "systemd yok"}
	}
	v := systemdVersion()
	if v < minSystemd {
		return Platform{Detail: "systemd sürümü en az 254 olmalı"}
	}
	return Platform{OK: true, PrivatePIDs: v >= privatePIDsSystemd}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func systemdVersion() int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemd-run", "--version").Output()
	if err != nil {
		return 0
	}
	line, _, _ := bytes.Cut(out, []byte{'\n'})
	f := strings.Fields(string(line))
	if len(f) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(f[1])
	return n
}
