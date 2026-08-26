//go:build android || ios || mobile || wasm || test_web_driver

package gl

// GL state pass-through. RGOClient patch — see state_desktop.go for the memo
// this stands in for: the web backend's handle types are not comparable, so
// mobile and web apply every call as upstream does.

type glState struct{}

func (p *painter) resetState() {}

func (p *painter) useProgram(prog Program) { p.ctx.UseProgram(prog) }

func (p *painter) blendFunc(src, dst uint32) { p.ctx.BlendFunc(src, dst) }

func (p *painter) bindBuffer(buf Buffer) { p.ctx.BindBuffer(arrayBuffer, buf) }

func (p *painter) activeTexture(unit uint32) { p.ctx.ActiveTexture(unit) }

func (p *painter) bindTexture(texture Texture) { p.ctx.BindTexture(texture2D, texture) }

func (p *painter) deleteTexture(texture Texture) { p.ctx.DeleteTexture(texture) }

func (p *painter) vertexStateChanged(prog Program, buf Buffer) bool { return true }
