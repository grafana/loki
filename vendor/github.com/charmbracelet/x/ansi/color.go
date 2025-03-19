package ansi

import (
	"image/color"
)

// Technically speaking, the 16 basic ANSI colors are arbitrary and can be
// customized at the terminal level. Given that, we're returning what we feel
// are good defaults.
//
// This could also be a slice, but we use a map to make the mappings very
// explicit.
//
// See: https://www.ditig.com/publications/256-colors-cheat-sheet
var lowANSI = map[uint32]uint32{
	0:  0x000000, // black
	1:  0x800000, // red
	2:  0x008000, // green
	3:  0x808000, // yellow
	4:  0x000080, // blue
	5:  0x800080, // magenta
	6:  0x008080, // cyan
	7:  0xc0c0c0, // white
	8:  0x808080, // bright black
	9:  0xff0000, // bright red
	10: 0x00ff00, // bright green
	11: 0xffff00, // bright yellow
	12: 0x0000ff, // bright blue
	13: 0xff00ff, // bright magenta
	14: 0x00ffff, // bright cyan
	15: 0xffffff, // bright white
}

// Color is a color that can be used in a terminal. ANSI (including
// ANSI256) and 24-bit "true colors" fall under this category.
type Color interface {
	color.Color
}

// BasicColor is an ANSI 3-bit or 4-bit color with a value from 0 to 15.
type BasicColor uint8

var _ Color = BasicColor(0)

const (
	// Black is the ANSI black color.
	Black BasicColor = iota

	// Red is the ANSI red color.
	Red

	// Green is the ANSI green color.
	Green

	// Yellow is the ANSI yellow color.
	Yellow

	// Blue is the ANSI blue color.
	Blue

	// Magenta is the ANSI magenta color.
	Magenta

	// Cyan is the ANSI cyan color.
	Cyan

	// White is the ANSI white color.
	White

	// BrightBlack is the ANSI bright black color.
	BrightBlack

	// BrightRed is the ANSI bright red color.
	BrightRed

	// BrightGreen is the ANSI bright green color.
	BrightGreen

	// BrightYellow is the ANSI bright yellow color.
	BrightYellow

	// BrightBlue is the ANSI bright blue color.
	BrightBlue

	// BrightMagenta is the ANSI bright magenta color.
	BrightMagenta

	// BrightCyan is the ANSI bright cyan color.
	BrightCyan

	// BrightWhite is the ANSI bright white color.
	BrightWhite
)

// RGBA returns the red, green, blue and alpha components of the color. It
// satisfies the color.Color interface.
func (c BasicColor) RGBA() (uint32, uint32, uint32, uint32) {
	ansi := uint32(c)
	if ansi > 15 {
		return 0, 0, 0, 0xffff
	}

	r, g, b := ansiToRGB(ansi)
	return toRGBA(r, g, b)
}

// ExtendedColor is an ANSI 256 (8-bit) color with a value from 0 to 255.
type ExtendedColor uint8

var _ Color = ExtendedColor(0)

// RGBA returns the red, green, blue and alpha components of the color. It
// satisfies the color.Color interface.
func (c ExtendedColor) RGBA() (uint32, uint32, uint32, uint32) {
	r, g, b := ansiToRGB(uint32(c))
	return toRGBA(r, g, b)
}

// TrueColor is a 24-bit color that can be used in the terminal.
// This can be used to represent RGB colors.
//
// For example, the color red can be represented as:
//
//	TrueColor(0xff0000)
type TrueColor uint32

var _ Color = TrueColor(0)

// RGBA returns the red, green, blue and alpha components of the color. It
// satisfies the color.Color interface.
func (c TrueColor) RGBA() (uint32, uint32, uint32, uint32) {
	r, g, b := hexToRGB(uint32(c))
	return toRGBA(r, g, b)
}

// ansiToRGB converts an ANSI color to a 24-bit RGB color.
//
//	r, g, b := ansiToRGB(57)
func ansiToRGB(ansi uint32) (uint32, uint32, uint32) {
	// For out-of-range values return black.
	if ansi > 255 {
		return 0, 0, 0
	}

	// Low ANSI.
	if ansi < 16 {
		h, ok := lowANSI[ansi]
		if !ok {
			return 0, 0, 0
		}
		r, g, b := hexToRGB(h)
		return r, g, b
	}

	// Grays.
	if ansi > 231 {
		s := (ansi-232)*10 + 8
		return s, s, s
	}

	// ANSI256.
	n := ansi - 16
	b := n % 6
	g := (n - b) / 6 % 6
	r := (n - b - g*6) / 36 % 6
	for _, v := range []*uint32{&r, &g, &b} {
		if *v > 0 {
			c := *v*40 + 55
			*v = c
		}
	}

	return r, g, b
}

// hexToRGB converts a number in hexadecimal format to red, green, and blue
// values.
//
//	r, g, b := hexToRGB(0x0000FF)
func hexToRGB(hex uint32) (uint32, uint32, uint32) {
	return hex >> 16 & 0xff, hex >> 8 & 0xff, hex & 0xff
}

// toRGBA converts an RGB 8-bit color values to 32-bit color values suitable
// for color.Color.
//
// color.Color requires 16-bit color values, so we duplicate the 8-bit values
// to fill the 16-bit values.
//
// This always returns 0xffff (opaque) for the alpha channel.
func toRGBA(r, g, b uint32) (uint32, uint32, uint32, uint32) {
	r |= r << 8
	g |= g << 8
	b |= b << 8
	return r, g, b, 0xffff
}
