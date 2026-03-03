//go:build windows
// +build windows

package uv

import (
	"fmt"
	"os"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/windows"
)

func makeRaw(inTty, outTty term.File) (inTtyState, outTtyState *term.State, err error) {
	if inTty == nil || outTty == nil || !term.IsTerminal(inTty.Fd()) || !term.IsTerminal(outTty.Fd()) {
		return nil, nil, ErrNotTerminal
	}

	// Save stdin state and enable VT inpu
	// We also need to enable VT input here.
	inTtyState, err = term.MakeRaw(inTty.Fd())
	if err != nil {
		return nil, nil, fmt.Errorf("error making terminal raw: %w", err)
	}

	// Enable VT input
	var imode uint32
	if err := windows.GetConsoleMode(windows.Handle(inTty.Fd()), &imode); err != nil {
		return nil, nil, fmt.Errorf("error getting console mode: %w", err)
	}

	if err := windows.SetConsoleMode(windows.Handle(inTty.Fd()), imode|windows.ENABLE_VIRTUAL_TERMINAL_INPUT); err != nil {
		return nil, nil, fmt.Errorf("error setting console mode: %w", err)
	}

	// Save output screen buffer state and enable VT processing.
	outTtyState, err = term.GetState(outTty.Fd())
	if err != nil {
		return nil, nil, fmt.Errorf("error getting terminal state: %w", err)
	}

	var omode uint32
	if err := windows.GetConsoleMode(windows.Handle(outTty.Fd()), &omode); err != nil {
		return nil, nil, fmt.Errorf("error getting console mode: %w", err)
	}

	if err := windows.SetConsoleMode(windows.Handle(outTty.Fd()),
		omode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|
			windows.DISABLE_NEWLINE_AUTO_RETURN); err != nil {
		return nil, nil, fmt.Errorf("error setting console mode: %w", err)
	}

	return inTtyState, outTtyState, nil
}

func getSize(_, outTty term.File) (w, h int, err error) {
	if outTty != nil {
		return term.GetSize(outTty.Fd()) //nolint:wrapcheck
	}
	return 0, 0, ErrNotTerminal
}

func getWinsize(inTty, outTty term.File) (ws Winsize, err error) {
	w, h, err := getSize(inTty, outTty)
	return Winsize{Col: uint16(w), Row: uint16(h)}, err
}

func optimizeMovements(*term.State) (useTabs, useBspace bool) {
	return supportsBackspace(0), supportsHardTabs(0)
}

func supportsBackspace(uint64) bool {
	return true
}

func supportsHardTabs(uint64) bool {
	return true
}

func startWinch(_, _ term.File) (chan os.Signal, error) {
	return nil, ErrPlatformNotSupported
}

func stopWinch(chan os.Signal) {
}
