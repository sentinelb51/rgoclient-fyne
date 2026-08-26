//go:build !android && !ios && !mobile && !wasm && !test_web_driver

package gl

// GL state memoisation. RGOClient patch — see PATCHES.md.
//
// Every draw sets the same handful of context state — program, blend factors,
// buffer binding, texture unit and binding, attribute pointers — and each set
// is a cgo call whether or not the state moves. A frame is runs of the same
// kind of object (glyph run after glyph run), so most of those calls repeat
// what the context already holds. The painter remembers what it last applied
// and skips the repeat. Everything in the package goes through these wrappers;
// a raw p.ctx state call would go stale against the memo, which is why the
// deletes are wrapped too.
//
// The zero glState knows nothing, so the first use of anything applies it.
// resetState returns to that, for a context that was just (re)created.

type glState struct {
	program       Program
	programSet    bool
	blendSrc      uint32
	blendDst      uint32
	blendSet      bool
	buffer        Buffer
	bufferSet     bool
	textureUnit   uint32
	unitSet       bool
	textures      [8]Texture
	texturesSet   [8]bool
	attribProgram Program
	attribBuffer  Buffer
	attribSet     bool
}

func (p *painter) resetState() {
	p.state = glState{}
}

func (p *painter) useProgram(prog Program) {
	if p.state.programSet && p.state.program == prog {
		return
	}
	p.ctx.UseProgram(prog)
	p.state.program = prog
	p.state.programSet = true
}

func (p *painter) blendFunc(src, dst uint32) {
	if p.state.blendSet && p.state.blendSrc == src && p.state.blendDst == dst {
		return
	}
	p.ctx.BlendFunc(src, dst)
	p.state.blendSrc, p.state.blendDst = src, dst
	p.state.blendSet = true
}

func (p *painter) bindBuffer(buf Buffer) {
	if p.state.bufferSet && p.state.buffer == buf {
		return
	}
	p.ctx.BindBuffer(arrayBuffer, buf)
	p.state.buffer = buf
	p.state.bufferSet = true
}

func (p *painter) activeTexture(unit uint32) {
	if p.state.unitSet && p.state.textureUnit == unit {
		return
	}
	p.ctx.ActiveTexture(unit)
	p.state.textureUnit = unit
	p.state.unitSet = true
}

// bindTexture binds on the active unit. Texture bindings are per-unit state,
// so the memo is too; a unit past the memo's reach just always binds.
func (p *painter) bindTexture(texture Texture) {
	idx := -1
	if p.state.unitSet {
		if i := p.state.textureUnit - texture0; i < uint32(len(p.state.textures)) {
			idx = int(i)
		}
	}
	if idx >= 0 && p.state.texturesSet[idx] && p.state.textures[idx] == texture {
		return
	}
	p.ctx.BindTexture(texture2D, texture)
	if idx >= 0 {
		p.state.textures[idx] = texture
		p.state.texturesSet[idx] = true
	}
}

// deleteTexture forgets any binding of the texture as it deletes it: GL reverts
// a deleted texture's bindings to zero underneath the memo.
func (p *painter) deleteTexture(texture Texture) {
	for i := range p.state.textures {
		if p.state.texturesSet[i] && p.state.textures[i] == texture {
			p.state.texturesSet[i] = false
		}
	}
	p.ctx.DeleteTexture(texture)
}

// vertexStateChanged reports whether the attribute pointers must be re-pointed
// — the program/buffer pair moved since the last draw — and records the pair.
// The layout is constant per program and a pointer captures the buffer it was
// set against, so a repeated pair still reads the right data after BufferData.
func (p *painter) vertexStateChanged(prog Program, buf Buffer) bool {
	if p.state.attribSet && p.state.attribProgram == prog && p.state.attribBuffer == buf {
		return false
	}
	p.state.attribProgram = prog
	p.state.attribBuffer = buf
	p.state.attribSet = true
	return true
}
