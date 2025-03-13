package views

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Storage type constants
const (
	storageTypeDataObj = "dataobj"
	storageTypeChunk   = "chunk"
	storageTypeBoth    = "both"
)

// RunView represents the benchmark run view
type RunView struct {
	RunConfig         RunConfig
	Running           bool
	Output            string
	Viewport          viewport.Model
	DiffViewport      viewport.Model
	showDiff          bool
	ready             bool   // track if we've received initial window size
	lastBenchmarkLine string // track the last benchmark line for stats formatting

	// Window size tracking
	width                int
	height               int
	verticalMarginHeight int

	// Profiling state
	cpuProfilePort int
	memProfilePort int
	tracePort      int
	cpuProfileOn   bool
	memProfileOn   bool
	traceProfileOn bool

	// Spinner for running state
	spinner spinner.Model
}

// NewRunView creates a new RunView
func NewRunView(config RunConfig) *RunView {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	// Set default storage type if not specified
	if config.StorageType == "" {
		config.StorageType = storageTypeBoth
	}

	return &RunView{
		RunConfig:      config,
		Running:        false,
		ready:          false,
		cpuProfilePort: 6060,
		memProfilePort: 6061,
		tracePort:      6062,
		showDiff:       false,
		spinner:        sp,
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
			if m.showDiff {
				if m.DiffViewport.AtTop() {
					m.Viewport.LineUp(1)
				} else {
					m.DiffViewport.LineUp(1)
				}
			} else if m.Viewport.Height > 0 {
				m.Viewport.LineUp(1)
			}
			return m, nil
		case "down", "j":
			if m.showDiff {
				if m.DiffViewport.AtBottom() {
					m.Viewport.LineDown(1)
				} else {
					m.DiffViewport.LineDown(1)
				}
			} else if m.Viewport.Height > 0 {
				m.Viewport.LineDown(1)
			}
			return m, nil
		case "pgup", "b":
			if m.showDiff {
				if m.DiffViewport.AtTop() {
					m.Viewport.HalfViewUp()
				} else {
					m.DiffViewport.HalfViewUp()
				}
			} else if m.Viewport.Height > 0 {
				m.Viewport.HalfViewUp()
			}
			return m, nil
		case "pgdown", " ":
			if m.showDiff {
				if m.DiffViewport.AtBottom() {
					m.Viewport.HalfViewDown()
				} else {
					m.DiffViewport.HalfViewDown()
				}
			} else if m.Viewport.Height > 0 {
				m.Viewport.HalfViewDown()
			}
			return m, nil
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
		case "s":
			// Cycle through storage types: dataobj -> chunk -> both
			switch m.RunConfig.StorageType {
			case storageTypeDataObj:
				m.RunConfig.StorageType = storageTypeChunk
			case storageTypeChunk:
				m.RunConfig.StorageType = storageTypeBoth
			case storageTypeBoth:
				m.RunConfig.StorageType = storageTypeDataObj
			default:
				m.RunConfig.StorageType = storageTypeBoth
			}
			return m, nil
		case "enter", "r":
			if !m.Running {
				m.Output = "" // Clear output before starting new run
				if m.Viewport.Height > 0 {
					m.Viewport.SetContent("")
				}
				log.Println("starting benchmark")
				return m, tea.Batch(m.startBenchmark(), m.spinner.Tick)
			}
		case "esc":
			return m, func() tea.Msg {
				return SwitchViewMsg{View: ListID}
			}
		case "p":
			if !m.cpuProfileOn {
				return m, m.startCPUProfile()
			}
			return m, m.stopCPUProfile()
		case "m":
			if !m.memProfileOn {
				return m, m.startMemProfile()
			}
			return m, m.stopMemProfile()
		case "x":
			if !m.traceProfileOn {
				return m, m.startTraceProfile()
			}
			return m, m.stopTraceProfile()
		case "d":
			m.showDiff = !m.showDiff
			m.updateViewportDimensions()
			return m, nil
		}

	case tea.WindowSizeMsg:
		headerHeight := lipgloss.Height(m.headerView())
		footerHeight := lipgloss.Height(m.footerView())
		m.width = msg.Width
		m.height = msg.Height
		m.verticalMarginHeight = headerHeight + footerHeight

		if !m.ready {
			// First time initialization
			log.Printf("Initializing viewport: width=%d height=%d", msg.Width, msg.Height-m.verticalMarginHeight)
			height := m.height - m.verticalMarginHeight
			if m.showDiff {
				height = (m.height - m.verticalMarginHeight) / 2
			}
			m.Viewport = viewport.New(m.width, height)
			m.DiffViewport = viewport.New(m.width, height)
			m.Viewport.YPosition = headerHeight // Place viewport below header
			m.DiffViewport.YPosition = headerHeight + height
			m.Viewport.Style = ViewportStyle
			m.DiffViewport.Style = ViewportStyle
			m.Viewport.SetContent("\n  Press ENTER to start the benchmark run\n")
			m.DiffViewport.SetContent("\n  No benchmark comparison available yet\n")
			m.ready = true
		} else {
			// Update dimensions
			m.updateViewportDimensions()
		}

		// Update content if we have any
		if m.Output != "" {
			m.Viewport.SetContent(m.Output)
		}

	case BenchmarkOutputMsg:
		log.Printf("Received benchmark output: length=%d", len(string(msg)))
		line := string(msg)

		// Process the line
		if strings.HasPrefix(strings.TrimSpace(line), "Benchmark") {
			// Split the line into benchmark name and stats
			parts := strings.Fields(line)
			benchName := parts[0]

			// Format stats with fixed width fields
			// Format: iterations time ns/op bytes B/op allocs allocs/op
			if len(parts) >= 6 {
				stats := fmt.Sprintf("%8s %12s ns/op %12s B/op %8s allocs/op",
					parts[1], // iterations
					parts[2], // time
					parts[4], // bytes
					parts[6], // allocs
				)
				line = benchName + "\n    " + stats + "\n"
			} else {
				// If we don't have all the expected parts, just join them as is
				stats := strings.Join(parts[1:], " ")
				line = benchName + "\n    " + stats + "\n"
			}
		}

		m.Output += line
		m.Viewport.SetContent(m.Output)
		m.Viewport.GotoBottom()

	case BenchmarkFinishedMsg:
		log.Println("Benchmark finished")
		m.Running = false

		// Check if we have old results to compare
		if _, err := os.Stat("result.old.txt"); err == nil {
			// Run benchstat comparison
			cmd := exec.Command("benchstat", "result.old.txt", "result.new.txt")
			output, err := cmd.CombinedOutput()
			if err != nil {
				log.Printf("Error running benchstat: %v", err)
				m.DiffViewport.SetContent(fmt.Sprintf("\n  Error running benchmark comparison: %v\n", err))
			} else {
				m.DiffViewport.SetContent("\n" + string(output))
				if strings.TrimSpace(string(output)) != "" {
					m.showDiff = true // Automatically show diff view when comparison is available
					m.updateViewportDimensions()
				}
			}
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.Running {
			cmds = append(cmds, cmd)
		}
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
		if m.showDiff {
			content = lipgloss.JoinVertical(lipgloss.Left,
				m.Viewport.View(),
				lipgloss.NewStyle().
					Foreground(lipgloss.Color("12")).
					Bold(true).
					Padding(0, 1).
					Render("Benchmark Comparison"),
				m.DiffViewport.View(),
			)
		} else {
			content = m.Viewport.View()
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.headerView(),
		content,
		m.footerView(),
	)
}

func (m *RunView) headerView() string {
	header := HeaderStyle.Render("Benchmark Configuration")
	var status string
	if m.Running {
		status = m.spinner.View()
	} else {
		status = "Idle"
	}
	config := ConfigStyle.Render(fmt.Sprintf(
		"Count: %d (+/- to adjust) • Trace: %v ('t' to toggle) • Storage: %s ('s' to change) • Status: %s • CPU: %s:%d • MEM: %s:%d • TRACE: %s:%d",
		m.RunConfig.Count,
		m.RunConfig.TraceEnabled,
		m.storageTypeDisplay(),
		status,
		m.profileStatus(m.cpuProfileOn), m.cpuProfilePort,
		m.profileStatus(m.memProfileOn), m.memProfilePort,
		m.profileStatus(m.traceProfileOn), m.tracePort,
	))

	// Format selected benchmarks with proper indentation
	var formattedSelected []string
	for _, s := range m.RunConfig.Selected {
		formattedSelected = append(formattedSelected, "  "+s)
	}

	selectedStyle := ConfigStyle.Foreground(lipgloss.Color("241"))
	var selected string
	if len(formattedSelected) > 0 {
		if len(formattedSelected) > 1 {
			selected = selectedStyle.Render(fmt.Sprintf("Selected: %d benchmarks", len(formattedSelected)))
		} else {
			selected = selectedStyle.Render("Selected:") + "\n" +
				selectedStyle.Render(strings.Join(formattedSelected, "\n"))
		}
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

func (m *RunView) storageTypeDisplay() string {
	switch m.RunConfig.StorageType {
	case storageTypeDataObj:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render("DataObj")
	case storageTypeChunk:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("Chunk")
	case storageTypeBoth:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Render("Both")
	default:
		return m.RunConfig.StorageType
	}
}

func (m *RunView) footerView() string {
	separator := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render(strings.Repeat("─", m.Viewport.Width))

	controls := []string{
		"↑/↓: scroll",
		"ENTER/r: restart",
		"s: storage",
		"p: cpu",
		"m: mem",
		"x: trace",
		"d: diff",
		"ESC: back",
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		separator,
		ControlStyle.Render(strings.Join(controls, " • ")),
	)
}

// startBenchmark starts the benchmark execution
func (m *RunView) startBenchmark() tea.Cmd {
	return func() tea.Msg {
		log.Println("Starting benchmark execution")
		m.Running = true
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
		args := []string{
			"test", "-v",
			"-test.run=^$",   // Skip tests
			"-test.benchmem", // Show memory stats
			fmt.Sprintf("-count=%d", m.RunConfig.Count),
		}

		// Add storage type filter if not running both
		if m.RunConfig.StorageType != storageTypeBoth {
			benchRegex := buildTestRegex(m.RunConfig.Selected, m.RunConfig.StorageType)
			args = append(args, "-test.bench="+benchRegex)
			log.Printf("Using storage type %s with regex: %s", m.RunConfig.StorageType, benchRegex)
		} else {
			// When running both, we need to match any storage type
			benchRegex := buildTestRegex(m.RunConfig.Selected, "")
			args = append(args, "-test.bench="+benchRegex)
			log.Printf("Using both storage types with regex: %s", benchRegex)
		}

		// Add profiling flags
		args = append(args,
			"-test.cpuprofile=cpu.prof", // CPU profiling
			"-test.memprofile=mem.prof", // Memory profiling
		)

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

		return nil
	}
}

func buildTestRegex(tests []string, storageType string) string {
	if len(tests) == 0 {
		// If no tests are selected, match all tests
		if storageType == "" {
			// Match any storage type
			return "^BenchmarkLogQL/.+/.*$"
		}
		// Match specific storage type
		return fmt.Sprintf("^BenchmarkLogQL/%s/.*$", storageType)
	}

	formatted := []string{}
	for _, t := range tests {
		// Escape special regex characters in the benchmark name
		var escaped string
		if storageType == "" {
			// When no storage type is specified, match any storage type
			escaped = strings.ReplaceAll(fmt.Sprintf("BenchmarkLogQL/.+/%s", t), "{", "\\{")
		} else {
			// When a storage type is specified, match only that storage type
			escaped = strings.ReplaceAll(fmt.Sprintf("BenchmarkLogQL/%s/%s", storageType, t), "{", "\\{")
		}
		escaped = strings.ReplaceAll(escaped, "}", "\\}")
		escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
		escaped = strings.ReplaceAll(escaped, "[", "\\[")
		escaped = strings.ReplaceAll(escaped, "]", "\\]")
		escaped = strings.ReplaceAll(escaped, "(", "\\(")
		escaped = strings.ReplaceAll(escaped, ")", "\\)")
		escaped = strings.ReplaceAll(escaped, "=", "\\=")
		escaped = strings.ReplaceAll(escaped, "|", "\\|")
		escaped = "^" + escaped + "$" // Ensure exact match
		formatted = append(formatted, escaped)
	}
	return strings.Join(formatted, "|")
}

func (m *RunView) profileStatus(on bool) string {
	if on {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Render("ON")
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("OFF")
}

// Profile control functions
func (m *RunView) startCPUProfile() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("go", "tool", "pprof", "-http", fmt.Sprintf(":%d", m.cpuProfilePort), "cpu.prof")
		if err := cmd.Start(); err != nil {
			m.cpuProfilePort++ // Try next port if this one fails
			return BenchmarkOutputMsg(fmt.Sprintf("Failed to start CPU profile: %v\n", err))
		}
		m.cpuProfileOn = true
		return BenchmarkOutputMsg("CPU profile started\n")
	}
}

func (m *RunView) stopCPUProfile() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("pkill", "-f", fmt.Sprintf("pprof.*:%d", m.cpuProfilePort))
		if err := cmd.Run(); err != nil {
			return BenchmarkOutputMsg(fmt.Sprintf("Failed to stop CPU profile: %v\n", err))
		}
		m.cpuProfileOn = false
		return BenchmarkOutputMsg("CPU profile stopped\n")
	}
}

