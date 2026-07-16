package main

import (
	"cmp"
	"fmt"
	"io"
	"math"
	"slices"
	"text/tabwriter"

	"github.com/dustin/go-humanize"
)

// reportRow is a flattened, display-ready view of a key's aggregate together
// with its derived locality fields.
type reportRow struct {
	key               string
	agg               *keyAgg
	entriesPerSection float64
}

// logsReportRow is a flattened, display-ready view of a sort-key value's
// logs-section locality.
type logsReportRow struct {
	key           string
	logsSections  int64
	objectSpread  int64
	bytes         int64
	idealSections int64
	// spreadFactor is logsSections/idealSections; 1.0 means the value's logs are
	// perfectly clustered into the fewest sections its volume requires.
	spreadFactor float64
}

// report writes the fleet summary and top-N tables for every enabled rollup.
func (c *collector) report(w io.Writer, top int) {
	c.writeProcessingSummary(w)
	if c.opts.byNameValue {
		rows := buildRows(c.nameValue, func(k labelValue) string { return k.name + "=" + k.value })
		writeReport(w, "Label name-value", rows, top, c.totalSections)
	}
	if c.opts.byName {
		rows := buildRows(c.name, func(k string) string { return k })
		writeReport(w, "Label name", rows, top, c.totalSections)
	}
	if c.opts.logsLocality {
		writeLogsReport(w, c.opts.sortKey, c.logsBySortValue, top, c.opts.logsSectionTargetBytes)
	}
}

// writeProcessingSummary prints how many index objects were processed, split by
// whether they were produced by the compactor, along with the total number of
// postings sections folded across them.
func (c *collector) writeProcessingSummary(w io.Writer) {
	total := c.compactedObjects + c.uncompactedObjects
	fmt.Fprintf(w, "\n=== index objects processed: %d (%d compacted, %d uncompacted), %d postings sections ===\n",
		total, c.compactedObjects, c.uncompactedObjects, c.totalSections)
}

// buildRows flattens a global map into display rows with derived fields.
func buildRows[K comparable](m map[K]*keyAgg, keyStr func(K) string) []reportRow {
	rows := make([]reportRow, 0, len(m))
	for k, agg := range m {
		var eps float64
		if agg.sectionSpread > 0 {
			eps = float64(agg.postingEntries) / float64(agg.sectionSpread)
		}
		rows = append(rows, reportRow{
			key:               keyStr(k),
			agg:               agg,
			entriesPerSection: eps,
		})
	}
	return rows
}

// writeReport prints the summary and top-N tables for a single rollup.
func writeReport(w io.Writer, title string, rows []reportRow, top int, totalSections int64) {
	fmt.Fprintf(w, "\n=== %s locality (%d keys, %d total postings sections) ===\n", title, len(rows), totalSections)
	if len(rows) == 0 {
		fmt.Fprintln(w, "  no label postings found")
		return
	}

	sectionSpread := make([]int64, len(rows))
	objectSpread := make([]int64, len(rows))
	for i, r := range rows {
		sectionSpread[i] = r.agg.sectionSpread
		objectSpread[i] = r.agg.objectSpread
	}

	var spreadStats, objectStats PercentileStats
	calcPercentiles(sectionSpread, &spreadStats)
	calcPercentiles(objectSpread, &objectStats)

	fmt.Fprintf(w, "  sectionSpread: p50=%.2f p95=%.2f p99=%.2f max=%.0f\n", spreadStats.Median, spreadStats.P95, spreadStats.P99, spreadStats.Max)
	fmt.Fprintf(w, "  objectSpread:  p50=%.2f p95=%.2f p99=%.2f max=%.0f\n", objectStats.Median, objectStats.P95, objectStats.P99, objectStats.Max)

	writeTopTable(w, fmt.Sprintf("Top %d by sectionSpread", top), rows, top, totalSections, func(a, b reportRow) int {
		if a.agg.sectionSpread != b.agg.sectionSpread {
			return cmp.Compare(b.agg.sectionSpread, a.agg.sectionSpread)
		}
		return cmp.Compare(b.agg.postingEntries, a.agg.postingEntries)
	})
}

