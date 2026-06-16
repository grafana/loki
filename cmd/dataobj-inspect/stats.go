package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/dustin/go-humanize"
	"github.com/fatih/color"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	statssection "github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
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
	stats, err := ReadStats(ctx, obj)
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
		case indexpointers.CheckSection(sec):
			cmd.printIndexPointersSectionStats(ctx, offset, sec)
		case pointers.CheckSection(sec):
			cmd.printPointersSectionStats(ctx, offset, sec)
		case streams.CheckSection(sec):
			cmd.printStreamsSectionStats(ctx, offset, sec)
		case logs.CheckSection(sec):
			cmd.printLogsSectionStats(ctx, offset, sec)
		case postings.CheckSection(sec):
			cmd.printPostingsSectionStats(ctx, offset, sec)
		case statssection.CheckSection(sec):
			cmd.printStatsSectionStats(ctx, offset, sec)
		default:
			exitWithErr(errors.New("unknown section"))
		}
	}
}

func (cmd *statsCommand) printIndexPointersSectionStats(ctx context.Context, offset int, sec *dataobj.Section) {
	indexPointersSec, err := indexpointers.Open(ctx, sec)
	if err != nil {
		exitWithErr(fmt.Errorf("failed to open indexpointers section: %w", err))
	}
	stats, err := indexpointers.ReadStats(ctx, indexPointersSec)
	if err != nil {
		exitWithErr(fmt.Errorf("failed to read section stats: %w", err))
	}
	bold := color.New(color.Bold)
	bold.Println("IndexPointers section:")
	bold.Printf(
		"\toffset: %d, tenant: %s, columns: %d, compressed size: %v, uncompressed size %v [%s – %s]\n",
		offset,
		sec.Tenant,
		len(stats.Columns),
		humanize.Bytes(stats.CompressedSize),
		humanize.Bytes(stats.UncompressedSize),
		stats.MinTimestamp.UTC().Format(time.RFC3339Nano),
		stats.MaxTimestamp.UTC().Format(time.RFC3339Nano),
	)
	for _, col := range stats.Columns {
		fmt.Printf(
			"\t\tname: %s, type: %v, %d populated rows, %v compressed (%v), %v\n",
			col.Name,
			col.Type,
			col.ValuesCount,
			humanize.Bytes(col.CompressedSize),
			col.Compression[17:],
			humanize.Bytes(col.UncompressedSize))
	}
}

func (cmd *statsCommand) printPointersSectionStats(ctx context.Context, offset int, sec *dataobj.Section) {
	pointersSec, err := pointers.Open(ctx, sec)
	if err != nil {
		exitWithErr(fmt.Errorf("failed to open pointers section: %w", err))
	}
	stats, err := pointers.ReadStats(ctx, pointersSec)
	if err != nil {
		exitWithErr(fmt.Errorf("failed to read section stats: %w", err))
	}
	bold := color.New(color.Bold)
	bold.Println("Pointers section:")
	bold.Printf(
		"\toffset: %d, tenant: %s, columns: %d, compressed size: %v, uncompressed size %v [%s – %s]\n",
		offset,
		sec.Tenant,
		len(stats.Columns),
		humanize.Bytes(stats.CompressedSize),
		humanize.Bytes(stats.UncompressedSize),
		stats.MinTimestamp.UTC().Format(time.RFC3339Nano),
		stats.MaxTimestamp.UTC().Format(time.RFC3339Nano),
	)
	for _, col := range stats.Columns {
		fmt.Printf(
			"\t\tname: %s, type: %v, %d populated rows, %v compressed (%v), %v\n",
			col.Name,
			col.Type,
			col.ValuesCount,
			humanize.Bytes(col.CompressedSize),
			col.Compression[17:],
			humanize.Bytes(col.UncompressedSize))
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

func (cmd *statsCommand) printPostingsSectionStats(ctx context.Context, offset int, sec *dataobj.Section) {
	postingsSec, err := postings.Open(ctx, sec)
	if err != nil {
		exitWithErr(fmt.Errorf("failed to open postings section: %w", err))
	}
	stats, err := postings.ReadStats(ctx, postingsSec)
	if err != nil {
		exitWithErr(fmt.Errorf("failed to read section stats: %w", err))
	}
	bold := color.New(color.Bold)
	bold.Println("Postings section:")
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

func (cmd *statsCommand) printStatsSectionStats(ctx context.Context, offset int, sec *dataobj.Section) {
	statsSec, err := statssection.Open(ctx, sec)
	if err != nil {
		exitWithErr(fmt.Errorf("failed to open stats section: %w", err))
	}
	stats, err := statssection.ReadStats(ctx, statsSec)
	if err != nil {
		exitWithErr(fmt.Errorf("failed to read section stats: %w", err))
	}
	bold := color.New(color.Bold)
	bold.Println("Stats section:")
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

type Stats struct {
	Size                   uint64
	Sections               int
	SectionSizes           []uint64
	Tenants                []string
	TenantSections         map[string]int
	SectionsPerTenantStats PercentileStats
	SectionSizeStats       PercentileStats
}

type PercentileStats struct {
	Median float64
	P95    float64
	P99    float64
}

// ReadStats returns statistics about the data object. ReadStats returns an
// error if the data object couldn't be inspected or if the provided ctx is
// canceled.
func ReadStats(_ context.Context, obj *dataobj.Object) (*Stats, error) {
	s := Stats{}
	s.Size = uint64(obj.Size())
	s.TenantSections = make(map[string]int)
	for _, sec := range obj.Sections() {
		s.Sections++
		s.SectionSizes = append(s.SectionSizes, uint64(sec.Reader.DataSize()+sec.Reader.MetadataSize()))
		s.Tenants = append(s.Tenants, sec.Tenant)
		s.TenantSections[sec.Tenant]++
	}
	// A tenant can have multiple sections, so we must deduplicate them.
	slices.Sort(s.Tenants)
	s.Tenants = slices.Compact(s.Tenants)
	calcSectionsPerTenantStats(&s)
	calcSectionSizeStats(&s)
	return &s, nil
}

// calcSectionsPerTenantStats calculates the median and other
// percentiles for the number of sections per tenant.
func calcSectionsPerTenantStats(s *Stats) {
	if len(s.TenantSections) == 0 {
		return
	}
	counts := make([]int, 0, len(s.TenantSections))
	for _, n := range s.TenantSections {
		counts = append(counts, n)
	}
	calcPercentiles(counts, &s.SectionsPerTenantStats)
}

// calcSectionSizeStats calculates the median and other percentiles for the
// size of sections.
func calcSectionSizeStats(s *Stats) {
	if len(s.SectionSizes) == 0 {
		return
	}
	// Make a copy of the section sizes.
	sizes := make([]uint64, len(s.SectionSizes))
	copy(sizes, s.SectionSizes)
	calcPercentiles(sizes, &s.SectionSizeStats)
}

func calcPercentiles[T int | uint | int64 | uint64](input []T, s *PercentileStats) {
	// Data must be sorted to calculate percentiles.
	slices.Sort(input)
	n := len(input)
	if n%2 == 0 {
		s.Median = float64(input[n/2-1]+input[n/2]) / 2.0
	} else {
		s.Median = float64(input[n/2])
	}
	idx := int(float64(n) * 0.95)
	if idx >= n {
		idx = n - 1
	}
	s.P95 = float64(input[idx])
	idx = int(float64(n) * 0.99)
	if idx >= n {
		idx = n - 1
	}
	s.P99 = float64(input[idx])
}
