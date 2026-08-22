# The patches

Seven. The first five are under Fyne's `internal/`, which is the whole reason
this fork exists, since none of it is reachable from an importing module. The
last two are in exported code — `widget` and `canvas` — where the work being
skipped is inside a method an importing module can call but not replace.
Nothing else in the tree is edited: `git diff upstream main` is exactly this
list.

Every patch is marked `RGOClient patch` in the source, so
`git grep -n "RGOClient patch"` finds all of them, and each is one commit on
`main`.

## 1. A settable frame rate

`pacing.go` (new, package `fyne`) — `SetFrameRate` / `FrameRate`, an atomic the
driver reads.

`internal/driver/glfw/loop.go` — the driver's frame interval was the literal
`time.NewTicker(time.Second / 60)`. It comes from `fyne.FrameRate()` and is
re-read each frame, so the setting applies without a restart. Patch 5 replaced
the ticker itself; the rate is now what the loop's wait is bounded by.

## 2. Vsync off

`pacing.go` — `SetVSync` / `VSync`.

`internal/driver/glfw/window_desktop.go` — `applySwapInterval`, plus a
`swapInterval` field on `window` remembering what was last applied to that
window's context, and its initialisation at window creation.
`window_wasm.go` gets the same method as a no-op; the browser owns presentation.

`internal/driver/glfw/loop.go` — `repaintWindow` calls it just before
`SwapBuffers`. That is the only moment in the driver where the window's GL
context is current on our goroutine: `RunWithContext` detaches it on the way
out, and the `fyne.Do` queue runs outside it entirely. `glfw.SwapInterval`
applies to the current context, so there is nowhere else to put the call.

Wayland is left alone. Upstream turns vsync off there on purpose and the
compositor paces frames itself through `wl_surface.frame`.

## 3. The font cache

`internal/painter/font.go` — `loadMeasureFont` memoises the parsed face per font
resource (`measureFontCache`, keyed by name and content length, since
`fyne.Resource` is an interface and a non-comparable dynamic type would panic as
a map key). `ClearFontCache` clears it with the rest.

Without it, every `Refresh` reaching a widget under a scoped theme re-parsed
every font that theme names — Montserrat, NotoSans, InterSymbols and Fyne's
4.2 MB `EmojiOneColor.otf` — because `CachedFontFace` keys on `{style, scope}`
and `cache.OverrideTheme` mints a scope from a counter that never repeats. The
client mints one per entry, `ui.WithCaret` being the only way to colour a caret
without colouring the focus ring with it, so an open and close of a settings
number box cost about 6 MB that nothing ever released. A fresh scope now costs a
map entry.

## 4. A present gate for Windows

`internal/driver/glfw/present_windows.go` (new) — `vblankGate`, filling the
`presentGate` seam that upstream leaves as `noGate{}` (always ready) everywhere
but Wayland.

`internal/driver/glfw/present_notwayland.go` — build tags narrowed so Windows
takes the new file rather than the stub.

With vsync off the driver presented on every tick it had something to draw. DWM
composes a window once per vertical blank and keeps only the newest buffer it was
handed, so everything past that rate was drawn, uploaded and discarded. Measured
on a 540Hz panel at `FrameRate` 1000: 1000 presents/sec before, 540 after.

`DwmGetCompositionTimingInfo` is the documented way to ask when the compositor
wants a frame and it is not usable — on Windows 11 it fails with `0x88980090` for
every caller, windowed or not, with composition enabled. The kernel-mode path
beneath it still works, so the gate waits on the adapter's vertical blank
(`D3DKMTOpenAdapterFromHdc` + `D3DKMTWaitForVerticalBlankEvent`, both gdi32) on a
goroutine of its own, since the wait blocks and the loop goroutine is the whole
client.

The shape is the Wayland gate's: presenting arms a wait, the wait arriving makes
the window presentable again. Nothing waits while nothing draws, so an idle
window still costs zero wakeups.

Three things it will not do:

- **Pace to anything but the primary display.** The adapter is opened from
  `GetDC(NULL)`, because `newPresentGate` runs at window *creation*, before there
  is an HWND to ask which monitor the window is on. A window dragged onto a second
  monitor with its own refresh rate is paced by the first one's.
- **Gate while vsync is on.** The driver already blocks in `SwapBuffers` until the
  display is ready; a second pacer on top could only fight it.
- **Hold a window shut.** The wait cannot be cancelled, so `gateTimeout` (50ms)
  reports ready anyway, and a wait that fails marks the gate dead — it degrades to
  `noGate` rather than retrying a handle the display driver has invalidated.

