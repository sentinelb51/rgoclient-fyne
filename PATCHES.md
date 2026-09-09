# The patches

Thirteen. Most are under Fyne's `internal/`, which is the whole reason this fork
exists, since none of it is reachable from an importing module. The sixth,
seventh and eleventh are in exported code — `widget` and `canvas` — where the
work being skipped, or the decision being taken, sits inside a method an
importing module can call but not replace. Nothing else in the tree is edited:
`git diff upstream main` is exactly this list.

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

## 8. A dirty frame repaints only what changed

Upstream's dirty state is one bool for the whole window: any `Refresh()`
anywhere clears the framebuffer and redraws every visible object. In a chat
client almost every frame is a caret blink, a typing dot or a presence change —
a few hundred pixels paying for the window.

The patch splits *whether* from *where*. The dirty flag still decides whether a
frame runs; where is computed fresh each frame by diffing every mounted object
against the rect it last painted at:

- `internal/damage.go` — `PaintRect` and `DamageRegion`, the damage a frame has
  to repaint, merged to at most four rects (each is a scissored paint pass, so
  the cap is a cap on passes).
- `internal/driver/common/damage.go` — the diff. `ComputeDamage` walks the
  trees once: an object new to the walk damages its rect, one whose rect moved
  damages both, one that was refreshed (collected from the refresh queue as
  `FreeDirtyTextures` drains it — the same drain, one map insert) damages where
  it stands, and one gone from the walk damages where it stood. Moves and hides
  never say where they happened — the exported `repaint()` helpers only set the
  bool — and the diff is what makes that not matter. `paintExtent` pads each
  rect by what its kind draws outside its bounds: shadows, line stroke, text
  vector pads, and 2 units for pixel rounding and edge softness.
  `SetDamageAll` covers what moves nothing and refreshes nothing yet changes
  every pixel: a scale reload, a content swap. Damage at 80% coverage or more
  is promoted to a full repaint.
- `internal/painter/gl/snapshot.go` — the previous frame, kept as a
  framebuffer-sized texture. A partial frame draws it back first (a full-frame
  quad, blend replace), then clears and repaints each damaged rect, then
  `CopyTexSubImage2D`s those rects back into the texture before the swap.
  Everything is calls the blur already makes, so no backend needs a framebuffer
  object or an extension probe, and the backbuffer being undefined after a swap
  never matters: every present still writes every pixel, one textured quad
  instead of one draw call per object. `SetOutputSize` invalidates the snapshot
  when the framebuffer changes size, which is what makes a resize a full frame.
- `internal/driver/glfw/canvas.go` — `paint` decides. Each damaged rect is
  painted under a root `ClipItem`, so the painter's existing rect test culls the
  draw calls and the scissor bounds the clear; a scroll container's clip
  intersects it on the way down (`ClipStack.Push` already intersects with its
  parent). `BuildDebug` forces the full path — the debug overlay draws for
  every walked object.
- `pacing.go` — `SetPartialRepaint` / `PartialRepaint`, on by default. Off is
  the escape hatch for a rendering artifact and the honest baseline for
  measuring; turning it off resets the diff's memory so turning it back on
  starts from "all new" rather than from stale rects.

