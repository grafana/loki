// Package help provides a simple help view for Bubble Tea applications.
package help

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// KeyMap is a map of keybindings used to generate help. Since it's an
// interface it can be any type, though struct or a map[string][]key.Binding
// are likely candidates.
//
// Note that if a key is disabled (via key.Binding.SetEnabled) it will not be
// rendered in the help view, so in theory generated help should self-manage.
type KeyMap interface {
	// ShortHelp returns a slice of bindings to be displayed in the short
	// version of the help. The help bubble will render help in the order in
	// which the help items are returned here.
	ShortHelp() []key.Binding

	// FullHelp returns an extended group of help items, grouped by columns.
	// The help bubble will render the help in the order in which the help
	// items are returned here.
	FullHelp() [][]key.Binding
}

// Styles is a set of available style definitions for the Help bubble.
type Styles struct {
	Ellipsis lipgloss.Style

	// Styling for the short help
	ShortKey       lipgloss.Style
	ShortDesc      lipgloss.Style
	ShortSeparator lipgloss.Style

	// Styling for the full help
	FullKey       lipgloss.Style
	FullDesc      lipgloss.Style
	FullSeparator lipgloss.Style
}

// DefaultStyles returns a set of default styles for the help bubble. Light or
// dark styles can be selected by passing true or false to the isDark
// parameter.
func DefaultStyles(isDark bool) Styles {
	lightDark := lipgloss.LightDark(isDark)

	keyStyle := lipgloss.NewStyle().Foreground(lightDark(lipgloss.Color("#909090"), lipgloss.Color("#626262")))
	descStyle := lipgloss.NewStyle().Foreground(lightDark(lipgloss.Color("#B2B2B2"), lipgloss.Color("#4A4A4A")))
	sepStyle := lipgloss.NewStyle().Foreground(lightDark(lipgloss.Color("#DADADA"), lipgloss.Color("#3C3C3C")))

	return Styles{
		ShortKey:       keyStyle,
		ShortDesc:      descStyle,
		ShortSeparator: sepStyle,
		Ellipsis:       sepStyle,
		FullKey:        keyStyle,
		FullDesc:       descStyle,
		FullSeparator:  sepStyle,
	}
}

// DefaultDarkStyles returns a set of default styles for dark backgrounds.
func DefaultDarkStyles() Styles {
	return DefaultStyles(true)
}

// DefaultLightStyles returns a set of default styles for light backgrounds.
func DefaultLightStyles() Styles {
	return DefaultStyles(false)
}

// Model contains the state of the help view.
type Model struct {
	ShowAll bool // if true, render the "full" help menu

	ShortSeparator string
	FullSeparator  string

	// The symbol we use in the short help when help items have been truncated
	// due to width. Periods of ellipsis by default.
	Ellipsis string

	Styles Styles

	width int
}

// New creates a new help view with some useful defaults.
func New() Model {
	return Model{
		ShortSeparator: " • ",
		FullSeparator:  "    ",
		Ellipsis:       "…",
		Styles:         DefaultDarkStyles(),
	}
}

// Update helps satisfy the Bubble Tea Model interface. It's a no-op.
func (m Model) Update(_ tea.Msg) (Model, tea.Cmd) {
	return m, nil
}

// View renders the help view's current state.
func (m Model) View(k KeyMap) string {
	if m.ShowAll {
		return m.FullHelpView(k.FullHelp())
	}
	return m.ShortHelpView(k.ShortHelp())
}

// SetWidth sets the maximum width for the help view.
func (m *Model) SetWidth(w int) {
	m.width = w
}

// Width returns the maximum width for the help view.
func (m Model) Width() int {
	return m.width
}

// ShortHelpView renders a single line help view from a slice of keybindings.
// If the line is longer than the maximum width it will be gracefully
// truncated, showing only as many help items as possible.
func (m Model) ShortHelpView(bindings []key.Binding) string {
	if len(bindings) == 0 {
		return ""
	}

	var b strings.Builder
	var totalWidth int
	separator := m.Styles.ShortSeparator.Inline(true).Render(m.ShortSeparator)

	for i, kb := range bindings {
		if !kb.Enabled() {
			continue
		}

		// Sep
		var sep string
		if totalWidth > 0 && i < len(bindings) {
			sep = separator
		}

		// Item
		str := sep +
			m.Styles.ShortKey.Inline(true).Render(kb.Help().Key) + " " +
			m.Styles.ShortDesc.Inline(true).Render(kb.Help().Desc)
		w := lipgloss.Width(str)

		// Tail
		if tail, ok := m.shouldAddItem(totalWidth, w); !ok {
			if tail != "" {
				b.WriteString(tail)
			}
			break
		}

		totalWidth += w
		b.WriteString(str)
	}

	return b.String()
}

// FullHelpView renders help columns from a slice of key binding slices. Each
// top level slice entry renders into a column.
func (m Model) FullHelpView(groups [][]key.Binding) string {
	if len(groups) == 0 {
		return ""
	}

	// Linter note: at this time we don't think it's worth the additional
	// code complexity involved in preallocating this slice.
	var (
		out []string

		totalWidth int
		separator  = m.Styles.FullSeparator.Inline(true).Render(m.FullSeparator)
	)

	// Iterate over groups to build columns
	for i, group := range groups {
		if group == nil || !shouldRenderColumn(group) {
			continue
		}
		var (
			sep          string
			keys         []string
			descriptions []string
		)

		// Sep
		if totalWidth > 0 && i < len(groups) {
			sep = separator
		}

		// Separate keys and descriptions into different slices
		for _, kb := range group {
			if !kb.Enabled() {
				continue
			}
			keys = append(keys, kb.Help().Key)
			descriptions = append(descriptions, kb.Help().Desc)
		}

		// Column
		col := lipgloss.JoinHorizontal(lipgloss.Top,
			sep,
			m.Styles.FullKey.Render(strings.Join(keys, "\n")),
			" ",
			m.Styles.FullDesc.Render(strings.Join(descriptions, "\n")),
		)
		w := lipgloss.Width(col)

		// Tail
		if tail, ok := m.shouldAddItem(totalWidth, w); !ok {
			if tail != "" {
				out = append(out, tail)
			}
			break
		}

		totalWidth += w
		out = append(out, col)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, out...)
}

func (m Model) shouldAddItem(totalWidth, width int) (tail string, ok bool) {
	// If there's room for an ellipsis, print that.
	if m.width > 0 && totalWidth+width > m.width {
		tail = " " + m.Styles.Ellipsis.Inline(true).Render(m.Ellipsis)

		if totalWidth+lipgloss.Width(tail) < m.width {
			return tail, false
		}
	}
	return "", true
}

func shouldRenderColumn(b []key.Binding) (ok bool) {
	for _, v := range b {
		if v.Enabled() {
			return true
		}
	}
	return false
}
