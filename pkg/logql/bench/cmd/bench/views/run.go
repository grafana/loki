package views

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// RunView represents the benchmark run view
type RunView struct {
	RunConfig RunConfig
	Running   bool
	Output    string
	Viewport  viewport.Model
	ready     bool // track if we've received initial window size
}

// NewRunView creates a new RunView
func NewRunView(config RunConfig) *RunView {
	return &RunView{
		RunConfig: config,
		Running:   false,
		ready:     false,
	}
}

func (m *RunView) SetSelectedTests(tests []string) {
	m.RunConfig.Selected = tests
}

func (m *RunView) Init() tea.Cmd {
	return nil
}

func (m *RunView) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.Viewport.Height > 0 {
				m.Viewport.LineUp(1)
				return m, nil
			}
		case "down", "j":
			if m.Viewport.Height > 0 {
				m.Viewport.LineDown(1)
				return m, nil
			}
		case "pgup", "b":
			if m.Viewport.Height > 0 {
				m.Viewport.HalfViewUp()
				return m, nil
			}
		case "pgdown", " ":
			if m.Viewport.Height > 0 {
				m.Viewport.HalfViewDown()
				return m, nil
			}
		case "+":
			m.RunConfig.Count++
			return m, nil
		case "-":
			if m.RunConfig.Count > 1 {
				m.RunConfig.Count--
			}
			return m, nil
		case "t":
			m.RunConfig.TraceEnabled = !m.RunConfig.TraceEnabled
			return m, nil
		case "enter", "r":
			if !m.Running {
				m.Output = "" // Clear output before starting new run
				if m.Viewport.Height > 0 {
					m.Viewport.SetContent("")
				}
				log.Println("starting benchmark")
				return m, m.startBenchmark()
			}
		case "esc":
			return m, func() tea.Msg {
				return SwitchViewMsg{View: ListID}
			}
		}

	case tea.WindowSizeMsg:

		headerHeight := lipgloss.Height(m.headerView())
		footerHeight := lipgloss.Height(m.footerView())
		verticalMarginHeight := headerHeight + footerHeight

		if !m.ready {
			// First time initialization
			log.Printf("Initializing viewport: width=%d height=%d", msg.Width, msg.Height-verticalMarginHeight)
			m.Viewport = viewport.New(msg.Width, msg.Height-verticalMarginHeight)
			m.Viewport.YPosition = headerHeight // Place viewport below header
			m.Viewport.Style = ViewportStyle
			m.Viewport.Width = msg.Width
			m.Viewport.Height = msg.Height - verticalMarginHeight
			m.Viewport.SetContent("\n  Press ENTER to start the benchmark run\n")
			m.ready = true
		} else {
			// Always update viewport dimensions
			m.Viewport.Width = msg.Width
			m.Viewport.Height = msg.Height - verticalMarginHeight
		}

		// Update content if we have any
		if m.Output != "" {
			m.Viewport.SetContent(m.Output)
		}

	case BenchmarkOutputMsg:
		log.Printf("Received benchmark output: length=%d", len(string(msg)))
		m.Output += string(msg)
		m.Viewport.SetContent(m.Output)
		m.Viewport.GotoBottom()

	case BenchmarkFinishedMsg:
		log.Println("Benchmark finished")
		m.Running = false
	}

	// Handle viewport messages
	if m.ready {
		var cmd tea.Cmd
		m.Viewport, cmd = m.Viewport.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *RunView) View() string {
	var content string
	if !m.ready {
		content = "\n  Initializing...\n"
	} else {
		content = m.Viewport.View()
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.headerView(),
		content,
		m.footerView(),
	)
}

func (m *RunView) headerView() string {
	header := HeaderStyle.Render("Benchmark Configuration")
	config := ConfigStyle.Render(fmt.Sprintf(
		"Count: %d (+/- to adjust) • Trace: %v ('t' to toggle) • Status: %s",
		m.RunConfig.Count,
		m.RunConfig.TraceEnabled,
		StatusText(m.Running),
	))

	// Format selected benchmarks with proper indentation
	var formattedSelected []string
	for _, s := range m.RunConfig.Selected {
		formattedSelected = append(formattedSelected, "  "+s)
	}

	selectedStyle := ConfigStyle.Foreground(lipgloss.Color("241"))
	var selected string
	if len(formattedSelected) > 0 {
		selected = selectedStyle.Render("Selected:") + "\n" +
			selectedStyle.Render(strings.Join(formattedSelected, "\n"))
	} else {
		selected = selectedStyle.Render("Selected: none")
	}

	separator := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render(strings.Repeat("─", m.Viewport.Width))

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		config,
		selected,
		separator,
	)
}

func (m *RunView) footerView() string {
	separator := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render(strings.Repeat("─", m.Viewport.Width))

	return lipgloss.JoinVertical(lipgloss.Left,
		separator,
		ControlStyle.Render("↑/↓: scroll • ENTER: start • ESC: back"),
	)
}

