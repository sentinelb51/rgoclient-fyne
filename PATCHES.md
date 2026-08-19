# The patches

Four, all under Fyne's `internal/` — which is the whole reason this fork
exists, since none of it is reachable from an importing module. Nothing else in
the tree is edited: `git diff upstream main` is exactly this list.

Every patch is marked `RGOClient patch` in the source, so
`git grep -n "RGOClient patch"` finds all of them, and each is one commit on
`main`.

## 1. A settable frame rate

`pacing.go` (new, package `fyne`) — `SetFrameRate` / `FrameRate`, an atomic the
driver reads.

`internal/driver/glfw/loop.go` — `runGL`'s ticker was the literal
`time.NewTicker(time.Second / 60)`. It now starts at `fyne.FrameRate()` and
re-reads it each tick, resetting when it has changed, so the setting applies
without a restart.

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

## Carrying them forward

`./update-fyne.sh vX.Y.Z` — see [README.md](README.md). The patches are small
and sit in code that rarely moves, but they are ours to carry. If upstream ever
exposes the frame rate or the swap interval, drop the matching patch rather than
keeping both.
