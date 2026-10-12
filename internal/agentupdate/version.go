package agentupdate

import (
	"strconv"
	"strings"
)

// Sürüm karşılaştırma. Yalnızca sürüm numarası taşıyan derlemeler (v1.2.3,
// 1.2.3, 1.2.3-rc1) karşılaştırılır; commit özeti (977e923), "dev" ve "test"
// gibi değerler sürüm sayılmaz: böyle derlemeler ne güncelleme alır ne de
// güncelleme olarak sunulur.

// Version ayrıştırılmış bir sürüm numarası.
type Version struct {
	Major, Minor, Patch int
	Pre                 string // ön sürüm etiketi ("rc1"); boş: kararlı
}

// ParseVersion "1.2.3", "v1.2.3", "1.2.3-rc1" biçimlerini çözer. Derleme üst
// verisi ("+abc") yok sayılır. Commit özeti gibi değerlerde ok=false.
func ParseVersion(s string) (Version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s, _, _ = strings.Cut(s, "+")
	core, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, false
	}
	var n [3]int
	for i, p := range parts {
		if p == "" || len(p) > 9 {
			return Version{}, false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return Version{}, false
			}
		}
		n[i], _ = strconv.Atoi(p)
	}
	if pre != "" {
		for _, r := range pre {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '.' || r == '-') {
				return Version{}, false
			}
		}
	}
	return Version{Major: n[0], Minor: n[1], Patch: n[2], Pre: pre}, true
}

// IsVersion metin bir sürüm numarası mı?
func IsVersion(s string) bool {
	_, ok := ParseVersion(s)
	return ok
}

// Compare -1, 0, +1 (semver sırası: kararlı sürüm aynı numaralı ön sürümden
// büyüktür; ön sürüm etiketleri nokta ile ayrılmış parçalar hâlinde, sayısal
// parçalar sayı olarak, diğerleri metin olarak karşılaştırılır).
func Compare(a, b Version) int {
	for _, d := range [3]int{a.Major - b.Major, a.Minor - b.Minor, a.Patch - b.Patch} {
		if d != 0 {
			return sign(d)
		}
	}
	switch {
	case a.Pre == b.Pre:
		return 0
	case a.Pre == "":
		return 1
	case b.Pre == "":
		return -1
	}
	ap, bp := strings.Split(a.Pre, "."), strings.Split(b.Pre, ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		an, aNum := strconv.Atoi(ap[i])
		bn, bNum := strconv.Atoi(bp[i])
		switch {
		case aNum == nil && bNum == nil:
			if an != bn {
				return sign(an - bn)
			}
		case aNum == nil:
			return -1 // sayısal parça metin parçadan küçük
		case bNum == nil:
			return 1
		default:
			if c := strings.Compare(ap[i], bp[i]); c != 0 {
				return c
			}
		}
	}
	return sign(len(ap) - len(bp))
}

func sign(d int) int {
	switch {
	case d < 0:
		return -1
	case d > 0:
		return 1
	}
	return 0
}

// Newer target sürümü current'tan daha yeni bir sürüm numarası mı? İkisinden
// biri sürüm numarası değilse (commit özeti, dev) false: böyle derlemeler
// güncellenmez ve güncelleme olarak sunulmaz; eşit ya da eski sürüm de false
// (sürüm düşürme yok).
func Newer(current, target string) bool {
	c, ok1 := ParseVersion(current)
	t, ok2 := ParseVersion(target)
	return ok1 && ok2 && Compare(t, c) > 0
}
