# The patches

Three, all under Fyne's `internal/` — which is the whole reason this fork
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

## Carrying them forward

`./update-fyne.sh vX.Y.Z` — see [README.md](README.md). The patches are small
and sit in code that rarely moves, but they are ours to carry. If upstream ever
exposes the frame rate or the swap interval, drop the matching patch rather than
keeping both.