// writeTopTable prints the top rows ranked highest-first by rank, using a
// tab-aligned table. rank follows [slices.SortFunc] ordering conventions.
func writeTopTable(w io.Writer, header string, rows []reportRow, top int, totalSections int64, rank func(a, b reportRow) int) {
	fmt.Fprintf(w, "\n%s:\n", header)

	ranked := make([]reportRow, len(rows))
	copy(ranked, rows)
	slices.SortFunc(ranked, rank)
	if top > 0 && len(ranked) > top {
		ranked = ranked[:top]
	}

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "KEY\tSECTIONS\tTOTAL\tOBJECTS\tENTRIES\tENTRIES/SEC")
	for _, r := range ranked {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%.1f\n",
			r.key,
			r.agg.sectionSpread,
			totalSections,
			r.agg.objectSpread,
			r.agg.postingEntries,
			r.entriesPerSection,
		)
	}
	_ = tw.Flush()
}

// writeLogsReport prints the logs-section locality summary and top-N table for
// the values of the sort-key label. idealSections and spreadFactor are
// meaningful here because logs sections are rolled by uncompressed size, which
// is the same unit as the per-value uncompressed size carried by each posting.
func writeLogsReport(w io.Writer, sortKey string, m map[string]*logsAgg, top int, targetBytes int64) {
	if targetBytes < 1 {
		targetBytes = 1
	}

	rows := make([]logsReportRow, 0, len(m))
	for val, agg := range m {
		var bytes int64
		for _, sz := range agg.sections {
			bytes += sz
		}
		ideal := int64(math.Ceil(float64(bytes) / float64(targetBytes)))
		if ideal < 1 {
			ideal = 1
		}
		ls := int64(len(agg.sections))
		rows = append(rows, logsReportRow{
			key:           sortKey + "=" + val,
			logsSections:  ls,
			objectSpread:  int64(len(agg.objects)),
			bytes:         bytes,
			idealSections: ideal,
			spreadFactor:  float64(ls) / float64(ideal),
		})
	}

	fmt.Fprintf(w, "\n=== logs-section locality by %q (%d values, target section size %s) ===\n", sortKey, len(rows), humanize.IBytes(uint64(targetBytes)))
	if len(rows) == 0 {
		fmt.Fprintf(w, "  no %q label postings found\n", sortKey)
		return
	}

	logsSections := make([]int64, len(rows))
	spreadFactors := make([]float64, len(rows))
	for i, r := range rows {
		logsSections[i] = r.logsSections
		spreadFactors[i] = r.spreadFactor
	}

	var sectionStats, spreadStats PercentileStats
	calcPercentiles(logsSections, &sectionStats)
	calcPercentiles(spreadFactors, &spreadStats)

	fmt.Fprintf(w, "  logsSections: p50=%.2f p95=%.2f p99=%.2f max=%.0f\n", sectionStats.Median, sectionStats.P95, sectionStats.P99, sectionStats.Max)
	fmt.Fprintf(w, "  spreadFactor: p50=%.2f p95=%.2f p99=%.2f max=%.2f\n", spreadStats.Median, spreadStats.P95, spreadStats.P99, spreadStats.Max)

	slices.SortFunc(rows, func(a, b logsReportRow) int {
		if a.spreadFactor != b.spreadFactor {
			return cmp.Compare(b.spreadFactor, a.spreadFactor)
		}
		return cmp.Compare(b.logsSections, a.logsSections)
	})
	if top > 0 && len(rows) > top {
		rows = rows[:top]
	}

	fmt.Fprintf(w, "\nTop %d by spreadFactor:\n", top)
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "KEY\tLOGS_SECTIONS\tIDEAL\tSPREAD_FACTOR\tOBJECTS\tBYTES")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%.2f\t%d\t%s\n",
			r.key,
			r.logsSections,
			r.idealSections,
			r.spreadFactor,
			r.objectSpread,
			humanize.IBytes(uint64(r.bytes)),
		)
	}
	_ = tw.Flush()
}
