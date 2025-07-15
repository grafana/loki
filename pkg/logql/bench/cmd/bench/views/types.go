package views

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Global program reference for sending messages
var globalProgram *tea.Program

// SetProgram sets the global program reference
func SetProgram(p *tea.Program) {
	globalProgram = p
}

// ViewID identifies different views in the application
type ViewID int

// View identifiers
const (
	ListID ViewID = iota
	RunID
)

// Shared messages
type (
	// SwitchViewMsg is sent when switching between views
	SwitchViewMsg struct {
		View ViewID
	}

	// BenchmarkOutputMsg is sent when new benchmark output is available
	BenchmarkOutputMsg string

	// BenchmarkFinishedMsg is sent when a benchmark run completes
	BenchmarkFinishedMsg struct{}
)

// Shared styles
var (
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12")).
			Padding(0, 1)

	ConfigStyle = lipgloss.NewStyle().
			Padding(0, 2)

	ControlStyle = lipgloss.NewStyle().
			Padding(0, 2).
			Foreground(lipgloss.Color("241"))

	ViewportStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62"))
)

// StatusText returns a styled status string based on running state
func StatusText(running bool) string {
	if running {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Render("Running")
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Render("Ready")
}

// Model interface that all views must implement
type Model interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (Model, tea.Cmd)
	View() string
}

// RunConfig holds the configuration for running benchmarks
type RunConfig struct {
	Count        int
	TraceEnabled bool
	Selected     []string
	StorageType  string // Storage type to use: "dataobj", "chunk", or "both"
}

// ViewportConfig holds the configuration for the viewport
type ViewportConfig struct {
	Width  int
	Height int
}
