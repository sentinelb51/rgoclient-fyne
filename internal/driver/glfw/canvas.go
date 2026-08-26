package glfw

import (
	"image"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/internal"
	"fyne.io/fyne/v2/internal/app"
	"fyne.io/fyne/v2/internal/build"
	"fyne.io/fyne/v2/internal/driver"
	"fyne.io/fyne/v2/internal/driver/common"
	"fyne.io/fyne/v2/theme"
)

// Declare conformity with Canvas interface
var _ fyne.Canvas = (*glCanvas)(nil)

type glCanvas struct {
	common.Canvas

	content fyne.CanvasObject
	menu    fyne.CanvasObject
	padded  bool
	size    fyne.Size

	onTypedRune func(rune)
	onTypedKey  func(*fyne.KeyEvent)
	onKeyDown   func(*fyne.KeyEvent)
	onKeyUp     func(*fyne.KeyEvent)
	// shortcut    fyne.ShortcutHandler

	scale, detectedScale, texScale float32

	context         driver.WithContext
	webExtraWindows *container.MultipleWindows

	damage internal.DamageRegion // RGOClient patch: reused each frame, paint only
}

func (c *glCanvas) Capture() image.Image {
	var img image.Image
	c.context.(*window).RunWithContext(func() {
		img = c.Painter().Capture(c)
	})
	return img
}

func (c *glCanvas) Content() fyne.CanvasObject {
	return c.content
}

func (c *glCanvas) DismissMenu() bool {
	if c.menu != nil && c.menu.(*MenuBar).IsActive() {
		c.menu.(*MenuBar).Toggle()
		return true
	}
	return false
}

func (c *glCanvas) InteractiveArea() (fyne.Position, fyne.Size) {
	return fyne.Position{}, c.Size()
}

func (c *glCanvas) MinSize() fyne.Size {
	return c.canvasSize(c.content.MinSize())
}

func (c *glCanvas) OnKeyDown() func(*fyne.KeyEvent) {
	return c.onKeyDown
}

func (c *glCanvas) OnKeyUp() func(*fyne.KeyEvent) {
	return c.onKeyUp
}

func (c *glCanvas) OnTypedKey() func(*fyne.KeyEvent) {
	return c.onTypedKey
}

func (c *glCanvas) OnTypedRune() func(rune) {
	return c.onTypedRune
}

func (c *glCanvas) Padded() bool {
	return c.padded
}

func (c *glCanvas) PixelCoordinateForPosition(pos fyne.Position) (int, int) {
	multiple := c.scale * c.texScale
	scaleInt := func(x float32) int {
		return int(math.Round(float64(x * multiple)))
	}

	return scaleInt(pos.X), scaleInt(pos.Y)
}

func (c *glCanvas) Resize(size fyne.Size) {
	// This might not be the ideal solution, but it effectively avoid the first frame to be blurry due to the
	// rounding of the size to the loower integer when scale == 1. It does not affect the other cases as far as we tested.
	// This can easily be seen with fyne/cmd/hello and a scale == 1 as the text will happear blurry without the following line.
	var nearestSize fyne.Size
	if c.scale == 1 {
		nearestSize = fyne.NewSize(float32(math.Ceil(float64(size.Width))), float32(math.Ceil(float64(size.Height))))
	} else {
		nearestSize = size
	}

	c.size = nearestSize

	if c.webExtraWindows != nil {
		c.webExtraWindows.Resize(size)
	}
	for _, overlay := range c.Overlays().List() {
		overlay.Resize(nearestSize)
	}

	content := c.content
	contentSize := c.contentSize(nearestSize)
	contentPos := c.contentPos()
	menu := c.menu
	menuHeight := c.menuHeight()

	content.Resize(contentSize)
	content.Move(contentPos)

	if menu != nil {
		menu.Refresh()
		menu.Resize(fyne.NewSize(nearestSize.Width, menuHeight))
	}
}

func (c *glCanvas) Scale() float32 {
	return c.scale
}

func (c *glCanvas) SetContent(content fyne.CanvasObject) {
	newSize := c.size.Max(c.canvasSize(content.MinSize()))

	c.setContent(content)

	c.Resize(newSize)
	c.SetDamageAll() // RGOClient patch: a swapped root moves nothing it can diff
	c.SetDirty()
}

func (c *glCanvas) SetOnKeyDown(typed func(*fyne.KeyEvent)) {
	c.onKeyDown = typed
}

func (c *glCanvas) SetOnKeyUp(typed func(*fyne.KeyEvent)) {
	c.onKeyUp = typed
}

func (c *glCanvas) SetOnTypedKey(typed func(*fyne.KeyEvent)) {
	c.onTypedKey = typed
}

func (c *glCanvas) SetOnTypedRune(typed func(rune)) {
	c.onTypedRune = typed
}

func (c *glCanvas) SetPadded(padded bool) {
	c.padded = padded

	c.content.Move(c.contentPos())
}

func (c *glCanvas) reloadScale() {
	w := c.context.(*window)
	windowVisible := w.visible
	if !windowVisible {
		return
	}

	c.scale = w.calculatedScale()
	// RGOClient patch: a scale change redraws every pixel while moving no object,
	// which the damage diff cannot see.
	c.SetDamageAll()
	c.SetDirty()

	c.context.RescaleContext()
}

func (c *glCanvas) Size() fyne.Size {
	return c.size
}

func (c *glCanvas) ToggleMenu() {
	if c.menu != nil {
		c.menu.(*MenuBar).Toggle()
	}
}

func (c *glCanvas) buildMenu(w *window, m *fyne.MainMenu) {
	c.setMenuOverlay(nil)
	if m == nil {
		return
	}
	if build.HasNativeMenu {
		setupNativeMenu(w, m)
	} else {
		c.setMenuOverlay(buildMenuOverlay(m, w))
	}
}

