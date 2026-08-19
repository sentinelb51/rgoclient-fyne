package glfw

import (
	"runtime"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/internal/app"
	"fyne.io/fyne/v2/internal/async"
	"fyne.io/fyne/v2/internal/cache"
	"fyne.io/fyne/v2/internal/driver/common"
	"fyne.io/fyne/v2/internal/painter"
	"fyne.io/fyne/v2/internal/scale"
)

type funcData struct {
	f    func()
	done chan struct{} // Zero allocation signalling channel
}

// channel for queuing functions on the main thread
var (
	funcQueue        = async.NewUnboundedChan[funcData]()
	running, drained atomic.Bool

	// RGOClient patch: funcs queued but not yet run. An unbounded channel forwards
	// from In to Out on a goroutine of its own, so the wake posted at enqueue can
	// reach the loop before the func does. The count is what tells the loop to
	// wait on the channel rather than on the OS queue, which nothing else will
	// wake for it.
	pendingFuncs atomic.Int64
)

// Arrange that main.main runs on main thread.
func init() {
	runtime.LockOSThread()
	async.SetMainGoroutine()
}

// force a function f to run on the main thread
func runOnMain(f func()) {
	runOnMainWithWait(f, true)
}

// force a function f to run on the main thread and specify if we should wait for it to return
func runOnMainWithWait(f func(), wait bool) {
	// If we are on main before app run just execute - otherwise add it to the main queue and wait.
	// We also need to run it as-is if the app is in the process of shutting down as the queue will be stopped.
	if (!running.Load() && async.IsMainGoroutine()) || drained.Load() {
		f()
		return
	}

	// RGOClient patch: the loop is blocked on the OS event queue, so queueing is
	// not enough on its own — wakeLoop is what ends the wait it is blocked in.
	if wait {
		done := common.DonePool.Get()
		defer common.DonePool.Put(done)

		pendingFuncs.Add(1)
		funcQueue.In() <- funcData{f: f, done: done}
		wakeLoop()
		<-done
	} else {
		pendingFuncs.Add(1)
		funcQueue.In() <- funcData{f: f}
		wakeLoop()
	}
}

func decideRepaint(visible, ready bool, checkDirtyAndClear func() bool) bool {
	return visible && ready && checkDirtyAndClear()
}

func (d *gLDriver) drawSingleFrame() {
	refreshed := false
	for _, win := range d.windowList() {
		w := win.(*window)
		if w.closing {
			continue
		}

		if decideRepaint(w.visible, w.frame.ready(), w.canvas.CheckDirtyAndClear) {
			w.RunWithContext(func() {
				if w.driver.repaintWindow(w) {
					refreshed = true
				}
			})
			w.updateAccessibility()
		} else {
			w.markCacheAlive()
		}
	}
	cache.Clean(refreshed)
}

func (w *window) markCacheAlive() {
	threshold := time.Now().Add(10*time.Second - cache.ValidDuration)
	if w.lastWalkedTime.Before(threshold) {
		w.canvas.WalkTrees(nil, func(node *common.RenderCacheNode, _ fyne.Position) {
			_ = cache.GetCanvasForObject(node.Obj())
			if wid, ok := node.Obj().(fyne.Widget); ok {
				_, _ = cache.CachedRenderer(wid)
			}
		})
		w.lastWalkedTime = time.Now()
	}
}

func (d *gLDriver) applyThemeToWindow(w fyne.Window) {
	if win, ok := w.(*window); ok {
		win.setDarkMode()
	}
}

