package cellbuf

import (
	"bytes"
	"slices"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

const nbsp = '\u00a0'

// Wrap returns a string that is wrapped to the specified limit applying any
// ANSI escape sequences in the string. It tries to wrap the string at word
// boundaries, but will break words if necessary.
//
// The breakpoints string is a list of characters that are considered
// breakpoints for word wrapping. A hyphen (-) is always considered a
// breakpoint.
//
// Note: breakpoints must be a string of 1-cell wide rune characters.
func Wrap(s string, limit int, breakpoints string) string {
	//nolint:godox
	// TODO: Use [PenWriter] once we get
	// https://github.com/charmbracelet/lipgloss/pull/489 out the door and
	// released.
	// The problem is that [ansi.Wrap] doesn't keep track of style and link
	// state, so combining both breaks styled space cells. To fix this, we use
	// non-breaking space cells for padding and styled blank cells. And since
	// both wrapping methods respect non-breaking spaces, we can use them to
	// preserve styled spaces in the output.

	if len(s) == 0 {
		return ""
	}

	if limit < 1 {
		return s
	}

	p := ansi.GetParser()
	defer ansi.PutParser(p)

	var (
		buf             bytes.Buffer
		word            bytes.Buffer
		space           bytes.Buffer
		style, curStyle Style
		link, curLink   Link
		curWidth        int
		wordLen         int
	)

	hasBlankStyle := func() bool {
		// Only follow reverse attribute, bg color and underline style
		return !style.Attrs.Contains(ReverseAttr) && style.Bg == nil && style.UlStyle == NoUnderline
	}

	addSpace := func() {
		curWidth += space.Len()
		buf.Write(space.Bytes())
		space.Reset()
	}

	addWord := func() {
		if word.Len() == 0 {
			return
		}

		curLink = link
		curStyle = style

		addSpace()
		curWidth += wordLen
		buf.Write(word.Bytes())
		word.Reset()
		wordLen = 0
	}

	addNewline := func() {
		if !curStyle.Empty() {
			buf.WriteString(ansi.ResetStyle)
		}
		if !curLink.Empty() {
			buf.WriteString(ansi.ResetHyperlink())
		}
		buf.WriteByte('\n')
		if !curLink.Empty() {
			buf.WriteString(ansi.SetHyperlink(curLink.URL, curLink.Params))
		}
		if !curStyle.Empty() {
			buf.WriteString(curStyle.Sequence())
		}
		curWidth = 0
		space.Reset()
	}

	var state byte
	for len(s) > 0 {
		seq, width, n, newState := ansi.DecodeSequence(s, state, p)
		switch width {
		case 0:
			if ansi.Equal(seq, "\t") { //nolint:nestif
				addWord()
				space.WriteString(seq)
				break
			} else if ansi.Equal(seq, "\n") {
				if wordLen == 0 {
					if curWidth+space.Len() > limit {
						curWidth = 0
					} else {
						// preserve whitespaces
						buf.Write(space.Bytes())
					}
					space.Reset()
				}

				addWord()
				addNewline()
				break
			} else if ansi.HasCsiPrefix(seq) && p.Command() == 'm' {
				// SGR style sequence [ansi.SGR]
				ReadStyle(p.Params(), &style)
			} else if ansi.HasOscPrefix(seq) && p.Command() == 8 {
				// Hyperlink sequence [ansi.SetHyperlink]
				ReadLink(p.Data(), &link)
			}

			word.WriteString(seq)
		default:
			if len(seq) == 1 {
				// ASCII
				r, _ := utf8.DecodeRuneInString(seq)
				if r != nbsp && unicode.IsSpace(r) && hasBlankStyle() {
					addWord()
					space.WriteRune(r)
					break
				} else if r == '-' || runeContainsAny(r, breakpoints) {
					addSpace()
					if curWidth+wordLen+width <= limit {
						addWord()
						buf.WriteString(seq)
						curWidth += width
						break
					}
				}
			}

			if wordLen+width > limit {
				// Hardwrap the word if it's too long
				addWord()
			}

			word.WriteString(seq)
			wordLen += width

			if curWidth+wordLen+space.Len() > limit {
				addNewline()
			}
		}

		s = s[n:]
		state = newState
	}

	if wordLen == 0 {
		if curWidth+space.Len() > limit {
			curWidth = 0
		} else {
			// preserve whitespaces
			buf.Write(space.Bytes())
		}
		space.Reset()
	}

	addWord()

	if !curLink.Empty() {
		buf.WriteString(ansi.ResetHyperlink())
	}
	if !curStyle.Empty() {
		buf.WriteString(ansi.ResetStyle)
	}

	return buf.String()
}

func runeContainsAny[T string | []rune](r rune, s T) bool {
	return slices.Contains([]rune(s), r)
}
