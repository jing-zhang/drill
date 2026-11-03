// Command icon renders the Drill app icon with no external tooling.
//
//	go run ./tools/icon
//
// It writes the production icon (build/appicon.png, which Wails also turns into
// the macOS .icns), a multi-size Windows .ico, and design candidates under
// docs/icon/ for review. Everything is drawn from code so the icon can be
// regenerated and reviewed in a diff.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

type rgb struct{ r, g, b uint8 }

var (
	// Background of the dark variants: a deep navy that matches the app chrome.
	bgTop    = rgb{0x1e, 0x27, 0x39}
	bgBottom = rgb{0x0d, 0x10, 0x16}

	// Accent ramp, borrowed from the UI (--accent #6aa8ff).
	accentLight = rgb{0x9c, 0xc6, 0xff}
	accent      = rgb{0x6a, 0xa8, 0xff}
	accentDark  = rgb{0x4a, 0x7f, 0xc9}

	accentTop    = rgb{0x6a, 0xa8, 0xff}
	accentBottom = rgb{0x3b, 0x68, 0xb0}

	ink       = rgb{0x0b, 0x0d, 0x12}
	paper     = rgb{0xee, 0xf2, 0xfa}
	barLight  = rgb{0xcf, 0xd8, 0xea}
	barMuted  = rgb{0x56, 0x60, 0x80}
	outlineCl = rgb{0x46, 0x50, 0x6a}
)

// productionVariant picks the mark used for build/appicon.png and the Windows
// .ico: 1 = list + play, 2 = checklist, 3 = check on an accent tile.
const productionVariant = 2

// Geometry lives in a 4096x4096 design space.
const design = 4096.0

// ---------------------------------------------------------------- primitives

func inRoundRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Min(math.Max(x, x0+r), x1-r)
	cy := math.Min(math.Max(y, y0+r), y1-r)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

func inTriangle(x, y, ax, ay, bx, by, cx, cy float64) bool {
	d1 := (x-bx)*(ay-by) - (ax-bx)*(y-by)
	d2 := (x-cx)*(by-cy) - (bx-cx)*(y-cy)
	d3 := (x-ax)*(cy-ay) - (cx-ax)*(y-ay)
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

// inCapsule reports whether the point is within halfWidth of segment a-b, which
// gives rounded strokes without any stroke machinery.
func inCapsule(x, y, ax, ay, bx, by, halfWidth float64) bool {
	dx, dy := bx-ax, by-ay
	lengthSq := dx*dx + dy*dy
	t := 0.0
	if lengthSq > 0 {
		t = ((x-ax)*dx + (y-ay)*dy) / lengthSq
		t = math.Max(0, math.Min(1, t))
	}
	px, py := ax+t*dx, ay+t*dy
	ex, ey := x-px, y-py
	return ex*ex+ey*ey <= halfWidth*halfWidth
}

func gradient(a, b rgb, y float64) rgb {
	t := math.Max(0, math.Min(1, y/design))
	lerp := func(from, to uint8) uint8 {
		return uint8(float64(from) + (float64(to)-float64(from))*t)
	}
	return rgb{lerp(a.r, b.r), lerp(a.g, b.g), lerp(a.b, b.b)}
}

// ------------------------------------------------------------------- drawing

func inBackground(x, y float64) bool {
	return inRoundRect(x, y, 112, 112, design-112, design-112, 880)
}

// Variant 1: an ordered list with a play affordance — "run the runbook".
func listAndPlay(x, y float64) (rgb, bool) {
	bars := []struct {
		top float64
		c   rgb
	}{
		{1240, accentLight},
		{1898, accent},
		{2556, accentDark},
	}
	for _, b := range bars {
		if inRoundRect(x, y, 780, b.top, 2200, b.top+300, 150) {
			return b.c, true
		}
	}
	if inTriangle(x, y, 2430, 1310, 2430, 2786, 3330, 2048) {
		return paper, true
	}
	return rgb{}, false
}

// Variant 2: a checklist with two items done and one still open.
func checklist(x, y float64) (rgb, bool) {
	rows := []struct {
		cy     float64
		fill   rgb
		bar    rgb
		barEnd float64
		done   bool
	}{
		{1250, accentLight, barLight, 3260, true},
		{2048, accent, barLight, 3260, true},
		{2846, ink, barMuted, 2900, false},
	}

	for _, r := range rows {
		boxX0, boxY0 := 780.0, r.cy-220
		boxX1, boxY1 := 1220.0, r.cy+220

		if inRoundRect(x, y, boxX0, boxY0, boxX1, boxY1, 120) {
			if r.done {
				if inCapsule(x, y, 900, r.cy-10, 1000, r.cy+100, 62) ||
					inCapsule(x, y, 1000, r.cy+100, 1120, r.cy-130, 62) {
					return ink, true
				}
				return r.fill, true
			}
			// Open item: a hollow box, so it reads as "not done yet".
			if !inRoundRect(x, y, boxX0+56, boxY0+56, boxX1-56, boxY1-56, 80) {
				return outlineCl, true
			}
		}
		if inRoundRect(x, y, 1420, r.cy-100, r.barEnd, r.cy+100, 100) {
			return r.bar, true
		}
	}
	return rgb{}, false
}

// Variant 3: a check on an accent tile. Boldest at small sizes.
func checkTile(x, y float64) (rgb, bool) {
	if inCapsule(x, y, 1160, 2140, 1730, 2700, 190) ||
		inCapsule(x, y, 1730, 2700, 2960, 1480, 190) {
		return paper, true
	}
	return rgb{}, false
}

// sample returns the colour covering one point, or ok=false for transparency.
func sample(variant int, x, y float64) (rgb, bool) {
	if !inBackground(x, y) {
		return rgb{}, false
	}
	switch variant {
	case 1:
		if c, ok := listAndPlay(x, y); ok {
			return c, true
		}
		return gradient(bgTop, bgBottom, y), true
	case 2:
		if c, ok := checklist(x, y); ok {
			return c, true
		}
		return gradient(bgTop, bgBottom, y), true
	case 3:
		if c, ok := checkTile(x, y); ok {
			return c, true
		}
		return gradient(accentTop, accentBottom, y), true
	}
	return rgb{}, false
}

// render draws one variant at size x size with 4x4 supersampling.
func render(variant, size int) *image.RGBA {
	const ss = 4
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	scale := design / float64(size)

	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var sr, sg, sb, sa float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) * scale
					y := (float64(py) + (float64(sy)+0.5)/ss) * scale
					if c, ok := sample(variant, x, y); ok {
						sr += float64(c.r)
						sg += float64(c.g)
						sb += float64(c.b)
						sa++
					}
				}
			}
			if sa == 0 {
				continue
			}
			img.Set(px, py, color.RGBA{
				R: uint8(sr/sa + 0.5),
				G: uint8(sg/sa + 0.5),
				B: uint8(sb/sa + 0.5),
				A: uint8(sa/float64(ss*ss)*255 + 0.5),
			})
		}
	}
	return img
}