// canvasSize computes the needed canvas size for the given content size
func (c *glCanvas) canvasSize(contentSize fyne.Size) fyne.Size {
	canvasSize := contentSize.Add(fyne.NewSize(0, c.menuHeight()))
	if c.Padded() {
		return canvasSize.Add(fyne.NewSquareSize(theme.Padding() * 2))
	}
	return canvasSize
}

func (c *glCanvas) contentPos() fyne.Position {
	contentPos := fyne.NewPos(0, c.menuHeight())
	if c.Padded() {
		return contentPos.Add(fyne.NewSquareOffsetPos(theme.Padding()))
	}
	return contentPos
}

func (c *glCanvas) contentSize(canvasSize fyne.Size) fyne.Size {
	contentSize := fyne.NewSize(canvasSize.Width, canvasSize.Height-c.menuHeight())
	if c.Padded() {
		return contentSize.Subtract(fyne.NewSquareSize(theme.Padding() * 2))
	}
	return contentSize
}

func (c *glCanvas) menuHeight() float32 {
	if c.menu == nil {
		return 0 // no menu or native menu -> does not consume space on the canvas
	}

	return c.menu.MinSize().Height
}

func (c *glCanvas) overlayChanged() {
	c.SetDirty()
}

func (c *glCanvas) paint(size fyne.Size) {
	if c.Content() == nil {
		return
	}

	// RGOClient patch: repaint only what changed. ComputeDamage diffs every
	// mounted object against where it last painted; a partial frame restores the
	// previous frame from the painter's snapshot, then clears and repaints each
	// damaged rect under a root clip, and the snapshot absorbs those rects
	// before the swap. The debug overlay draws for every walked object, so it
	// forces the full path.
	full := build.Mode == fyne.BuildDebug || !fyne.PartialRepaint()
	if full {
		c.ResetDamage() // tracking re-enabled must not diff against stale rects
	} else {
		var dt time.Time
		if frameTiming {
			dt = time.Now()
		}
		c.ComputeDamage(size, &c.damage)
		if frameTiming {
			ftFrameDamage = time.Since(dt)
		}
		full = c.damage.Full()
	}
	if frameTiming {
		ftFrameFull = full
	}
	if !full && !c.Painter().RestorePreviousFrame() {
		full = true
	}

	if full {
		c.Painter().Clear()
		c.paintTree(size, nil)
		c.Painter().SnapshotFrame(nil, true)
		return
	}

	rects := c.damage.Rects()
	for i := range rects {
		c.paintTree(size, &rects[i])
	}
	c.Painter().SnapshotFrame(rects, false)
}

// paintTree walks and paints every tree. With a damage rect it paints just that
// region: the rect is the root clip, so the painter's rect test culls the draw
// calls and the scissor bounds the clear and every pixel — a nested scroll clip
// intersects it on the way down (ClipStack.Push). RGOClient patch: the damage
// parameter and its clip; nil is upstream's whole-canvas paint.
func (c *glCanvas) paintTree(size fyne.Size, damage *internal.PaintRect) {
	clips := &internal.ClipStack{}
	if damage != nil {
		inner := clips.Push(damage.Pos, damage.Size)
		c.Painter().StartClipping(inner.Rect())
		c.Painter().Clear()
	}

	paint := func(node *common.RenderCacheNode, pos fyne.Position) {
		obj := node.Obj()
		if driver.IsClip(obj) {
			inner := clips.Push(pos, obj.Size())
			c.Painter().StartClipping(inner.Rect())
		}
		if size.Width <= 0 || size.Height <= 0 { // iconifying on Windows can do bad things
			return
		}
		c.Painter().Paint(obj, pos, size, clips.Top())
	}
	afterPaint := func(node *common.RenderCacheNode, pos fyne.Position) {
		if driver.IsClip(node.Obj()) {
			clips.Pop()
			if top := clips.Top(); top != nil {
				c.Painter().StartClipping(top.Rect())
			} else {
				c.Painter().StopClipping()
			}
		}

		if build.Mode == fyne.BuildDebug {
			c.DrawDebugOverlay(node.Obj(), pos, size, clips.Top())
		}
	}
	c.WalkTrees(paint, afterPaint)

	if damage != nil {
		clips.Pop()
		c.Painter().StopClipping()
	}
}

func (c *glCanvas) setContent(content fyne.CanvasObject) {
	c.content = content
	c.SetContentTreeAndFocusMgr(content)
}

func (c *glCanvas) setMenuOverlay(b fyne.CanvasObject) {
	c.menu = b
	c.SetMenuTreeAndFocusMgr(b)

	if c.menu != nil && !c.size.IsZero() {
		c.content.Resize(c.contentSize(c.size))
		c.content.Move(c.contentPos())

		c.menu.Refresh()
		c.menu.Resize(fyne.NewSize(c.size.Width, c.menu.MinSize().Height))
	}
}

func (c *glCanvas) applyThemeOutOfTreeObjects() {
	if c.menu != nil {
		app.ApplyThemeTo(c.menu, c) // Ensure our menu gets the theme change message as it's out-of-tree
	}

	c.SetPadded(c.padded) // refresh the padding for potential theme differences
}

func newCanvas() *glCanvas {
	c := &glCanvas{scale: 1.0, texScale: 1.0, padded: true}
	connectKeyboard(c)
	c.Initialize(c, c.overlayChanged)
	c.EnableDamageTracking() // RGOClient patch
	c.setContent(&canvas.Rectangle{FillColor: theme.Color(theme.ColorNameBackground)})
	return c
}
