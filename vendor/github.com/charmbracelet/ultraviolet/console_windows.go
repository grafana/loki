//go:build windows
// +build windows

package uv

func newConsole(in File, out File, env Environ) *WinCon {
	c := &console{
		input:   in,
		output:  out,
		environ: env,
	}
	return &WinCon{console: c}
}
