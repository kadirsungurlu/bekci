package mesh

import (
	"bytes"
	"compress/zlib"
	_ "embed"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"github.com/kadirsungurlu/bekci/internal/i18n"
)

// Topoloji şemasının PNG'si (bildirimlere eklenir). Yerleşim BuildTopology'den
// gelir; görsel dil panel SVG'siyle (web MeshTopology) aynıdır: sağlıklı = düz
// çizgi + tik (yeşil), koptu = düz çizgi + çarpı (kırmızı), tek yönlü = çalışan
// yönde ok (amber), ölçülemiyor = kesikli gri.

// Inter (OFL, font/OFL.txt): Latin + Türkçe alt kümesi, 400 ve 600 ağırlık.
//
//go:embed font/Inter-Regular.ttf
var interRegular []byte

//go:embed font/Inter-SemiBold.ttf
var interSemiBold []byte

const renderScale = 2 // SVG birimi başına piksel

var (
	colBg       = rgb(0xf8fafc)
	colBox      = rgb(0xffffff)
	colBoxLine  = rgb(0xcbd5e1)
	colMainLine = rgb(0x64748b)
	colText     = rgb(0x0f172a)
	colUp       = rgb(0x16a34a)
	colDown     = rgb(0xdc2626)
	colOneWay   = rgb(0xd97706)
	colUnknown  = rgb(0x94a3b8)
)

func rgb(v uint32) color.RGBA {
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xff}
}

var fonts struct {
	once          sync.Once
	regular, bold *sfnt.Font
	err           error
}

func loadFonts() (*sfnt.Font, *sfnt.Font, error) {
	fonts.once.Do(func() {
		if fonts.regular, fonts.err = opentype.Parse(interRegular); fonts.err == nil {
			fonts.bold, fonts.err = opentype.Parse(interSemiBold)
		}
	})
	return fonts.regular, fonts.bold, fonts.err
}

// TopologySummary özet satırının düz metni (e-posta alt metni, altyazı).
func TopologySummary(lang string, t Topology) string {
	parts := summaryParts(lang, t.Summary)
	s := make([]string, len(parts))
	for i, p := range parts {
		s[i] = p.text
	}
	return strings.Join(s, " · ")
}

type summaryPart struct {
	text string
	col  color.RGBA
	tick bool // tümü sağlıklı: tik işareti
}

func summaryParts(lang string, c TopoSummaryCounts) []summaryPart {
	if c.Down == 0 && c.OneWay == 0 && c.Unknown == 0 {
		return []summaryPart{{text: i18n.T(lang, "mesh.topo.allok"), col: colUp, tick: true}}
	}
	var out []summaryPart
	for _, p := range []struct {
		n   int
		key string
		col color.RGBA
	}{{c.Up, "mesh.topo.up", colUp}, {c.Down, "mesh.topo.down", colDown}, {c.OneWay, "mesh.topo.oneway", colOneWay}, {c.Unknown, "mesh.topo.unknown", colUnknown}} {
		if p.n > 0 {
			out = append(out, summaryPart{text: i18n.T(lang, p.key, p.n), col: p.col})
		}
	}
	return out
}

