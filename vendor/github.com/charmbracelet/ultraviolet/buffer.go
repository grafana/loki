package uv

import (
	"bytes"
	"image"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Position represents a position in a coordinate system.
type Position = image.Point

// Pos is a shorthand for creating a new [Position].
func Pos(x, y int) Position {
	return Position{X: x, Y: y}
}

// Rectangle represents a rectangular area.
type Rectangle = image.Rectangle

// Rect is a shorthand for creating a new [Rectangle].
func Rect(x, y, w, h int) Rectangle {
	return Rectangle{Min: image.Point{X: x, Y: y}, Max: image.Point{X: x + w, Y: y + h}}
}

// Line represents cells in a line.
type Line []Cell

// NewLine creates a new line with the given width, filled with empty cells.
func NewLine(width int) Line {
	l := make(Line, width)
	for i := range l {
		l[i] = EmptyCell
	}
	return l
}

// Set sets the cell at the given x position.
func (l Line) Set(x int, c *Cell) {
	lineWidth := len(l)
	if x < 0 || x >= lineWidth {
		return
	}

	// When a wide cell is partially overwritten, we need
	// to fill the rest of the cell with space cells to
	// avoid rendering issues.
	var prev *Cell
	if prev = l.At(x); prev != nil { //nolint:nestif
		if pw := prev.Width; pw > 1 {
			// Writing to the first wide cell
			for j := 0; j < pw && x+j < lineWidth; j++ {
				l[x+j] = *prev
				l[x+j].Empty()
			}
		} else if pw == 0 {
			// Writing to wide cell placeholders
			for j := 1; x-j >= 0; j++ {
				if wide := l.At(x - j); wide != nil {
					if ww := wide.Width; ww > 1 && j < ww {
						for k := range ww {
							l[x-j+k] = *wide
							l[x-j+k].Empty()
						}
						break
					}
				}
			}
		}
	}

	if c == nil {
		// Nil cells are treated as blank empty cells.
		l[x] = EmptyCell
		return
	}

	l[x] = *c
	cw := c.Width
	if x+cw > lineWidth {
		// If the cell is too wide, we write blanks with the same style.
		for i := 0; i < cw && x+i < lineWidth; i++ {
			l[x+i] = *c
			l[x+i].Empty()
		}
		return
	}

	if cw > 1 {
		// Mark wide cells with an zero cells.
		// We set the wide cell down below
		for j := 1; j < cw && x+j < lineWidth; j++ {
			l[x+j] = Cell{}
		}
	}
}

// At returns the cell at the given x position.
// If the cell does not exist, it returns nil.
func (l Line) At(x int) *Cell {
	if x < 0 || x >= len(l) {
		return nil
	}

	return &l[x]
}

// String returns the string representation of the line. Any trailing spaces
// are removed.
func (l Line) String() string {
	var buf strings.Builder
	var pending bytes.Buffer
	for _, c := range l {
		if c.IsZero() {
			continue
		}
		if c.Equal(&EmptyCell) {
			pending.WriteByte(' ')
			continue
		}
		if pending.Len() > 0 {
			buf.WriteString(pending.String())
			pending.Reset()
		}
		buf.WriteString(c.String())
	}

	return buf.String()
}

// Render renders the line to a string with all the required attributes and
// styles.
func (l Line) Render() string {
	var buf strings.Builder
	renderLine(&buf, l)
	return buf.String()
}

func renderLine(buf io.StringWriter, l Line) {
	var pen Style
	var link Link
	var pending bytes.Buffer

	for _, c := range l {
		if c.IsZero() {
			continue
		}
		if c.Equal(&EmptyCell) {
			if !pen.IsZero() {
				_, _ = buf.WriteString(ansi.ResetStyle)
				pen = Style{}
			}
			if !link.IsZero() {
				_, _ = buf.WriteString(ansi.ResetHyperlink())
				link = Link{}
			}
			pending.WriteByte(' ')
			continue
		}

		if pending.Len() > 0 {
			_, _ = buf.WriteString(pending.String())
			pending.Reset()
		}

		if c.Style.IsZero() && !pen.IsZero() {
			_, _ = buf.WriteString(ansi.ResetStyle)
			pen = Style{}
		}
		if !c.Style.Equal(&pen) {
			seq := c.Style.Diff(&pen)
			_, _ = buf.WriteString(seq)
			pen = c.Style
		}

		// Write the URL escape sequence
		if c.Link != link && link.URL != "" {
			_, _ = buf.WriteString(ansi.ResetHyperlink())
			link = Link{}
		}
		if c.Link != link {
			_, _ = buf.WriteString(ansi.SetHyperlink(c.Link.URL, c.Link.Params))
			link = c.Link
		}

		_, _ = buf.WriteString(c.String())
	}

	if link.URL != "" {
		_, _ = buf.WriteString(ansi.ResetHyperlink())
	}
	if !pen.IsZero() {
		_, _ = buf.WriteString(ansi.ResetStyle)
	}
}

// Lines represents a slice of lines.
type Lines []Line

// Height returns the height of the lines.
func (ls Lines) Height() int {
	return len(ls)
}

// Width returns the width of the widest line.
func (ls Lines) Width() int {
	maxWidth := 0
	for _, l := range ls {
		maxWidth = max(maxWidth, len(l))
	}
	return maxWidth
}

// String returns the string representation of the lines.
func (ls Lines) String() string {
	var buf strings.Builder
	for i, l := range ls {
		buf.WriteString(l.String())
		if i < len(ls)-1 {
			_ = buf.WriteByte('\n')
		}
	}
	return buf.String()
}

// Render renders the lines to a styled string with all the required attributes
// and styles.
func (ls Lines) Render() string {
	var buf strings.Builder
	for i, l := range ls {
		renderLine(&buf, l)
		if i < len(ls)-1 {
			_ = buf.WriteByte('\n')
		}
	}
	return buf.String()
}

// Buffer represents a cell buffer that contains the contents of a screen.
type Buffer struct {
	// Lines is a slice of lines that make up the cells of the buffer.
	Lines []Line
}

var _ Drawable = (*Buffer)(nil)

// NewBuffer creates a new buffer with the given width and height.
// This is a convenience function that initializes a new buffer and resizes it.
func NewBuffer(width int, height int) *Buffer {
	b := new(Buffer)
	b.Lines = make([]Line, height)
	for i := range b.Lines {
		b.Lines[i] = make(Line, width)
		for j := range b.Lines[i] {
			b.Lines[i][j] = EmptyCell
		}
	}
	b.Resize(width, height)
	return b
}

// String returns the string representation of the buffer.
func (b *Buffer) String() string {
	return Lines(b.Lines).String()
}

// Render renders the buffer to a styled string with all the required
// attributes and styles.
func (b *Buffer) Render() string {
	return Lines(b.Lines).Render()
}

// Line returns a pointer to the line at the given y position.
// If the line does not exist, it returns nil.
func (b *Buffer) Line(y int) Line {
	if y < 0 || y >= len(b.Lines) {
		return nil
	}
	return b.Lines[y]
}

// CellAt returns the cell at the given position. It returns nil if the
// position is out of bounds.
func (b *Buffer) CellAt(x int, y int) *Cell {
	if y < 0 || y >= len(b.Lines) {
		return nil
	}
	return b.Lines[y].At(x)
}

// SetCell sets the cell at the given x, y position.
func (b *Buffer) SetCell(x, y int, c *Cell) {
	if y < 0 || y >= len(b.Lines) {
		return
	}

	b.Lines[y].Set(x, c)
}

// Height implements Screen.
func (b *Buffer) Height() int {
	return len(b.Lines)
}

// Width implements Screen.
func (b *Buffer) Width() int {
	if len(b.Lines) == 0 {
		return 0
	}
	return len(b.Lines[0])
}

// Bounds returns the bounds of the buffer.
// The origin is always at (0, 0) and the maximum coordinates are determined by
// the width and height of the buffer.
func (b *Buffer) Bounds() Rectangle {
	return Rect(0, 0, b.Width(), b.Height())
}

// Resize resizes the buffer to the given width and height.
func (b *Buffer) Resize(width int, height int) {
	curWidth, curHeight := b.Width(), b.Height()
	if curWidth == width && curHeight == height {
		// No need to resize if the dimensions are the same.
		return
	}

	if width > curWidth {
		line := make(Line, width-curWidth)
		for i := range line {
			line[i] = EmptyCell
		}
		for i := range b.Lines {
			b.Lines[i] = append(b.Lines[i], line...)
		}
	} else if width < curWidth {
		for i := range b.Lines {
			b.Lines[i] = b.Lines[i][:width]
		}
	}

	if height > len(b.Lines) {
		for i := len(b.Lines); i < height; i++ {
			line := make(Line, width)
			for j := range line {
				line[j] = EmptyCell
			}
			b.Lines = append(b.Lines, line)
		}
	} else if height < len(b.Lines) {
		b.Lines = b.Lines[:height]
	}
}

// Fill fills the buffer with the given cell and rectangle.
func (b *Buffer) Fill(c *Cell) {
	b.FillArea(c, b.Bounds())
}

// FillArea fills the buffer with the given cell and rectangle.
func (b *Buffer) FillArea(c *Cell, area Rectangle) {
	cellWidth := 1
	if c != nil && c.Width > 1 {
		cellWidth = c.Width
	}
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x += cellWidth {
			b.SetCell(x, y, c)
		}
	}
}

