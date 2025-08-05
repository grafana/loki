package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/alecthomas/kingpin/v2"
	"github.com/dustin/go-humanize"
	"github.com/fatih/color"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// statsCommand prints stats for each data object in files.
type statsCommand struct {
	files *[]string
}

func (cmd *statsCommand) run(c *kingpin.ParseContext) error {
	for _, f := range *cmd.files {
		cmd.printStats(f)
	}
	return nil
}

// summarizeFile prints stats for the data object file.
func (cmd *statsCommand) printStats(name string) {
	f, err := os.Open(name)
	if err != nil {
		exitWithError(fmt.Errorf("failed to open file: %w", err))
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		exitWithError(fmt.Errorf("failed to read fileinfo: %w", err))
	}
	dataObj, err := dataobj.FromReaderAt(f, fi.Size())
	if err != nil {
		exitWithError(fmt.Errorf("failed to read dataobj: %w", err))
	}
	for offset, sec := range dataObj.Sections() {
		switch {
		case streams.CheckSection(sec):
			cmd.printStreamsSectionStats(context.TODO(), offset, sec)
		case logs.CheckSection(sec):
			cmd.printLogsSectionStats(context.TODO(), offset, sec)
		default:
			exitWithError(errors.New("unknown section"))
		}
	}
}

func (cmd *statsCommand) printStreamsSectionStats(ctx context.Context, offset int, sec *dataobj.Section) {
	streamsSec, err := streams.Open(ctx, sec)
	if err != nil {
		exitWithError(fmt.Errorf("failed to open streams section: %w", err))
	}
	stats, err := streams.ReadStats(ctx, streamsSec)
	if err != nil {
		exitWithError(fmt.Errorf("failed to read section stats: %w", err))
	}
	bold := color.New(color.Bold)
	bold.Println("Streams section:")
	bold.Printf(
		"\toffset: %d, columns: %d, compressed size: %v; uncompressed size %v\n",
		offset,
		len(stats.Columns),
		humanize.Bytes(stats.CompressedSize),
		humanize.Bytes(stats.UncompressedSize),
	)
	for _, col := range stats.Columns {
		fmt.Printf("\t\tname: %s, type: %v, %d populated rows, %v compressed (%v), %v uncompressed\n", col.Name, col.Type[12:], col.ValuesCount, humanize.Bytes(col.CompressedSize), col.Compression[17:], humanize.Bytes(col.UncompressedSize))
	}
}

func (cmd *statsCommand) printLogsSectionStats(ctx context.Context, offset int, sec *dataobj.Section) {
	logsSec, err := logs.Open(ctx, sec)
	if err != nil {
		exitWithError(fmt.Errorf("failed to open logs section: %w", err))
	}
	stats, err := logs.ReadStats(ctx, logsSec)
	if err != nil {
		exitWithError(fmt.Errorf("failed to read section stats: %w", err))
	}
	bold := color.New(color.Bold)
	bold.Println("Logs section:")
	bold.Printf(
		"\toffset: %d, columns: %d, compressed size: %v; uncompressed size %v\n",
		offset,
		len(stats.Columns),
		humanize.Bytes(stats.CompressedSize),
		humanize.Bytes(stats.UncompressedSize),
	)
	for _, col := range stats.Columns {
		fmt.Printf("\t\tname: %s, type: %v, %d populated rows, %v compressed (%v), %v uncompressed\n", col.Name, col.Type[12:], col.ValuesCount, humanize.Bytes(col.CompressedSize), col.Compression[17:], humanize.Bytes(col.UncompressedSize))
	}
}

func addStatsCommand(app *kingpin.Application) {
	cmd := &statsCommand{}
	summary := app.Command("stats", "Print stats for the data object.").Action(cmd.run)
	cmd.files = summary.Arg("file", "The file to print.").ExistingFiles()
}
