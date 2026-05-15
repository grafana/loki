package uv

import (
	"github.com/charmbracelet/x/ansi"
)

// MouseMode represents the mouse tracking mode for the terminal.
type MouseMode uint8

// Mouse tracking modes.
//
// These determine which mouse events the terminal reports.
const (
	MouseModeNone   MouseMode = iota // Disable mouse tracking.
	MouseModePress                   // Press only (DEC mode 9). Reports button press events.
	MouseModeClick                   // Click tracking (DEC mode 1000). Reports button press and release.
	MouseModeDrag                    // Drag tracking (DEC mode 1002). Reports press, release, and drag.
	MouseModeMotion                  // Motion tracking (DEC mode 1003). Reports all mouse events including motion.
)

// MouseEncoding represents the encoding used for mouse events.
type MouseEncoding uint8

// Mouse encodings.
//
// These determine how mouse coordinates and buttons are encoded in the
// terminal's escape sequences. The encoding is only meaningful when mouse
// tracking is enabled via [MouseMode].
const (
	MouseEncodingLegacy MouseEncoding = iota // Legacy X10-compatible encoding. Coordinates limited to 223.
	MouseEncodingSGR                         // SGR encoding (DEC mode 1006). No coordinate limit, distinguishes press/release.

	// TODO: support these additional encodings in the future.
	// MouseEncodingUTF8                          // UTF-8 encoding (DEC mode 1005). Coordinates limited to 223.
	// MouseEncodingUrxvt                         // urxvt encoding (DEC mode 1015). No coordinate limit.
	// MouseEncodingSGRPixel                      // SGR-pixel encoding (DEC mode 1016). Reports pixel coordinates.
)

// MouseButton represents the button that was pressed during a mouse message.
type MouseButton = ansi.MouseButton

// Mouse event buttons
//
// This is based on X11 mouse button codes.
//
//	1 = left button
//	2 = middle button (pressing the scroll wheel)
//	3 = right button
//	4 = turn scroll wheel up
//	5 = turn scroll wheel down
//	6 = push scroll wheel left
//	7 = push scroll wheel right
//	8 = 4th button (aka browser backward button)
//	9 = 5th button (aka browser forward button)
//	10
//	11
//
// Other buttons are not supported.
const (
	MouseNone       = ansi.MouseNone
	MouseLeft       = ansi.MouseLeft
	MouseMiddle     = ansi.MouseMiddle
	MouseRight      = ansi.MouseRight
	MouseWheelUp    = ansi.MouseWheelUp
	MouseWheelDown  = ansi.MouseWheelDown
	MouseWheelLeft  = ansi.MouseWheelLeft
	MouseWheelRight = ansi.MouseWheelRight
	MouseBackward   = ansi.MouseBackward
	MouseForward    = ansi.MouseForward
	MouseButton10   = ansi.MouseButton10
	MouseButton11   = ansi.MouseButton11
)

// Mouse represents a Mouse message. Use [MouseEvent] to represent all mouse
// messages.
//
// The X and Y coordinates are zero-based, with (0,0) being the upper left
// corner of the terminal.
//
//	// Catch all mouse events
//	switch Event := Event.(type) {
//	case MouseEvent:
//	    m := Event.Mouse()
//	    fmt.Println("Mouse event:", m.X, m.Y, m)
//	}
//
//	// Only catch mouse click events
//	switch Event := Event.(type) {
//	case MouseClickEvent:
//	    fmt.Println("Mouse click event:", Event.X, Event.Y, Event)
//	}
type Mouse struct {
	X, Y   int
	Button MouseButton
	Mod    KeyMod
}

// String returns a string representation of the mouse message.
func (m Mouse) String() (s string) {
	if m.Mod.Contains(ModCtrl) {
		s += "ctrl+"
	}
	if m.Mod.Contains(ModAlt) {
		s += "alt+"
	}
	if m.Mod.Contains(ModShift) {
		s += "shift+"
	}

	str := m.Button.String()
	if str == "" {
		s += "unknown"
	} else if str != "none" { // motion events don't have a button
		s += str
	}

	return s
}