// RenderPNG şemayı 2x ölçekte PNG olarak çizer; düğüm adları names'ten
// (Main'in katalog adı dilde), özet satırı lang dilinde.
func RenderPNG(t Topology, names Names, lang string) ([]byte, error) {
	if t.Width <= 0 || t.Height <= 0 || t.Width > 4000 || t.Height > 4000 {
		return nil, fmt.Errorf("mesh: geçersiz şema boyutu %dx%d", t.Width, t.Height)
	}
	regular, bold, err := loadFonts()
	if err != nil {
		return nil, err
	}
	const s = renderScale
	face := func(f *sfnt.Font, size float64) (font.Face, error) {
		return opentype.NewFace(f, &opentype.FaceOptions{Size: size * s, DPI: 72, Hinting: font.HintingNone})
	}
	nameFace, err := face(regular, 12.5)
	if err != nil {
		return nil, err
	}
	mainFace, err := face(bold, 12.5)
	if err != nil {
		return nil, err
	}
	sumFace, err := face(regular, 12)
	if err != nil {
		return nil, err
	}
	defer nameFace.Close()
	defer mainFace.Close()
	defer sumFace.Close()

	p := &painter{dst: image.NewRGBA(image.Rect(0, 0, t.Width*s, t.Height*s))}
	draw.Draw(p.dst, p.dst.Bounds(), image.NewUniform(colBg), image.Point{}, draw.Src)

	pos := make(map[int64]pt, len(t.Nodes))
	for _, n := range t.Nodes {
		pos[n.ID] = pt{float64(n.X * s), float64(n.Y * s)}
	}
	// çizgiler (uçları kutuların altında kalır); ölçülemeyenler altta
	for _, st := range []string{PairUnknown, PairUp, PairOneWay, PairDown} {
		for _, l := range t.Links {
			if l.State != st && (st != PairUnknown || l.State == PairUp || l.State == PairDown || l.State == PairOneWay) {
				continue
			}
			a, b := pos[l.A], pos[l.B]
			switch st {
			case PairUp:
				p.line(a, b, 2*s, colUp, 0, 0)
			case PairDown:
				p.line(a, b, 2*s, colDown, 0, 0)
			case PairOneWay:
				p.line(a, b, 2*s, colOneWay, 0, 0)
			default:
				p.line(a, b, 1.5*s, colUnknown, 6*s, 5*s)
			}
		}
	}
	// durum işaretleri: çizginin ortası; çakışırsa (çapraz çizgiler merkezde
	// buluşur) çizgi boyunca kaydırılır
	var placed []pt
	const mr = 8 * s // işaret yarıçapı
	for _, l := range t.Links {
		if l.State != PairUp && l.State != PairDown && l.State != PairOneWay {
			continue
		}
		a, b := pos[l.A], pos[l.B]
		m := lerp(a, b, 0.5)
		for _, f := range []float64{0.5, 0.42, 0.58, 0.35, 0.65, 0.28, 0.72} {
			c := lerp(a, b, f)
			if !near(placed, c, 2.2*mr) && !inBox(t.Nodes, c, mr+2*s) {
				m = c
				break
			}
		}
		placed = append(placed, m)
		switch l.State {
		case PairUp:
			p.badge(m, mr, colUp)
			p.fill(colUp, tick(m, mr*0.42, 2*s)...)
		case PairDown:
			p.badge(m, mr, colDown)
			p.fill(colDown, cross(m, mr*0.36, 2*s)...)
		case PairOneWay:
			from, to := a, b // çalışan yön
			if !l.Forward {
				from, to = b, a
			}
			p.badge(m, mr, colOneWay)
			p.fill(colOneWay, arrow(m, unit(from, to), mr*0.6))
		}
	}
	// kutular ve adlar
	const bw, bh, br = TopoBoxW * s, TopoBoxH * s, 8 * s
	for _, n := range t.Nodes {
		c := pos[n.ID]
		line, lw, f := colBoxLine, 1.5*s, nameFace
		if n.Main {
			line, lw, f = colMainLine, 2*s, mainFace
		}
		x0, y0 := c.x-bw/2, c.y-bh/2
		p.fill(line, roundRect(x0, y0, bw, bh, br))
		p.fill(colBox, roundRect(x0+lw, y0+lw, bw-2*lw, bh-2*lw, br-lw))
		name := fitText(f, names.In(lang, n.ID), bw-20*s)
		p.text(f, name, c.x-measure(f, name)/2, c.y, colText)
	}
	// özet satırı
	parts := summaryParts(lang, t.Summary)
	const dot, gap, sep = 4 * s, 6 * s, 16 * s
	total := 0.0
	for i, q := range parts {
		total += 2*dot + gap + measure(sumFace, q.text)
		if i > 0 {
			total += sep
		}
	}
	x, y := (float64(t.Width*s)-total)/2, float64((t.Height-TopoSummary/2)*s)
	for _, q := range parts {
		if q.tick {
			p.fill(q.col, tick(pt{x + dot, y}, dot*1.2, 1.8*s)...)
		} else {
			p.fill(q.col, circle(pt{x + dot, y}, dot))
		}
		x += 2*dot + gap
		p.text(sumFace, q.text, x, y, colText)
		x += measure(sumFace, q.text) + sep
	}

	return encodePNG(p.dst)
}

