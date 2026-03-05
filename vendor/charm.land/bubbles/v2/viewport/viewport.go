package viewport

import (
	"cmp"
	"math"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	defaultHorizontalStep = 6
)

// Option is a configuration option that works in conjunction with [New]. For
// example:
//
//	timer := New(WithWidth(10, WithHeight(5)))
type Option func(*Model)

// WithWidth is an initialization option that sets the width of the
// viewport. Pass as an argument to [New].
func WithWidth(w int) Option {
	return func(m *Model) {
		m.width = w
	}
}

// WithHeight is an initialization option that sets the height of the
// viewport. Pass as an argument to [New].
func WithHeight(h int) Option {
	return func(m *Model) {
		m.height = h
	}
}

// New returns a new model with the given width and height as well as default
// key mappings.
func New(opts ...Option) (m Model) {
	for _, opt := range opts {
		opt(&m)
	}
	m.setInitialValues()
	return m
}

// Model is the Bubble Tea model for this viewport element.
type Model struct {
	width  int
	height int
	KeyMap KeyMap

	// Whether or not to wrap text. If false, it'll allow horizontal scrolling
	// instead.
	SoftWrap bool

	// Whether or not to fill to the height of the viewport with empty lines.
	FillHeight bool

	// Whether or not to respond to the mouse. The mouse must be enabled in
	// Bubble Tea for this to work. For details, see the Bubble Tea docs.
	MouseWheelEnabled bool

	// The number of lines the mouse wheel will scroll. By default, this is 3.
	MouseWheelDelta int

	// yOffset is the vertical scroll position.
	yOffset int

	// xOffset is the horizontal scroll position.
	xOffset int

	// horizontalStep is the number of columns we move left or right during a
	// default horizontal scroll.
	horizontalStep int

	// YPosition is the position of the viewport in relation to the terminal
	// window. It's used in high performance rendering only.
	YPosition int

	// Style applies a lipgloss style to the viewport. Realistically, it's most
	// useful for setting borders, margins and padding.
	Style lipgloss.Style

	// LeftGutterFunc allows to define a [GutterFunc] that adds a column into
	// the left of the viewport, which is kept when horizontal scrolling.
	// This can be used for things like line numbers, selection indicators,
	// show statuses, etc. It is expected that the real-width (as measured by
	// [lipgloss.Width]) of the returned value is always consistent, regardless
	// of index, soft wrapping, etc.
	LeftGutterFunc GutterFunc

	initialized      bool
	lines            []string
	longestLineWidth int

	// HighlightStyle highlights the ranges set with [SetHighligths].
	HighlightStyle lipgloss.Style

	// SelectedHighlightStyle highlights the highlight range focused during
	// navigation.
	// Use [SetHighligths] to set the highlight ranges, and [HightlightNext]
	// and [HihglightPrevious] to navigate.
	SelectedHighlightStyle lipgloss.Style

	// StyleLineFunc allows to return a [lipgloss.Style] for each line.
	// The argument is the line index.
	StyleLineFunc func(int) lipgloss.Style

	highlights []highlightInfo
	hiIdx      int
}

// GutterFunc can be implemented and set into [Model.LeftGutterFunc].
//
// Example implementation showing line numbers:
//
//	func(info GutterContext) string {
//		if info.Soft {
//			return "     │ "
//		}
//		if info.Index >= info.TotalLines {
//			return "   ~ │ "
//		}
//		return fmt.Sprintf("%4d │ ", info.Index+1)
//	}
type GutterFunc func(GutterContext) string

// NoGutter is the default gutter used.
var NoGutter = func(GutterContext) string { return "" }

// GutterContext provides context to a [GutterFunc].
type GutterContext struct {
	// Index is the line index of the line which the gutter is being rendered for.
	Index int

	// TotalLines is the total number of lines in the viewport.
	TotalLines int

	// Soft is whether or not the line is soft wrapped.
	Soft bool
}

func (m *Model) setInitialValues() {
	m.KeyMap = DefaultKeyMap()
	m.MouseWheelEnabled = true
	m.MouseWheelDelta = 3
	m.horizontalStep = defaultHorizontalStep
	m.LeftGutterFunc = NoGutter
	m.initialized = true
}

// Init exists to satisfy the tea.Model interface for composability purposes.
func (m Model) Init() tea.Cmd {
	return nil
}

// Height returns the height of the viewport.
func (m Model) Height() int {
	return m.height
}

// SetHeight sets the height of the viewport.
func (m *Model) SetHeight(h int) {
	m.height = h
}

