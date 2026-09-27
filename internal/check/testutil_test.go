package check

import (
	"net"
	"strings"
	"testing"
)

// splitUserPassHostPort "user:pass@host:port" veya "pass@host:port" biçimini
// çözer; kullanılıcı adı verilmemişse boş döner. Canlı entegrasyon
// testlerinin ortam değişkenlerini okumak için kullanılır.
func splitUserPassHostPort(t *testing.T, s string) (user, pass, host, port string) {
	t.Helper()
	cred, hostport, ok := strings.Cut(s, "@")
	if !ok {
		t.Fatalf("beklenen biçim user:pass@host:port ya da pass@host:port, geldi: %q", s)
	}
	if u, p, ok := strings.Cut(cred, ":"); ok {
		user, pass = u, p
	} else {
		pass = cred
	}
	h, p, err := net.SplitHostPort(hostport)
	if err != nil {
		t.Fatalf("host:port çözülemedi (%q): %v", hostport, err)
	}
	host, port = h, p
	return
}