// encodePNG opak RGBA'yı 8 bit paletli PNG'ye yazar: en sık 256 renk palet,
// diğerleri (seyrek kenar yumuşatma tonları) en yakın palet rengine eşlenir.
// RGB'ye göre ~3 kat küçük ve image/png'nin satır başına süzgeç denemesinden
// (büyük tuvalde ~30 ms) hızlı.
func encodePNG(img *image.RGBA) ([]byte, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	key := func(px []byte) uint32 { return uint32(px[0]) | uint32(px[1])<<8 | uint32(px[2])<<16 }
	counts := map[uint32]int{}
	for y := 0; y < h; y += 2 { // palet için satırların yarısı yeterli; görülmeyen renk en yakına eşlenir
		row := img.Pix[y*img.Stride : y*img.Stride+4*w]
		last, run := key(row), 0
		for i := 0; i+4 <= len(row); i += 4 {
			k := key(row[i : i+4 : i+4])
			if k != last {
				counts[last] += run
				last, run = k, 0
			}
			run++
		}
		counts[last] += run
	}
	colors := make([]uint32, 0, len(counts))
	for k := range counts {
		colors = append(colors, k)
	}
	sort.Slice(colors, func(i, j int) bool {
		if counts[colors[i]] != counts[colors[j]] {
			return counts[colors[i]] > counts[colors[j]]
		}
		return colors[i] < colors[j]
	})
	pal := colors[:min(len(colors), 256)]
	index := make(map[uint32]uint8, len(colors))
	for i, k := range pal {
		index[k] = uint8(i)
	}
	nearest := func(k uint32) uint8 {
		if i, ok := index[k]; ok {
			return i
		}
		best, bd := 0, math.MaxInt
		for i, q := range pal {
			dr, dg, db := int(k&0xff)-int(q&0xff), int(k>>8&0xff)-int(q>>8&0xff), int(k>>16)-int(q>>16)
			if d := dr*dr + dg*dg + db*db; d < bd {
				best, bd = i, d
			}
		}
		index[k] = uint8(best)
		return uint8(best)
	}

	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")
	chunk := func(typ string, data []byte) {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(data)))
		buf.Write(n[:])
		c := crc32.NewIEEE()
		c.Write([]byte(typ))
		c.Write(data)
		buf.WriteString(typ)
		buf.Write(data)
		binary.BigEndian.PutUint32(n[:], c.Sum32())
		buf.Write(n[:])
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], uint32(w))
	binary.BigEndian.PutUint32(ihdr[4:], uint32(h))
	ihdr[8], ihdr[9] = 8, 3 // 8 bit, paletli
	chunk("IHDR", ihdr)
	plte := make([]byte, 0, 3*len(pal))
	for _, k := range pal {
		plte = append(plte, byte(k), byte(k>>8), byte(k>>16))
	}
	chunk("PLTE", plte)

	var z bytes.Buffer
	zw, err := zlib.NewWriterLevel(&z, zlib.BestSpeed)
	if err != nil {
		return nil, err
	}
	// kenar yumuşatma bölgelerinde renk sık değişir: harita önünde doğrudan
	// eşlemeli küçük önbellek
	type slot struct {
		k  uint32
		v  uint8
		ok bool
	}
	var cache [1 << 12]slot
	lookup := func(k uint32) uint8 {
		c := &cache[(k*2654435761)>>20]
		if !c.ok || c.k != k {
			*c = slot{k, nearest(k), true}
		}
		return c.v
	}
	out := make([]byte, 1+w) // süzgeç 0 (yok): paletli görüntüde önerilen
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+4*w]
		last := key(row)
		li := lookup(last)
		o := out[1:]
		for x := range o {
			if k := key(row[4*x : 4*x+4 : 4*x+4]); k != last {
				last, li = k, lookup(k)
			}
			o[x] = li
		}
		if _, err := zw.Write(out); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	chunk("IDAT", z.Bytes())
	chunk("IEND", nil)
	return buf.Bytes(), nil
}

// ---- çizim yardımcıları ----

type pt struct{ x, y float64 }

func lerp(a, b pt, f float64) pt { return pt{a.x + (b.x-a.x)*f, a.y + (b.y-a.y)*f} }

func unit(a, b pt) pt {
	dx, dy := b.x-a.x, b.y-a.y
	l := math.Hypot(dx, dy)
	if l == 0 {
		return pt{1, 0}
	}
	return pt{dx / l, dy / l}
}

func near(ps []pt, c pt, d float64) bool {
	for _, q := range ps {
		if math.Hypot(q.x-c.x, q.y-c.y) < d {
			return true
		}
	}
	return false
}

// inBox c (m pay ile) bir düğüm kutusunun üstünde mi (şema birimi x ölçek).
func inBox(nodes []TopoNode, c pt, m float64) bool {
	for _, n := range nodes {
		x, y := float64(n.X*renderScale), float64(n.Y*renderScale)
		if math.Abs(c.x-x) < TopoBoxW*renderScale/2+m && math.Abs(c.y-y) < TopoBoxH*renderScale/2+m {
			return true
		}
	}
	return false
}

