package internal

import (
	"math"

	"fyne.io/fyne/v2"
)

// Damage regions. An RGOClient patch, not upstream API — see PATCHES.md.
//
// A PaintRect is a region of a canvas in canvas units. A DamageRegion
// accumulates the rectangles a frame has to repaint, merging them so the count
// stays bounded: painting is a scissored pass per rectangle, so the cap is a
// cap on passes, and a merge trades pixels for a walk.

// PaintRect is an axis-aligned region of a canvas in canvas units.
type PaintRect struct {
	Pos  fyne.Position
	Size fyne.Size
}

// Empty reports whether the rect covers no pixels.
func (r PaintRect) Empty() bool {
	return r.Size.Width <= 0 || r.Size.Height <= 0
}

// Union returns the smallest rect covering both r and o.
func (r PaintRect) Union(o PaintRect) PaintRect {
	if r.Empty() {
		return o
	}
	if o.Empty() {
		return r
	}
	x1 := fyne.Min(r.Pos.X, o.Pos.X)
	y1 := fyne.Min(r.Pos.Y, o.Pos.Y)
	x2 := fyne.Max(r.Pos.X+r.Size.Width, o.Pos.X+o.Size.Width)
	y2 := fyne.Max(r.Pos.Y+r.Size.Height, o.Pos.Y+o.Size.Height)

	return PaintRect{Pos: fyne.NewPos(x1, y1), Size: fyne.NewSize(x2-x1, y2-y1)}
}

func (r PaintRect) area() float32 {
	if r.Empty() {
		return 0
	}

	return r.Size.Width * r.Size.Height
}

// clamp returns r cut down to the canvas bound.
func (r PaintRect) clamp(bound fyne.Size) PaintRect {
	x1 := fyne.Max(r.Pos.X, 0)
	y1 := fyne.Max(r.Pos.Y, 0)
	x2 := fyne.Min(r.Pos.X+r.Size.Width, bound.Width)
	y2 := fyne.Min(r.Pos.Y+r.Size.Height, bound.Height)

	return PaintRect{Pos: fyne.NewPos(x1, y1), Size: fyne.NewSize(x2-x1, y2-y1)}
}

// maxDamageRects caps how many scissored paint passes a frame may take. Adding
// past the cap merges into whichever existing rect wastes the least area.
const maxDamageRects = 4

// DamageRegion collects the rectangles one frame has to repaint. The zero value
// is ready after Reset; the backing array is reused so a frame adds nothing to
// the heap.
type DamageRegion struct {
	rects [maxDamageRects]PaintRect
	count int
	full  bool
	bound fyne.Size
}

// Reset empties the region and sets the canvas bound rects are clamped to.
func (d *DamageRegion) Reset(bound fyne.Size) {
	d.count = 0
	d.full = false
	d.bound = bound
}

// SetFull marks the whole canvas damaged. Rects already added are kept but
// irrelevant.
func (d *DamageRegion) SetFull() {
	d.full = true
}

// Full reports whether the whole canvas must repaint.
func (d *DamageRegion) Full() bool {
	return d.full
}

// Rects returns the damaged rectangles. Meaningless when Full.
func (d *DamageRegion) Rects() []PaintRect {
	return d.rects[:d.count]
}

// Coverage reports how much of the canvas the region covers, 0 to 1, counting
// overlap twice — it is a promotion heuristic, not a measurement.
func (d *DamageRegion) Coverage() float32 {
	total := d.bound.Width * d.bound.Height
	if d.full {
		return 1
	}
	if total <= 0 {
		return 0
	}

	var sum float32
	for i := 0; i < d.count; i++ {
		sum += d.rects[i].area()
	}

	return sum / total
}

// Add records a damaged rect, clamped to the bound. A rect overlapping or
// adjacent to one already held merges for free; past the cap it merges into
// whichever rect the union wastes least on.
func (d *DamageRegion) Add(r PaintRect) {
	if d.full {
		return
	}
	r = r.clamp(d.bound)
	if r.Empty() {
		return
	}

	// A union no larger than its parts is free: the rects overlapped or lined up.
	for i := 0; i < d.count; i++ {
		u := d.rects[i].Union(r)
		if u.area() <= d.rects[i].area()+r.area() {
			d.rects[i] = u
			d.coalesce(i)
			return
		}
	}

	if d.count < maxDamageRects {
		d.rects[d.count] = r
		d.count++
		return
	}

	best, bestWaste := 0, float32(math.MaxFloat32)
	for i := 0; i < d.count; i++ {
		u := d.rects[i].Union(r)
		waste := u.area() - d.rects[i].area() - r.area()
		if waste < bestWaste {
			best, bestWaste = i, waste
		}
	}
	d.rects[best] = d.rects[best].Union(r)
	d.coalesce(best)
}

// coalesce re-merges rects a grown rect i now overlaps, repeating until stable.
func (d *DamageRegion) coalesce(i int) {
	for {
		merged := false
		for j := 0; j < d.count; j++ {
			if j == i {
				continue
			}
			u := d.rects[i].Union(d.rects[j])
			if u.area() <= d.rects[i].area()+d.rects[j].area() {
				d.rects[i] = u
				d.count--
				d.rects[j] = d.rects[d.count]
				if i == d.count {
					i = j
				}
				merged = true
				break
			}
		}
		if !merged {
			return
		}
	}
}
