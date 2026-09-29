package store

import "encoding/json"

// Durum sayfası dizilimi (migration 17): yerleşim, genişlik, bölüm sırası ve
// görünürlüğü. Varsayılanlar önceki görünümün aynısıdır; mevcut sayfalar
// değişmez.
//
// "Son olaylar" bölümünün görünürlüğü için tek doğru kaynak show_incidents
// sütunudur (migration 13): layout sütununda saklanmaz, okunurken oradan
// doldurulur. Böylece eski istemciler (show_incidents gönderen) ve yeni
// istemciler (layout.blocks gönderen) aynı değeri görür.

// Yerleşimler.
const (
	LayoutList    = "list"    // her monitör çubuklarıyla alt alta (varsayılan)
	LayoutGrid    = "grid"    // geniş ekranda gruplar iki sütunda kart olarak
	LayoutCompact = "compact" // çubuksuz, monitör başına tek sık satır
)

// Genişlikler.
const (
	WidthNarrow = "narrow" // ~800 px (varsayılan)
	WidthWide   = "wide"   // ~1200 px
)

// Sayfa bölümleri.
const (
	BlockOverall       = "overall"       // "Tüm sistemler çalışıyor" genel durum kutusu
	BlockAnnouncements = "announcements" // yayındaki duyurular
	BlockGroups        = "groups"        // monitör grupları
	BlockIncidents     = "incidents"     // son 14 günün olayları
)

// DefaultBlockOrder önceki sabit sıra.
var DefaultBlockOrder = []string{BlockOverall, BlockAnnouncements, BlockGroups, BlockIncidents}

func validBlock(id string) bool {
	for _, b := range DefaultBlockOrder {
		if b == id {
			return true
		}
	}
	return false
}

// PageBlock sayfadaki bir bölüm ve görünürlüğü.
type PageBlock struct {
	ID      string `json:"id"`
	Visible bool   `json:"visible"`
}

// PageLayout sayfanın dizilimi (API biçimi). Blocks her zaman dört bölümü de
// sırasıyla içerir (NormalizeLayout).
type PageLayout struct {
	Style  string      `json:"style"`
	Width  string      `json:"width"`
	Blocks []PageBlock `json:"blocks"`
}

// Visible bölüm görünür mü?
func (l PageLayout) Visible(id string) bool {
	for _, b := range l.Blocks {
		if b.ID == id {
			return b.Visible
		}
	}
	return true
}

// DefaultLayout önceki görünüm: liste, dar, sabit sıra, olaylar showIncidents'a göre.
func DefaultLayout(showIncidents bool) PageLayout {
	return NormalizeLayout(PageLayout{}, showIncidents)
}

// NormalizeLayout geçersiz değerleri varsayılana çeker: bilinmeyen yerleşim ve
// genişlik varsayılan olur; bilinmeyen veya tekrarlanan bölümler atılır,
// eksik bölümler görünür olarak sona eklenir. Olaylar bölümünün görünürlüğü
// her zaman showIncidents'tır.
func NormalizeLayout(l PageLayout, showIncidents bool) PageLayout {
	out := PageLayout{Style: l.Style, Width: l.Width, Blocks: make([]PageBlock, 0, len(DefaultBlockOrder))}
	switch out.Style {
	case LayoutList, LayoutGrid, LayoutCompact:
	default:
		out.Style = LayoutList
	}
	switch out.Width {
	case WidthNarrow, WidthWide:
	default:
		out.Width = WidthNarrow
	}
	seen := map[string]bool{}
	add := func(b PageBlock) {
		if !validBlock(b.ID) || seen[b.ID] {
			return
		}
		seen[b.ID] = true
		if b.ID == BlockIncidents {
			b.Visible = showIncidents
		}
		out.Blocks = append(out.Blocks, b)
	}
	for _, b := range l.Blocks {
		add(b)
	}
	for _, id := range DefaultBlockOrder {
		add(PageBlock{ID: id, Visible: true})
	}
	return out
}

// storedLayout veritabanındaki biçim: sıra ve gizli bölümler (olaylar hariç).
type storedLayout struct {
	Style  string   `json:"style,omitempty"`
	Width  string   `json:"width,omitempty"`
	Order  []string `json:"order,omitempty"`
	Hidden []string `json:"hidden,omitempty"`
}

// encodeLayout layout sütununa yazılacak metin; varsayılan dizilim "" olarak saklanır.
func encodeLayout(l PageLayout) string {
	l = NormalizeLayout(l, true)
	if isDefaultLayout(l) {
		return ""
	}
	st := storedLayout{Style: l.Style, Width: l.Width}
	for _, b := range l.Blocks {
		st.Order = append(st.Order, b.ID)
		if !b.Visible && b.ID != BlockIncidents {
			st.Hidden = append(st.Hidden, b.ID)
		}
	}
	data, _ := json.Marshal(st)
	return string(data)
}

func isDefaultLayout(l PageLayout) bool {
	if l.Style != LayoutList || l.Width != WidthNarrow || len(l.Blocks) != len(DefaultBlockOrder) {
		return false
	}
	for i, b := range l.Blocks {
		if b.ID != DefaultBlockOrder[i] || (!b.Visible && b.ID != BlockIncidents) {
			return false
		}
	}
	return true
}

// decodeLayout sütundaki metni çözer; boş veya bozuksa varsayılan dizilim.
func decodeLayout(s string, showIncidents bool) PageLayout {
	var st storedLayout
	if s == "" || json.Unmarshal([]byte(s), &st) != nil {
		return DefaultLayout(showIncidents)
	}
	hidden := map[string]bool{}
	for _, id := range st.Hidden {
		hidden[id] = true
	}
	l := PageLayout{Style: st.Style, Width: st.Width}
	for _, id := range st.Order {
		l.Blocks = append(l.Blocks, PageBlock{ID: id, Visible: !hidden[id]})
	}
	// Sırada olmayan ama gizli işaretli bölüm (el ile düzenlenmiş veri) gizli kalır.
	for _, id := range DefaultBlockOrder {
		if hidden[id] {
			l.Blocks = append(l.Blocks, PageBlock{ID: id, Visible: false})
		}
	}
	return NormalizeLayout(l, showIncidents)
}

func init() {
	// 17: durum sayfası dizilimi (yerleşim, genişlik, bölüm sırası/görünürlüğü).
	// Boş metin varsayılan dizilimdir: mevcut sayfalar aynı görünür. Olaylar
	// bölümünün görünürlüğü show_incidents sütununda kalır.
	RegisterMigration(17, `ALTER TABLE status_pages ADD COLUMN layout TEXT NOT NULL DEFAULT '';`)
}
