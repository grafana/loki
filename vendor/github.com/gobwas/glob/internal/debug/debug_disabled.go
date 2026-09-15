//go:build !globdebug
// +build !globdebug

package debug

const Enabled = false

func Printf(f string, args ...any) {}
