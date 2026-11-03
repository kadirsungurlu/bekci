package probe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

const testMeshPing = 50 * time.Millisecond

func meshTestKey(t *testing.T) (string, []byte) {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b), b
}

func startTestMesh(t *testing.T, id int64, listen string) *Mesh {
	t.Helper()
	m := NewMesh(MeshConfig{
		SelfID:        id,
		ListenAddr:    listen,
		PingInterval:  testMeshPing,
		LossThreshold: 3,
		Logf: func(format string, args ...any) {
			if os.Getenv("MESH_LOG") != "" {
				t.Logf("[%d] "+format, append([]any{id}, args...)...)
			}
		},
	})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

func meshAddr(m *Mesh) string {
	if p := m.ListenPort(); p != 0 {
		return fmt.Sprintf("127.0.0.1:%d", p)
	}
	return ""
}

func meshLink(m *Mesh, peer int64) MeshLinkReport {
	for _, r := range m.Report() {
		if r.Peer == peer {
			return r
		}
	}
	return MeshLinkReport{}
}

func waitMesh(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("zaman aşımı: %s", what)
}

func waitUp(t *testing.T, m *Mesh, peer int64) {
	t.Helper()
	waitMesh(t, fmt.Sprintf("eş %d up", peer), func() bool {
		r := meshLink(m, peer)
		return r.State == "up" && r.LastOKAt > 0 && r.RTTms >= 0
	})
}

// pair iki mesh kurar; listen1/listen2 tarafların dinleyip dinlemediği.
func meshPair(t *testing.T, listen1, listen2 bool) (*Mesh, *Mesh) {
	t.Helper()
	la := func(b bool) string {
		if b {
			return "127.0.0.1:0"
		}
		return ""
	}
	key, _ := meshTestKey(t)
	a := startTestMesh(t, 1, la(listen1))
	b := startTestMesh(t, 2, la(listen2))
	a.SetPeers(listen1, []MeshPeer{{ID: 2, Addr: meshAddr(b), Key: key}})
	b.SetPeers(listen2, []MeshPeer{{ID: 1, Addr: meshAddr(a), Key: key}})
	return a, b
}

func TestMeshBothSidesUp(t *testing.T) {
	a, b := meshPair(t, true, true)
	waitUp(t, a, 2)
	waitUp(t, b, 1)
	// Tek bağlantı: küçük id bağlanır, iki taraf aynı bağlantıyı görür.
	waitMesh(t, "tek bağlantı", func() bool { return a.sessionDialer(2) == 1 && b.sessionDialer(1) == 1 })
	time.Sleep(5 * testMeshPing)
	if r := meshLink(a, 2); r.State != "up" || r.LossStreak != 0 {
		t.Fatalf("a: %+v", r)
	}
	if r := meshLink(b, 1); r.State != "up" || r.LossStreak != 0 {
		t.Fatalf("b: %+v", r)
	}
}

func TestMeshNATDirection(t *testing.T) {
	cases := []struct {
		name             string
		listen1, listen2 bool
		dialer           int64
	}{
		{"kucuk-dinlemiyor", false, true, 1},
		{"buyuk-dinlemiyor", true, false, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, b := meshPair(t, c.listen1, c.listen2)
			waitUp(t, a, 2)
			waitUp(t, b, 1)
			if d := a.sessionDialer(2); d != c.dialer {
				t.Fatalf("a tarafında bağlanan %d, beklenen %d", d, c.dialer)
			}
			if d := b.sessionDialer(1); d != c.dialer {
				t.Fatalf("b tarafında bağlanan %d, beklenen %d", d, c.dialer)
			}
		})
	}
}

func TestMeshUnknownWhenNeitherListens(t *testing.T) {
	a, b := meshPair(t, false, false)
	time.Sleep(6 * testMeshPing)
	for _, r := range []MeshLinkReport{meshLink(a, 2), meshLink(b, 1)} {
		if r.State != "unknown" || r.LossStreak != 0 {
			t.Fatalf("beklenen unknown: %+v", r)
		}
	}
}

