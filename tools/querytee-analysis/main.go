package main

import (
	"context"
	"fmt"
	"os"

	"github.com/alecthomas/kingpin/v2"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/querytee/comparator"
)

func main() {
	app := kingpin.New("querytee-analysis", "Analyze query-tee response mismatches to predict root causes.")
	addAnalyzeCommand(app)
	kingpin.MustParse(app.Parse(os.Args[1:]))
}

func addAnalyzeCommand(app *kingpin.Application) {
	var cfg AnalyzeConfig

	cmd := app.Command("analyze", "Fetch mismatch logs, re-query both cells, analyze root causes, and output CSV.")
	cmd.Flag("loki-endpoint", "Loki endpoint to query for query-tee logs").Required().StringVar(&cfg.LokiEndpoint)
	cmd.Flag("loki-query", "LogQL stream selector for query-tee logs").Default(`{app="querytee"}`).StringVar(&cfg.LokiQuery)
	cmd.Flag("org-id", "Tenant ID for querying the Loki endpoint").Required().StringVar(&cfg.OrgID)
	cmd.Flag("since", "Query logs from this duration ago until now (e.g. 1h, 30m). Alternative to --from/--to.").DurationVar(&cfg.Since)
	cmd.Flag("from", "Start of time range (RFC3339). Use with --to.").StringVar(&cfg.From)
	cmd.Flag("to", "End of time range (RFC3339). Use with --from.").StringVar(&cfg.To)
	cmd.Flag("cell-a-endpoint", "Loki endpoint for cell A (re-queries)").Required().StringVar(&cfg.CellAEndpoint)
	cmd.Flag("cell-b-endpoint", "Loki endpoint for cell B (re-queries)").Required().StringVar(&cfg.CellBEndpoint)
	cmd.Flag("value-comparison-tolerance", "Floating-point tolerance for sample value comparison").Default("0.000001").Float64Var(&cfg.ValueComparisonTolerance)
	cmd.Flag("mode", "Analysis mode: 'basic' (Phase 1+2) or 'deep' (Phase 1+2+3 detailed analysis)").Default("basic").EnumVar(&cfg.Mode, "basic", "deep")
	cmd.Flag("correlation-ids", "Comma-separated correlation IDs to limit analysis to specific mismatches").StringVar(&cfg.CorrelationIDsStr)
	cmd.Flag("output", "Output CSV file path").Default("results.csv").StringVar(&cfg.Output)
	cmd.Flag("limit", "Maximum number of successfully analyzed entries (failed queries are skipped and don't count)").Default("100").IntVar(&cfg.Limit)
	cmd.Flag("concurrency", "Number of concurrent analyses").Default("5").IntVar(&cfg.Concurrency)

	cmd.Action(func(_ *kingpin.ParseContext) error {
		return runAnalyze(&cfg)
	})
}

func runAnalyze(cfg *AnalyzeConfig) error {
	ctx := context.Background()

	parsed, err := parseAnalyzeConfig(cfg)
	if err != nil {
		return err
	}

	// 1. Fetch mismatch logs from Loki.
	lines, err := fetchMismatchLogs(ctx, parsed)
	if err != nil {
		return fmt.Errorf("fetching mismatch logs: %w", err)
	}
	if len(lines) == 0 {
		level.Info(logger).Log("msg", "no mismatch logs found in the given time range")
		return nil
	}

	// 2. Parse log lines into structured entries.
	entries := parseMismatchLogs(lines)
	level.Info(logger).Log("msg", "parsed mismatch entries", "count", len(entries))
	if len(entries) == 0 {
		return nil
	}

	// 3. Filter by correlation IDs if specified.
	if len(parsed.CorrelationIDs) > 0 {
		entries = filterByCorrelationIDs(entries, parsed.CorrelationIDs)
		level.Info(logger).Log("msg", "filtered entries by correlation IDs", "remaining", len(entries))
		if len(entries) == 0 {
			level.Info(logger).Log("msg", "no entries matched the specified correlation IDs")
			return nil
		}
	}

	// 4. Set up Loki clients for cell A/B re-queries.
	cellA := NewLokiClient(parsed.CellAEndpoint)
	cellB := NewLokiClient(parsed.CellBEndpoint)

	// 5. Build the comparator (same logic query-tee uses).
	cmp := comparator.NewSamplesComparator(comparator.SampleComparisonOptions{
		Tolerance: parsed.ValueComparisonTolerance,
	})

	// 6. Build the Phase 3 analyzer chain (used only in deep mode).
	analyzers := []Analyzer{
		&StreamAnalyzer{},
		&MetricAnalyzer{},
		&StatusMismatchAnalyzer{},
		&ResultTypeMismatchAnalyzer{},
	}

	// 7. Run analysis — process entries until limit successful analyses or pool exhausted.
	coordinator := NewAnalysisCoordinator(cellA, cellB, cmp, parsed.Concurrency, parsed.DeepMode, analyzers...)
	results := coordinator.AnalyzeAll(ctx, entries, parsed.Limit)

	// 8. Log summary.
	causeCounts := make(map[PredictedCause]int)
	for _, r := range results {
		causeCounts[r.PredictedCause]++
	}
	for cause, count := range causeCounts {
		level.Info(logger).Log("msg", "analysis summary", "predicted_cause", cause, "count", count)
	}

	// 9. Write CSV.
	if err := writeCSV(parsed.Output, results); err != nil {
		return fmt.Errorf("writing CSV: %w", err)
	}

	level.Info(logger).Log("msg", "wrote results", "results", len(results), "loaded_entries", len(entries),
		"limit", parsed.Limit, "mode", parsed.Mode, "output", parsed.Output)
	return nil
}

func filterByCorrelationIDs(entries []*MismatchEntry, ids map[string]struct{}) []*MismatchEntry {
	var filtered []*MismatchEntry
	for _, e := range entries {
		if _, ok := ids[e.CorrelationID]; ok {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