func (d *gLDriver) runGL() {
	if !running.CompareAndSwap(false, true) {
		return // Run was called twice.
	}

	d.init()
	if d.trayStart != nil {
		d.trayStart()
	}

	fyne.CurrentApp().Settings().AddListener(func(set fyne.Settings) {
		painter.ClearFontCache()
		cache.ResetThemeCaches()
		app.ApplySettingsWithCallback(set, fyne.CurrentApp(), func(w fyne.Window) {
			d.applyThemeToWindow(w)
			c, ok := w.Canvas().(*glCanvas)
			if !ok {
				return
			}
			c.applyThemeOutOfTreeObjects()
			c.reloadScale()
		})
	})

	if f := fyne.CurrentApp().Lifecycle().(*app.Lifecycle).OnStarted(); f != nil {
		f()
	}

	// RGOClient patch: the loop waits on the OS event queue rather than polling it
	// on a ticker. waitEvents returns the moment an event lands, so the frame rate
	// caps how often the client draws instead of setting how often it looks — an
	// event no longer sits in the OS queue until the next tick collects it — and a
	// window with nothing to draw waits at idleWait instead of at the frame rate.
	//
	// Everything that gives the loop work either arrives on this goroutine, since
	// GLFW runs its callbacks inside the wait, or wakes it: runOnMainWithWait posts
	// an empty event after queueing, and Quit posts one after closing done.
	rate := fyne.FrameRate()
	next := time.Now()
	for {
		if d.drainFuncQueue() {
			return
		}

		if wanted := fyne.FrameRate(); wanted != rate {
			rate = wanted
			next = time.Now()
		}

		if time.Until(next) < waitResolution {
			d.pollEvents()
			for i := 0; i < len(d.windows); i++ {
				w := d.windows[i].(*window)
				if !w.mousePosUpdateProcessed {
					w.processMouseMoved(w.newMousePosX, w.newMousePosY)
					w.mousePosUpdateProcessed = true
				}

				if w.viewport == nil {
					continue
				}

				if w.viewport.ShouldClose() {
					d.destroyWindow(w, i)
					i-- // Trailing windows are moved forward one step.
					continue
				}

				expand := w.shouldExpand
				fullScreen := w.fullScreen

				if expand && !fullScreen {
					w.fitContent()
					shouldExpand := w.shouldExpand
					w.shouldExpand = false
					view := w.viewport

					if shouldExpand && runtime.GOOS != "js" {
						view.SetSize(w.shouldWidth, w.shouldHeight)
					}
				}
			}

			d.animation.TickAnimations()
			d.drawSingleFrame()

			// The deadline moves whether or not anything was painted, so a canvas
			// left dirty by a present gate that was not ready is retried a frame
			// later rather than spun on.
			next = time.Now().Add(frameInterval(rate))
		}

		wait := time.Until(next)
		if idleWait > wait && !d.framePending() {
			// Nothing is waiting to be drawn, so wait on the OS queue rather than on
			// the frame clock, and leave the deadline behind us: whatever arrives is
			// then drawn on the wakeup that carries it, not a frame after it.
			next = time.Now()
			wait = idleWait
		}

		if wait > 0 {
			// Never shorter than the OS wait can express: it rounds down, so a wait
			// under waitResolution returns at once and spins the loop until the
			// deadline passes. A frame rate that high is bounded by the frame itself.
			waitEvents(max(wait, waitResolution))
		}
	}
}

// drainFuncQueue runs everything queued for the main thread and reports whether
// the driver is shutting down, in which case the caller must return.
//
// RGOClient patch: this was a case of runGL's select. The loop can no longer
// block on a channel — it blocks in the OS event queue instead — so the queue is
// drained before each wait rather than waited on.
func (d *gLDriver) drainFuncQueue() bool {
	for {
		var f funcData

		if pendingFuncs.Load() > 0 {
			// A queued func has not reached the out channel yet. The forwarding
			// goroutine is a scheduler hop away and no OS event is coming, so wait
			// for it here rather than sleeping through it.
			select {
			case <-d.done:
				d.shutdown()
				return true
			case f = <-funcQueue.Out():
			}
		} else {
			select {
			case <-d.done:
				d.shutdown()
				return true
			case f = <-funcQueue.Out():
			default:
				return false
			}
		}

		pendingFuncs.Add(-1)
		f.f()
		if f.done != nil {
			f.done <- struct{}{}
		}
	}
}