type painter struct {
	dst *image.RGBA
	z   vector.Rasterizer
}

// fill çokgenleri (aynı yönlü; birleşim) col ile doldurur. Rasterleştirici
// yalnız şeklin sınır kutusu kadar açılır (büyük tuvalde hız).
func (p *painter) fill(col color.RGBA, polys ...[]pt) {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, poly := range polys {
		for _, q := range poly {
			minX, minY = math.Min(minX, q.x), math.Min(minY, q.y)
			maxX, maxY = math.Max(maxX, q.x), math.Max(maxY, q.y)
		}
	}
	r := image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX))+1, int(math.Ceil(maxY))+1)
	if r = r.Intersect(p.dst.Bounds()); r.Empty() {
		return
	}
	ox, oy := float64(r.Min.X), float64(r.Min.Y)
	p.z.Reset(r.Dx(), r.Dy())
	p.z.DrawOp = draw.Over
	for _, poly := range polys {
		if len(poly) < 3 {
			continue
		}
		p.z.MoveTo(float32(poly[0].x-ox), float32(poly[0].y-oy))
		for _, q := range poly[1:] {
			p.z.LineTo(float32(q.x-ox), float32(q.y-oy))
		}
		p.z.ClosePath()
	}
	p.z.Draw(p.dst, r, image.NewUniform(col), image.Point{})
}

// line a-b arası w kalınlıkta kenarı yumuşatılmış düz çizgi; on > 0 ise
// kesikli (on dolu, off boş). Rasterleştirici yerine uzaklık ile kapsama:
// maliyet çizgi boyu x kalınlık (çapraz çizgilerin sınır kutusu tuvalin
// çoğunu kaplar).
func (p *painter) line(a, b pt, w float64, col color.RGBA, on, off float64) {
	u := unit(a, b)
	l := math.Hypot(b.x-a.x, b.y-a.y)
	hw := w/2 + 1
	bounds := p.dst.Bounds()
	cover := func(x, y int) {
		cx, cy := float64(x)+0.5-a.x, float64(y)+0.5-a.y
		t := cx*u.x + cy*u.y
		d := math.Abs(cx*u.y - cy*u.x)
		c := clamp01(w/2 + 0.5 - d)
		if c == 0 {
			return
		}
		c *= clamp01(min(t, l-t) + 0.5)
		if on > 0 {
			tt := math.Mod(t, on+off)
			if tt < 0 {
				tt += on + off
			}
			c *= clamp01(min(tt, on-tt) + 0.5)
		}
		if c > 0 && image.Pt(x, y).In(bounds) {
			blend(p.dst, x, y, col, c)
		}
	}
	if math.Abs(u.x) >= math.Abs(u.y) { // yatıka yakın: sütun sütun
		x0, x1 := math.Min(a.x, b.x)-1, math.Max(a.x, b.x)+1
		span := hw / math.Abs(u.x)
		for x := int(math.Floor(x0)); x <= int(math.Ceil(x1)); x++ {
			yc := a.y + (float64(x)+0.5-a.x)*u.y/u.x
			for y := int(math.Floor(yc - span)); y <= int(math.Ceil(yc+span)); y++ {
				cover(x, y)
			}
		}
		return
	}
	y0, y1 := math.Min(a.y, b.y)-1, math.Max(a.y, b.y)+1
	span := hw / math.Abs(u.y)
	for y := int(math.Floor(y0)); y <= int(math.Ceil(y1)); y++ {
		xc := a.x + (float64(y)+0.5-a.y)*u.x/u.y
		for x := int(math.Floor(xc - span)); x <= int(math.Ceil(xc+span)); x++ {
			cover(x, y)
		}
	}
}

func clamp01(v float64) float64 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}

// blend opak zemine c kapsamayla col'u bindirir.
func blend(img *image.RGBA, x, y int, col color.RGBA, c float64) {
	i := img.PixOffset(x, y)
	px := img.Pix[i : i+3 : i+3]
	px[0] = uint8(float64(px[0]) + (float64(col.R)-float64(px[0]))*c + 0.5)
	px[1] = uint8(float64(px[1]) + (float64(col.G)-float64(px[1]))*c + 0.5)
	px[2] = uint8(float64(px[2]) + (float64(col.B)-float64(px[2]))*c + 0.5)
}

