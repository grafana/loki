package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/grafana/loki/v3/pkg/logql/bench"
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

// selectionModel represents the UI model for benchmark selection
type selectionModel struct {
	list     list.Model
	selected map[string]struct{}
	quitting bool
}

func newSelectionModel(items []string) selectionModel {
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

	// Set initial size to use full terminal width
	l.SetSize(100, 30)

	return selectionModel{
		list:     l,
		selected: selected,
	}
}

func (m selectionModel) Init() tea.Cmd {
	return nil
}

func (m selectionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle filter mode differently
		if m.list.FilterState() == list.Filtering {
			switch msg.String() {
			case "esc":
				m.list.ResetFilter() // Clear filter and exit filter mode
				return m, nil
			default:
				// Let the list handle all other keys in filter mode
				var cmd tea.Cmd
				m.list, cmd = m.list.Update(msg)
				return m, cmd
			}
		}

		// Normal mode key handling
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case " ":
			// Toggle selection of current item
			item := m.list.SelectedItem().(benchmarkItem)
			if _, exists := m.selected[item.name]; exists {
				delete(m.selected, item.name)
			} else {
				m.selected[item.name] = struct{}{}
			}
			return m, nil
		case "right", "l":
			// Select all visible items
			for _, item := range m.list.VisibleItems() {
				m.selected[item.(benchmarkItem).name] = struct{}{}
			}
			return m, nil
		case "left", "h":
			// Unselect all visible items
			for _, item := range m.list.VisibleItems() {
				delete(m.selected, item.(benchmarkItem).name)
			}
			return m, nil
		case "enter":
			if len(m.selected) > 0 {
				return m, tea.Quit
			}
		}
	case tea.WindowSizeMsg:
		h, v := msg.Width, msg.Height
		m.list.SetSize(h, v)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m selectionModel) View() string {
	if m.quitting {
		return ""
	}

	// Add selection count to the title
	m.list.Title = fmt.Sprintf("Select benchmarks to run (%d selected)", len(m.selected))

	return m.list.View()
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: bench [list|run]")
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "list":
		listBenchmarks()
	case "run":
		runBenchmarks()
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

func listBenchmarks() {
	// Load the generator config
	config, err := bench.LoadConfig(bench.DefaultDataDir)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Generate test cases
	cases := config.GenerateTestCases()

	// Print each benchmark name
	for _, c := range cases {
		fmt.Printf("BenchmarkLogQL/%s\n", c.Name())
	}
}

func runBenchmarks() {
	// Load the generator config
	config, err := bench.LoadConfig(bench.DefaultDataDir)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Generate test cases
	cases := config.GenerateTestCases()

	// Create list of benchmark names
	var names []string
	for _, c := range cases {
		names = append(names, fmt.Sprintf("BenchmarkLogQL/%s", c.Name()))
	}

	if len(names) == 0 {
		fmt.Println("No benchmarks found")
		return
	}

	// Create and run the selection UI with alternate screen mode
	p := tea.NewProgram(
		newSelectionModel(names),
		tea.WithAltScreen(),       // Use alternate screen
		tea.WithMouseCellMotion(), // Enable mouse support
	)

	m, err := p.Run()
	if err != nil {
		fmt.Printf("Error running selection UI: %v\n", err)
		os.Exit(1)
	}
	handleSelectedBenchmarks(m)
}

func handleSelectedBenchmarks(m tea.Model) {
	// Get selected benchmarks
	model := m.(selectionModel)
	if model.quitting || len(model.selected) == 0 {
		fmt.Println("No benchmarks selected")
		return
	}

	// Convert selected map to slice and sort
	var selected []string
	for name := range model.selected {
		// Escape special regex characters in the benchmark name
		escaped := strings.ReplaceAll(name, "{", "\\{")
		escaped = strings.ReplaceAll(escaped, "}", "\\}")
		escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
		escaped = strings.ReplaceAll(escaped, "[", "\\[")
		escaped = strings.ReplaceAll(escaped, "]", "\\]")
		escaped = strings.ReplaceAll(escaped, "(", "\\(")
		escaped = strings.ReplaceAll(escaped, ")", "\\)")
		escaped = strings.ReplaceAll(escaped, "=", "\\=")
		escaped = strings.ReplaceAll(escaped, "|", "\\|")
		escaped = "^" + escaped + "$" // Ensure exact match
		selected = append(selected, escaped)
	}

	// Build the benchmark regex
	regex := strings.Join(selected, "|")
	benchCmd := fmt.Sprintf("go test -v -test.run=^$ -test.bench='%s' -test.benchmem -count=1 -timeout=1h", regex)

	// Show the command that will be run
	fmt.Printf("\nWill run command:\n%s\n\n", benchCmd)

	// Ask for confirmation
	fmt.Print("Run these benchmarks? [y/N] ")
	var answer string
	if _, err := fmt.Scanln(&answer); err != nil {
		fmt.Println("Error reading input:", err)
		os.Exit(1)
	}
	if !strings.HasPrefix(strings.ToLower(answer), "y") {
		fmt.Println("Cancelled")
		return
	}

	// Run the benchmarks
	runCmd := exec.Command("go", "test", "-v",
		"-test.run=^$",              // Skip tests
		"-test.bench="+regex,        // Run only selected benchmarks
		"-test.benchmem",            // Show memory stats
		"-test.cpuprofile=cpu.prof", // CPU profiling
		"-test.memprofile=mem.prof", // Memory profiling
		"-test.trace=trace.out",     // Execution tracing
		"-count=1",                  // Run once
		"-timeout=1h")
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	if err := runCmd.Run(); err != nil {
		fmt.Printf("Error running benchmarks: %v\n", err)
		os.Exit(1)
	}
}
