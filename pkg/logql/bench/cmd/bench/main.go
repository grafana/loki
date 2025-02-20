package main

import (
	"fmt"
	"io"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/grafana/loki/v3/pkg/logql/bench"
	"github.com/grafana/loki/v3/pkg/logql/bench/cmd/bench/views"
)

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
	}
}

func (m mainModel) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		func() tea.Msg { return tea.WindowSizeMsg{} }, // Request initial window size
	)
}

func (m mainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Always store window dimensions
		m.width = msg.Width
		m.height = msg.Height

		if m.currentView == views.ListID {
			m.listView.SetSize(msg.Width, msg.Height)
		} else if m.currentView == views.RunID && m.runView != nil {
			// Update run view size through its Update method
			newModel, _ := m.runView.Update(msg)
			if runView, ok := newModel.(*views.RunView); ok {
				m.runView = runView
			}
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.currentView == views.ListID {
				return m, tea.Quit
			} else {
				m.currentView = views.ListID
				m.runView = nil // Clear the run view
				m.listView.Refresh()
				m.listView.SetSize(m.width, m.height) // Ensure list view has correct size
				return m, tea.Batch(
					tea.ClearScreen,
					func() tea.Msg { return tea.WindowSizeMsg{Width: m.width, Height: m.height} },
				)
			}
		case "esc":
			if m.currentView != views.ListID {
				m.currentView = views.ListID
				m.runView = nil // Clear the run view
				m.listView.Refresh()
				m.listView.SetSize(m.width, m.height) // Ensure list view has correct size
				return m, tea.Batch(
					tea.ClearScreen,
					func() tea.Msg { return tea.WindowSizeMsg{Width: m.width, Height: m.height} },
				)
			}
		}

	case views.SwitchViewMsg:
		switch msg.View {
		case views.RunID:
			// Switch to run view
			m.currentView = views.RunID
			m.runView = views.NewRunView(views.RunConfig{
				Count:        1,
				TraceEnabled: false,
				Selected:     m.listView.GetSelectedTests(),
			})
			cmds = append(cmds,
				tea.ClearScreen,
				func() tea.Msg { return tea.WindowSizeMsg{Width: m.width, Height: m.height} },
			)
		case views.ListID:
			// Switch back to list view
			m.currentView = views.ListID
			m.runView = nil
			m.listView.Refresh()
			m.listView.SetSize(m.width, m.height) // Ensure list view has correct size
			cmds = append(cmds,
				tea.ClearScreen,
				func() tea.Msg { return tea.WindowSizeMsg{Width: m.width, Height: m.height} },
			)
		}
		return m, tea.Batch(cmds...)
	}

	// Handle view-specific updates
	if m.currentView == views.ListID {
		newModel, cmd := m.listView.Update(msg)
		if listView, ok := newModel.(*views.ListView); ok {
			m.listView = listView
		}
		return m, cmd
	} else if m.currentView == views.RunID && m.runView != nil {
		newModel, cmd := m.runView.Update(msg)
		if runView, ok := newModel.(*views.RunView); ok {
			m.runView = runView
		}
		return m, cmd
	}

	return m, nil
}

func (m mainModel) View() string {
	switch m.currentView {
	case views.ListID:
		return m.listView.View()
	case views.RunID:
		return m.runView.View()
	default:
		return "Unknown view"
	}
}

// loadBenchmarks loads available benchmarks
func loadBenchmarks() []string {
	config, err := bench.LoadConfig(bench.DefaultDataDir)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	cases := config.GenerateTestCases()
	var names []string
	for _, c := range cases {
		names = append(names, fmt.Sprintf("BenchmarkLogQL/%s", c.Name()))
	}
	return names
}

func main() {
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
	if len(os.Args) < 2 {
		fmt.Println("Usage: bench [list|run]")
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "list":
		listBenchmarks()
	case "run":
		p := tea.NewProgram(
			initialModel(),
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
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
