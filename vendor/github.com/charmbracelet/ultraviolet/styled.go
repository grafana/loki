package uv

import (
	"bytes"
	"image/color"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// StyledString is a string that can be decomposed into a series of styled
// lines and cells. It is used to disassemble a rendered string with ANSI
// escape codes into a series of cells that can be used in a [Buffer].
// A StyledString supports reading [ansi.SGR] and [ansi.Hyperlink] escape
// codes.
type StyledString struct {
	// Text is the original string that was used to create the styled string.
	Text string
	// Wrap determines whether the styled string should wrap to the next line.
	Wrap bool
	// Tail is the string that will be appended to the end of the line when the
	// string is truncated i.e. when [StyledString.Wrap] is false.
	Tail string
}

var _ Drawable = (*StyledString)(nil)

// NewStyledString creates a new [StyledString] for the given method and styled
// string. The method is used to calculate the width of each line.
func NewStyledString(str string) *StyledString {
	ss := new(StyledString)
	ss.Text = str
	return ss
}

// String returns the text of the styled string.
//
// It implements the [fmt.Stringer] interface.
func (s *StyledString) String() string {
	return s.Text
}

// Lines returns the styled string decomposed into a slice of [Line]s.
func (s *StyledString) Lines(m ansi.Method) []Line {
	return printString(nil, m, 0, 0, Rectangle{}, s.Text, false, "")
}

// Draw renders the styled string to the given buffer at the
// specified area.
func (s *StyledString) Draw(buf Screen, area Rectangle) {
	// Clear the area before drawing.
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			buf.SetCell(x, y, nil)
		}
	}
	str := s.Text
	// We need to normalize newlines "\n" to "\r\n" to emulate a raw terminal
	// output.
	str = strings.ReplaceAll(str, "\r\n", "\n")
	printString(buf, buf.WidthMethod(), area.Min.X, area.Min.Y, area, str, !s.Wrap, s.Tail)
}

// Height returns the number of lines in the styled string. This is the number
// of lines that the styled string will occupy when rendered to the screen.
func (s *StyledString) Height() int {
	return strings.Count(s.Text, "\n") + 1
}

// UnicodeWidth returns the cells width of the widest line in the styled string
// using the [ansi.GraphemeWidth] method.
func (s *StyledString) UnicodeWidth() int {
	w, _ := s.widthHeight(ansi.GraphemeWidth)
	return w
}

// WcWidth returns the cells width of the widest line in the styled string
// using the [ansi.WcWidth] method.
func (s *StyledString) WcWidth() int {
	w, _ := s.widthHeight(ansi.WcWidth)
	return w
}

func (s *StyledString) widthHeight(m ansi.Method) (w, h int) {
	lines := strings.Split(s.Text, "\n")
	h = len(lines)
	for _, l := range lines {
		w = max(w, m.StringWidth(l))
	}
	return
}

// Bounds returns the minimum area that can contain the whole styled string.
func (s *StyledString) Bounds() Rectangle {
	w, h := s.widthHeight(ansi.GraphemeWidth)
	return Rect(0, 0, w, h)
}

// printString draws a string starting at the given position. If s is nil, it
// will build and return a slice of [Line]s instead (unwrapped, ignoring bounds).
// passThrough reports whether a zero-width sequence should be carried in a
// cell's content and replayed to the terminal, rather than dropped.
//
// The string-type sequences carry private data rather than driving the cursor:
// APC for image protocols, DCS for device control, SOS and PM for whatever an
// application agrees with its terminal. One means it to reach the terminal, and
// a cell carrying one still advances by its own width, so the renderer's column
// model still holds.
//
// Everything else is dropped, as it was before:
//
//   - A CSI can move the cursor or erase part of the screen, and an ESC Fs
//     sequence can reset the terminal outright. Replaying one from inside a
//     cell would move the real cursor somewhere the model cannot see, which is
//     the drift a cell buffer exists to keep out.
//   - An OSC is dropped despite carrying data, because a cell is painted again
//     every time it changes and on every full repaint. A window title survives
//     that, but a clipboard write (OSC 52) or a notification (OSC 9) is not
//     something to fire again on each resize. Hyperlinks, the one OSC a cell
//     has a place for, are read into [Link] above.
func passThrough[T []byte | string](seq T) bool {
	if !ansi.HasApcPrefix(seq) && !ansi.HasDcsPrefix(seq) &&
		!ansi.HasSosPrefix(seq) && !ansi.HasPmPrefix(seq) {
		return false
	}
	return terminated(seq)
}