// Clear clears the buffer with space cells and rectangle.
func (b *Buffer) Clear() {
	b.ClearArea(b.Bounds())
}

// ClearArea clears the buffer with space cells within the specified
// rectangles. Only cells within the rectangle's bounds are affected.
func (b *Buffer) ClearArea(area Rectangle) {
	b.FillArea(nil, area)
}

// CloneArea clones the area of the buffer within the specified rectangle. If
// the area is out of bounds, it returns nil.
func (b *Buffer) CloneArea(area Rectangle) *Buffer {
	bounds := b.Bounds()
	if !area.In(bounds) {
		return nil
	}
	n := NewBuffer(area.Dx(), area.Dy())
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			c := b.CellAt(x, y)
			if c == nil || c.IsZero() {
				continue
			}
			n.SetCell(x-area.Min.X, y-area.Min.Y, c)
		}
	}
	return n
}

// Clone clones the entire buffer into a new buffer.
func (b *Buffer) Clone() *Buffer {
	return b.CloneArea(b.Bounds())
}

// Draw draws the buffer to the given screen at the specified area.
// It implements the [Drawable] interface.
func (b *Buffer) Draw(scr Screen, area Rectangle) {
	if area.Empty() {
		return
	}

	// Ensure the area is within the bounds of the screen.
	bounds := scr.Bounds()
	if !area.Overlaps(bounds) {
		return
	}

	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; {
			c := b.CellAt(x-area.Min.X, y-area.Min.Y)
			if c == nil || c.IsZero() {
				x++
				continue
			}
			scr.SetCell(x, y, c)
			width := c.Width
			if width <= 0 {
				width = 1
			}
			x += width
		}
	}
}

