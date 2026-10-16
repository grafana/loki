package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormat(t *testing.T) {
	// Queries longer than the line width are broken into several lines.
	const longQuery = `sum by (app, env, cluster, namespace) (rate({app="foo",env="dev",cluster="eu-west-0",namespace="loki-dev-001"} |= "error" | json | level="error" | line_format "{{.msg}}" [5m]))`
	const formattedLongQuery = `sum by (app, env, cluster, namespace)(
  rate(
    {app="foo", env="dev", cluster="eu-west-0", namespace="loki-dev-001"}
      |= "error"
      | json
      | level="error"
      | line_format "{{.msg}}" [5m]
  )
)
`

	for _, command := range []string{"format", "fmt"} {
		t.Run(command, func(t *testing.T) {
			r, w, err := os.Pipe()
			require.NoError(t, err)
			_, err = w.WriteString(longQuery)
			require.NoError(t, err)
			require.NoError(t, w.Close())

			stdin := os.Stdin
			os.Stdin = r
			t.Cleanup(func() { os.Stdin = stdin })

			out, err := runApplication(t, command)
			require.NoError(t, err)
			require.Equal(t, formattedLongQuery, out)
		})
	}

	t.Run("invalid query", func(t *testing.T) {
		var out strings.Builder
		f := &FormatCommand{In: strings.NewReader(`{app="foo"`), Out: &out}

		require.ErrorContains(t, f.run(nil), "failed to parse the query")
		require.Empty(t, out.String())
	})

	t.Run("short query", func(t *testing.T) {
		var out strings.Builder
		f := &FormatCommand{In: strings.NewReader("{app=\"foo\"}\n"), Out: &out}

		require.NoError(t, f.run(nil))
		require.Equal(t, "{app=\"foo\"}\n", out.String())
	})
}
