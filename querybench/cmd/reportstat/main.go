// Command reportstat compares two querybench JSON reports side-by-side. It
// writes a markdown comparison by default, or the bare table as CSV with
// -format csv.
//
// Each argument is "<name>:<path>", where <name> is the short label used for
// that report in the output.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/grafana/loki-query-benchmark/internal/compare"
	"github.com/grafana/loki-query-benchmark/internal/report"
)

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Fatalf("reportstat: %v", err)
	}
}

func run() error {
	format := flag.String("format", "markdown", "output format: markdown or csv")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: reportstat [-format markdown|csv] <name-a>:<report-a.json> <name-b>:<report-b.json>\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// Accept -format before or after the positional report arguments; the standard
	// flag package stops at the first positional, so parse the remainder in a loop.
	positionals := parseInterleaved()
	if len(positionals) != 2 {
		flag.Usage()
		return fmt.Errorf("expected exactly 2 <name>:<path> arguments, got %d", len(positionals))
	}

	a, err := parseInput(positionals[0])
	if err != nil {
		return err
	}
	b, err := parseInput(positionals[1])
	if err != nil {
		return err
	}

	switch *format {
	case "markdown":
		fmt.Print(compare.RenderMarkdown(a, b))
		return nil
	case "csv":
		rendered, err := compare.RenderCSV(a, b)
		if err != nil {
			return fmt.Errorf("render csv: %w", err)
		}
		fmt.Print(rendered)
		return nil
	default:
		return fmt.Errorf("invalid -format %q: want markdown or csv", *format)
	}
}

// parseInterleaved returns the positional arguments, parsing any flags that
// appear between or after them. The standard flag package treats the first
// non-flag token as the end of flags, so a flag written after a positional is
// otherwise mistaken for a positional.
func parseInterleaved() []string {
	var positionals []string
	rest := flag.Args()
	for len(rest) > 0 {
		positionals = append(positionals, rest[0])
		// flag.CommandLine uses ExitOnError, so a bad flag exits with usage here
		// rather than returning; only valid flags advance the loop.
		flag.CommandLine.Parse(rest[1:])
		rest = flag.CommandLine.Args()
	}
	return positionals
}

// parseInput splits "<name>:<path>", loads the report, and labels it. The name
// may not be empty, and the path keeps any further colons (e.g. a Windows
// drive), which the first split preserves.
func parseInput(arg string) (compare.Input, error) {
	name, path, ok := strings.Cut(arg, ":")
	if !ok || name == "" || path == "" {
		return compare.Input{}, fmt.Errorf("argument %q is not in <name>:<path> form", arg)
	}
	r, err := report.Load(path)
	if err != nil {
		return compare.Input{}, err
	}
	return compare.Input{Name: name, Report: r}, nil
}