// InsertLine inserts n lines at the given line position, with the given
// optional cell, within the specified rectangles. If no rectangles are
// specified, it inserts lines in the entire buffer. Only cells within the
// rectangle's horizontal bounds are affected. Lines are pushed out of the
// rectangle bounds and lost. This follows terminal [ansi.IL] behavior.
// It returns the pushed out lines.
func (b *Buffer) InsertLine(y, n int, c *Cell) {
	b.InsertLineArea(y, n, c, b.Bounds())
}

// InsertLineArea inserts new lines at the given line position, with the
// given optional cell, within the rectangle bounds. Only cells within the
// rectangle's horizontal bounds are affected. Lines are pushed out of the
// rectangle bounds and lost. This follows terminal [ansi.IL] behavior.
func (b *Buffer) InsertLineArea(y, n int, c *Cell, area Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= b.Height() {
		return
	}

	// Limit number of lines to insert to available space
	if y+n > area.Max.Y {
		n = area.Max.Y - y
	}

	// Move existing lines down within the bounds
	for i := area.Max.Y - 1; i >= y+n; i-- {
		for x := area.Min.X; x < area.Max.X; x++ {
			// We don't need to clone c here because we're just moving lines down.
			b.Lines[i][x] = b.Lines[i-n][x]
		}
	}

	// Clear the newly inserted lines within bounds
	for i := y; i < y+n; i++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			b.SetCell(x, i, c)
		}
	}
}

