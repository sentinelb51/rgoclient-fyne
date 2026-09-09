package cache

import (
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/internal/async"
)

var renderers async.Map[fyne.Widget, *RendererEntry]

type isBaseWidget interface {
	ExtendBaseWidget(fyne.Widget)
	super() fyne.Widget
}

// RendererEntry is one widget's place in the renderer cache.
//
// RGOClient patch: a widget may keep the entry it was handed and read the
// renderer back from it, which is a pointer load rather than a hash of an
// interface — see PATCHES.md. The map stays the registry, so expiry and
// destruction are unchanged; the entry only says whether it is still the one
// the map holds.
type RendererEntry struct {
	expiringCache
	renderer fyne.WidgetRenderer
	live     atomic.Bool
}

// Renderer returns the cached renderer while this entry is still the one the
// cache would hand out, and nil once it has been removed. A live entry is
// marked alive by the read, exactly as a map lookup would have marked it.
func (e *RendererEntry) Renderer() fyne.WidgetRenderer {
	if e == nil || !e.live.Load() {
		return nil
	}

	e.setAlive()
	return e.renderer
}

// destroy retires the entry, after which Renderer reports it as gone.
// The caller is responsible for removing it from the renderers map.
func (e *RendererEntry) destroy() {
	e.live.Store(false)
	e.renderer.Destroy()
}

// Renderer looks up the render implementation for a widget
// If one does not exist, it creates and caches a renderer for the widget.
func Renderer(wid fyne.Widget) fyne.WidgetRenderer {
	renderer, _ := RendererWithEntry(wid)
	return renderer
}

// RendererWithEntry is Renderer, returning the cache entry that backs the
// renderer alongside it so that a caller repeating the same lookup can hold on
// to it rather than hashing the widget interface again.
func RendererWithEntry(wid fyne.Widget) (fyne.WidgetRenderer, *RendererEntry) {
	if entry := cachedRendererEntry(wid); entry != nil {
		return entry.renderer, entry
	}
	if wid == nil {
		return nil, nil
	}

	entry := &RendererEntry{renderer: wid.CreateRenderer()}
	entry.live.Store(true)
	entry.setAlive()
	renderers.Store(wid, entry)

	return entry.renderer, entry
}

// CachedRenderer looks up the cached render implementation for a widget
// If a renderer does not exist in the cache, it returns nil, false.
func CachedRenderer(wid fyne.Widget) (fyne.WidgetRenderer, bool) {
	entry := cachedRendererEntry(wid)
	if entry == nil {
		return nil, false
	}

	return entry.renderer, true
}

// cachedRendererEntry returns the live cache entry for a widget, resolving an
// extended widget to the type that extends it first, or nil if there is none.
func cachedRendererEntry(wid fyne.Widget) *RendererEntry {
	if wid == nil {
		return nil
	}

	if wd, ok := wid.(isBaseWidget); ok {
		if wd.super() != nil {
			wid = wd.super()
		}
	}

	entry, ok := renderers.Load(wid)
	if !ok {
		return nil
	}

	entry.setAlive()
	return entry
}

// DestroyRenderer frees a render implementation for a widget.
// This is typically for internal use only.
func DestroyRenderer(wid fyne.Widget) {
	entry, ok := renderers.LoadAndDelete(wid)
	if !ok {
		return
	}
	if entry != nil {
		entry.destroy()
	}
	overrides.Delete(wid)
}

// IsRendered returns true of the widget currently has a renderer.
// One will be created the first time a widget is shown but may be removed after it is hidden.
func IsRendered(wid fyne.Widget) bool {
	_, found := renderers.Load(wid)
	return found
}
