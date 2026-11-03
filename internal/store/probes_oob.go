package store

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"unicode/utf8"
)

// 41: OOB (bant dışı) ikinci hat.
//
//   - probes.oob_ip: ajanın ikinci hattının IP'si (Main'in gördüğü adres). IP
//     kilidi bu adresi de kabul eder; ajan bu IP'nin bağlı olduğu arayüzü
//     kendisi bulur (internal/probe/bind_*.go). Boş: OOB yok.
//   - probes.oob_name: bildirim ve panelde hattın adı (boş: "OOB").
//   - probes.uplink: ajanın son bildirdiği etkin hat ('primary' | 'oob'),
//     uplink_since: o hatta geçiş anı (unix sn; 0: hiç geçiş yok).
//   - probes.oob_iface_missing: ajan OOB IP'sinin bağlı olduğu arayüzü bulamadı
//     (NAT arkasında genel IP; PROBE_OOB_IFACE ile ezilebilir).
func init() {
	RegisterMigration(41, `
ALTER TABLE probes ADD COLUMN oob_ip TEXT NOT NULL DEFAULT '';
ALTER TABLE probes ADD COLUMN oob_name TEXT NOT NULL DEFAULT '';
ALTER TABLE probes ADD COLUMN uplink TEXT NOT NULL DEFAULT 'primary';
ALTER TABLE probes ADD COLUMN uplink_since INTEGER NOT NULL DEFAULT 0;
ALTER TABLE probes ADD COLUMN oob_iface_missing INTEGER NOT NULL DEFAULT 0;`)
}

// Hat değerleri (probes.uplink, X-Probe-Uplink başlığı).
const (
	UplinkPrimary = "primary"
	UplinkOOB     = "oob"
)

// DefaultOOBName OOB hattının adı boşken kullanılan ad.
const DefaultOOBName = "OOB"

// OOBNameMax OOB hat adının en fazla uzunluğu (karakter).
const OOBNameMax = 40

// ProbeOOB probe başına OOB alanları (Probe içinde gömülü).
type ProbeOOB struct {
	OOBIP           string `json:"oob_ip"`
	OOBName         string `json:"oob_name"`
	Uplink          string `json:"uplink"`
	UplinkSince     int64  `json:"uplink_since"`
	OOBIfaceMissing bool   `json:"oob_iface_missing"`
}

// OOBLabel hattın görünen adı (boşsa "OOB").
func (o ProbeOOB) OOBLabel() string {
	if o.OOBName == "" {
		return DefaultOOBName
	}
	return o.OOBName
}

// OnOOB ajan şu an OOB hattı üzerinden mi bağlı?
func (o ProbeOOB) OnOOB() bool { return o.Uplink == UplinkOOB }

// ErrOOBIP geçersiz OOB IP'si.
var ErrOOBIP = errors.New("geçersiz OOB IP adresi")

// ErrOOBName OOB adı çok uzun ya da denetim karakteri içeriyor.
var ErrOOBName = errors.New("geçersiz OOB adı")

// NormalizeOOB OOB IP'sini ve adını doğrular, olağan biçime çevirir. Boş IP
// OOB'yi kapatır (ad da silinir).
func NormalizeOOB(ip, name string) (string, string, error) {
	ip, name = strings.TrimSpace(ip), strings.TrimSpace(name)
	if ip == "" {
		return "", "", nil
	}
	a, err := netip.ParseAddr(ip)
	if err != nil || a.Zone() != "" || a.IsUnspecified() || a.IsLoopback() || a.IsMulticast() {
		return "", "", ErrOOBIP
	}
	if utf8.RuneCountInString(name) > OOBNameMax || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", "", ErrOOBName
	}
	return a.Unmap().String(), name, nil
}

// SameIP iki adres aynı mı (IPv4-mapped IPv6 ve yazım farkı yok sayılır)?
func SameIP(a, b string) bool {
	x, err1 := netip.ParseAddr(strings.TrimSpace(a))
	y, err2 := netip.ParseAddr(strings.TrimSpace(b))
	if err1 != nil || err2 != nil {
		return false
	}
	return x.Unmap().WithZone("") == y.Unmap().WithZone("")
}

// SetProbeOOB OOB IP'sini ve adını yazar (doğrulama: NormalizeOOB). OOB
// kaldırılınca hat 'primary'ye, arayüz uyarısı sıfıra döner.
func (s *Store) SetProbeOOB(ctx context.Context, id int64, ip, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE probes SET oob_ip = ?, oob_name = ?,
		uplink = CASE WHEN ? = '' THEN 'primary' ELSE uplink END,
		oob_iface_missing = CASE WHEN ? = '' THEN 0 ELSE oob_iface_missing END WHERE id = ?`, ip, name, ip, ip, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetProbeUplink ajanın etkin hattını yazar; değer zaten aynıysa dokunmaz.
// changed: hat gerçekten değişti (eşzamanlı iki istekten yalnızca biri true alır).
func (s *Store) SetProbeUplink(ctx context.Context, id int64, uplink string, now int64) (bool, error) {
	if uplink != UplinkOOB {
		uplink = UplinkPrimary
	}
	res, err := s.db.ExecContext(ctx,
		"UPDATE probes SET uplink = ?, uplink_since = ? WHERE id = ? AND uplink <> ?", uplink, now, id, uplink)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetProbeOOBIfaceMissing ajanın "OOB arayüzü bulunamadı" bildirimini yazar.
func (s *Store) SetProbeOOBIfaceMissing(ctx context.Context, id int64, missing bool) error {
	_, err := s.db.ExecContext(ctx, "UPDATE probes SET oob_iface_missing = ? WHERE id = ?", boolInt(missing), id)
	return err
}
