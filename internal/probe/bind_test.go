package probe

import (
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestResolveOOB(t *testing.T) {
	old := listIfaces
	defer func() { listIfaces = old }()
	listIfaces = func() ([]ifaceAddrs, error) {
		return []ifaceAddrs{
			{Name: "eth0", Index: 2, Addrs: []netip.Addr{netip.MustParseAddr("10.0.0.5")}},
			{Name: "eth1", Index: 3, Addrs: []netip.Addr{netip.MustParseAddr("fe80::1"), netip.MustParseAddr("192.168.9.7"), netip.MustParseAddr("2001:db8::7")}},
		}, nil
	}
	b, err := resolveOOB("192.168.9.7", "")
	if err != nil || b.Iface != "eth1" || b.Index != 3 || b.Local.String() != "192.168.9.7" {
		t.Fatalf("tam eşleşme: %+v %v", b, err)
	}
	if _, err := resolveOOB("203.0.113.9", ""); !errors.Is(err, ErrOOBIfaceMissing) {
		t.Fatalf("yok: %v", err)
	}
	// NAT: genel IP arayüzde yok, PROBE_OOB_IFACE ile arayüzün aynı aileden adresi.
	b, err = resolveOOB("203.0.113.9", "eth1")
	if err != nil || b.Local.String() != "192.168.9.7" || b.Index != 3 {
		t.Fatalf("override v4: %+v %v", b, err)
	}
	b, err = resolveOOB("2001:db8:ffff::1", "eth1")
	if err != nil || b.Local.String() != "2001:db8::7" {
		t.Fatalf("override v6 (link-local atlanmalı): %+v %v", b, err)
	}
	if _, err := resolveOOB("203.0.113.9", "eth9"); err == nil || errors.Is(err, ErrOOBIfaceMissing) {
		t.Fatalf("bilinmeyen arayüz hata vermeli: %v", err)
	}
	if _, err := resolveOOB("2001:db8:ffff::1", "eth0"); err == nil {
		t.Fatal("ailesi uymayan arayüz hata vermeli")
	}
	if _, err := resolveOOB("bozuk", ""); err == nil {
		t.Fatal("geçersiz IP")
	}
}

// Gerçek soket: loopback'e bağlı dialer kaynak adresi olarak OOB IP'sini kullanır.
func TestOOBDialerLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	go func() {
		if c, err := ln.Accept(); err == nil {
			c.Close()
		}
	}()
	b, err := resolveOOB("127.0.0.1", "")
	if err != nil {
		t.Skipf("loopback arayüzü yok: %v", err)
	}
	d := b.dialer(&net.Dialer{Timeout: 2 * time.Second})
	c, err := d.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("bağlanamadı: %v", err)
	}
	defer c.Close()
	if got := c.LocalAddr().(*net.TCPAddr).IP.String(); got != "127.0.0.1" {
		t.Fatalf("kaynak adres %s", got)
	}
}
