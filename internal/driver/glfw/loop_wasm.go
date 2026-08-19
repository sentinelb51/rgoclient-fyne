//go:build wasm || test_web_driver

package glfw

import (
	"time"

	"fyne.io/fyne/v2"

	"github.com/fyne-io/gl-js"
	"github.com/fyne-io/glfw-js"
)

func (d *gLDriver) initGLFW() {
	err := glfw.Init(gl.ContextWatcher)
	if err != nil {
		fyne.LogError("failed to initialise GLFW", err)
		return
	}
}

func (d *gLDriver) pollEvents() {
	glfw.PollEvents() // This call blocks while window is being resized, which prevents freeDirtyTextures from being called
}

// RGOClient patch: glfw-js has no WaitEventsTimeout and the browser owns the
// frame clock, so here the wait stays a sleep and the loop keeps the shape it
// had. idleWait is zero for the same reason: with nothing able to end the sleep
// early, extending it would only delay whatever was queued during one.
const idleWait = time.Duration(0)

func waitEvents(timeout time.Duration) {
	time.Sleep(timeout)
}

func postEmptyEvent() {}

func (d *gLDriver) Terminate() {
	glfw.Terminate()
}
