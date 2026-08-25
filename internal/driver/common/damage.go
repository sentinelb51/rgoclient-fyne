package common

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/internal"
	paint "fyne.io/fyne/v2/internal/painter"
)

// Damage tracking. RGOClient patch — see PATCHES.md.
//
// The dirty flag says a frame is wanted; this file says where. Each frame diffs
// every mounted object against the rect it last painted at, so a move, a
// resize, an appearance, a disappearance and a Refresh all damage the right
// pixels without any of the exported Move/Hide call sites knowing. The store is
// written only by ComputeDamage, on the driver goroutine.

type objectExtent struct {
	rect internal.PaintRect
	gen  uint64
}

// EnableDamageTracking turns the diff on. Called once by a driver whose paint
// consumes ComputeDamage; a canvas without it pays nothing.
func (c *Canvas) EnableDamageTracking() {
	c.trackDamage = true
	c.lastRects = make(map[fyne.CanvasObject]objectExtent, 512)
	c.refreshedFrame = make(map[fyne.CanvasObject]struct{}, 64)
}

// SetDamageAll marks the next frame full-canvas: for changes that move no
// object and refresh nothing yet change every pixel — a scale or theme reload,
// a content swap.
func (c *Canvas) SetDamageAll() {
	c.damageAll = true
}

// ResetDamage forgets everything the diff knows, so tracking re-enabled after
// full-repaint frames starts from "all new" rather than from stale rects.
func (c *Canvas) ResetDamage() {
	clear(c.lastRects)
	clear(c.refreshedFrame)
}

// fullCoverage is the fraction of the canvas past which a region is promoted to
// a full repaint — per-rect passes each walk the tree, and near-full damage
// makes the restore of the previous frame pure overhead.
const fullCoverage = 0.8

// ComputeDamage walks the trees once, diffing each object's painted extent
// against the last frame's and folding in what was refreshed, then resolves the
// region for this frame. Objects gone from the tree damage where they last
// stood. Must run after FreeDirtyTextures has drained the refresh queue.
func (c *Canvas) ComputeDamage(bound fyne.Size, region *internal.DamageRegion) {
	region.Reset(bound)
	if !c.trackDamage {
		region.SetFull()
		return
	}
	full := c.damageAll
	c.damageAll = false

	c.rectGen++
	gen := c.rectGen
	c.WalkTrees(func(node *RenderCacheNode, pos fyne.Position) {
		obj := node.Obj()
		cur := paintExtent(obj, pos)
		entry, seen := c.lastRects[obj]
		switch {
		case !seen:
			region.Add(cur)
		case entry.rect != cur:
			region.Add(entry.rect)
			region.Add(cur)
		default:
			if _, refreshed := c.refreshedFrame[obj]; refreshed {
				region.Add(cur)
			}
		}
		c.lastRects[obj] = objectExtent{rect: cur, gen: gen}
	}, nil)

	for obj, entry := range c.lastRects {
		if entry.gen != gen {
			region.Add(entry.rect)
			delete(c.lastRects, obj)
		}
	}
	clear(c.refreshedFrame)

	if full || region.Coverage() >= fullCoverage {
		region.SetFull()
	}
}

// extentPad covers what every draw can spill past its rect: pixel rounding and
// the shader edge softness.
const extentPad = 2

// paintExtent is the rect an object's draw can touch — its bounds plus what its
// kind paints outside them. Primitives are walked individually, so a widget
// needs no padding of its own.
func paintExtent(obj fyne.CanvasObject, pos fyne.Position) internal.PaintRect {
	size := obj.Size()
	pad := float32(extentPad)
	switch t := obj.(type) {
	case *canvas.Rectangle:
		pad += shadowPad(t.Shadow)
	case *canvas.Circle:
		pad += shadowPad(t.Shadow)
	case *canvas.Ellipse:
		pad += shadowPad(t.Shadow)
	case *canvas.Line:
		pad += t.StrokeWidth/2 + 1
	case *canvas.Text:
		pad += fyne.Max(paint.VectorPad(t), paint.TextVectorPad)
	}

	return internal.PaintRect{
		Pos:  pos.AddXY(-pad, -pad),
		Size: fyne.NewSize(size.Width+2*pad, size.Height+2*pad),
	}
}

func shadowPad(s canvas.Shadow) float32 {
	if !paint.IsShadowVisible(s) {
		return 0
	}
	pads := paint.GetShadowPaddings(s)

	return fyne.Max(fyne.Max(pads[0], pads[1]), fyne.Max(pads[2], pads[3]))
}
