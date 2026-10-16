package commands

import (
	"fmt"
	"io"
	"os"

	"github.com/alecthomas/kingpin/v2"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// FormatCommand formats a LogQL query.
type FormatCommand struct {
	// In is the source of the query. It defaults to stdin.
	In io.Reader
	// Out is the destination of the formatted query. It defaults to stdout.
	Out io.Writer
}

// Register the format command with the kingpin application.
func (f *FormatCommand) Register(app *kingpin.Application) {
	app.Command("format", "Format a LogQL query. The query is read from stdin.").
		Alias("fmt").
		Action(f.run)
}

func (f *FormatCommand) run(_ *kingpin.ParseContext) error {
	in, out := f.In, f.Out
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}

	b, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("failed to read the query: %w", err)
	}

	expr, err := syntax.ParseExpr(string(b))
	if err != nil {
		return fmt.Errorf("failed to parse the query: %w", err)
	}

	_, err = fmt.Fprintf(out, "%s\n", syntax.Prettify(expr))
	return err
}
