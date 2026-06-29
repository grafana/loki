//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || aix
// +build darwin dragonfly freebsd linux netbsd openbsd solaris aix

package uv

import (
	"github.com/charmbracelet/x/term"
	"github.com/charmbracelet/x/termios"
)

func makeRaw(inTty, outTty term.File) (inTtyState, outTtyState *term.State, err error) {
	if inTty == nil && outTty == nil {
		return nil, nil, ErrNotTerminal
	}

	// Check if we have a terminal.
	for _, f := range []term.File{inTty, outTty} {
		if f == nil {
			continue
		}
		inTtyState, err = term.MakeRaw(f.Fd())
		if err == nil {
			break
		}
	}

	if err != nil {
		return nil, nil, err //nolint:wrapcheck
	}

	return inTtyState, outTtyState, nil
}

func getWinsize(inTty, outTty term.File) (ws Winsize, err error) {
	// Try both inTty and outTty to get the size.
	err = ErrNotTerminal
	for _, f := range []term.File{inTty, outTty} {
		if f == nil {
			continue
		}
		size, err := termios.GetWinsize(int(f.Fd()))
		if err == nil {
			return Winsize(*size), nil
		}
	}
	return
}

func getSize(inTty, outTty term.File) (w, h int, err error) {
	ws, err := getWinsize(inTty, outTty)
	return int(ws.Col), int(ws.Row), err
}

func optimizeMovements(state *term.State) (useTabs, useBspace bool) {
	return supportsHardTabs(uint64(state.Oflag)), supportsBackspace(uint64(state.Lflag)) //nolint:unconvert,nolintlint
}
