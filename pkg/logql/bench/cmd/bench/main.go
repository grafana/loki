package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/grafana/loki/v3/pkg/logql/bench"
	"github.com/grafana/loki/v3/pkg/logql/bench/cmd/bench/views"
)

var (
	suiteFlag = flag.String("suite", "fast", "benchmark suite to run: fast, regression, or exhaustive")
)

// parseSuiteFlag parses the suite flag and returns the appropriate Suite values to load
func parseSuiteFlag() []bench.Suite {
	switch *suiteFlag {
	case "fast":
		return []bench.Suite{bench.SuiteFast}
	case "regression":
		return []bench.Suite{bench.SuiteFast, bench.SuiteRegression}
	case "exhaustive":
		return []bench.Suite{bench.SuiteFast, bench.SuiteRegression, bench.SuiteExhaustive}
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid suite %q (must be fast, regression, or exhaustive)\n", *suiteFlag)
		os.Exit(1)
		return nil
	}
}

// mainModel represents the overall application state
type mainModel struct {
	currentView views.ViewID
	listView    *views.ListView
	runView     *views.RunView
	width       int
	height      int
}

func initialModel() mainModel {
	return mainModel{
		currentView: views.ListID,
		listView:    views.NewListView(loadBenchmarks()),
		runView: views.NewRunView(views.RunConfig{
			Count:        1,
			TraceEnabled: false,
			Selected:     []string{},
			StorageType:  "both", // Default to running both storage types
		}),
	}
}

// Init is required by the tea.Model interface but doesn't need to do anything
func (m mainModel) Init() tea.Cmd {
	return nil
}

func (m mainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Always store window dimensions
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.currentView == views.ListID && m.listView.FilterState() != list.Filtering {
				// Stop all profiling tools before quitting
				m.runView.StopAllProfiling()()
				return m, tea.Quit
			}
			m.currentView = views.ListID
			newModel, cmd := m.listView.Update(msg)
			if listView, ok := newModel.(*views.ListView); ok {
				m.listView = listView
			}
			return m, cmd
		case "esc":
			if m.currentView != views.ListID {
				m.currentView = views.ListID
				return m, nil
			}
		default:
			if m.currentView == views.ListID {
				newModel, cmd := m.listView.Update(msg)
				if listView, ok := newModel.(*views.ListView); ok {
					m.listView = listView
				}
				return m, cmd
			}
			if m.currentView == views.RunID {
				newModel, cmd := m.runView.Update(msg)
				if runView, ok := newModel.(*views.RunView); ok {
					m.runView = runView
				}
				return m, cmd
			}
		}

	case views.SwitchViewMsg:
		switch msg.View {
		case views.RunID:
			// Switch to run view
			m.currentView = views.RunID
			m.runView.SetSelectedTests(m.listView.GetSelectedTests())
			return m, nil
		case views.ListID:
			// Switch back to list view
			m.currentView = views.ListID
			return m, nil
		}
	}

	var cmds []tea.Cmd
	newModel, cmd := m.listView.Update(msg)
	if listView, ok := newModel.(*views.ListView); ok {
		m.listView = listView
		cmds = append(cmds, cmd)
	}

	newModel, cmd = m.runView.Update(msg)
	if runView, ok := newModel.(*views.RunView); ok {
		m.runView = runView
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m mainModel) View() string {
	switch m.currentView {
	case views.ListID:
		return m.listView.View()
	case views.RunID:
		return m.runView.View()
	default:
		log.Printf("Main: Unknown view ID: %d", m.currentView)
		return "Unknown view"
	}
}

// loadBenchmarks loads available benchmarks
func loadBenchmarks() []string {
	queriesDir := "./queries"
	registry := bench.NewQueryRegistry(queriesDir)

	suites := parseSuiteFlag()
	if err := registry.Load(suites...); err != nil {
		fmt.Printf("Error loading query registry: %v\n", err)
		os.Exit(1)
	}

	metadata, err := bench.LoadMetadata(bench.DefaultDataDir)
	if err != nil {
		fmt.Printf("Error loading metadata: %v\n", err)
		os.Exit(1)
	}

	config, err := bench.LoadConfig(bench.DefaultDataDir)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	resolver := bench.NewMetadataVariableResolver(metadata, config.Seed)

	queryDefs := registry.GetQueries(false, suites...)
	var names []string
	for _, def := range queryDefs {
		expanded, err := registry.ExpandQuery(def, resolver, false)
		if err != nil {
			log.Fatalf("Error expanding query %q: %v", def.Description, err)
		}
		for _, c := range expanded {
			names = append(names, c.Name())
		}
	}
	return names
}

func main() {
	// Parse flags before processing commands
	flag.Parse()

	if len(os.Getenv("DEBUG")) > 0 {
		f, err := tea.LogToFile("debug.log", "debug")
		if err != nil {
			fmt.Println("fatal:", err)
			os.Exit(1)
		}
		defer f.Close()
	} else {
		log.SetOutput(io.Discard)
	}

	// Get command from remaining args after flag parsing
	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: bench [flags] [list|run]")
		fmt.Println("Flags:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	cmd := args[0]
	switch cmd {
	case "list":
		listBenchmarks()
	case "run":
		p := tea.NewProgram(
			initialModel(),
			tea.WithAltScreen(),
		)
		views.SetProgram(p) // Set global program reference for message sending
		if _, err := p.Run(); err != nil {
			fmt.Printf("Error running UI: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

func listBenchmarks() {
	queriesDir := "./queries"
	registry := bench.NewQueryRegistry(queriesDir)

	suites := parseSuiteFlag()
	if err := registry.Load(suites...); err != nil {
		fmt.Printf("Error loading query registry: %v\n", err)
		os.Exit(1)
	}

	metadata, err := bench.LoadMetadata(bench.DefaultDataDir)
	if err != nil {
		fmt.Printf("Error loading metadata: %v\n", err)
		os.Exit(1)
	}

	config, err := bench.LoadConfig(bench.DefaultDataDir)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	resolver := bench.NewMetadataVariableResolver(metadata, config.Seed)

	queryDefs := registry.GetQueries(false, suites...)
	for _, def := range queryDefs {
		expanded, err := registry.ExpandQuery(def, resolver, false)
		if err != nil {
			log.Fatalf("Error expanding query %q: %v", def.Description, err)
		}
		for _, c := range expanded {
			fmt.Printf("BenchmarkLogQL/%s\n", c.Name())
		}
	}
}
