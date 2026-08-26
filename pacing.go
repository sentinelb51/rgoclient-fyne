package fyne

import "sync/atomic"

// Toolkit knobs. RGOClient patches, not upstream API.
//
// Each is a decision upstream takes somewhere an importing module cannot reach:
// the driver loop's hard-coded 60 Hz ticker and its Wayland-only
// glfw.SwapInterval, both under internal/, and widget.Entry's caret animation,
// which is unexported. These are the seam the patches add. See PATCHES.md.

// DefaultFrameRate is what the driver runs at until told otherwise, and what
// upstream hard-codes.
const DefaultFrameRate = 60

// The rate is clamped rather than refused: it reaches here from a settings file
// a person can edit, and a zero in that file would stop the driver loop.
const (
	minFrameRate = 1
	maxFrameRate = 1000
)

var (
	frameRate      atomic.Int64
	vsync          atomic.Bool
	partialRepaint atomic.Bool
	cursorBlink    atomic.Bool
)

func init() {
	frameRate.Store(DefaultFrameRate)
	vsync.Store(true)
	partialRepaint.Store(true)
	cursorBlink.Store(true)
}

// SetFrameRate sets how many times a second the desktop driver ticks animations
// and considers a repaint. It is a ceiling and not a rate: the loop waits on the
// OS event queue, so a window with nothing to draw costs nothing to leave open,
// and input is processed as it arrives rather than at this rate.
// Safe from any goroutine; applied on the next frame.
func SetFrameRate(fps int) {
	frameRate.Store(int64(min(max(fps, minFrameRate), maxFrameRate)))
}

// FrameRate is the ceiling SetFrameRate last set.
func FrameRate() int {
	return int(frameRate.Load())
}

// SetVSync decides whether a window waits for the display's next refresh before
// presenting. On, drawing is capped at the monitor's rate and the whole driver
// loop blocks inside SwapBuffers while it waits; off, a frame is presented as
// soon as it is drawn and may tear. Applied on each window's next repaint, and
// ignored under Wayland, where the compositor owns presentation and upstream
// already turns it off.
func SetVSync(enabled bool) {
	vsync.Store(enabled)
}

// VSync reports what SetVSync last set.
func VSync() bool {
	return vsync.Load()
}

// SetPartialRepaint decides whether the desktop driver repaints only the
// regions that changed since the previous frame, restoring the rest from a
// snapshot of it, or clears and repaints the whole window on any change as
// upstream does. On is the default; off is the escape hatch for a rendering
// artifact and the honest baseline for measuring. Safe from any goroutine;
// applied on the next frame.
func SetPartialRepaint(enabled bool) {
	partialRepaint.Store(enabled)
}

// PartialRepaint reports what SetPartialRepaint last set.
func PartialRepaint() bool {
	return partialRepaint.Load()
}

/* The entry caret */

// SetCursorBlink decides whether a focused entry's caret fades in and out or
// stands still. Upstream blinks whenever animations are on and offers no way to
// separate the two; off, the caret is painted once in the application theme's
// Primary and left alone. Safe from any goroutine; an entry already focused
// takes it on its next refresh.
func SetCursorBlink(enabled bool) {
	cursorBlink.Store(enabled)
}

// CursorBlink reports what SetCursorBlink last set.
func CursorBlink() bool {
	return cursorBlink.Load()
}