// badge işaretin zemini: beyaz daire + renkli halka.
func (p *painter) badge(c pt, r float64, col color.RGBA) {
	p.fill(col, circle(c, r))
	p.fill(colBox, circle(c, r-1.5*renderScale))
}

// text dikeyde cy'ye ortalı yazar.
func (p *painter) text(f font.Face, s string, x, cy float64, col color.RGBA) {
	m := f.Metrics()
	base := cy + float64(m.Ascent-m.Descent)/128 // (ascent-descent)/2, 26.6
	d := font.Drawer{Dst: p.dst, Src: image.NewUniform(col), Face: f, Dot: fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.Int26_6(base * 64)}}
	d.DrawString(s)
}

func measure(f font.Face, s string) float64 { return float64(font.MeasureString(f, s)) / 64 }

// fitText metni genişliğe sığdırır; sığmazsa sonunu "…" ile kısaltır.
func fitText(f font.Face, s string, w float64) string {
	if measure(f, s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 1 {
		r = r[:len(r)-1]
		t := strings.TrimRight(string(r), " ") + "…"
		if measure(f, t) <= w {
			return t
		}
	}
	return "…"
}

// ccw çokgeni pozitif alanlı yöne çevirir (birleşimde yönler aynı olmalı).
func ccw(poly []pt) []pt {
	a := 0.0
	for i := range poly {
		j := (i + 1) % len(poly)
		a += poly[i].x*poly[j].y - poly[j].x*poly[i].y
	}
	if a < 0 {
		for i, j := 0, len(poly)-1; i < j; i, j = i+1, j-1 {
			poly[i], poly[j] = poly[j], poly[i]
		}
	}
	return poly
}

func circle(c pt, r float64) []pt {
	n := 16 + int(r/2)
	poly := make([]pt, n)
	for i := range poly {
		a := 2 * math.Pi * float64(i) / float64(n)
		poly[i] = pt{c.x + r*math.Cos(a), c.y + r*math.Sin(a)}
	}
	return ccw(poly)
}

// segment a-b arası w kalınlıkta düz uçlu çizgi.
func segment(a, b pt, w float64) []pt {
	u := unit(a, b)
	n := pt{-u.y * w / 2, u.x * w / 2}
	return ccw([]pt{{a.x + n.x, a.y + n.y}, {b.x + n.x, b.y + n.y}, {b.x - n.x, b.y - n.y}, {a.x - n.x, a.y - n.y}})
}

// stroke yuvarlak uçlu çizgi (işaret çizgileri).
func stroke(a, b pt, w float64) [][]pt {
	return [][]pt{segment(a, b, w), circle(a, w/2), circle(b, w/2)}
}

func tick(c pt, r, w float64) [][]pt {
	p1, p2, p3 := pt{c.x - r, c.y}, pt{c.x - r*0.3, c.y + r*0.7}, pt{c.x + r, c.y - r*0.65}
	return append(stroke(p1, p2, w), stroke(p2, p3, w)...)
}

func cross(c pt, r, w float64) [][]pt {
	return append(stroke(pt{c.x - r, c.y - r}, pt{c.x + r, c.y + r}, w), stroke(pt{c.x - r, c.y + r}, pt{c.x + r, c.y - r}, w)...)
}

// arrow c merkezli, u yönünü gösteren dolu ok başı.
func arrow(c, u pt, r float64) []pt {
	n := pt{-u.y, u.x}
	tip := pt{c.x + u.x*r, c.y + u.y*r}
	back := pt{c.x - u.x*r*0.6, c.y - u.y*r*0.6}
	return ccw([]pt{tip, {back.x + n.x*r*0.5, back.y + n.y*r*0.5}, {back.x - n.x*r*0.5, back.y - n.y*r*0.5}})
}

func roundRect(x, y, w, h, r float64) []pt {
	var poly []pt
	corners := []struct{ cx, cy, a0 float64 }{
		{x + w - r, y + r, -math.Pi / 2}, {x + w - r, y + h - r, 0}, {x + r, y + h - r, math.Pi / 2}, {x + r, y + r, math.Pi},
	}
	const steps = 8
	for _, c := range corners {
		for i := 0; i <= steps; i++ {
			a := c.a0 + math.Pi/2*float64(i)/steps
			poly = append(poly, pt{c.cx + r*math.Cos(a), c.cy + r*math.Sin(a)})
		}
	}
	return ccw(poly)
}