func (d *gLDriver) shutdown() {
	d.Terminate()
	l := fyne.CurrentApp().Lifecycle().(*app.Lifecycle)
	if f := l.OnStopped(); f != nil {
		l.QueueEvent(f)
	}

	// as we are shutting down make sure we drain the pending funcQueue and close it out.
	for len(funcQueue.Out()) > 0 {
		f := <-funcQueue.Out()
		if f.done != nil {
			f.done <- struct{}{}
		}
	}
	drained.Store(true)
	funcQueue.Close()
}

// framePending reports whether anything is waiting to be drawn or dispatched.
//
// RGOClient patch: this is what lets the loop sleep on the OS queue instead of
// on the frame clock. A dirty canvas, an animation to tick and a mouse move to
// dispatch are all set on this goroutine, so a false answer cannot go stale
// while the loop is inside a wait.
func (d *gLDriver) framePending() bool {
	if d.animation.HasAnimations() {
		return true
	}

	for _, win := range d.windows {
		w := win.(*window)
		if w.closing || !w.visible {
			continue
		}

		if w.shouldExpand || !w.mousePosUpdateProcessed || w.canvas.IsDirty() {
			return true
		}

		if w.viewport != nil && w.viewport.ShouldClose() {
			return true
		}
	}

	return false
}

// frameInterval is one frame of the driver loop at the requested rate. The rate
// is clamped by fyne.SetFrameRate, so it is never zero here.
func frameInterval(fps int) time.Duration {
	return time.Second / time.Duration(fps)
}

// waitResolution is the granularity of the OS wait — a millisecond on Win32 and
// on X11, which is also one frame at the highest rate fyne.SetFrameRate allows.
// A deadline nearer than that counts as due: waiting for it would round down to
// no wait at all and spin the loop until it passed.
const waitResolution = time.Millisecond

// wakeLoop ends the wait the loop is in, so work queued for the main thread runs
// now rather than when the wait times out. Before Run it does nothing: there is
// no loop to wake and GLFW may not be initialised.
func wakeLoop() {
	if running.Load() {
		postEmptyEvent()
	}
}

func (d *gLDriver) destroyWindow(w *window, index int) {
	w.visible = false
	w.viewport.Destroy()
	w.destroy(d)

	if index < len(d.windows)-1 {
		copy(d.windows[index:], d.windows[index+1:])
	}
	d.windows[len(d.windows)-1] = nil
	d.windows = d.windows[:len(d.windows)-1]

	if len(d.windows) == 0 {
		d.Quit()
	}
}

func (d *gLDriver) repaintWindow(w *window) bool {
	canvas := w.canvas
	freed := false
	if canvas.EnsureMinSize() {
		w.shouldExpand = true
	}
	freed = canvas.FreeDirtyTextures() > 0

	updateGLContext(w)
	canvas.paint(canvas.Size())

	view := w.viewport
	visible := w.visible

	if view != nil && visible {
		w.applySwapInterval() // RGOClient patch: the GL context is current here and nowhere else
		w.frame.requestFrame()
		view.SwapBuffers()
	}

	// mark that we have walked the window and don't
	// need to walk it again to mark caches alive
	w.lastWalkedTime = time.Now()
	return freed
}

// refreshWindow requests that the specified window be redrawn
func refreshWindow(w *window) {
	w.canvas.SetDirty()
}

func updateGLContext(w *window) {
	canvas := w.canvas
	size := canvas.Size()

	// w.width and w.height are not correct if we are maximised, so figure from canvas
	winWidth := float32(scale.ToScreenCoordinate(canvas, size.Width)) * canvas.texScale
	winHeight := float32(scale.ToScreenCoordinate(canvas, size.Height)) * canvas.texScale

	canvas.Painter().SetFrameBufferScale(canvas.texScale)
	canvas.Painter().SetOutputSize(int(winWidth), int(winHeight))
}