## 5. The loop waits instead of polling

`internal/driver/glfw/loop.go` — `runGL` was a `select` over `d.done`, the main
thread's func queue and a ticker whose case called `glfw.PollEvents()`. It is now
a plain loop: drain the func queue, draw if the frame deadline has passed, then
block in `glfw.WaitEventsTimeout` until the next one.

`internal/driver/glfw/loop_desktop.go`, `loop_wasm.go` — `waitEvents`,
`postEmptyEvent` and `idleWait`, the three parts of that which are not the same
call on both platforms. glfw-js has no timed wait and the browser owns the frame
clock, so wasm keeps a sleep and an `idleWait` of zero.

What it buys:

- **Input is seen when it lands.** Polling left an event in the OS queue until the
  next tick collected it, so the frame rate set how often the client *looked* as
  much as how often it drew. The wait returns on the event itself and GLFW runs
  the callbacks from inside it, so only the drawing they ask for is paced.
- **An idle window costs ten wakeups a second rather than the frame rate's.** At
  `FrameRate` 1000 the loop woke a thousand times a second to find nothing dirty
  and draw nothing.

What it needs:

- `runOnMainWithWait` posts an empty event after queueing, and `Quit` posts one
  after closing `done`. Neither is an OS event, and the loop is inside the wait.
  `PostEmptyEvent` is one of the few GLFW calls documented as safe from any
  goroutine.
- `pendingFuncs`, a count of what has been queued and not yet run. `funcQueue` is
  an unbounded channel and forwards from `In` to `Out` on a goroutine of its own,
  so the wake can reach the loop before the func does; a positive count is what
  tells the drain to wait on the channel rather than go back to sleep through it.
- `framePending`, and the two read-only accessors it is built from —
  `common.Canvas.IsDirty` and `animation.Runner.HasAnimations`. The loop may only
  sleep past its frame deadline if nothing is waiting to be drawn. Both flags are
  set on the loop's own goroutine, so a `false` cannot go stale inside a wait.
- `idleWait` (100ms), the ceiling on that sleep, for what can reach the loop
  without waking it: an animation started off the main goroutine, a cache sweep
  come due. Nothing is painted when it expires unless something asks.

The deadline moves on every frame *attempt* rather than on every paint, so a
canvas still dirty because the present gate was not ready is retried a frame
later rather than spun on. Below `waitResolution` — a millisecond, which is both
the OS wait's granularity and one frame at the highest rate `SetFrameRate`
allows — a deadline counts as due, since waiting for it would round down to no
wait at all.

## 6. RichText re-wraps only when the width moves

`widget/richtext.go` — `Resize` called `Refresh()` for any change of size.
`Refresh` clears the min-size cache, re-runs `updateRowBounds` and marks the
canvas dirty; row bounds are wrapped against the width, so none of that is owed
to a height.

A virtualised column resizes each mounted row twice per settle — once at the
estimated height it is placed by, once at the height it measured — so every body
on screen was wrapped twice per settle and the window marked dirty for each.
Guarded on the width, RGOClient's channel open went from 635µs/324KB to
499µs/258KB and a prepended page of history from 334µs/133KB to 270µs/108KB.

Truncation is excluded: it drops the rows that do not fit, which is the one
thing in `lineBounds` that reads the height. So is the deprecated
`Wrapping: fyne.TextWrap(fyne.TextTruncateClip)`, which turns clipping on
underneath a `Truncation` of `Off`.

## 7. An icon parses its file once

`canvas/image.go` — `MinSize` refreshed when `i.Image == nil || i.aspect == 0`.
For an SVG resource `Image` stays nil until `renderSVG` rasterises it, which
needs a non-zero size, so an icon that is measured before it is laid out — or
never laid out at all — answered every `MinSize` by re-reading the file. All
`Refresh` leaves behind at that point is `i.aspect`, so the aspect alone is the
honest guard.

The driver asks every object in the tree for its minimum on every dirty frame,
which made this an XML parse per icon per frame. On RGOClient's message column
the frame walk went from 179µs and 61KB of garbage to 111µs and 8.4KB, and at
the mounted cap from 252µs/66KB to 187µs/13KB.

## Carrying them forward

`./update-fyne.sh vX.Y.Z` — see [README.md](README.md). The patches are small
and sit in code that rarely moves, but they are ours to carry. If upstream ever
exposes the frame rate or the swap interval, drop the matching patch rather than
keeping both.