// terminated reports whether a string-type sequence ended with ST.
//
// The parser hands back whatever it has when the input runs out, so a string
// that stops mid-sequence still arrives here. Carrying an unterminated
// introducer into a cell would be worse than dropping it: the terminal would
// swallow everything painted after that cell, looking for an end that never
// comes.
//
// Only the two-byte ST is accepted. The one-byte C1 form, 0x9c, is also a
// UTF-8 continuation byte, so a sequence that ran out partway through a
// character such as U+071C ("\u071c", encoded dc 9c) ends in a byte
// indistinguishable from a terminator. The parser and the terminal need not
// resolve that ambiguity the same way, and guessing wrong reintroduces exactly
// the swallowing this guards against.
func terminated[T []byte | string](seq T) bool {
	n := len(seq)
	return n >= 2 && seq[n-1] == '\\' && seq[n-2] == ansi.ESC
}

func printString[T []byte | string](
	s Screen,
	m WidthMethod,
	x, y int,
	bounds Rectangle, str T,
	truncate bool, tail string,
) (lines []Line) {
	p := ansi.GetParser()
	defer ansi.PutParser(p)

	var tailc Cell
	if truncate && len(tail) > 0 {
		tailc = *NewCell(m, tail)
	}

	// A [WidthMethod] the ansi package doesn't know about falls back to
	// wcwidth, so the segmenter below always agrees with the decoder.
	decoder, method := ansi.DecodeSequenceWc[T], ansi.WcWidth
	if m == ansi.GraphemeWidth {
		decoder, method = ansi.DecodeSequence[T], ansi.GraphemeWidth
	}

	if s == nil {
		lines = []Line{}
	}

	var cell Cell
	var style Style
	var link Link
	var state byte
	lastX, lastY := -1, -1 // last cell written, for folding in combining marks
	var pending []byte     // pass-through sequences awaiting a cell to ride on
	for len(str) > 0 {
		seq, width, n, newState := decoder(str, state, p)
		// The decoder's ASCII fast path doesn't check for trailing combining
		// sequences, so re-decode as a full cluster when one starts here.
		// The str[1] >= 0xc0 check skips the segmenter for plain ASCII.
		if n == 1 && seq[0] > 0x1f && seq[0] < 0x7f && len(str) > 1 && str[1] >= 0xc0 {
			if cluster, cw := ansi.FirstGraphemeCluster(str, method); len(cluster) > 1 {
				seq, width, n = cluster, cw, len(cluster)
			}
		}
		switch {
		// Any positive width is a printable grapheme cluster. Wcwidth measures
		// a cluster per codepoint, so this is not bounded by how wide a glyph
		// can be: a ZWJ emoji sequence such as "👨‍👩‍👧‍👦" is eight columns,
		// and capping the width here would fall through to the escape sequence
		// branch and drop the cluster from the screen.
		case width > 0:
			cell.Width = width
			cell.Content = string(seq)
			// Any pass-through sequences seen since the last cell belong in
			// front of this glyph, the order they arrived in. Checked rather
			// than joined unconditionally, because there are none at all on the
			// overwhelmingly common path and the join is not free.
			if len(pending) > 0 {
				pending = append(pending, cell.Content...)
				cell.Content = string(pending)
				pending = pending[:0]
			}
			cell.Style = style
			cell.Link = link

			if s == nil {
				// Building lines: unwrapped, no bounds
				if y >= len(lines) {
					lines = append(lines, Line{})
				}
				lines[y] = append(lines[y], cell)
				lastX, lastY = len(lines[y])-1, y
				x += width
			} else {
				// Drawing to screen: handle wrapping, truncation, and bounds
				if !truncate && x+cell.Width > bounds.Max.X && y+1 < bounds.Max.Y {
					// Wrap the string to the width of the window
					x = bounds.Min.X
					y++
				}

				pos := Pos(x, y)
				if pos.In(bounds) {
					if truncate && tailc.Width > 0 && x+cell.Width > bounds.Max.X-tailc.Width {
						// Truncate the string and append the tail if any.
						cell = tailc
						cell.Style = style
						cell.Link = link
						s.SetCell(x, y, &cell)
						lastX, lastY = x, y
						x += tailc.Width
					} else {
						// Print the cell to the screen
						s.SetCell(x, y, &cell)
						lastX, lastY = x, y
						x += width
					}
				}
			}

			// Reset cell for next iteration
			cell = Cell{}
		default:
			// Valid sequences always have a non-zero Cmd.
			// TODO: Handle cursor movement and other sequences
			switch {
			case ansi.HasCsiPrefix(seq) && p.Command() == 'm':
				// SGR - Select Graphic Rendition
				ReadStyle(p.Params(), &style)
			case ansi.HasOscPrefix(seq) && p.Command() == 8:
				// Hyperlinks
				ReadLink(p.Data(), &link)
			case ansi.Equal(seq, T("\n")):
				if s == nil {
					// When building lines, we need to ensure empty lines are represented.
					if y >= len(lines) {
						lines = append(lines, Line{})
					}
				}
				y++
				// Always treat a NL as CR-LF similar to Termios ONLCR.
				fallthrough
			case ansi.Equal(seq, T("\r")):
				if s == nil {
					x = 0
				} else {
					x = bounds.Min.X
				}
			case len(seq) > 0 && seq[0] >= 0xc0:
				// A zero-width grapheme, i.e. a combining mark the re-decode
				// above couldn't fold because an escape sequence sits between
				// it and its base, as in "a\x1b[31m\u0301". Every escape
				// introducer is a C0 or C1 byte, so a UTF-8 lead byte here is
				// always text. The mark belongs to the cell we last wrote; the
				// accumulator below would let the next glyph clobber it.
				if s == nil {
					if lastY >= 0 && lastY < len(lines) && lastX < len(lines[lastY]) {
						lines[lastY][lastX].Content += string(seq)
					}
				} else if lastX >= 0 {
					if prev := s.CellAt(lastX, lastY); prev != nil {
						folded := *prev
						folded.Content += string(seq)
						s.SetCell(lastX, lastY, &folded)
					}
				}
			case passThrough(seq):
				pending = append(pending, string(seq)...)
			}
		}

		// Advance the state and data
		state = newState
		str = str[n:]

		if s != nil && y >= bounds.Max.Y {
			// We've reached the bottom of the bounds, stop processing further
			// lines.
			break
		}
	}

	// Pass-through sequences left at the end of the string have no glyph to
	// ride in front of, so fold them into the last cell written instead. The
	// alternative is a width-0 cell one past the content, which lands outside
	// the bounds whenever the string filled them.
	if len(pending) > 0 {
		if s == nil {
			if lastY >= 0 && lastY < len(lines) && lastX >= 0 && lastX < len(lines[lastY]) {
				lines[lastY][lastX].Content += string(pending)
			}
		} else if lastX >= 0 {
			if prev := s.CellAt(lastX, lastY); prev != nil {
				folded := *prev
				folded.Content += string(pending)
				s.SetCell(lastX, lastY, &folded)
			}
		}
	}

	// Make sure to set the last cell if it's not empty.
	if !cell.IsZero() && s != nil {
		s.SetCell(x, y, &cell)
	}

	return lines
}

