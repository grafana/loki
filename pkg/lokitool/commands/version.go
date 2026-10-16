package commands

import (
	"fmt"
	"io"
	"os"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/common/version"
)

// VersionCommand prints the version of the tool.
type VersionCommand struct {
	// Out is the destination of the output. It defaults to stdout.
	Out io.Writer
}

// Register the version command with the kingpin application.
func (v *VersionCommand) Register(app *kingpin.Application) {
	app.Command("version", "Get the version of the lokitool CLI").Action(v.printVersion)
}

func (v *VersionCommand) printVersion(_ *kingpin.ParseContext) error {
	out := v.Out
	if out == nil {
		out = os.Stdout
	}
	_, err := fmt.Fprintln(out, version.Print("loki"))
	return err
}
