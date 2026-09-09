package widget

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/internal/cache"
)

type minSizeWidget struct {
	BaseWidget
}

func (w *minSizeWidget) CreateRenderer() fyne.WidgetRenderer {
	return NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

// BenchmarkBaseWidget_MinSize asks a tree's worth of mounted widgets for their
// minimum, which is what the driver does to every visible object on every dirty
// frame. The renderers are trivial, so what it measures is the lookup.
func BenchmarkBaseWidget_MinSize(b *testing.B) {
	const mounted = 250
	widgets := make([]fyne.Widget, mounted)
	for i := range widgets {
		w := &minSizeWidget{}
		w.ExtendBaseWidget(w)
		cache.Renderer(w) // as showing the widget would have
		widgets[i] = w
	}

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for _, w := range widgets {
			w.MinSize()
		}
	}
}
