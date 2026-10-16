package commands

import (
	"fmt"
	"os"
	"runtime/pprof"

	"github.com/alecthomas/kingpin/v2"
	log "github.com/sirupsen/logrus"

	// The old commands are replaced by their pkg/lokitool counterparts step by step.
	tool "github.com/grafana/loki/v3/pkg/tool/commands"
)

// NewApplication returns the lokitool application with all commands registered.
// The caller must call Close on the returned GlobalFlags after the application has run.
func NewApplication() (*kingpin.Application, *GlobalFlags) {
	app := kingpin.New("lokitool", "A command-line tool to interact with Loki.")

	global := &GlobalFlags{}
	global.Register(app)

	new(VersionCommand).Register(app)

	new(tool.RuleCommand).Register(app)
	new(tool.AuditCommand).Register(app)

	return app, global
}

// GlobalFlags holds the flags that apply to every command.
type GlobalFlags struct {
	LogLevel   string
	CPUProfile string
	MemProfile string

	cpuFile *os.File
	memFile *os.File
}

// Register the global flags and the actions that apply them.
func (g *GlobalFlags) Register(app *kingpin.Application) {
	app.Flag("log.level", "Log level.").
		Default("warning").
		EnumVar(&g.LogLevel, "error", "warning", "info", "debug")
	app.Flag("cpuprofile", "Write a CPU profile to this file.").StringVar(&g.CPUProfile)
	app.Flag("memprofile", "Write a memory profile to this file when the command exits.").StringVar(&g.MemProfile)

	// Colours are enabled when stderr is a terminal. CLICOLOR and CLICOLOR_FORCE override this.
	log.SetFormatter(&log.TextFormatter{EnvironmentOverrideColors: true})
	log.SetOutput(os.Stderr)

	app.PreAction(g.setLogLevel)
	// Actions run after kingpin handled --help, so profiles are only written for real commands.
	app.Action(g.startProfiling)
}

// Close stops profiling and writes the memory profile.
func (g *GlobalFlags) Close() {
	if g.cpuFile != nil {
		pprof.StopCPUProfile()
		if err := g.cpuFile.Close(); err != nil {
			log.WithError(err).Error("failed to close CPU profile")
		}
	}

	if g.memFile != nil {
		if err := pprof.WriteHeapProfile(g.memFile); err != nil {
			log.WithError(err).Error("failed to write memory profile")
		}
		if err := g.memFile.Close(); err != nil {
			log.WithError(err).Error("failed to close memory profile")
		}
	}
}

func (g *GlobalFlags) setLogLevel(_ *kingpin.ParseContext) error {
	// Kingpin runs pre-actions before it reports parse errors and before it
	// applies defaults when it shows help. The level is empty in these cases.
	if g.LogLevel == "" {
		return nil
	}

	level, err := log.ParseLevel(g.LogLevel)
	if err != nil {
		return err
	}
	log.SetLevel(level)
	return nil
}

func (g *GlobalFlags) startProfiling(_ *kingpin.ParseContext) error {
	if g.CPUProfile != "" {
		f, err := os.Create(g.CPUProfile)
		if err != nil {
			return fmt.Errorf("could not create CPU profile: %w", err)
		}
		g.cpuFile = f
		if err := pprof.StartCPUProfile(f); err != nil {
			return fmt.Errorf("could not start CPU profile: %w", err)
		}
	}

	if g.MemProfile != "" {
		f, err := os.Create(g.MemProfile)
		if err != nil {
			return fmt.Errorf("could not create memory profile: %w", err)
		}
		g.memFile = f
	}

	return nil
}
