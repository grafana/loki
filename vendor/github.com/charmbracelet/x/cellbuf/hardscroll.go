package cellbuf

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// scrollOptimize optimizes the screen to transform the old buffer into the new
// buffer.
func (s *Screen) scrollOptimize() {
	height := s.newbuf.Height()
	if s.oldnum == nil || len(s.oldnum) < height {
		s.oldnum = make([]int, height)
	}

	// Calculate the indices
	s.updateHashmap()
	if len(s.hashtab) < height {
		return
	}

	// Pass 1 - from top to bottom scrolling up
	for i := 0; i < height; {
		for i < height && (s.oldnum[i] == newIndex || s.oldnum[i] <= i) {
			i++
		}
		if i >= height {
			break
		}

		shift := s.oldnum[i] - i // shift > 0
		start := i

		i++
		for i < height && s.oldnum[i] != newIndex && s.oldnum[i]-i == shift {
			i++
		}
		end := i - 1 + shift

		if !s.scrolln(shift, start, end, height-1) {
			continue
		}
	}

	// Pass 2 - from bottom to top scrolling down
	for i := height - 1; i >= 0; {
		for i >= 0 && (s.oldnum[i] == newIndex || s.oldnum[i] >= i) {
			i--
		}
		if i < 0 {
			break
		}

		shift := s.oldnum[i] - i // shift < 0
		end := i

		i--
		for i >= 0 && s.oldnum[i] != newIndex && s.oldnum[i]-i == shift {
			i--
		}

		start := i + 1 - (-shift)
		if !s.scrolln(shift, start, end, height-1) {
			continue
		}
	}
}

// scrolln scrolls the screen up by n lines.
func (s *Screen) scrolln(n, top, bot, maxY int) (v bool) { //nolint:unparam
	const (
		nonDestScrollRegion = false
		memoryBelow         = false
	)

	blank := s.clearBlank()
	if n > 0 {
		// Scroll up (forward)
		v = s.scrollUp(n, top, bot, 0, maxY, blank)
		if !v {
			s.buf.WriteString(ansi.SetTopBottomMargins(top+1, bot+1))

			// XXX: How should we handle this in inline mode when not using alternate screen?
			s.cur.X, s.cur.Y = -1, -1
			v = s.scrollUp(n, top, bot, top, bot, blank)
			s.buf.WriteString(ansi.SetTopBottomMargins(1, maxY+1))
			s.cur.X, s.cur.Y = -1, -1
		}

		if !v {
			v = s.scrollIdl(n, top, bot-n+1, blank)
		}

		// Clear newly shifted-in lines.
		if v &&
			(nonDestScrollRegion || (memoryBelow && bot == maxY)) {
			if bot == maxY {
				s.move(0, bot-n+1)
				s.clearToBottom(nil)
			} else {
				for i := 0; i < n; i++ {
					s.move(0, bot-i)
					s.clearToEnd(nil, false)
				}
			}
		}
	} else if n < 0 {
		// Scroll down (backward)
		v = s.scrollDown(-n, top, bot, 0, maxY, blank)
		if !v {
			s.buf.WriteString(ansi.SetTopBottomMargins(top+1, bot+1))

			// XXX: How should we handle this in inline mode when not using alternate screen?
			s.cur.X, s.cur.Y = -1, -1
			v = s.scrollDown(-n, top, bot, top, bot, blank)
			s.buf.WriteString(ansi.SetTopBottomMargins(1, maxY+1))
			s.cur.X, s.cur.Y = -1, -1

			if !v {
				v = s.scrollIdl(-n, bot+n+1, top, blank)
			}

			// Clear newly shifted-in lines.
			if v &&
				(nonDestScrollRegion || (memoryBelow && top == 0)) {
				for i := 0; i < -n; i++ {
					s.move(0, top+i)
					s.clearToEnd(nil, false)
				}
			}
		}
	}

	if !v {
		return
	}

	s.scrollBuffer(s.curbuf, n, top, bot, blank)

	// shift hash values too, they can be reused
	s.scrollOldhash(n, top, bot)

	return true
}

