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
	"github.com/grafana/loki/v3/pkg/dataobj/tools"
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
		exitWithErr(fmt.Errorf("failed to open file: %w", err))
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		exitWithErr(fmt.Errorf("failed to read fileinfo: %w", err))
	}
	obj, err := dataobj.FromReaderAt(f, fi.Size())
	if err != nil {
		exitWithErr(fmt.Errorf("failed to read dataobj: %w", err))
	}
	cmd.printObjStats(context.TODO(), obj)
}

func (cmd *statsCommand) printObjStats(ctx context.Context, obj *dataobj.Object) {
	stats, err := tools.ReadStats(ctx, obj)
	if err != nil {
		exitWithErr(fmt.Errorf("failed to read data object stats: %w", err))
	}
	bold := color.New(color.Bold)
	bold.Println("Object:")
	fmt.Printf(
		"\tcompressed size: %v, sections: %d, tenants: %d\n",
		humanize.Bytes(stats.Size),
		stats.Sections,
		len(stats.Tenants),
	)
	fmt.Printf(
		"\tcompressed section sizes:\n\t\tp50: %v, p95: %v, p99: %v\n",
		humanize.Bytes(uint64(stats.SectionSizeStats.Median)),
		humanize.Bytes(uint64(stats.SectionSizeStats.P95)),
		humanize.Bytes(uint64(stats.SectionSizeStats.P99)),
	)
	fmt.Printf(
		"\tnumber of sections per tenant:\n\t\tp50: %.2f, p95: %.2f, p99: %.2f\n",
		stats.SectionsPerTenantStats.Median,
		stats.SectionsPerTenantStats.P95,
		stats.SectionsPerTenantStats.P99,
	)
	for offset, sec := range obj.Sections() {
		switch {
		case streams.CheckSection(sec):
			cmd.printStreamsSectionStats(ctx, offset, sec)
		case logs.CheckSection(sec):
			cmd.printLogsSectionStats(ctx, offset, sec)
		default:
			exitWithErr(errors.New("unknown section"))
		}
	}
}

func (cmd *statsCommand) printStreamsSectionStats(ctx context.Context, offset int, sec *dataobj.Section) {
	streamsSec, err := streams.Open(ctx, sec)
	if err != nil {
		exitWithErr(fmt.Errorf("failed to open streams section: %w", err))
	}
	stats, err := streams.ReadStats(ctx, streamsSec)
	if err != nil {
		exitWithErr(fmt.Errorf("failed to read section stats: %w", err))
	}
	bold := color.New(color.Bold)
	bold.Println("Streams section:")
	bold.Printf(
		"\toffset: %d, tenant: %s, columns: %d, compressed size: %v, uncompressed size %v\n",
		offset,
		sec.Tenant,
		len(stats.Columns),
		humanize.Bytes(stats.CompressedSize),
		humanize.Bytes(stats.UncompressedSize),
	)
	for _, col := range stats.Columns {
		fmt.Printf(
			"\t\tname: %s, type: %v, %d populated rows, %v compressed (%v), %v uncompressed\n",
			col.Name,
			col.Type,
			col.ValuesCount,
			humanize.Bytes(col.CompressedSize),
			col.Compression[17:],
			humanize.Bytes(col.UncompressedSize))
	}
}

func (cmd *statsCommand) printLogsSectionStats(ctx context.Context, offset int, sec *dataobj.Section) {
	logsSec, err := logs.Open(ctx, sec)
	if err != nil {
		exitWithErr(fmt.Errorf("failed to open logs section: %w", err))
	}
	stats, err := logs.ReadStats(ctx, logsSec)
	if err != nil {
		exitWithErr(fmt.Errorf("failed to read section stats: %w", err))
	}
	bold := color.New(color.Bold)
	bold.Println("Logs section:")
	bold.Printf(
		"\toffset: %d, tenant: %s, columns: %d, compressed size: %v, uncompressed size %v\n",
		offset,
		sec.Tenant,
		len(stats.Columns),
		humanize.Bytes(stats.CompressedSize),
		humanize.Bytes(stats.UncompressedSize),
	)
	for _, col := range stats.Columns {
		fmt.Printf(
			"\t\tname: %s, type: %v, %d populated rows, %v compressed (%v), %v uncompressed\n",
			col.Name,
			col.Type,
			col.ValuesCount,
			humanize.Bytes(col.CompressedSize),
			col.Compression[17:],
			humanize.Bytes(col.UncompressedSize))
	}
}

func addStatsCommand(app *kingpin.Application) {
	cmd := &statsCommand{}
	summary := app.Command("stats", "Print stats for the data object.").Action(cmd.run)
	cmd.files = summary.Arg("file", "The file to print.").ExistingFiles()
}
