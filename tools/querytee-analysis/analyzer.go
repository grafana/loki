package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/querytee/comparator"
)

// AnalysisCoordinator runs the analysis pipeline against each mismatch entry
// concurrently. In basic mode it runs Phases 1 and 2; in deep mode it also
// runs Phase 3 detailed analyzers.
type AnalysisCoordinator struct {
	cellA       *LokiClient
	cellB       *LokiClient
	comparator  *comparator.SamplesComparator
	analyzers   []Analyzer
	concurrency int
	deepMode    bool
}

func NewAnalysisCoordinator(
	cellA, cellB *LokiClient,
	cmp *comparator.SamplesComparator,
	concurrency int,
	deepMode bool,
	analyzers ...Analyzer,
) *AnalysisCoordinator {
	if concurrency <= 0 {
		concurrency = 1
	}
	return &AnalysisCoordinator{
		cellA:       cellA,
		cellB:       cellB,
		comparator:  cmp,
		analyzers:   analyzers,
		concurrency: concurrency,
		deepMode:    deepMode,
	}
}

// analysisOutcome is the result of analyzing a single mismatch entry.
type analysisOutcome struct {
	results []*AnalysisResult
	failed  bool // true when queries failed even after retries
}

// AnalyzeAll processes entries concurrently until limit successful analyses are
// collected or all entries are exhausted. Entries whose queries fail (even after
// retries) are skipped and do not count toward the limit. If limit <= 0, all
// entries are processed.
func (c *AnalysisCoordinator) AnalyzeAll(ctx context.Context, entries []*MismatchEntry, limit int) []*AnalysisResult {
	work := make(chan *MismatchEntry, c.concurrency)
	outCh := make(chan analysisOutcome, c.concurrency)

	var wg sync.WaitGroup
	for i := 0; i < c.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for entry := range work {
				results, failed := c.analyzeSingle(ctx, entry)
				outCh <- analysisOutcome{results: results, failed: failed}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(outCh)
	}()

	done := make(chan struct{})
	go func() {
		defer close(work)
		for _, entry := range entries {
			select {
			case work <- entry:
			case <-done:
				return
			}
		}
	}()

	var allResults []*AnalysisResult
	successful, skipped := 0, 0
	for out := range outCh {
		if out.failed {
			skipped++
			continue
		}
		allResults = append(allResults, out.results...)
		successful++
		if limit > 0 && successful >= limit {
			close(done)
			break
		}
	}

	for out := range outCh {
		if !out.failed {
			allResults = append(allResults, out.results...)
			successful++
		} else {
			skipped++
		}
	}

	level.Info(logger).Log("msg", "analysis complete", "successful", successful, "skipped_failures", skipped,
		"total_candidates", len(entries), "limit", limit)
	return allResults
}

