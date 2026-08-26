package gl

// RGOClient instrumentation: objects drawn since the last take. Only the loop
// goroutine paints, so a plain int is safe.
var drawCount int

// TakeDrawCount returns the number of objects drawn since the last call and
// resets the count.
func TakeDrawCount() int {
	c := drawCount
	drawCount = 0
	return c
}
