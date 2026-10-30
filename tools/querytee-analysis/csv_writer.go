package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"time"

	"github.com/go-kit/log/level"
)

var csvHeaders = []string{
	"correlation_id",
	"tenant",
	"query",
	"query_type",
	"start_time",
	"end_time",
	"original_mismatch_cause",
	// Analysis result
	"phase",
	"predicted_cause",
	// Phase 1
	"phase1_comparison_mismatch_cause",
	// Phase 2: log count comparison
	"phase2_count_query",
	"phase2_cell_a_count",
	"phase2_cell_b_count",
	// Phase 3 details (deep mode)
	"detected_mismatch_type",
	"mismatched_item",
	"details",
}

func writeCSV(path string, results []*AnalysisResult) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}

	w := csv.NewWriter(f)

	if err := w.Write(csvHeaders); err != nil {
		f.Close()
		return fmt.Errorf("writing CSV header: %w", err)
	}

	for _, r := range results {
		if r == nil || r.MismatchEntry == nil {
			continue
		}
		row := resultToRow(r)
		if err := w.Write(row); err != nil {
			f.Close()
			return fmt.Errorf("writing CSV row: %w", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return fmt.Errorf("flushing CSV writer: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("closing output file: %w", err)
	}

	level.Info(logger).Log("msg", "wrote CSV output", "path", path, "rows", len(results))
	return nil
}

func resultToRow(r *AnalysisResult) []string {
	return []string{
		r.MismatchEntry.CorrelationID,
		r.MismatchEntry.Tenant,
		r.MismatchEntry.Query,
		r.MismatchEntry.QueryType,
		formatTime(r.MismatchEntry.StartTime),
		formatTime(r.MismatchEntry.EndTime),
		r.MismatchEntry.MismatchCause,
		// Analysis result
		fmt.Sprintf("%d", r.Phase),
		string(r.PredictedCause),
		// Phase 1
		r.Phase1ComparisonMismatchCause,
		// Phase 2: log count
		r.Phase2CountQuery,
		fmt.Sprintf("%d", r.Phase2CellACount),
		fmt.Sprintf("%d", r.Phase2CellBCount),
		// Phase 3 details
		r.DetectedMismatchType,
		r.MismatchedItem,
		r.Details,
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
