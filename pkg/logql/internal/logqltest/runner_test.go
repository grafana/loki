package logqltest

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLogQLScripts runs every declarative `.logqltest` script under testdata/ through the
// logqltest harness. See README.md for the DSL.
func TestLogQLScripts(t *testing.T) {
	files, err := filepath.Glob("testdata/*.logqltest")
	require.NoError(t, err)
	require.NotEmpty(t, files, "no .logqltest scripts found under testdata/")

	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			// Different scripts are independent (each builds its own store).
			t.Parallel()

			b, err := os.ReadFile(f)
			require.NoError(t, err)
			RunScript(t, filepath.Base(f), string(b))
		})
	}
}

func TestRunScript_ShouldSupportClear(t *testing.T) {
	// Verify that `clear` resets the loaded streams between scenarios.
	RunScript(t, "clear", `
load
  {app="foo"} "x" @ 30s

clear

load
  {app="bar"} "x" @ 30s

eval instant at 60s count_over_time({app=~"foo|bar"}[1m])
  {app="bar"} 1
`)
}

func TestFloatsEqual(t *testing.T) {
	require.True(t, floatsEqual(1, 1))
	require.True(t, floatsEqual(1, 1+1e-12))   // within absolute epsilon
	require.True(t, floatsEqual(1e10, 1e10+1)) // within relative epsilon
	require.True(t, floatsEqual(math.NaN(), math.NaN()))
	require.True(t, floatsEqual(math.Inf(1), math.Inf(1)))
	require.False(t, floatsEqual(1, 1.1))
	require.False(t, floatsEqual(100, 101))
	require.False(t, floatsEqual(math.NaN(), 1))
	require.False(t, floatsEqual(math.Inf(1), math.Inf(-1)))
}