// Width returns the width of the viewport.
func (m Model) Width() int {
	return m.width
}

// SetWidth sets the width of the viewport.
func (m *Model) SetWidth(w int) {
	m.width = w
}

// AtTop returns whether or not the viewport is at the very top position.
func (m Model) AtTop() bool {
	return m.YOffset() <= 0
}

// AtBottom returns whether or not the viewport is at or past the very bottom
// position.
func (m Model) AtBottom() bool {
	return m.YOffset() >= m.maxYOffset()
}

// PastBottom returns whether or not the viewport is scrolled beyond the last
// line. This can happen when adjusting the viewport height.
func (m Model) PastBottom() bool {
	return m.YOffset() > m.maxYOffset()
}

// ScrollPercent returns the amount scrolled as a float between 0 and 1.
func (m Model) ScrollPercent() float64 {
	total, _, _ := m.calculateLine(0)
	if m.Height() >= total {
		return 1.0
	}
	y := float64(m.YOffset())
	h := float64(m.Height())
	t := float64(total)
	v := y / (t - h)
	return clamp(v, 0, 1)
}

// HorizontalScrollPercent returns the amount horizontally scrolled as a float
// between 0 and 1.
func (m Model) HorizontalScrollPercent() float64 {
	if m.xOffset >= m.longestLineWidth-m.Width() {
		return 1.0
	}
	y := float64(m.xOffset)
	h := float64(m.Width())
	t := float64(m.longestLineWidth)
	v := y / (t - h)
	return clamp(v, 0, 1)
}

// SetContent set the pager's text content. Line endings will be normalized to '\n'.
func (m *Model) SetContent(s string) {
	m.SetContentLines(strings.Split(s, "\n"))
}

// SetContentLines allows to set the lines to be shown instead of the content.
// If a given line has a \n in it, it will still be split into multiple lines
// similar to that of [Model.SetContent]. See also [Model.SetContent].
func (m *Model) SetContentLines(lines []string) {
	// if there's no content, set content to actual nil instead of one empty
	// line.
	m.lines = lines
	if len(m.lines) == 1 && ansi.StringWidth(m.lines[0]) == 0 {
		m.lines = nil
	} else {
		// iterate in reverse, so we can safely modify the slice.
		var subLines []string
		for i := len(m.lines) - 1; i >= 0; i-- {
			if !strings.ContainsAny(m.lines[i], "\r\n") {
				continue
			}

			m.lines[i] = strings.ReplaceAll(m.lines[i], "\r\n", "\n") // normalize line endings
			subLines = strings.Split(m.lines[i], "\n")
			if len(subLines) > 1 {
				m.lines = slices.Insert(m.lines, i+1, subLines[1:]...)
				m.lines[i] = subLines[0]
			}
		}
	}

	m.longestLineWidth = maxLineWidth(m.lines)
	m.ClearHighlights()

	if m.YOffset() > m.maxYOffset() {
		m.GotoBottom()
	}
}

// GetContent returns the entire content as a single string.
// Line endings are normalized to '\n'.
func (m Model) GetContent() string {
	return strings.Join(m.lines, "\n")
}

// calculateLine taking soft wrapping into account, returns the total viewable
// lines and the real-line index for the given yoffset, as well as the virtual
// line offset.
func (m Model) calculateLine(yoffset int) (total, ridx, voffset int) {
	if !m.SoftWrap {
		total = len(m.lines)
		ridx = min(yoffset, len(m.lines))
		return total, ridx, 0
	}

	maxWidth := float64(m.maxWidth())
	var lineHeight int

	for i, line := range m.lines {
		lineHeight = max(1, int(math.Ceil(float64(ansi.StringWidth(line))/maxWidth)))

		if yoffset >= total && yoffset < total+lineHeight {
			ridx = i
			voffset = yoffset - total
		}
		total += lineHeight
	}

	if yoffset >= total {
		ridx = len(m.lines)
		voffset = 0
	}

	return total, ridx, voffset
}

// maxYOffset returns the maximum possible value of the y-offset based on the
// viewport's content and set height.
func (m Model) maxYOffset() int {
	total, _, _ := m.calculateLine(0)
	return max(0, total-m.Height()+m.Style.GetVerticalFrameSize())
}

// maxXOffset returns the maximum possible value of the x-offset based on the
// viewport's content and set width.
func (m Model) maxXOffset() int {
	return max(0, m.longestLineWidth-m.Width())
}