func (m *RunView) startMemProfile() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("go", "tool", "pprof", "-http", fmt.Sprintf(":%d", m.memProfilePort), "mem.prof")
		if err := cmd.Start(); err != nil {
			m.memProfilePort++ // Try next port if this one fails
			return BenchmarkOutputMsg(fmt.Sprintf("Failed to start memory profile: %v\n", err))
		}
		m.memProfileOn = true
		return BenchmarkOutputMsg("Memory profile started\n")
	}
}

func (m *RunView) stopMemProfile() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("pkill", "-f", fmt.Sprintf("pprof.*:%d", m.memProfilePort))
		if err := cmd.Run(); err != nil {
			return BenchmarkOutputMsg(fmt.Sprintf("Failed to stop memory profile: %v\n", err))
		}
		m.memProfileOn = false
		return BenchmarkOutputMsg("Memory profile stopped\n")
	}
}

func (m *RunView) startTraceProfile() tea.Cmd {
	return func() tea.Msg {
		if _, err := os.Stat("trace.out"); err != nil {
			return BenchmarkOutputMsg("No trace file found\n")
		}
		cmd := exec.Command("go", "tool", "trace", "-http", fmt.Sprintf(":%d", m.tracePort), "trace.out")
		if err := cmd.Start(); err != nil {
			m.tracePort++ // Try next port if this one fails
			return BenchmarkOutputMsg(fmt.Sprintf("Failed to start trace viewer: %v\n", err))
		}
		m.traceProfileOn = true
		return BenchmarkOutputMsg("Trace viewer started\n")
	}
}

func (m *RunView) stopTraceProfile() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("pkill", "-f", fmt.Sprintf("trace.*:%d", m.tracePort))
		if err := cmd.Run(); err != nil {
			return BenchmarkOutputMsg(fmt.Sprintf("Failed to stop trace viewer: %v\n", err))
		}
		m.traceProfileOn = false
		return BenchmarkOutputMsg("Trace viewer stopped\n")
	}
}

// StopAllProfiling stops all active profiling tools
func (m *RunView) StopAllProfiling() tea.Cmd {
	return func() tea.Msg {
		_ = m.stopCPUProfile()()
		_ = m.stopMemProfile()()
		_ = m.stopTraceProfile()()
		return nil
	}
}

func (m *RunView) updateViewportDimensions() {
	headerHeight := lipgloss.Height(m.headerView())
	height := m.height - m.verticalMarginHeight
	if m.showDiff {
		height = (m.height - m.verticalMarginHeight) / 2
	}

	m.Viewport.Width = m.width
	m.DiffViewport.Width = m.width
	m.Viewport.Height = height
	m.DiffViewport.Height = height
	m.DiffViewport.YPosition = headerHeight + height
}
