//go:build windows

package glfw

import (
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"fyne.io/fyne/v2"
)

// RGOClient patch: the present gate for Windows.
//
// Off Wayland the gate is noGate{} — always ready — so with vsync off the driver
// presents on every tick it has something to draw, however far that outruns the
// compositor. DWM composes a window once per vertical blank and keeps only the
// newest buffer it was handed, so presents beyond that rate are drawn, uploaded
// and thrown away.
//
// DwmGetCompositionTimingInfo is the documented way to ask when the compositor
// wants a frame, and it is not usable: on Windows 11 it fails with 0x88980090 for
// every caller, windowed or not, with composition enabled. The kernel-mode path
// beneath it still works, so this waits on the adapter's vertical blank instead.
//
// The gate is only active while vsync is off. With vsync on the driver already
// blocks inside SwapBuffers until the display is ready, and a second pacer on top
// of that could only fight it.

// gateTimeout is how long a vertical blank may fail to arrive before the gate
// reports ready anyway. D3DKMTWaitForVerticalBlankEvent cannot be cancelled, so
// this is what stops a display that has stopped blanking — asleep, mode
// switching, detached — from freezing the window with it. Three frames at 60Hz.
const gateTimeout = 50 * time.Millisecond

var (
	gdi32 = syscall.NewLazyDLL("gdi32.dll")

	procGetDC              = user32.NewProc("GetDC")
	procReleaseDC          = user32.NewProc("ReleaseDC")
	procOpenAdapterFromHdc = gdi32.NewProc("D3DKMTOpenAdapterFromHdc")
	procCloseAdapter       = gdi32.NewProc("D3DKMTCloseAdapter")
	procWaitForVBlank      = gdi32.NewProc("D3DKMTWaitForVerticalBlankEvent")
)

// d3dkmtOpenAdapterFromHdc is D3DKMT_OPENADAPTERFROMHDC. The LUID is split into
// its two halves so the struct needs no padding of its own.
type d3dkmtOpenAdapterFromHdc struct {
	hdc           uintptr
	adapter       uint32
	luidLow       uint32
	luidHigh      int32
	vidPnSourceID uint32
}

// d3dkmtWaitForVerticalBlankEvent is D3DKMT_WAITFORVERTICALBLANKEVENT. The device
// is left zero: the wait is on the adapter's presentation source, not on a device.
type d3dkmtWaitForVerticalBlankEvent struct {
	adapter       uint32
	device        uint32
	vidPnSourceID uint32
}

// d3dkmtCloseAdapter is D3DKMT_CLOSEADAPTER.
type d3dkmtCloseAdapter struct {
	adapter uint32
}

// vblankGate paces presentation to the display's vertical blank, in the shape the
// Wayland gate already has: presenting arms a wait, and the wait arriving is what
// makes the window presentable again. Nothing waits while nothing is drawing, so
// an idle window costs no wakeups.
type vblankGate struct {
	adapter       uint32
	vidPnSourceID uint32

	kick chan struct{}
	done chan struct{}
	stop sync.Once

	// waiting is set from the loop goroutine and cleared by the waiter, so ready()
	// never blocks. since stamps when it was set, for gateTimeout.
	waiting atomic.Bool
	since   atomic.Int64

	// dead is set when a wait fails. The gate cannot recover a handle the display
	// driver has invalidated, and a gate that reports not-ready forever is a frozen
	// window, so it degrades to noGate rather than retrying.
	dead atomic.Bool
}

func newPresentGate(*window) presentGate {
	// The adapter is the primary display's. A window dragged onto a second monitor
	// with its own refresh rate is paced by the first one's — reaching the window's
	// own monitor means its HWND, which does not exist yet when this is called.
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return noGate{}
	}
	defer procReleaseDC.Call(0, hdc)

	open := d3dkmtOpenAdapterFromHdc{hdc: hdc}
	if status, _, _ := procOpenAdapterFromHdc.Call(uintptr(unsafe.Pointer(&open))); status != 0 {
		return noGate{}
	}

	g := &vblankGate{
		adapter:       open.adapter,
		vidPnSourceID: open.vidPnSourceID,
		kick:          make(chan struct{}, 1),
		done:          make(chan struct{}),
	}

	go g.run()

	return g
}

// run waits for one vertical blank per present. It is its own goroutine because
// the wait is a blocking syscall and the loop goroutine is the whole client.
func (g *vblankGate) run() {
	wait := d3dkmtWaitForVerticalBlankEvent{adapter: g.adapter, vidPnSourceID: g.vidPnSourceID}

	for {
		select {
		case <-g.done:
			close := d3dkmtCloseAdapter{adapter: g.adapter}
			procCloseAdapter.Call(uintptr(unsafe.Pointer(&close)))
			return
		case <-g.kick:
			status, _, _ := procWaitForVBlank.Call(uintptr(unsafe.Pointer(&wait)))
			if status != 0 {
				g.dead.Store(true)
			}
			g.waiting.Store(false)
		}
	}
}

// ready reports whether the compositor has taken the last frame presented.
func (g *vblankGate) ready() bool {
	if !g.waiting.Load() {
		return true
	}

	return time.Since(time.Unix(0, g.since.Load())) > gateTimeout
}

// requestFrame arms the wait for the blank the frame about to be presented will
// be composed at.
func (g *vblankGate) requestFrame() {
	if g.dead.Load() || fyne.VSync() {
		return
	}
	if g.waiting.Swap(true) {
		return // a wait is already outstanding, and one blank answers both
	}

	g.since.Store(time.Now().UnixNano())

	select {
	case g.kick <- struct{}{}:
	default:
		g.waiting.Store(false) // the waiter is not listening; do not gate on it
	}
}

func (g *vblankGate) markReady() {
	g.waiting.Store(false)
}

func (g *vblankGate) free() {
	g.stop.Do(func() { close(g.done) })
}