// maxWidth returns the maximum width of the viewport. It accounts for the frame
// size, in addition to the gutter size.
func (m Model) maxWidth() int {
	var gutterSize int
	if m.LeftGutterFunc != nil {
		gutterSize = ansi.StringWidth(m.LeftGutterFunc(GutterContext{}))
	}
	return max(0, m.Width()-m.Style.GetHorizontalFrameSize()-gutterSize)
}

// maxHeight returns the maximum height of the viewport. It accounts for the frame
// size.
func (m Model) maxHeight() int {
	return max(0, m.Height()-m.Style.GetVerticalFrameSize())
}

// visibleLines returns the lines that should currently be visible in the
// viewport.
func (m Model) visibleLines() (lines []string) {
	maxHeight := m.maxHeight()
	maxWidth := m.maxWidth()

	if maxHeight == 0 || maxWidth == 0 {
		return nil
	}

	total, ridx, voffset := m.calculateLine(m.YOffset())
	if total > 0 {
		bottom := clamp(ridx+maxHeight, ridx, len(m.lines))
		lines = m.styleLines(slices.Clone(m.lines[ridx:bottom]), ridx)
		lines = m.highlightLines(lines, ridx)
	}

	for m.FillHeight && len(lines) < maxHeight {
		lines = append(lines, "")
	}

	// if longest line fit within width, no need to do anything else.
	if (m.xOffset == 0 && m.longestLineWidth <= maxWidth) || maxWidth == 0 {
		return m.setupGutter(lines, total, ridx)
	}

	if m.SoftWrap {
		return m.softWrap(lines, maxWidth, maxHeight, total, ridx, voffset)
	}

	// Cut the lines to the viewport width.
	for i := range lines {
		lines[i] = ansi.Cut(lines[i], m.xOffset, m.xOffset+maxWidth)
	}
	return m.setupGutter(lines, total, ridx)
}

// styleLines styles the lines using [Model.StyleLineFunc].
func (m Model) styleLines(lines []string, offset int) []string {
	if m.StyleLineFunc == nil {
		return lines
	}
	for i := range lines {
		lines[i] = m.StyleLineFunc(i + offset).Render(lines[i])
	}
	return lines
}

// highlightLines highlights the lines with [Model.HighlightStyle] and
// [Model.SelectedHighlightStyle].
func (m Model) highlightLines(lines []string, offset int) []string {
	if len(m.highlights) == 0 {
		return lines
	}
	for i := range lines {
		ranges := makeHighlightRanges(
			m.highlights,
			i+offset,
			m.HighlightStyle,
		)
		lines[i] = lipgloss.StyleRanges(lines[i], ranges...)
		if m.hiIdx < 0 {
			continue
		}
		sel := m.highlights[m.hiIdx]
		if hi, ok := sel.lines[i+offset]; ok {
			lines[i] = lipgloss.StyleRanges(lines[i], lipgloss.NewRange(
				hi[0],
				hi[1],
				m.SelectedHighlightStyle,
			))
		}
	}
	return lines
}

func (m Model) softWrap(lines []string, maxWidth, maxHeight, total, ridx, voffset int) []string {
	wrappedLines := make([]string, 0, maxHeight)

	var idx, lineWidth int
	var truncatedLine string

	for i, line := range lines {
		// If the line is less than or equal to the max width, it can be added
		// as is.
		lineWidth = ansi.StringWidth(line)

		if lineWidth <= maxWidth {
			if m.LeftGutterFunc != nil {
				line = m.LeftGutterFunc(GutterContext{
					Index:      i + ridx,
					TotalLines: total,
					Soft:       false,
				}) + line
			}
			wrappedLines = append(wrappedLines, line)
			continue
		}

		idx = 0
		for lineWidth > idx {
			truncatedLine = ansi.Cut(line, idx, maxWidth+idx)
			if m.LeftGutterFunc != nil {
				truncatedLine = m.LeftGutterFunc(GutterContext{
					Index:      i + ridx,
					TotalLines: total,
					Soft:       idx > 0,
				}) + truncatedLine
			}
			wrappedLines = append(wrappedLines, truncatedLine)
			idx += maxWidth
		}
	}

	return wrappedLines[voffset:min(voffset+maxHeight, len(wrappedLines))]
}

// setupGutter sets up the left gutter using [Model.LeftGutterFunc].
func (m Model) setupGutter(lines []string, total, ridx int) []string {
	if m.LeftGutterFunc == nil {
		return lines
	}

	for i := range lines {
		lines[i] = m.LeftGutterFunc(GutterContext{
			Index:      i + ridx,
			TotalLines: total,
			Soft:       false,
		}) + lines[i]
	}
	return lines
}

