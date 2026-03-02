//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !aix && !windows
// +build !darwin,!dragonfly,!freebsd,!linux,!netbsd,!openbsd,!solaris,!aix,!windows

package uv

import "github.com/charmbracelet/x/term"

func makeRaw(_, _ term.File) (inTtyState, outTtyState *term.State, err error) {
	return nil, nil, ErrPlatformNotSupported
}

func getSize(_, _ term.File) (w, h int, err error) {
	return 0, 0, ErrPlatformNotSupported
}

func getWinsize(_, _ term.File) (ws Winsize, err error) {
	return Winsize{}, ErrPlatformNotSupported
}

func optimizeMovements(*term.State) (useTabs, useBspace bool) {
	return false, false
}
