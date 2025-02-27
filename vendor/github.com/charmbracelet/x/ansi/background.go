package ansi

import (
	"fmt"
	"image/color"
)

// Colorizer is a [color.Color] interface that can be formatted as a string.
type Colorizer interface {
	color.Color
	fmt.Stringer
}

// HexColorizer is a [color.Color] that can be formatted as a hex string.
type HexColorizer struct{ color.Color }

var _ Colorizer = HexColorizer{}

// String returns the color as a hex string. If the color is nil, an empty
// string is returned.
func (h HexColorizer) String() string {
	if h.Color == nil {
		return ""
	}
	r, g, b, _ := h.RGBA()
	// Get the lower 8 bits
	r &= 0xff
	g &= 0xff
	b &= 0xff
	return fmt.Sprintf("#%02x%02x%02x", uint8(r), uint8(g), uint8(b)) //nolint:gosec
}

// XRGBColorizer is a [color.Color] that can be formatted as an XParseColor
// rgb: string.
//
// See: https://linux.die.net/man/3/xparsecolor
type XRGBColorizer struct{ color.Color }

var _ Colorizer = XRGBColorizer{}

// String returns the color as an XParseColor rgb: string. If the color is nil,
// an empty string is returned.
func (x XRGBColorizer) String() string {
	if x.Color == nil {
		return ""
	}
	r, g, b, _ := x.RGBA()
	// Get the lower 8 bits
	return fmt.Sprintf("rgb:%04x/%04x/%04x", r, g, b)
}

// XRGBAColorizer is a [color.Color] that can be formatted as an XParseColor
// rgba: string.
//
// See: https://linux.die.net/man/3/xparsecolor
type XRGBAColorizer struct{ color.Color }

var _ Colorizer = XRGBAColorizer{}

// String returns the color as an XParseColor rgba: string. If the color is nil,
// an empty string is returned.
func (x XRGBAColorizer) String() string {
	if x.Color == nil {
		return ""
	}
	r, g, b, a := x.RGBA()
	// Get the lower 8 bits
	return fmt.Sprintf("rgba:%04x/%04x/%04x/%04x", r, g, b, a)
}

// SetForegroundColor returns a sequence that sets the default terminal
// foreground color.
//
//	OSC 10 ; color ST
//	OSC 10 ; color BEL
//
// Where color is the encoded color number.
//
// See: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html#h3-Operating-System-Commands
func SetForegroundColor(c color.Color) string {
	var s string
	switch c := c.(type) {
	case Colorizer:
		s = c.String()
	case fmt.Stringer:
		s = c.String()
	default:
		s = HexColorizer{c}.String()
	}
	return "\x1b]10;" + s + "\x07"
}

// RequestForegroundColor is a sequence that requests the current default
// terminal foreground color.
//
// See: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html#h3-Operating-System-Commands
const RequestForegroundColor = "\x1b]10;?\x07"

// ResetForegroundColor is a sequence that resets the default terminal
// foreground color.
//
// See: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html#h3-Operating-System-Commands
const ResetForegroundColor = "\x1b]110\x07"

// SetBackgroundColor returns a sequence that sets the default terminal
// background color.
//
//	OSC 11 ; color ST
//	OSC 11 ; color BEL
//
// Where color is the encoded color number.
//
// See: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html#h3-Operating-System-Commands
func SetBackgroundColor(c color.Color) string {
	var s string
	switch c := c.(type) {
	case Colorizer:
		s = c.String()
	case fmt.Stringer:
		s = c.String()
	default:
		s = HexColorizer{c}.String()
	}
	return "\x1b]11;" + s + "\x07"
}

// RequestBackgroundColor is a sequence that requests the current default
// terminal background color.
//
// See: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html#h3-Operating-System-Commands
const RequestBackgroundColor = "\x1b]11;?\x07"

// ResetBackgroundColor is a sequence that resets the default terminal
// background color.
//
// See: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html#h3-Operating-System-Commands
const ResetBackgroundColor = "\x1b]111\x07"

// SetCursorColor returns a sequence that sets the terminal cursor color.
//
//	OSC 12 ; color ST
//	OSC 12 ; color BEL
//
// Where color is the encoded color number.
//
// See: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html#h3-Operating-System-Commands
func SetCursorColor(c color.Color) string {
	var s string
	switch c := c.(type) {
	case Colorizer:
		s = c.String()
	case fmt.Stringer:
		s = c.String()
	default:
		s = HexColorizer{c}.String()
	}
	return "\x1b]12;" + s + "\x07"
}

// RequestCursorColor is a sequence that requests the current terminal cursor
// color.
//
// See: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html#h3-Operating-System-Commands
const RequestCursorColor = "\x1b]12;?\x07"

// ResetCursorColor is a sequence that resets the terminal cursor color.
//
// See: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html#h3-Operating-System-Commands
const ResetCursorColor = "\x1b]112\x07"
