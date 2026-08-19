package fyne

import "sync/atomic"

// Frame pacing. An RGOClient patch, not upstream API.
//
// Upstream polls the OS event queue on a ticker hard-coded at 60 Hz
// (internal/driver/glfw/loop.go) and calls glfw.SwapInterval only under Wayland
// (internal/driver/glfw/window_desktop.go). Both files are under internal/, so
// neither is reachable from an importing module; these two knobs are the seam
// the patch adds. See PATCHES.md.

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
	frameRate atomic.Int64
	vsync     atomic.Bool
)

func init() {
	frameRate.Store(DefaultFrameRate)
	vsync.Store(true)
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
