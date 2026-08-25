package gl

import (
	"math"

	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/internal"
)

// The frame snapshot. RGOClient patch — see PATCHES.md.
//
// The previous frame is kept as a framebuffer-sized texture so a repaint can
// redraw only what changed: the snapshot is drawn back first, covering every
// pixel, the damaged regions are cleared and painted on top, and the same
// regions are copied back into the texture. Everything here is
// CopyTexSubImage2D and a textured quad — the calls the blur already makes —
// so no backend needs a framebuffer object or an extension check, and the
// backbuffer being undefined after a swap never matters: every present redraws
// every pixel.

// RestorePreviousFrame draws the previous frame back into the framebuffer.
// It reports false when there is nothing valid to restore — first frame, a
// resize, a lost snapshot — in which case the caller must paint the whole
// canvas. The scissor must be off when this is called.
func (p *painter) RestorePreviousFrame() bool {
	if !p.snapshotValid || p.snapW != p.fbWidth || p.snapH != p.fbHeight ||
		p.fbWidth <= 0 || p.fbHeight <= 0 {
		return false
	}

	// A full-frame quad through the texture program, exact because the texture
	// is the framebuffer's size. The framebuffer's bottom row sits at v=0, so v
	// follows NDC y and no flip is needed.
	points := []float32{
		// x, y, z, u, v
		-1, -1, 0, 0, 0,
		-1, 1, 0, 0, 1,
		1, -1, 0, 1, 0,
		1, 1, 0, 1, 1,
	}

	p.ctx.UseProgram(p.program.ref)
	p.updateBuffer(p.program.buff, points)
	p.UpdateVertexArray(p.program, "vert", 3, 5, 0)
	p.UpdateVertexArray(p.program, "vertTexCoord", 2, 5, 3)

	p.SetUniform1f(p.program, "cornerRadius", 0)
	p.SetUniform2f(p.program, "size", float32(p.fbWidth), float32(p.fbHeight))
	p.SetUniform4f(p.program, "inset", 0, 0, 1, 1)
	p.SetUniform1f(p.program, "alpha", 1.0)

	// Replace outright: the snapshot is a finished frame, blending it over
	// whatever the backbuffer holds would be wrong for translucent pixels.
	p.ctx.BlendFunc(one, zero)
	p.logError()

	p.ctx.ActiveTexture(texture0)
	p.ctx.BindTexture(texture2D, p.snapshotTex)
	p.logError()

	p.ctx.DrawArrays(triangleStrip, 0, 4)
	p.logError()

	return true
}

// SnapshotFrame copies the framebuffer into the snapshot texture — the whole of
// it when full, otherwise just the regions this frame repainted. Must run after
// painting and before the swap, with the scissor off.
func (p *painter) SnapshotFrame(regions []internal.PaintRect, full bool) {
	if p.fbWidth <= 0 || p.fbHeight <= 0 {
		return
	}

	fresh := false
	if p.snapW != p.fbWidth || p.snapH != p.fbHeight {
		if p.snapW != 0 {
			p.ctx.DeleteTexture(p.snapshotTex)
		}
		p.snapshotTex = p.newTexture(canvas.ImageScalePixels)
		p.ctx.TexImage2D(texture2D, 0, p.fbWidth, p.fbHeight, colorFormatRGBA, unsignedByte, nil)
		p.logError()
		p.snapW, p.snapH = p.fbWidth, p.fbHeight
		fresh = true
	}

	p.ctx.ActiveTexture(texture0)
	p.ctx.BindTexture(texture2D, p.snapshotTex)

	if full || fresh {
		p.ctx.CopyTexSubImage2D(texture2D, 0, 0, 0, 0, 0, p.fbWidth, p.fbHeight)
		p.logError()
		p.snapshotValid = true
		return
	}

	for _, r := range regions {
		x0, y0, w, h := p.pixelRegion(r)
		if w <= 0 || h <= 0 {
			continue
		}
		p.ctx.CopyTexSubImage2D(texture2D, 0, x0, y0, x0, y0, w, h)
	}
	p.logError()
	p.snapshotValid = true
}

// pixelRegion converts a canvas-unit rect to framebuffer pixels, rounded
// outward and clamped, with y measured from the framebuffer's bottom.
func (p *painter) pixelRegion(r internal.PaintRect) (x, y, w, h int) {
	x0 := int(math.Floor(float64(r.Pos.X * p.pixScale)))
	y0 := int(math.Floor(float64(r.Pos.Y * p.pixScale)))
	x1 := int(math.Ceil(float64((r.Pos.X + r.Size.Width) * p.pixScale)))
	y1 := int(math.Ceil(float64((r.Pos.Y + r.Size.Height) * p.pixScale)))

	x0 = max(x0, 0)
	y0 = max(y0, 0)
	x1 = min(x1, p.fbWidth)
	y1 = min(y1, p.fbHeight)

	return x0, p.fbHeight - y1, x1 - x0, y1 - y0
}