// SetYOffset sets the Y offset.
func (m *Model) SetYOffset(n int) {
	m.yOffset = clamp(n, 0, m.maxYOffset())
}

// YOffset returns the current Y offset - the vertical scroll position.
func (m *Model) YOffset() int { return m.yOffset }

// EnsureVisible ensures that the given line and column are in the viewport.
func (m *Model) EnsureVisible(line, colstart, colend int) {
	maxWidth := m.maxWidth()
	if colend <= maxWidth {
		m.SetXOffset(0)
	} else {
		m.SetXOffset(colstart - m.horizontalStep) // put one step to the left, feels more natural
	}

	if line < m.YOffset() || line >= m.YOffset()+m.maxHeight() {
		m.SetYOffset(line)
	}
}

// PageDown moves the view down by the number of lines in the viewport.
func (m *Model) PageDown() {
	if m.AtBottom() {
		return
	}
	m.ScrollDown(m.Height())
}

// PageUp moves the view up by one height of the viewport.
func (m *Model) PageUp() {
	if m.AtTop() {
		return
	}
	m.ScrollUp(m.Height())
}

// HalfPageDown moves the view down by half the height of the viewport.
func (m *Model) HalfPageDown() {
	if m.AtBottom() {
		return
	}
	m.ScrollDown(m.Height() / 2) //nolint:mnd
}

// HalfPageUp moves the view up by half the height of the viewport.
func (m *Model) HalfPageUp() {
	if m.AtTop() {
		return
	}
	m.ScrollUp(m.Height() / 2) //nolint:mnd
}

// ScrollDown moves the view down by the given number of lines.
func (m *Model) ScrollDown(n int) {
	if m.AtBottom() || n == 0 || len(m.lines) == 0 {
		return
	}
	// Make sure the number of lines by which we're going to scroll isn't
	// greater than the number of lines we actually have left before we reach
	// the bottom.
	m.SetYOffset(m.YOffset() + n)
	m.hiIdx = m.findNearestMatch()
}

// ScrollUp moves the view up by the given number of lines.
func (m *Model) ScrollUp(n int) {
	if m.AtTop() || n == 0 || len(m.lines) == 0 {
		return
	}
	// Make sure the number of lines by which we're going to scroll isn't
	// greater than the number of lines we are from the top.
	m.SetYOffset(m.YOffset() - n)
	m.hiIdx = m.findNearestMatch()
}

// SetHorizontalStep sets the amount of cells that the viewport moves in the
// default viewport keymapping. If set to 0 or less, horizontal scrolling is
// disabled.
func (m *Model) SetHorizontalStep(n int) {
	m.horizontalStep = max(0, n)
}

// XOffset returns the current X offset - the horizontal scroll position.
func (m *Model) XOffset() int { return m.xOffset }

// SetXOffset sets the X offset.
// No-op when soft wrap is enabled.
func (m *Model) SetXOffset(n int) {
	if m.SoftWrap {
		return
	}
	m.xOffset = clamp(n, 0, m.maxXOffset())
}

// ScrollLeft moves the viewport to the left by the given number of columns.
func (m *Model) ScrollLeft(n int) {
	m.SetXOffset(m.xOffset - n)
}

// ScrollRight moves viewport to the right by the given number of columns.
func (m *Model) ScrollRight(n int) {
	m.SetXOffset(m.xOffset + n)
}

// TotalLineCount returns the total number of lines (both hidden and visible) within the viewport.
func (m Model) TotalLineCount() int {
	total, _, _ := m.calculateLine(0)
	return total
}

// VisibleLineCount returns the number of the visible lines within the viewport.
func (m Model) VisibleLineCount() int {
	return len(m.visibleLines())
}

// GotoTop sets the viewport to the top position.
func (m *Model) GotoTop() (lines []string) {
	if m.AtTop() {
		return nil
	}
	m.SetYOffset(0)
	m.hiIdx = m.findNearestMatch()
	return m.visibleLines()
}

// GotoBottom sets the viewport to the bottom position.
func (m *Model) GotoBottom() (lines []string) {
	m.SetYOffset(m.maxYOffset())
	m.hiIdx = m.findNearestMatch()
	return m.visibleLines()
}

