package ansi

import (
	"slices"
	"unicode"
	"unicode/utf8"
)

// zeroWidthTables are the categories whose runes don't advance the cursor:
// nonspacing and enclosing marks, and format characters such as ZWJ and the
// variation selectors.
var zeroWidthTables = []*unicode.RangeTable{unicode.Mn, unicode.Me, unicode.Cf}

// zeroWidthBMP is a bitset of the [zeroWidthTables] runes in the basic
// multilingual plane. Measuring runs a lookup per rune, and a bit test is a
// good deal cheaper than searching three range tables. Runes above the BMP are
// rare enough in terminal output to leave on the slow path.
var zeroWidthBMP [0x10000 / 64]uint64

// zeroWidthHigh holds the [zeroWidthTables] runes above the BMP, expanded and
// sorted so a lookup is one binary search.
var zeroWidthHigh []rune

func init() {
	for _, t := range zeroWidthTables {
		for _, r := range t.R16 {
			for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) {
				zeroWidthBMP[c>>6] |= 1 << (c & 63)
			}
		}
		for _, r := range t.R32 {
			// R32 range bounds are always valid runes.
			for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) { //nolint:gosec
				zeroWidthHigh = append(zeroWidthHigh, c)
			}
		}
	}
	slices.Sort(zeroWidthHigh)
}

// isZeroWidthHigh reports whether a rune above the BMP is zero width.
func isZeroWidthHigh(r rune) bool {
	_, ok := slices.BinarySearch(zeroWidthHigh, r)
	return ok
}

// wcRuneWidth returns the number of columns a single rune advances the cursor,
// i.e. wcwidth(3).
//
// Nonspacing and enclosing marks, along with format characters such as ZWJ and
// the variation selectors, never advance the cursor. [runewidth] reports some
// of them, most notably the Indic combining marks, as single width, so they're
// classified here instead.
func wcRuneWidth(r rune) int {
	switch {
	case r < 0x20, r >= 0x7f && r < 0xa0:
		// C0 and C1 controls, and DEL, don't print.
		return 0
	case r < 0x7f:
		// Printable ASCII. Everything above it can be East Asian Ambiguous,
		// so it has to go through runewidth.
		return 1
	case r < 0x10000:
		if zeroWidthBMP[r>>6]&(1<<(r&63)) != 0 {
			return 0
		}
	case isZeroWidthHigh(r):
		return 0
	}
	return wcOptions.RuneWidth(r)
}

// wcClusterWidth returns the number of columns a grapheme cluster occupies
// under [WcWidth], which is the sum of the widths of its codepoints.
//
// Terminals that don't implement Unicode core mode (DEC mode 2027) advance the
// cursor once per codepoint and know nothing about cluster boundaries. So a
// cluster can be wider than the glyph it draws: Unicode 15.1 merged Indic
// conjuncts into single clusters, and a terminal still puts "स्ते" in two
// columns, one per consonant. ZWJ emoji sequences behave the same way, with
// "👨‍👩‍👧‍👦" taking the eight columns of the four emoji it's built from.
//
// Measuring the cluster as a unit is [GraphemeWidth]'s job.
func wcClusterWidth[T string | []byte](cluster T) int {
	var width int
	switch c := any(cluster).(type) {
	case string:
		for _, r := range c {
			width += wcRuneWidth(r)
		}
	case []byte:
		// Ranging over string(c) would copy the slice, so decode in place.
		for len(c) > 0 {
			r, size := utf8.DecodeRune(c)
			width += wcRuneWidth(r)
			c = c[size:]
		}
	}
	return width
}