// --------------------------------------------------------------------- files

func main() {
	if err := os.MkdirAll("docs/icon", 0o755); err != nil {
		log.Fatal(err)
	}

	// Design candidates, for review at a size you can actually see.
	for _, v := range []struct {
		n    int
		name string
	}{{1, "a-list-play"}, {2, "b-checklist"}, {3, "c-check-tile"}} {
		if err := writePNG("docs/icon/"+v.name+".png", render(v.n, 512)); err != nil {
			log.Fatal(err)
		}
	}

	// Production icon: the checklist mark (two steps done, one still open).
	if err := writePNG("build/appicon.png", render(productionVariant, 1024)); err != nil {
		log.Fatal(err)
	}

	sizes := []int{16, 32, 48, 64, 128, 256}
	entries := make([]icoEntry, 0, len(sizes))
	for _, s := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, render(productionVariant, s)); err != nil {
			log.Fatal(err)
		}
		entries = append(entries, icoEntry{size: s, data: buf.Bytes()})
	}
	if err := writeICO("build/windows/icon.ico", entries); err != nil {
		log.Fatal(err)
	}
	log.Println("wrote build/appicon.png, build/windows/icon.ico, docs/icon/*.png")
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	return f.Close()
}

type icoEntry struct {
	size int
	data []byte
}

// writeICO writes a PNG-compressed ICO. Windows Vista and later accept PNG
// payloads, and every size gets its own frame so it stays crisp in Explorer.
func writeICO(path string, entries []icoEntry) error {
	var buf bytes.Buffer
	headerLen := 6 + 16*len(entries)
	offset := headerLen

	binary.Write(&buf, binary.LittleEndian, uint16(0))            // reserved
	binary.Write(&buf, binary.LittleEndian, uint16(1))            // type: icon
	binary.Write(&buf, binary.LittleEndian, uint16(len(entries))) // image count

	for _, e := range entries {
		buf.WriteByte(byte(e.size % 256)) // 0 means 256
		buf.WriteByte(byte(e.size % 256))
		buf.WriteByte(0)                                    // palette size
		buf.WriteByte(0)                                    // reserved
		binary.Write(&buf, binary.LittleEndian, uint16(1))  // colour planes
		binary.Write(&buf, binary.LittleEndian, uint16(32)) // bits per pixel
		binary.Write(&buf, binary.LittleEndian, uint32(len(e.data)))
		binary.Write(&buf, binary.LittleEndian, uint32(offset))
		offset += len(e.data)
	}
	for _, e := range entries {
		buf.Write(e.data)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
