//go:build !(windows || darwin || linux || openbsd || freebsd)

package gl

func (p *painter) updateBuffer(vbo Buffer, points []float32) {
	p.bindBuffer(vbo)
	p.logError()
	p.ctx.BufferSubData(arrayBuffer, points)
	p.logError()
}
