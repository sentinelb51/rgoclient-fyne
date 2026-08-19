//go:build windows

package glfw

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
)

// A vertical blank that never arrives must not hold the window shut: the wait
// cannot be cancelled, so gateTimeout is the only thing between a display that
// has stopped blanking and a client that never draws again.
func TestVBlankGateTimesOut(t *testing.T) {
	g := &vblankGate{kick: make(chan struct{}, 1), done: make(chan struct{})}

	if !g.ready() {
		t.Fatal("a gate with no wait outstanding must be ready")
	}

	g.waiting.Store(true)
	g.since.Store(time.Now().UnixNano())
	if g.ready() {
		t.Fatal("a gate waiting on a blank must not be ready")
	}

	g.since.Store(time.Now().Add(-2 * gateTimeout).UnixNano())
	if !g.ready() {
		t.Fatalf("a wait outstanding for more than %v must report ready anyway", gateTimeout)
	}
}

// A dead gate — its adapter handle invalidated by the display driver — must stop
// gating rather than keep a window it can no longer pace.
func TestVBlankGateDeadStopsGating(t *testing.T) {
	g := &vblankGate{kick: make(chan struct{}, 1), done: make(chan struct{})}
	g.dead.Store(true)

	g.requestFrame()

	if !g.ready() {
		t.Fatal("a dead gate must report ready")
	}
	if len(g.kick) != 0 {
		t.Fatal("a dead gate must not arm a wait")
	}
}

// With vsync on the driver blocks in SwapBuffers until the display is ready, so
// the gate must stand down rather than pace the same frames a second time.
func TestVBlankGateIdleUnderVSync(t *testing.T) {
	g := &vblankGate{kick: make(chan struct{}, 1), done: make(chan struct{})}

	restore := fyne.VSync()
	fyne.SetVSync(true)
	defer fyne.SetVSync(restore)

	g.requestFrame()

	if !g.ready() || len(g.kick) != 0 {
		t.Fatal("the gate must not arm a wait while vsync is pacing")
	}
}
