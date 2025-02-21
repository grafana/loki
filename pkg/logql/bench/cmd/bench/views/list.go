package views

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// benchmarkItem represents a benchmark in the list
type benchmarkItem struct {
	name string
}

func (i benchmarkItem) Title() string       { return i.name }
func (i benchmarkItem) Description() string { return "" }
func (i benchmarkItem) FilterValue() string { return i.name }

// selectionDelegate implements a custom delegate for the list
type selectionDelegate struct {
	list.DefaultDelegate
	selected map[string]struct{}
}

func newSelectionDelegate(selected map[string]struct{}) selectionDelegate {
	d := selectionDelegate{
		DefaultDelegate: list.NewDefaultDelegate(),
		selected:        selected,
	}

	// Customize the styles for a more compact look
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Foreground(lipgloss.Color("12"))
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(lipgloss.Color("12"))
	d.Styles.NormalTitle = d.Styles.NormalTitle.UnsetWidth()
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.UnsetWidth()

	return d
}

// Height returns the height of a list item
func (d selectionDelegate) Height() int {
	return 1 // Single line height
}

// Spacing returns the spacing between list items
func (d selectionDelegate) Spacing() int {
	return 0 // No spacing between items
}

func (d selectionDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	benchmark := item.(benchmarkItem)
	selected := ""
	if _, ok := d.selected[benchmark.name]; ok {
		selected = "✓"
	} else {
		selected = " "
	}

	title := selected + " " + benchmark.name
	if index == m.Index() {
		title = d.Styles.SelectedTitle.Render(title)
	} else {
		title = d.Styles.NormalTitle.Render(title)
	}

	fmt.Fprint(w, title)
}

// ListView represents the benchmark selection view
type ListView struct {
	list     list.Model
	selected map[string]struct{}
}

// NewListView creates a new ListView
func NewListView(items []string) *ListView {
	selected := make(map[string]struct{})
	listItems := make([]list.Item, len(items))
	for i, name := range items {
		listItems[i] = benchmarkItem{name: name}
	}

	delegate := newSelectionDelegate(selected)
	l := list.New(listItems, delegate, 0, 0)

	// Customize list styles
	styles := list.DefaultStyles()
	styles.Title = styles.Title.
		Foreground(lipgloss.Color("12")).
		Bold(true).
		Padding(0, 1)

	styles.FilterPrompt = lipgloss.NewStyle().
		Foreground(lipgloss.Color("12"))
	styles.FilterCursor = lipgloss.NewStyle().
		Foreground(lipgloss.Color("12"))

	// Status bar style
	styles.StatusBar = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(0, 1)

	// Help style - make it more visible with high contrast
	styles.HelpStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "12", Dark: "12"}). // Bright blue for better visibility
		Bold(true)

	l.Styles = styles

	// Configure list
	l.Title = "Select benchmarks to run"
	l.SetShowHelp(true)
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(true)
	l.SetShowFilter(true)
	l.DisableQuitKeybindings()
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(
				key.WithKeys("space"),
				key.WithHelp("space", "toggle selection"),
			),
			key.NewBinding(
				key.WithKeys("left", "h"),
				key.WithHelp("←/h", "unselect all"),
			),
			key.NewBinding(
				key.WithKeys("right", "l"),
				key.WithHelp("→/l", "select all"),
			),
			key.NewBinding(
				key.WithKeys("/"),
				key.WithHelp("/", "filter"),
			),
			key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "clear filter"),
			),
			key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("enter", "confirm"),
			),
			key.NewBinding(
				key.WithKeys("q", "ctrl+c"),
				key.WithHelp("q", "quit"),
			),
		}
	}
	l.AdditionalFullHelpKeys = l.AdditionalShortHelpKeys

	return &ListView{
		list:     l,
		selected: selected,
	}
}

func (m *ListView) Init() tea.Cmd {
	return nil
}

func (m *ListView) FilterState() list.FilterState {
	return m.list.FilterState()
}

func (m *ListView) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height)
	case tea.KeyMsg:
		// Handle filter mode differently
		if m.list.FilterState() == list.Filtering {
			switch msg.String() {
			case "esc":
				m.list.ResetFilter() // Clear filter and exit filter mode
				return m, nil
			default:
				// Let the list handle all other keys in filter mode
				m.list, cmd = m.list.Update(msg)
				return m, cmd
			}
		}

		// Normal mode key handling
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case " ":
			// Toggle selection of current item
			item := m.list.SelectedItem().(benchmarkItem)
			if _, exists := m.selected[item.name]; exists {
				delete(m.selected, item.name)
			} else {
				m.selected[item.name] = struct{}{}
			}
			m.list.Title = fmt.Sprintf("Select benchmarks to run (%d selected)", len(m.selected))
			return m, nil
		case "right", "l":
			// Select all visible items
			for _, item := range m.list.VisibleItems() {
				m.selected[item.(benchmarkItem).name] = struct{}{}
			}
			m.list.Title = fmt.Sprintf("Select benchmarks to run (%d selected)", len(m.selected))
			return m, nil
		case "left", "h":
			// Unselect all visible items
			for _, item := range m.list.VisibleItems() {
				delete(m.selected, item.(benchmarkItem).name)
			}
			m.list.Title = fmt.Sprintf("Select benchmarks to run (%d selected)", len(m.selected))
			return m, nil
		case "enter":
			if len(m.selected) > 0 {
				return m, func() tea.Msg {
					return SwitchViewMsg{View: RunID}
				}
			}
		}
	}

	// Handle other updates
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *ListView) View() string {
	return m.list.View()
}

// GetSelectedTests returns the list of selected benchmark tests
func (m *ListView) GetSelectedTests() []string {
	var selected []string
	for name := range m.selected {
		selected = append(selected, name)
	}
	return selected
}
