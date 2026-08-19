//go:build !wasm && !test_web_driver

package glfw

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/internal/build"

	"github.com/go-gl/glfw/v3.4/glfw"
)

// platform values returned by forcePlatform to override GLFW's auto-detection.
const (
	platformAuto    = ""
	platformX11     = "x11"
	platformWayland = "wayland"
)

func (d *gLDriver) initGLFW() {
	switch forcePlatform() {
	case platformX11:
		glfw.InitHint(glfw.PlatformHint, int(glfw.PlatformX11))
	case platformWayland:
		glfw.InitHint(glfw.PlatformHint, int(glfw.PlatformWayland))
	}

	err := glfw.Init()
	if err != nil {
		fyne.LogError("failed to initialise GLFW", err)
		return
	}

	initCursors()
	if glfw.GetPlatform() == glfw.PlatformWayland {
		build.IsWayland = true
	}
}

func (d *gLDriver) pollEvents() {
	glfw.PollEvents() // This call blocks while window is being resized, which prevents freeDirtyTextures from being called
}

// RGOClient patch: the loop's wait on the OS event queue, and the two knobs it
// needs that are not the same call on every platform.

// idleWait is how long the loop sleeps with nothing to draw. It is not a frame
// clock — nothing is painted until something asks — but a ceiling on how long
// the loop can miss work that reached it without a wake: an animation started
// off the main goroutine, or a cache sweep come due.
const idleWait = 100 * time.Millisecond

// waitEvents blocks until an OS event arrives, an empty event is posted, or the
// timeout expires. GLFW runs its callbacks from here, so input is processed the
// moment it lands and only the drawing it asks for waits for the frame rate.
func waitEvents(timeout time.Duration) {
	glfw.WaitEventsTimeout(timeout.Seconds())
}

// postEmptyEvent ends a wait in progress. It is one of the few GLFW calls that
// is documented as safe from any goroutine.
func postEmptyEvent() {
	glfw.PostEmptyEvent()
}

func (d *gLDriver) Terminate() {
	glfw.Terminate()
}