// SetHighlights sets ranges of characters to highlight.
// For instance, `[]int{[]int{2, 10}, []int{20, 30}}` will highlight characters
// 2 to 10 and 20 to 30.
// Note that highlights are not expected to transpose each other, and are also
// expected to be in order.
// Use [Model.SetHighlights] to set the highlight ranges, and
// [Model.HighlightNext] and [Model.HighlightPrevious] to navigate.
// Use [Model.ClearHighlights] to remove all highlights.
func (m *Model) SetHighlights(matches [][]int) {
	if len(matches) == 0 || len(m.lines) == 0 {
		return
	}
	m.highlights = parseMatches(m.GetContent(), matches)
	m.hiIdx = m.findNearestMatch()
	m.showHighlight()
}

// ClearHighlights clears previously set highlights.
func (m *Model) ClearHighlights() {
	m.highlights = nil
	m.hiIdx = -1
}

func (m *Model) showHighlight() {
	if m.hiIdx == -1 {
		return
	}
	line, colstart, colend := m.highlights[m.hiIdx].coords()
	m.EnsureVisible(line, colstart, colend)
}

// HighlightNext highlights the next match.
func (m *Model) HighlightNext() {
	if m.highlights == nil {
		return
	}
	m.hiIdx = (m.hiIdx + 1) % len(m.highlights)
	m.showHighlight()
}

// HighlightPrevious highlights the previous match.
func (m *Model) HighlightPrevious() {
	if m.highlights == nil {
		return
	}
	m.hiIdx = (m.hiIdx - 1 + len(m.highlights)) % len(m.highlights)
	m.showHighlight()
}

func (m Model) findNearestMatch() int {
	for i, match := range m.highlights {
		if match.lineStart >= m.YOffset() {
			return i
		}
	}
	return -1
}

// Update handles standard message-based viewport updates.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	m = m.updateAsModel(msg)
	return m, nil
}

// Author's note: this method has been broken out to make it easier to
// potentially transition Update to satisfy tea.Model.
func (m Model) updateAsModel(msg tea.Msg) Model {
	if !m.initialized {
		m.setInitialValues()
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.KeyMap.PageDown):
			m.PageDown()

		case key.Matches(msg, m.KeyMap.PageUp):
			m.PageUp()

		case key.Matches(msg, m.KeyMap.HalfPageDown):
			m.HalfPageDown()

		case key.Matches(msg, m.KeyMap.HalfPageUp):
			m.HalfPageUp()

		case key.Matches(msg, m.KeyMap.Down):
			m.ScrollDown(1)

		case key.Matches(msg, m.KeyMap.Up):
			m.ScrollUp(1)

		case key.Matches(msg, m.KeyMap.Left):
			m.ScrollLeft(m.horizontalStep)

		case key.Matches(msg, m.KeyMap.Right):
			m.ScrollRight(m.horizontalStep)
		}

	case tea.MouseWheelMsg:
		if !m.MouseWheelEnabled {
			break
		}
		switch msg.Button {
		case tea.MouseWheelDown:
			// NOTE: some terminal emulators don't send the shift event for
			// mouse actions.
			if msg.Mod.Contains(tea.ModShift) {
				m.ScrollRight(m.horizontalStep)
				break
			}
			m.ScrollDown(m.MouseWheelDelta)
		case tea.MouseWheelUp:
			// NOTE: some terminal emulators don't send the shift event for
			// mouse actions.
			if msg.Mod.Contains(tea.ModShift) {
				m.ScrollLeft(m.horizontalStep)
				break
			}
			m.ScrollUp(m.MouseWheelDelta)
		case tea.MouseWheelLeft:
			m.ScrollLeft(m.horizontalStep)
		case tea.MouseWheelRight:
			m.ScrollRight(m.horizontalStep)
		}
	}

	return m
}

// View renders the viewport into a string.
func (m Model) View() string {
	w, h := m.Width(), m.Height()
	if sw := m.Style.GetWidth(); sw != 0 {
		w = min(w, sw)
	}
	if sh := m.Style.GetHeight(); sh != 0 {
		h = min(h, sh)
	}

	if w == 0 || h == 0 {
		return ""
	}

	contentWidth := w - m.Style.GetHorizontalFrameSize()
	contentHeight := h - m.Style.GetVerticalFrameSize()
	contents := lipgloss.NewStyle().
		Width(contentWidth).   // pad to width.
		Height(contentHeight). // pad to height.
		Render(strings.Join(m.visibleLines(), "\n"))
	return m.Style.
		UnsetWidth().UnsetHeight(). // Style size already applied in contents.
		Render(contents)
}

func clamp[T cmp.Ordered](v, low, high T) T {
	if high < low {
		low, high = high, low
	}
	return min(high, max(low, v))
}

func maxLineWidth(lines []string) int {
	result := 0
	for _, line := range lines {
		result = max(result, ansi.StringWidth(line))
	}
	return result
}
