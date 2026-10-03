package api

import (
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Oturum yönetimi: liste (geçerli oturum işaretli, cihaz bilgisi), tek
// kapatma, diğerlerini kapatma; kapatılan oturum 401 alır.
func TestSessions(t *testing.T) {
	a := setupAdmin(t)
	b := a.loginAs("kadir", "cok-gizli-sifre")
	c := a.loginAs("kadir", "cok-gizli-sifre")
	var list []store.Session
	a.mustDo("GET", "/api/auth/sessions", nil, &list, 200)
	if len(list) != 3 {
		t.Fatalf("3 oturum bekleniyordu: %d", len(list))
	}
	cur := 0
	for _, se := range list {
		if se.Current {
			cur++
		}
		if se.ID == "" || se.Via != store.SessionViaPassword || se.IP == "" || se.CreatedAt == 0 || se.LastSeenAt == 0 {
			t.Fatalf("oturum kaydı eksik: %+v", se)
		}
	}
	if cur != 1 {
		t.Fatalf("yalnızca bir oturum current olmalı: %d", cur)
	}
	// b kendi listesinde kendini görür; a'nın oturumunu kapatır.
	var bl []store.Session
	b.mustDo("GET", "/api/auth/sessions", nil, &bl, 200)
	var other string
	for _, se := range bl {
		if !se.Current && other == "" {
			other = se.ID
		}
	}
	b.mustDo("DELETE", "/api/auth/sessions/"+other, nil, nil, 200)
	b.mustDo("DELETE", "/api/auth/sessions/"+other, nil, nil, 404)
	b.mustDo("DELETE", "/api/auth/sessions/kisa", nil, nil, 404)
	// Kapatılan oturum (a ya da c) artık giremez; kalan ikisi girer.
	alive := 0
	for _, e := range []*env{a, b, c} {
		if e.do("GET", "/api/auth/sessions", nil, nil) == 200 {
			alive++
		}
	}
	if alive != 2 {
		t.Fatalf("bir oturum kapanmış olmalı: %d", alive)
	}
	// Diğerlerini kapat: yalnızca b kalır.
	var res map[string]int
	b.mustDo("DELETE", "/api/auth/sessions", nil, &res, 200)
	if res["revoked"] != 1 {
		t.Fatalf("1 oturum kapanmalıydı: %v", res)
	}
	b.mustDo("GET", "/api/auth/sessions", nil, &list, 200)
	if len(list) != 1 || !list[0].Current {
		t.Fatalf("yalnızca geçerli oturum kalmalı: %+v", list)
	}
	var audit []store.AuditEntry
	b.mustDo("GET", "/api/audit?action=user.session_revoke", nil, &audit, 200)
	if len(audit) != 2 {
		t.Fatalf("işlem kaydı: %d", len(audit))
	}
}
