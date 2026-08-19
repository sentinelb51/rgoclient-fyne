//go:build wasm || (!windows && !linux && !freebsd && !openbsd && !netbsd) || (!windows && x11 && !wayland)

package glfw

func newPresentGate(_ *window) presentGate { return noGate{} }
