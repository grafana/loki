package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kingpin/v2"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// commandPaths returns the full path of every command, e.g. ["rules", "list"].
func commandPaths(cmds []*kingpin.CmdModel) [][]string {
	var paths [][]string
	for _, cmd := range cmds {
		paths = append(paths, strings.Fields(cmd.FullCommand))
		paths = append(paths, commandPaths(cmd.Commands)...)
	}
	return paths
}

// TestApplicationHelp builds the application and renders the usage of every command.
// Kingpin validates the complete command tree when it parses, for example
// it rejects flags that have the same name as a flag of a parent command.
func TestApplicationHelp(t *testing.T) {
	app, _ := NewApplication()

	var out bytes.Buffer
	app.UsageWriter(&out)
	app.ErrorWriter(&out)
	app.Terminate(func(code int) {
		t.Fatalf("application terminated with code %d: %s", code, out.String())
	})

	paths := commandPaths(app.Model().Commands)
	require.NotEmpty(t, paths)

	for _, path := range paths {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			out.Reset()
			app.Usage(path)
			require.Contains(t, out.String(), "usage: lokitool")
		})
	}
}

func TestGlobalFlags(t *testing.T) {
	t.Cleanup(func() { log.SetLevel(log.InfoLevel) })

	t.Run("log level", func(t *testing.T) {
		app, global := NewApplication()
		defer global.Close()

		_, err := app.Parse([]string{"--log.level=debug", "version"})
		require.NoError(t, err)
		require.Equal(t, log.DebugLevel, log.GetLevel())
	})

	t.Run("default log level", func(t *testing.T) {
		app, global := NewApplication()
		defer global.Close()

		_, err := app.Parse([]string{"version"})
		require.NoError(t, err)
		require.Equal(t, log.InfoLevel, log.GetLevel())
	})

	t.Run("invalid log level", func(t *testing.T) {
		app, _ := NewApplication()
		app.Terminate(func(int) {})
		app.ErrorWriter(&bytes.Buffer{})

		_, err := app.Parse([]string{"--log.level=trace", "version"})
		require.Error(t, err)
	})

	t.Run("profiles", func(t *testing.T) {
		dir := t.TempDir()
		cpu := filepath.Join(dir, "cpu.prof")
		mem := filepath.Join(dir, "mem.prof")

		app, global := NewApplication()
		_, err := app.Parse([]string{"--cpuprofile=" + cpu, "--memprofile=" + mem, "version"})
		require.NoError(t, err)
		global.Close()

		for _, f := range []string{cpu, mem} {
			info, err := os.Stat(f)
			require.NoError(t, err)
			require.NotZero(t, info.Size(), f)
		}
	})
}