// analyzeSingle implements the analysis pipeline for a single mismatch:
//
//  1. Re-run the original query against both cells. If the responses match,
//     the mismatch was caused by ingestion pipeline delay.
//  2. Run a log count comparison using count_over_time on the same stream
//     labels and pipeline. Compares per-series counts and reports each
//     discrepancy (missing series, count differences).
//  3. (Deep mode only) Run detailed analyzers on the Phase 1 responses to
//     classify the specific type of mismatch.
//
// analyzeSingle returns the analysis results and whether the entry failed
// (queries errored even after retries). Failed entries are skipped by the
// coordinator and do not count toward the processing limit.
func (c *AnalysisCoordinator) analyzeSingle(ctx context.Context, entry *MismatchEntry) ([]*AnalysisResult, bool) {
	baseResult := &AnalysisResult{
		MismatchEntry:  entry,
		PredictedCause: PredictedCauseUnknown,
	}

	// ------------------------------------------------------------------
	// Phase 1: re-run original query
	// ------------------------------------------------------------------
	rawA, rawB, err := c.reQuery(ctx, entry, entry.Query)
	if err != nil {
		level.Warn(logger).Log("msg", "phase 1 re-query failed, skipping entry", "correlation_id", entry.CorrelationID, "err", err)
		baseResult.Phase = 1
		baseResult.Details = "phase 1 re-query failed: " + err.Error()
		return []*AnalysisResult{baseResult}, true
	}

	summary, cmpErr := c.comparator.Compare(rawA, rawB, time.Now())
	if cmpErr == nil {
		baseResult.Phase = 1
		baseResult.PredictedCause = PredictedCauseIngestionDelay
		return []*AnalysisResult{baseResult}, false
	}

	if summary != nil {
		baseResult.Phase1ComparisonMismatchCause = summary.MismatchCause
	}

	// ------------------------------------------------------------------
	// Phase 2: log count comparison (per-series)
	// ------------------------------------------------------------------
	countQuery, evalTime, buildErr := BuildLogCountQuery(entry.Query, entry.StartTime, entry.EndTime)
	if buildErr != nil {
		level.Warn(logger).Log("msg", "failed to build log count query, skipping entry", "correlation_id", entry.CorrelationID, "err", buildErr)
		baseResult.Phase = 2
		baseResult.Phase2CountQuery = ""
		baseResult.Details = "failed to build log count query: " + buildErr.Error()
		return []*AnalysisResult{baseResult}, true
	}

	phase2Result, phase2Err := c.runPhase2(ctx, entry, countQuery, evalTime, baseResult)
	if phase2Err != nil {
		level.Warn(logger).Log("msg", "phase 2 log count query failed, skipping entry", "correlation_id", entry.CorrelationID, "err", phase2Err)
		baseResult.Phase = 2
		baseResult.Phase2CountQuery = countQuery
		baseResult.Details = "phase 2 log count query failed: " + phase2Err.Error()
		return []*AnalysisResult{baseResult}, true
	}

	if !c.deepMode {
		return []*AnalysisResult{phase2Result}, false
	}

	// ------------------------------------------------------------------
	// Phase 3: detailed analysis of Phase 1 responses (deep mode only)
	// ------------------------------------------------------------------
	resultType := detectResultType(rawA, rawB)

	allResults := []*AnalysisResult{phase2Result}

	for _, a := range c.analyzers {
		if !a.CanAnalyze(entry, resultType) {
			continue
		}
		results, err := a.Analyze(ctx, entry, rawA, rawB)
		if err != nil {
			level.Warn(logger).Log("msg", "phase 3 analyzer error", "analyzer", a.Name(),
				"correlation_id", entry.CorrelationID, "err", err)
			continue
		}
		for _, r := range results {
			r.MismatchEntry = entry
			r.Phase = 3
			r.Phase1ComparisonMismatchCause = baseResult.Phase1ComparisonMismatchCause
			r.Phase2CountQuery = phase2Result.Phase2CountQuery
			r.Phase2CellACount = phase2Result.Phase2CellACount
			r.Phase2CellBCount = phase2Result.Phase2CellBCount
			allResults = append(allResults, r)
		}
	}

	return allResults, false
}

// runPhase2 executes the count query on both cells, compares per-series
// counts, and returns a single summary AnalysisResult with discrepancy counts.
func (c *AnalysisCoordinator) runPhase2(
	ctx context.Context,
	entry *MismatchEntry,
	countQuery string,
	evalTime time.Time,
	baseResult *AnalysisResult,
) (*AnalysisResult, error) {
	if c.cellA == nil || c.cellB == nil {
		return nil, fmt.Errorf("cell clients not configured")
	}

	dataA, errA := c.cellA.InstantQuery(ctx, entry.Tenant, countQuery, evalTime)
	dataB, errB := c.cellB.InstantQuery(ctx, entry.Tenant, countQuery, evalTime)
	if errA != nil || errB != nil {
		return nil, fmt.Errorf("cellA=%v, cellB=%v", errA, errB)
	}

	vecA, errA := extractVector(dataA)
	vecB, errB := extractVector(dataB)
	if errA != nil || errB != nil {
		return nil, fmt.Errorf("extracting vectors: cellA=%v, cellB=%v", errA, errB)
	}

	summary := summarizeCountVectors(vecA, vecB)

	r := &AnalysisResult{
		MismatchEntry:                 entry,
		Phase:                         2,
		Phase1ComparisonMismatchCause: baseResult.Phase1ComparisonMismatchCause,
		Phase2CountQuery:              countQuery,
		Phase2CellACount:              summary.totalA,
		Phase2CellBCount:              summary.totalB,
		Details:                       summary.details,
	}

	if summary.hasDiscrepancies() {
		r.PredictedCause = PredictedCauseLogCountMismatch
	} else {
		r.PredictedCause = PredictedCauseQueryExecBug
	}

	return r, nil
}