What a partial frame costs: one extra tree walk for the diff (~100µs at
RGOClient's mounted cap), a full-frame textured quad, and a damage-sized GPU
copy. What it saves: the clear, and every draw call outside the damage — a
caret blink is a restore quad plus a handful of draws instead of the whole
window. Scroll frames move a rect bigger than the coverage threshold and
promote themselves to full, which is exactly the frame they were before.

The mobile driver shares `common.Canvas` but never enables tracking, so it
keeps upstream's full repaint; the diff maps are only allocated where the glfw
canvas turns them on.

## 9. Frame-cost instrumentation

`internal/driver/glfw/frametime.go` (new), hooks in `loop.go` and `canvas.go`,
`internal/painter/gl/stats.go` (new, the drawn-object counter).

`RGO_FRAMETIME=1` logs, every 120 painted frames: average prep (the min-size
walk plus the refresh-queue drain), damage (the diff walk), draw (GL calls),
swap, the worst draw, how many frames were full repaints, and objects drawn per
frame. Off — the default — it costs one bool test per frame and an integer
increment per drawn object. It is what patch 10 was measured with; a claim
about frame cost starts here rather than at a guess.

## 10. Repeated GL state and per-frame cache churn skipped

Three separate costs, all paid per frame, none of which changes what is drawn:

- **GL state memoisation** — `internal/painter/gl/state_desktop.go` (new;
  `state_generic.go` is the mobile/web pass-through, the js handle types not
  being comparable). Every draw set program, blend factors, buffer binding,
  texture unit and attribute pointers whether or not they moved — each a cgo
  call. The painter now remembers what it last applied and skips the repeat;
  attribute pointers are re-pointed only when the program/buffer pair moves,
  the layout being constant per program. Every state call in the package goes
  through the wrappers — including the deletes, which forget what they delete —
  and `Init` resets the memo with the context. Draw phase on a message-column
  scroll: **~1.74 → ~1.43 µs of CPU per drawn object**.
- **Per-draw coordinate allocations** — `rectCoords`, `vecRectCoordsWithPad`
  and `lineCoords` each returned a fresh `[]float32` per object per frame. They
  now fill scratch slices owned by the painter (their own allocations: cgo
  rejects an interior pointer into a struct that holds Go pointers), the coords
  being uploaded before the next draw builds its own.
- **Per-frame cache churn** — `EnsureMinSize` called `SetCanvasForObject` for
  every mounted object on every dirty frame, and each call allocates a cache
  entry whether or not it stores it: ~57k allocations/s during a scroll at
  RGOClient's mounted count. It now checks `GetCanvasForObject` first, which
  also refreshes the entry's expiry — the blind store never did. And
  `FreeDirtyTextures` ranged every cached texture every painted frame looking
  for expired ones; expiry has minute granularity, so the sweep now runs at
  most once a second per canvas.

## 11. A caret that can stand still

`pacing.go` — `SetCursorBlink` / `CursorBlink`, on by default, which is what
upstream does.

`widget/entry_cursor_anim.go` — `start()` paints the caret once and returns
rather than running the animation.

Upstream ties the blink to `Settings().ShowAnimations()`: global, no exported
setter, and turning it off takes every other animation with it. Its
no-animations branch in `widget/entry.go` is also no use, because it colours the
caret from the *widget-scoped* theme where the animation reads the *application*
one — the difference RGOClient's `ui.WithCaret` is built on, a scoped theme being
the only way to colour a caret without colouring the focus ring it shares
`ColorNamePrimary` with. So the static caret is painted in `start()` from the
application theme instead of by letting that branch run.

An entry already focused when the knob moves keeps what it has until its next
refresh; there is no registry of live carets to walk.

## 12. One wake per drain, not one per queued func

`internal/driver/glfw/loop.go` — `wakePosted`, a claim `wakeLoop` takes with a
compare-and-swap and `runGL` releases at the one point it parks.

Patch 5 made every `DoFromGoroutine` post an empty event, because a loop blocked
in the OS event queue is not woken by a channel send. That post is a cgo call
into a Win32 `PostMessage` and it ends the wait, so *n* funcs queued before the
loop got to any of them cost *n* syscalls and *n* whole loop passes —
`pollEvents`, the mouse fixups, the damage check — to run work one pass would
have drained. `pendingFuncs` already makes the drain wait for a func that has
been counted, so the second post buys nothing.

Measured on a 200,000-func flood against an idle loop, Ryzen 9 9950X3D:
**1.39 s wall / 1.79 s CPU → 99 ms / 190 ms**, 8.9 µs → 0.94 µs of CPU per
queued func. On a gateway's shape — 3,000 bursts of 32 with a 1 ms gap — CPU
**1.31 s → 0.20 s**, 13.7 µs → 2.1 µs each.

**Where the claim is released is the whole patch, and it is not obvious.**
`pollEvents` consumes the post in the *same pass* that drains the func it was
posted for, so a claim released at the top of the loop leaves the rest of that
pass — the drain and the frame — as a window in which a producer finds the claim
still standing, skips its post, and is then waited straight through to
`idleWait`. That version measured **3% of enqueues stalled for 100 ms**, and is
what the comment in the source warns against. Released instead immediately
before `waitEvents` and followed by a re-read of `pendingFuncs`: a producer that
skipped its post has already counted itself there, and one that has not counted
itself yet finds the claim free and posts. `sync/atomic` is sequentially
consistent, so the store and the load cannot both miss — the park half of a
check-then-park.

`Quit` posts directly rather than through `wakeLoop`, so shutdown is never the
wake that gets coalesced away.

To re-prove it after a rebase: queue one func at a time against an idle loop and
time it. Correct is tens of microseconds; a lost wake is ~100 ms (`idleWait`), so
the failure is loud when it is looked for and silent otherwise. Do it with
several producer goroutines and with both arms of `runOnMainWithWait` — the
single-producer case passes even with the claim released in the wrong place.

## 13. The cache's clock is read once a frame, not once a lookup

`internal/cache/base.go` — `aliveNow`, an `atomic.Int64` of Unix nanoseconds
that `Clean` publishes, and `expiringCache.expires` narrowed from a `time.Time`
to the same.

`setAlive` stamped an entry with `time.Now()`, and it runs on every *lookup*:
`Renderer`, `CachedRenderer`, `GetCanvasForObject`, `GetTexture`,
`GetFontMetrics`, `GetSvg`. On RGOClient's tree that is thousands of calls per
frame, each one a vDSO read on Linux and a `QueryPerformanceCounter` on Windows.

Every lifetime in this package is `ValidDuration` — a minute unless `FYNE_CACHE`
says otherwise — and `Clean` already reads the clock once per paint event, at
the top, before it decides whether to do any work. So the stamp is that reading:
an entry set alive between two paints carries the earlier one, which is at most
one frame stale against a minute. Expiry is unchanged — `isExpired` still takes
the `now` its caller measured, and every caller is `Clean` itself.

Measured with RGOClient's `internal/app` virtual benchmarks (4-core Xeon,
software driver): `time.Now` was 3.2% of the client's CPU samples before and
absent after; a wheel tick over 250 mounted rows **~437 → ~355 µs**, a channel
open **~1.40 → ~1.20 ms**.

The one thing to keep on a rebase: `timeMock.setTime` in `base_test.go`
publishes to `aliveNow` as well as swapping `timeNow`, because it is standing in
for the paint loop that would have. Without that, every expiry test silently
measures the process start instead.

## Carrying them forward

`./update-fyne.sh vX.Y.Z` — see [README.md](README.md). The patches are small
and sit in code that rarely moves, but they are ours to carry. If upstream ever
exposes the frame rate or the swap interval, drop the matching patch rather than
keeping both.
