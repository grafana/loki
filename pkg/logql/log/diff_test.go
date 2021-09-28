package log

import (
	"strings"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/stretchr/testify/require"
)

const (
	prev = "test"
)

func newDiffFilterWithPrev(pretty bool, prev []byte) *DiffFilter {
	return &DiffFilter{
		dmp:    diffmatchpatch.New(),
		pretty: pretty,
		prev:   prev,
	}
}

func TestDiffFilter_Filter(t *testing.T) {
	t.Run("should not keep a line matching the previous", func(t *testing.T) {
		df := newDiffFilterWithPrev(false, []byte(prev))
		keep := df.Filter([]byte(prev))
		require.Equal(t, false, keep)
	})

	t.Run("should keep line with a diff", func(t *testing.T) {
		df := newDiffFilterWithPrev(false, []byte(prev))
		keep := df.Filter([]byte(prev + "2"))
		require.Equal(t, true, keep)
	})

	t.Run("pretty should have no effect on filtering", func(t *testing.T) {
		df := newDiffFilterWithPrev(true, []byte(prev))
		keep := df.Filter([]byte(prev))
		require.Equal(t, false, keep)
	})
}

// green uses ANSI escape codes to color the string green.
func green(str string) string {
	ansiFgGreen := "\x1b[32m"
	ansiFgDefault := "\x1b[0m"
	return ansiFgGreen + str + ansiFgDefault
}

func TestDiffFilter_Process(t *testing.T) {
	t.Run("should not process a line matching the previous", func(t *testing.T) {
		stage := newDiffFilterWithPrev(false, []byte(prev)).ToStage()
		line, keep := stage.Process([]byte(prev), nil)
		require.Equal(t, false, keep)
		require.Equal(t, prev, string(line))
	})

	t.Run("should return a patch for a line differing from the previous", func(t *testing.T) {
		stage := newDiffFilterWithPrev(false, []byte(prev)).ToStage()
		line, keep := stage.Process([]byte(prev+"2"), nil)
		require.Equal(t, true, keep)
		require.Equal(
			t,
			strings.Join([]string{
				"@@ -1,4 +1,5 @@",
				" test",
				"+2",
			}, "\n")+"\n",
			string(line),
		)
	})

	t.Run("pretty should return a human readable diff for a line differing from the previous", func(t *testing.T) {
		stage := newDiffFilterWithPrev(true, []byte(prev)).ToStage()
		line, keep := stage.Process([]byte(prev+"2"), nil)
		require.Equal(t, true, keep)
		require.Equal(t, "test"+green("2"), string(line))
	})
}