// scrollBuffer scrolls the buffer by n lines.
func (s *Screen) scrollBuffer(b *Buffer, n, top, bot int, blank *Cell) {
	if top < 0 || bot < top || bot >= b.Height() {
		// Nothing to scroll
		return
	}

	if n < 0 {
		// shift n lines downwards
		limit := top - n
		for line := bot; line >= limit && line >= 0 && line >= top; line-- {
			copy(b.Lines[line], b.Lines[line+n])
		}
		for line := top; line < limit && line <= b.Height()-1 && line <= bot; line++ {
			b.FillRect(blank, Rect(0, line, b.Width(), 1))
		}
	}

	if n > 0 {
		// shift n lines upwards
		limit := bot - n
		for line := top; line <= limit && line <= b.Height()-1 && line <= bot; line++ {
			copy(b.Lines[line], b.Lines[line+n])
		}
		for line := bot; line > limit && line >= 0 && line >= top; line-- {
			b.FillRect(blank, Rect(0, line, b.Width(), 1))
		}
	}

	s.touchLine(b.Width(), b.Height(), top, bot-top+1, true)
}

// touchLine marks the line as touched.
func (s *Screen) touchLine(width, height, y, n int, changed bool) {
	if n < 0 || y < 0 || y >= height {
		return // Nothing to touch
	}

	for i := y; i < y+n && i < height; i++ {
		if changed {
			s.touch[i] = lineData{firstCell: 0, lastCell: width - 1}
		} else {
			delete(s.touch, i)
		}
	}
}

// scrollUp scrolls the screen up by n lines.
func (s *Screen) scrollUp(n, top, bot, minY, maxY int, blank *Cell) bool {
	if n == 1 && top == minY && bot == maxY {
		s.move(0, bot)
		s.updatePen(blank)
		s.buf.WriteByte('\n')
	} else if n == 1 && bot == maxY {
		s.move(0, top)
		s.updatePen(blank)
		s.buf.WriteString(ansi.DeleteLine(1))
	} else if top == minY && bot == maxY {
		if s.xtermLike {
			s.move(0, bot)
		} else {
			s.move(0, top)
		}
		s.updatePen(blank)
		if s.xtermLike {
			s.buf.WriteString(ansi.ScrollUp(n))
		} else {
			s.buf.WriteString(strings.Repeat("\n", n))
		}
	} else if bot == maxY {
		s.move(0, top)
		s.updatePen(blank)
		s.buf.WriteString(ansi.DeleteLine(n))
	} else {
		return false
	}
	return true
}

// scrollDown scrolls the screen down by n lines.
func (s *Screen) scrollDown(n, top, bot, minY, maxY int, blank *Cell) bool {
	if n == 1 && top == minY && bot == maxY {
		s.move(0, top)
		s.updatePen(blank)
		s.buf.WriteString(ansi.ReverseIndex)
	} else if n == 1 && bot == maxY {
		s.move(0, top)
		s.updatePen(blank)
		s.buf.WriteString(ansi.InsertLine(1))
	} else if top == minY && bot == maxY {
		s.move(0, top)
		s.updatePen(blank)
		if s.xtermLike {
			s.buf.WriteString(ansi.ScrollDown(n))
		} else {
			s.buf.WriteString(strings.Repeat(ansi.ReverseIndex, n))
		}
	} else if bot == maxY {
		s.move(0, top)
		s.updatePen(blank)
		s.buf.WriteString(ansi.InsertLine(n))
	} else {
		return false
	}
	return true
}

// scrollIdl scrolls the screen n lines by using [ansi.DL] at del and using
// [ansi.IL] at ins.
func (s *Screen) scrollIdl(n, del, ins int, blank *Cell) bool {
	if n < 0 {
		return false
	}

	// Delete lines
	s.move(0, del)
	s.updatePen(blank)
	s.buf.WriteString(ansi.DeleteLine(n))

	// Insert lines
	s.move(0, ins)
	s.updatePen(blank)
	s.buf.WriteString(ansi.InsertLine(n))

	return true
}