// DeleteLineArea deletes lines at the given line position, with the given
// optional cell, within the rectangle bounds. Only cells within the
// rectangle's bounds are affected. Lines are shifted up within the bounds and
// new blank lines are created at the bottom. This follows terminal [ansi.DL]
// behavior.
func (b *Buffer) DeleteLineArea(y, n int, c *Cell, area Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= b.Height() {
		return
	}

	// Limit deletion count to available space in scroll region
	if n > area.Max.Y-y {
		n = area.Max.Y - y
	}

	// Shift cells up within the bounds
	for dst := y; dst < area.Max.Y-n; dst++ {
		src := dst + n
		for x := area.Min.X; x < area.Max.X; x++ {
			// We don't need to clone c here because we're just moving cells up.
			b.Lines[dst][x] = b.Lines[src][x]
		}
	}

	// Fill the bottom n lines with blank cells
	for i := area.Max.Y - n; i < area.Max.Y; i++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			b.SetCell(x, i, c)
		}
	}
}

// DeleteLine deletes n lines at the given line position, with the given
// optional cell, within the specified rectangles. If no rectangles are
// specified, it deletes lines in the entire buffer.
func (b *Buffer) DeleteLine(y, n int, c *Cell) {
	b.DeleteLineArea(y, n, c, b.Bounds())
}

// InsertCell inserts new cells at the given position, with the given optional
// cell, within the specified rectangles. If no rectangles are specified, it
// inserts cells in the entire buffer. This follows terminal [ansi.ICH]
// behavior.
func (b *Buffer) InsertCell(x, y, n int, c *Cell) {
	b.InsertCellArea(x, y, n, c, b.Bounds())
}

// InsertCellArea inserts new cells at the given position, with the given
// optional cell, within the rectangle bounds. Only cells within the
// rectangle's bounds are affected, following terminal [ansi.ICH] behavior.
func (b *Buffer) InsertCellArea(x, y, n int, c *Cell, area Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= b.Height() ||
		x < area.Min.X || x >= area.Max.X || x >= b.Width() {
		return
	}

	// Limit number of cells to insert to available space
	if x+n > area.Max.X {
		n = area.Max.X - x
	}

	// Move existing cells within rectangle bounds to the right
	for i := area.Max.X - 1; i >= x+n && i-n >= area.Min.X; i-- {
		// We don't need to clone c here because we're just moving cells to the
		// right.
		b.Lines[y][i] = b.Lines[y][i-n]
	}

	// Clear the newly inserted cells within rectangle bounds
	for i := x; i < x+n && i < area.Max.X; i++ {
		b.SetCell(i, y, c)
	}
}

// DeleteCell deletes cells at the given position, with the given optional
// cell, within the specified rectangles. If no rectangles are specified, it
// deletes cells in the entire buffer. This follows terminal [ansi.DCH]
// behavior.
func (b *Buffer) DeleteCell(x, y, n int, c *Cell) {
	b.DeleteCellArea(x, y, n, c, b.Bounds())
}