func TestMeshListenerRestart(t *testing.T) {
	key, _ := meshTestKey(t)
	a := startTestMesh(t, 1, "127.0.0.1:0")
	addr := meshAddr(a)
	b := startTestMesh(t, 2, "")
	a.SetPeers(true, []MeshPeer{{ID: 2, Key: key}})
	b.SetPeers(false, []MeshPeer{{ID: 1, Addr: addr, Key: key}})
	waitUp(t, b, 1)
	waitUp(t, a, 2)

	a.Close()
	waitMesh(t, "b tarafında down", func() bool {
		r := meshLink(b, 1)
		return r.State == "down" && r.LossStreak >= 3
	})

	a2 := startTestMesh(t, 1, addr)
	a2.SetPeers(true, []MeshPeer{{ID: 2, Key: key}})
	waitUp(t, b, 1)
	waitUp(t, a2, 2)
}

// expectClosed bağlantının karşı tarafça kapatıldığını (zaman aşımı değil) doğrular.
func expectClosed(t *testing.T, c net.Conn) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 4096)
	for {
		_, err := c.Read(buf)
		if err == nil {
			continue
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatal("bağlantı kapatılmadı")
		}
		return
	}
}

func rawFrame(t *testing.T, typ meshFrameType, from, to int64, seq uint64, ts time.Time, key []byte) []byte {
	t.Helper()
	b, err := encodeMeshFrame(meshFrame{Type: typ, From: from, To: to, Seq: seq, TS: ts.UnixNano()}, key)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMeshRejectsBadFrames(t *testing.T) {
	keyHex, key := meshTestKey(t)
	_, wrong := meshTestKey(t)
	a := startTestMesh(t, 1, "127.0.0.1:0")
	addr := meshAddr(a)
	// Eş 2 dinlemiyor; a dinliyor ve bekliyor.
	a.SetPeers(true, []MeshPeer{{ID: 2, Key: keyHex}})

	dial := func(t *testing.T) net.Conn {
		t.Helper()
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	now := time.Now()

	t.Run("yanlis-anahtar", func(t *testing.T) {
		c := dial(t)
		c.Write(rawFrame(t, meshHello, 2, 1, 10, now, wrong))
		expectClosed(t, c)
	})
	t.Run("hmacsiz", func(t *testing.T) {
		c := dial(t)
		full := rawFrame(t, meshHello, 2, 1, 11, now, key)
		body := full[4 : len(full)-meshMacLen]
		raw := make([]byte, 4, 4+len(body))
		binary.BigEndian.PutUint32(raw, uint32(len(body)))
		c.Write(append(raw, body...))
		expectClosed(t, c)
	})
	t.Run("bilinmeyen-es", func(t *testing.T) {
		c := dial(t)
		c.Write(rawFrame(t, meshHello, 99, 1, 12, now, key))
		expectClosed(t, c)
	})
	t.Run("saat-kaymasi", func(t *testing.T) {
		c := dial(t)
		c.Write(rawFrame(t, meshHello, 2, 1, 13, now.Add(-10*time.Minute), key))
		expectClosed(t, c)
	})
	t.Run("4kb-ustu", func(t *testing.T) {
		c := dial(t)
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], meshMaxBody+1)
		c.Write(hdr[:])
		c.Write(make([]byte, 1024))
		expectClosed(t, c)
		if _, err := encodeMeshFrame(meshFrame{Payload: make([]byte, meshMaxBody)}, key); !errors.Is(err, errMeshTooLarge) {
			t.Fatalf("gönderen taraf da sınırı uygulamalı: %v", err)
		}
	})
	t.Run("tekrar-oynatma", func(t *testing.T) {
		hello := rawFrame(t, meshHello, 2, 1, 1000, time.Now(), key)
		c1 := dial(t)
		c1.Write(hello)
		// Kabul edildi: a hemen ping gönderir.
		c1.SetReadDeadline(time.Now().Add(3 * time.Second))
		body, err := readMeshBody(c1)
		if err != nil {
			t.Fatalf("geçerli hello kabul edilmedi: %v", err)
		}
		f, err := parseMeshFrame(body)
		if err != nil || f.Type != meshPing || !verifyMeshMAC(body, key) {
			t.Fatalf("beklenen ping: %+v %v", f, err)
		}
		// Aynı hello yeni bağlantıda: eski sıra no -> red.
		c2 := dial(t)
		c2.Write(hello)
		expectClosed(t, c2)
		// Oturum içinde tekrar sıra no -> bağlantı kapanır.
		c1.Write(rawFrame(t, meshPing, 2, 1, 1000, time.Now(), key))
		expectClosed(t, c1)
	})
}
