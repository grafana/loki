package commands

import (
	"bytes"
	"testing"

	"github.com/alecthomas/kingpin/v2"
	"github.com/stretchr/testify/require"
)

func TestVersionCommand(t *testing.T) {
	var out bytes.Buffer

	app := kingpin.New("lokitool", "")
	(&VersionCommand{Out: &out}).Register(app)

	_, err := app.Parse([]string{"version"})
	require.NoError(t, err)
	require.Contains(t, out.String(), "loki, version")
}
