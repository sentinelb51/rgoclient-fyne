package glfw

import (
	"log"
	"os"
	"time"
)

// RGOClient instrumentation: RGO_FRAMETIME=1 logs a frame-cost split every 120
// painted frames — prep (min-size + texture free), damage (the diff walk),
// draw (GL calls), swap — plus how many frames were full repaints and the
// objects drawn per frame. Off, it costs one bool test per frame.
var frameTiming = os.Getenv("RGO_FRAMETIME") == "1"

type frameStats struct {
	frames, fullFrames, draws int
	prep, damage, draw, swap  time.Duration
	maxDraw                   time.Duration
}

var ftStats frameStats

// Set by glCanvas.paint while frameTiming, read by recordFrame on the same
// goroutine — the loop is the only painter.
var (
	ftFrameDamage time.Duration
	ftFrameFull   bool
)

func recordFrame(prep, paint, swap time.Duration, draws int) {
	s := &ftStats
	s.frames++
	s.prep += prep
	s.damage += ftFrameDamage
	draw := paint - ftFrameDamage
	s.draw += draw
	s.swap += swap
	s.draws += draws
	if ftFrameFull {
		s.fullFrames++
	}
	if draw > s.maxDraw {
		s.maxDraw = draw
	}
	ftFrameDamage, ftFrameFull = 0, false

	if s.frames < 120 {
		return
	}
	n := time.Duration(s.frames)
	log.Printf("frametime: %d frames (%d full) avg prep %v damage %v draw %v swap %v maxDraw %v draws/frame %d",
		s.frames, s.fullFrames, s.prep/n, s.damage/n, s.draw/n, s.swap/n, s.maxDraw, s.draws/s.frames)
	ftStats = frameStats{}
}