// ReadStyle reads a Select Graphic Rendition (SGR) escape sequences from a
// list of parameters into pen.
func ReadStyle(params ansi.Params, pen *Style) {
	if len(params) == 0 {
		*pen = Style{}
		return
	}

	for i := 0; i < len(params); i++ {
		param, hasMore, _ := params.Param(i, 0)
		switch param {
		case 0: // Reset
			*pen = Style{}
		case 1: // Bold
			pen.Attrs |= AttrBold
		case 2: // Dim/Faint
			pen.Attrs |= AttrFaint
		case 3: // Italic
			pen.Attrs |= AttrItalic
		case 4: // Underline
			nextParam, _, ok := params.Param(i+1, 0)
			if hasMore && ok { // Only accept subparameters i.e. separated by ":"
				switch nextParam {
				case 0, 1, 2, 3, 4, 5:
					i++
					switch nextParam {
					case 0: // No Underline
						pen.Underline = UnderlineStyleNone
					case 1: // Single Underline
						pen.Underline = UnderlineStyleSingle
					case 2: // Double Underline
						pen.Underline = UnderlineStyleDouble
					case 3: // Curly Underline
						pen.Underline = UnderlineStyleCurly
					case 4: // Dotted Underline
						pen.Underline = UnderlineStyleDotted
					case 5: // Dashed Underline
						pen.Underline = UnderlineStyleDashed
					}
				}
			} else {
				// Single Underline
				pen.Underline = UnderlineStyleSingle
			}
		case 5: // Slow Blink
			pen.Attrs |= AttrBlink
		case 6: // Rapid Blink
			pen.Attrs |= AttrRapidBlink
		case 7: // Reverse
			pen.Attrs |= AttrReverse
		case 8: // Conceal
			pen.Attrs |= AttrConceal
		case 9: // Crossed-out/Strikethrough
			pen.Attrs |= AttrStrikethrough
		case 22: // Normal Intensity (not bold or faint)
			pen.Attrs &^= (AttrBold | AttrFaint)
		case 23: // Not italic, not Fraktur
			pen.Attrs &^= AttrItalic
		case 24: // Not underlined
			pen.Underline = UnderlineStyleNone
		case 25: // Blink off
			pen.Attrs &^= (AttrBlink | AttrRapidBlink)
		case 27: // Positive (not reverse)
			pen.Attrs &^= AttrReverse
		case 28: // Reveal
			pen.Attrs &^= AttrConceal
		case 29: // Not crossed out
			pen.Attrs &^= AttrStrikethrough
		case 30, 31, 32, 33, 34, 35, 36, 37: // Set foreground
			pen.Fg = ansi.Black + ansi.BasicColor(param-30) //nolint:gosec
		case 38: // Set foreground 256 or truecolor
			var c color.Color
			n := ansi.ReadStyleColor(params[i:], &c)
			if n > 0 {
				pen.Fg = c
				i += n - 1
			}
		case 39: // Default foreground
			pen.Fg = nil
		case 40, 41, 42, 43, 44, 45, 46, 47: // Set background
			pen.Bg = ansi.Black + ansi.BasicColor(param-40) //nolint:gosec
		case 48: // Set background 256 or truecolor
			var c color.Color
			n := ansi.ReadStyleColor(params[i:], &c)
			if n > 0 {
				pen.Bg = c
				i += n - 1
			}
		case 49: // Default Background
			pen.Bg = nil
		case 58: // Set underline color
			var c color.Color
			n := ansi.ReadStyleColor(params[i:], &c)
			if n > 0 {
				pen.UnderlineColor = c
				i += n - 1
			}
		case 59: // Default underline color
			pen.UnderlineColor = nil
		case 90, 91, 92, 93, 94, 95, 96, 97: // Set bright foreground
			pen.Fg = ansi.BrightBlack + ansi.BasicColor(param-90) //nolint:gosec
		case 100, 101, 102, 103, 104, 105, 106, 107: // Set bright background
			pen.Bg = ansi.BrightBlack + ansi.BasicColor(param-100) //nolint:gosec
		}
	}
}

// ReadLink reads a hyperlink escape sequence from a data buffer into link.
func ReadLink(p []byte, link *Link) {
	// OSC 8 sequences have this structure `OSC 8 ; params ; URI ST`.
	// Only the first two semicolons are delimiters, semicolons that follow after are part of the URI.
	params := bytes.SplitN(p, []byte{';'}, 3)
	if len(params) != 3 {
		return
	}
	link.Params = string(params[1])
	link.URL = string(params[2])
}
