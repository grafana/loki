//go:build !windows
// +build !windows

package uv

func newConsole(in File, out File, env Environ) *TTY {
	c := &console{
		input:   in,
		output:  out,
		environ: env,
	}
	return &TTY{console: c}
}