// startBenchmark starts the benchmark execution
func (m *RunView) startBenchmark() tea.Cmd {
	return func() tea.Msg {
		log.Println("Starting benchmark execution")
		// Move old results if they exist
		if _, err := os.Stat("result.new.txt"); err == nil {
			if err := os.Rename("result.new.txt", "result.old.txt"); err != nil {
				return BenchmarkOutputMsg(fmt.Sprintf("Error moving old results: %v\n", err))
			}
		}

		// Create output file for new results
		outputFile, err := os.Create("result.new.txt")
		if err != nil {
			return BenchmarkOutputMsg(fmt.Sprintf("Error creating output file: %v\n", err))
		}

		// Build the benchmark command
		regex := strings.Join(m.RunConfig.Selected, "|")
		args := []string{
			"test", "-v",
			"-test.run=^$",              // Skip tests
			"-test.bench=" + regex,      // Run only selected benchmarks
			"-test.benchmem",            // Show memory stats
			"-test.cpuprofile=cpu.prof", // CPU profiling
			"-test.memprofile=mem.prof", // Memory profiling
			fmt.Sprintf("-count=%d", m.RunConfig.Count),
		}

		if m.RunConfig.TraceEnabled {
			args = append(args, "-test.trace=trace.out")
		}

		args = append(args, "-timeout=1h")

		log.Printf("Running command: go %s", strings.Join(args, " "))

		// Create and configure the command
		cmd := exec.Command("go", args...)

		// Create a pipe for capturing output
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			outputFile.Close()
			return BenchmarkOutputMsg(fmt.Sprintf("Error creating stdout pipe: %v\n", err))
		}

		stderr, err := cmd.StderrPipe()
		if err != nil {
			outputFile.Close()
			return BenchmarkOutputMsg(fmt.Sprintf("Error creating stderr pipe: %v\n", err))
		}

		// Start the command
		if err := cmd.Start(); err != nil {
			log.Printf("Error starting benchmark: %v", err)
			outputFile.Close()
			return BenchmarkOutputMsg(fmt.Sprintf("Error starting benchmark: %v\n", err))
		}

		// Create a channel for the command completion
		done := make(chan struct{})

		// Start goroutines to handle output
		go func() {
			scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
			for scanner.Scan() {
				line := scanner.Text() + "\n"
				log.Printf("Benchmark output: %q", line)

				if _, err := outputFile.WriteString(line); err != nil {
					log.Printf("Error writing to output file: %v", err)
					globalProgram.Send(BenchmarkOutputMsg(fmt.Sprintf("Error writing to output file: %v\n", err)))
				}
				globalProgram.Send(BenchmarkOutputMsg(line))
			}
			if err := scanner.Err(); err != nil {
				log.Printf("Scanner error: %v", err)
				globalProgram.Send(BenchmarkOutputMsg(fmt.Sprintf("Error reading benchmark output: %v\n", err)))
			}
		}()

		// Wait for command completion in a goroutine
		go func() {
			if err := cmd.Wait(); err != nil {
				log.Printf("Benchmark failed: %v", err)
				globalProgram.Send(BenchmarkOutputMsg(fmt.Sprintf("Benchmark failed: %v\n", err)))
			}
			log.Println("Benchmark command completed")

			// Close the output file after the command completes
			if err := outputFile.Close(); err != nil {
				log.Printf("Error closing output file: %v", err)
				globalProgram.Send(BenchmarkOutputMsg(fmt.Sprintf("Error closing output file: %v\n", err)))
			}

			close(done)
			globalProgram.Send(BenchmarkFinishedMsg{})
		}()

		m.Running = true
		return nil
	}
}

// // wordWrap wraps text at the specified width
// func wordWrap(text string, width int) string {
// 	if width <= 0 {
// 		return text
// 	}

// 	var wrapped strings.Builder
// 	lines := strings.Split(text, "\n")

// 	for i, line := range lines {
// 		if len(line) <= width {
// 			wrapped.WriteString(line)
// 		} else {
// 			// Preserve indentation
// 			indent := ""
// 			for _, r := range line {
// 				if r == ' ' || r == '\t' {
// 					indent += string(r)
// 				} else {
// 					break
// 				}
// 			}

// 			// Wrap the line
// 			currentWidth := 0
// 			words := strings.Fields(strings.TrimSpace(line))
// 			for j, word := range words {
// 				wordLength := len(word)
// 				if currentWidth+wordLength+1 > width && currentWidth > 0 {
// 					wrapped.WriteString("\n" + indent)
// 					currentWidth = len(indent)
// 				} else if j > 0 {
// 					wrapped.WriteString(" ")
// 					currentWidth++
// 				}
// 				wrapped.WriteString(word)
// 				currentWidth += wordLength
// 			}
// 		}
// 		if i < len(lines)-1 {
// 			wrapped.WriteString("\n")
// 		}
// 	}

// 	return wrapped.String()
// }