// DeleteCellArea deletes cells at the given position, with the given
// optional cell, within the rectangle bounds. Only cells within the
// rectangle's bounds are affected, following terminal [ansi.DCH] behavior.
func (b *Buffer) DeleteCellArea(x, y, n int, c *Cell, area Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= b.Height() ||
		x < area.Min.X || x >= area.Max.X || x >= b.Width() {
		return
	}

	// Calculate how many positions we can actually delete
	remainingCells := area.Max.X - x
	if n > remainingCells {
		n = remainingCells
	}

	// Shift the remaining cells to the left
	for i := x; i < area.Max.X-n; i++ {
		if i+n < area.Max.X {
			// We need to use SetCell here to ensure we blank out any wide
			// cells we encounter.
			b.SetCell(i, y, b.CellAt(i+n, y))
		}
	}

	// Fill the vacated positions with the given cell
	for i := area.Max.X - n; i < area.Max.X; i++ {
		b.SetCell(i, y, c)
	}
}

// ScreenBuffer is a buffer that can be used as a [Screen].
type ScreenBuffer struct {
	*RenderBuffer
	Method ansi.Method
}

var (
	_ Screen   = ScreenBuffer{}
	_ Drawable = ScreenBuffer{}
)

// NewScreenBuffer creates a new ScreenBuffer with the given width and height.
func NewScreenBuffer(width, height int) ScreenBuffer {
	return ScreenBuffer{
		RenderBuffer: NewRenderBuffer(width, height),
		Method:       ansi.WcWidth,
	}
}

// WidthMethod returns the width method used by the screen.
// It defaults to [ansi.WcWidth].
func (s ScreenBuffer) WidthMethod() WidthMethod {
	return s.Method
}