type countSummary struct {
	totalA              int64
	totalB              int64
	seriesMatch         int
	seriesCountMismatch int
	seriesMissingCellA  int
	seriesMissingCellB  int
	details             string
}

func (s *countSummary) hasDiscrepancies() bool {
	return s.seriesCountMismatch > 0 || s.seriesMissingCellA > 0 || s.seriesMissingCellB > 0
}

// summarizeCountVectors compares two count vectors per-series and returns
// aggregated counts of matching series, mismatched series, and missing series.
func summarizeCountVectors(vecA, vecB loghttp.Vector) countSummary {
	mapA := vectorByLabels(vecA)
	mapB := vectorByLabels(vecB)

	var s countSummary
	s.totalA = sumVector(vecA)
	s.totalB = sumVector(vecB)

	for lbl, sA := range mapA {
		sB, ok := mapB[lbl]
		if !ok {
			s.seriesMissingCellB++
			continue
		}
		cA := int64(math.Round(float64(sA.Value)))
		cB := int64(math.Round(float64(sB.Value)))
		if cA == cB {
			s.seriesMatch++
		} else {
			s.seriesCountMismatch++
		}
	}

	for lbl := range mapB {
		if _, ok := mapA[lbl]; !ok {
			s.seriesMissingCellA++
		}
	}

	totalSeries := s.seriesMatch + s.seriesCountMismatch + s.seriesMissingCellA + s.seriesMissingCellB
	if !s.hasDiscrepancies() {
		s.details = fmt.Sprintf("all %d series match", totalSeries)
	} else {
		s.details = fmt.Sprintf("%d series total: %d match, %d count_mismatch, %d missing_in_cell_a, %d missing_in_cell_b",
			totalSeries, s.seriesMatch, s.seriesCountMismatch, s.seriesMissingCellA, s.seriesMissingCellB)
	}

	return s
}

func extractVector(data *loghttp.QueryResponseData) (loghttp.Vector, error) {
	if data == nil {
		return nil, fmt.Errorf("nil response data")
	}
	vec, ok := data.Result.(loghttp.Vector)
	if !ok {
		return nil, fmt.Errorf("expected vector result, got %T", data.Result)
	}
	return vec, nil
}

func sumVector(vec loghttp.Vector) int64 {
	var total float64
	for _, s := range vec {
		total += float64(s.Value)
	}
	return int64(math.Round(total))
}

func vectorByLabels(vec loghttp.Vector) map[string]model.Sample {
	m := make(map[string]model.Sample, len(vec))
	for i := range vec {
		m[vec[i].Metric.String()] = vec[i]
	}
	return m
}

// reQuery runs the given query against both cells and returns raw response bytes.
func (c *AnalysisCoordinator) reQuery(ctx context.Context, entry *MismatchEntry, query string) (rawA, rawB []byte, err error) {
	if c.cellA == nil || c.cellB == nil {
		return nil, nil, fmt.Errorf("cell clients not configured")
	}
	rawA, errA := c.cellA.QueryRangeRaw(ctx, entry.Tenant, query, entry.StartTime, entry.EndTime, 0, 0)
	rawB, errB := c.cellB.QueryRangeRaw(ctx, entry.Tenant, query, entry.StartTime, entry.EndTime, 0, 0)
	if errA != nil || errB != nil {
		return nil, nil, fmt.Errorf("cellA=%v, cellB=%v", errA, errB)
	}
	return rawA, rawB, nil
}

// detectResultType inspects raw JSON responses to determine the Loki result
// type (streams, matrix, vector, scalar).
func detectResultType(responses ...[]byte) string {
	for _, b := range responses {
		if len(b) == 0 {
			continue
		}
		var envelope struct {
			Data struct {
				ResultType loghttp.ResultType `json:"resultType"`
			} `json:"data"`
		}
		if err := json.Unmarshal(b, &envelope); err == nil && envelope.Data.ResultType != "" {
			return string(envelope.Data.ResultType)
		}
	}
	return ""
}
