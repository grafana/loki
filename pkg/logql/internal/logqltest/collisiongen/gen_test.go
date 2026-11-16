package collisiongen

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite testdata/label_collisions_parsers.logqltest")

// scriptPath is the generated script, relative to this package.
var scriptPath = filepath.Join("..", "testdata", "label_collisions_parsers.logqltest")

// TestScriptIsUpToDate fails when the script differs from what the model generates. Run the test
// with -update to rewrite the script, then read the diff: it is the effect of the change.
func TestScriptIsUpToDate(t *testing.T) {
	got, err := Generate()
	require.NoError(t, err)

	if *update {
		require.NoError(t, os.WriteFile(scriptPath, []byte(got), 0o644))
		return
	}

	want, err := os.ReadFile(scriptPath)
	require.NoError(t, err)
	if string(want) != got {
		require.Failf(t, "the script is out of date", "%s\nRun: go test ./pkg/logql/internal/logqltest/collisiongen -update", firstDifference(string(want), got))
	}
}

// firstDifference describes the first line that differs, with the line before it.
func firstDifference(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return fmt.Sprintf("line %d differs.\nin the script: %s\ngenerated:     %s", i+1, wl, gl)
		}
	}
	return "the scripts have the same lines"
}