// TrimSpace trims trailing spaces from the end of each line in the given
// string.
func TrimSpace(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		// Check if we have a trailing '\r' and preserve it
		hasCR := strings.HasSuffix(line, "\r")
		if hasCR {
			line = strings.TrimSuffix(line, "\r")
		}
		line = strings.TrimRight(line, " ")
		if hasCR {
			line = line + "\r"
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// RenderBuffer represents a buffer that keeps track of the current and new
// state of the screen, allowing for efficient rendering by only updating the
// parts of the screen that have changed.
type RenderBuffer struct {
	*Buffer
	Touched []*LineData
}

// NewRenderBuffer creates a new [RenderBuffer] with the given width and height.
func NewRenderBuffer(width, height int) *RenderBuffer {
	return &RenderBuffer{
		Buffer:  NewBuffer(width, height),
		Touched: make([]*LineData, height),
	}
}

// TouchLine marks a line n times starting at the given x position as touched.
func (b *RenderBuffer) TouchLine(x, y, n int) {
	if y < 0 || y >= len(b.Lines) {
		return
	}

	if y >= len(b.Touched) {
		b.Touched = append(b.Touched, make([]*LineData, y-len(b.Touched)+1)...)
	}

	// Re-check bounds: a concurrent resize may have cleared Touched
	if y >= len(b.Touched) {
		return
	}

	ch := b.Touched[y]
	if ch == nil {
		ch = &LineData{FirstCell: x, LastCell: x + n}
	} else {
		ch.FirstCell = min(ch.FirstCell, x)
		ch.LastCell = max(ch.LastCell, x+n)
	}
	b.Touched[y] = ch
}

// Touch marks the cell at the given x, y position as touched.
func (b *RenderBuffer) Touch(x, y int) {
	b.TouchLine(x, y, 0)
}

// TouchedLines returns the number of touched lines in the buffer.
func (b *RenderBuffer) TouchedLines() int {
	if b.Touched == nil {
		return 0
	}
	count := 0
	for _, t := range b.Touched {
		if t != nil {
			count++
		}
	}
	return count
}

// SetCell sets the cell at the given x, y position and marks the line as
// touched.
func (b *RenderBuffer) SetCell(x, y int, c *Cell) {
	if !cellEqual(b.CellAt(x, y), c) {
		width := 1
		if c != nil && c.Width > 0 {
			width = c.Width
		}
		b.TouchLine(x, y, width)
	}
	b.Buffer.SetCell(x, y, c)
}

// InsertLine inserts n lines at the given line position, with the given
// optional cell, within the specified rectangles. If no rectangles are
// specified, it inserts lines in the entire buffer. Only cells within the
// rectangle's horizontal bounds are affected. Lines are pushed out of the
// rectangle bounds and lost. This follows terminal [ansi.IL] behavior.
func (b *RenderBuffer) InsertLine(y, n int, c *Cell) {
	b.InsertLineArea(y, n, c, b.Bounds())
}

// InsertLineArea inserts new lines at the given line position, with the given
// optional cell, within the rectangle bounds. Only cells within the
// rectangle's horizontal bounds are affected. Lines are pushed out of the
// rectangle bounds and lost. This follows terminal [ansi.IL] behavior.
func (b *RenderBuffer) InsertLineArea(y, n int, c *Cell, area Rectangle) {
	b.Buffer.InsertLineArea(y, n, c, area)
	for i := area.Min.Y; i < area.Max.Y; i++ {
		b.TouchLine(area.Min.X, i, area.Max.X-area.Min.X)
		b.TouchLine(area.Min.X, i-n, area.Max.X-area.Min.X)
	}
}

// DeleteLine deletes n lines at the given line position, with the given
// optional cell, within the specified rectangles. If no rectangles are
// specified, it deletes lines in the entire buffer.
func (b *RenderBuffer) DeleteLine(y, n int, c *Cell) {
	b.DeleteLineArea(y, n, c, b.Bounds())
}

// DeleteLineArea deletes lines at the given line position, with the given
// optional cell, within the rectangle bounds. Only cells within the
// rectangle's bounds are affected. Lines are shifted up within the bounds and
// new blank lines are created at the bottom. This follows terminal [ansi.DL]
// behavior.
func (b *RenderBuffer) DeleteLineArea(y, n int, c *Cell, area Rectangle) {
	b.Buffer.DeleteLineArea(y, n, c, area)
	for i := area.Min.Y; i < area.Max.Y; i++ {
		b.TouchLine(area.Min.X, i, area.Max.X-area.Min.X)
		b.TouchLine(area.Min.X, i+n, area.Max.X-area.Min.X)
	}
}

// InsertCell inserts new cells at the given position, with the given optional
// cell, within the specified rectangles. If no rectangles are specified, it
// inserts cells in the entire buffer. This follows terminal [ansi.ICH]
// behavior.
func (b *RenderBuffer) InsertCell(x, y, n int, c *Cell) {
	b.InsertCellArea(x, y, n, c, b.Bounds())
}

// InsertCellArea inserts new cells at the given position, with the given
// optional cell, within the rectangle bounds. Only cells within the
// rectangle's bounds are affected, following terminal [ansi.ICH] behavior.
func (b *RenderBuffer) InsertCellArea(x, y, n int, c *Cell, area Rectangle) {
	b.Buffer.InsertCellArea(x, y, n, c, area)
	if x+n > area.Max.X {
		n = area.Max.X - x
	}
	b.TouchLine(x, y, n)
}

// DeleteCell deletes cells at the given position, with the given optional
// cell, within the specified rectangles. If no rectangles are specified, it
// deletes cells in the entire buffer. This follows terminal [ansi.DCH]
// behavior.
func (b *RenderBuffer) DeleteCell(x, y, n int, c *Cell) {
	b.DeleteCellArea(x, y, n, c, b.Bounds())
}

// DeleteCellArea deletes cells at the given position, with the given
// optional cell, within the rectangle bounds. Only cells within the
// rectangle's bounds are affected, following terminal [ansi.DCH] behavior.
func (b *RenderBuffer) DeleteCellArea(x, y, n int, c *Cell, area Rectangle) {
	b.Buffer.DeleteCellArea(x, y, n, c, area)
	remainingCells := area.Max.X - x
	if n > remainingCells {
		n = remainingCells
	}
	b.TouchLine(x, y, n)
}
