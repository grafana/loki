package correctness

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logline/verification"
)

type debugResponse struct {
	HintQuery          string           `json:"hint_query"`
	HintQueryType      string           `json:"hint_query_type"`
	RangeStart         time.Time        `json:"range_start"`
	RangeEnd           time.Time        `json:"range_end"`
	Selector           string           `json:"selector"`
	Label              string           `json:"label,omitempty"`
	Needle             string           `json:"needle"`
	OverlappingIndexes int              `json:"overlapping_indexes"`
	HintDuration       string           `json:"hint_duration"`
	Entries            []debugEntry     `json:"entries"`
	HintRanges         []debugHintRange `json:"hint_ranges"`
	Summary            debugSummary     `json:"summary"`
}

type debugEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Line      string    `json:"line"`
	Covered   bool      `json:"covered"`
	CoveredBy *int      `json:"covered_by,omitempty"`
}

type debugHintRange struct {
	Index      int       `json:"index"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Source     string    `json:"source"`
	EntryCount int       `json:"entry_count"`
}

type debugSummary struct {
	TotalResults   int  `json:"total_results"`
	CoveredResults int  `json:"covered_results"`
	FalseNegatives int  `json:"false_negatives"`
	FalsePositives int  `json:"false_positives"`
	Correct        bool `json:"correct"`
}

type debugRequest struct {
	start     time.Time
	end       time.Time
	selector  string
	label     string
	needle    string
	queryType hintQueryType
}

func (s *Service) handleDebug(w http.ResponseWriter, r *http.Request) {
	req, err := parseDebugRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	hq, err := buildDebugHintQuery(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid hint query: %v", err), http.StatusBadRequest)
		return
	}

	overlapping := s.indexStore.IndexesForRange(req.start, req.end)

	entries, _, err := s.queryRange(r.Context(), hq.query, req.start, req.end, s.cfg.LogQueryLimit)
	if err != nil {
		level.Error(s.logger).Log("msg", "correctness debug query failed", "query", hq.query, "err", err)
		http.Error(w, "failed to query Loki", http.StatusInternalServerError)
		return
	}
	resultEntries := protoEntries(entries)

	hintStart := time.Now()
	hints, _, err := s.hintProvider.ProvideHints(
		r.Context(),
		s.cfg.TenantID,
		hq.expr,
		model.TimeFromUnixNano(req.start.UnixNano()),
		model.TimeFromUnixNano(req.end.UnixNano()),
	)
	hintDuration := time.Since(hintStart)
	if err != nil {
		if errors.Is(err, hintprovider.ErrUnsupported) {
			http.Error(w, hintprovider.ErrUnsupported.Error(), http.StatusBadRequest)
			return
		}

		level.Error(s.logger).Log("msg", "correctness debug hint lookup failed", "query", hq.query, "err", err)
		http.Error(w, "failed to fetch hints", http.StatusInternalServerError)
		return
	}

	var hintRanges []hintprovider.HintTimeRange
	if hints != nil {
		hintRanges = hints.TimeRanges
	}

	resp := debugResponse{
		HintQuery:          hq.query,
		HintQueryType:      string(req.queryType),
		RangeStart:         req.start,
		RangeEnd:           req.end,
		Selector:           req.selector,
		Label:              req.label,
		Needle:             req.needle,
		OverlappingIndexes: len(overlapping),
		HintDuration:       hintDuration.String(),
		Entries:            buildDebugEntries(resultEntries, hintRanges),
		HintRanges:         buildDebugHintRanges(resultEntries, hintRanges),
	}

	vr := verifyHints(hintRanges, resultEntries)
	resp.Summary = debugSummary{
		TotalResults:   vr.TotalResults,
		CoveredResults: vr.CoveredResults,
		FalseNegatives: vr.FalseNegatives,
		FalsePositives: vr.FalsePositives,
		Correct:        vr.Correct,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		level.Error(s.logger).Log("msg", "correctness debug response encode failed", "err", err)
	}
}

func parseDebugRequest(r *http.Request) (debugRequest, error) {
	query := r.URL.Query()

	startRaw := query.Get("start")
	if startRaw == "" {
		return debugRequest{}, fmt.Errorf("start is required")
	}

	endRaw := query.Get("end")
	if endRaw == "" {
		return debugRequest{}, fmt.Errorf("end is required")
	}

	selector := query.Get("selector")
	if selector == "" {
		return debugRequest{}, fmt.Errorf("selector is required")
	}

	needle := query.Get("needle")
	if needle == "" {
		return debugRequest{}, fmt.Errorf("needle is required")
	}

	queryType := hintQueryType(query.Get("query_type"))
	if queryType == "" {
		queryType = hintQueryTypeLineFilter
	}
	switch queryType {
	case hintQueryTypeLineFilter, hintQueryTypeLabelFilter, hintQueryTypeSMLabelFilter, hintQueryTypeJSONLabelFilter:
	default:
		return debugRequest{}, fmt.Errorf(
			"query_type must be %q, %q, %q, or %q",
			hintQueryTypeLineFilter, hintQueryTypeLabelFilter, hintQueryTypeSMLabelFilter, hintQueryTypeJSONLabelFilter,
		)
	}

	label := query.Get("label")
	if queryType != hintQueryTypeLineFilter && label == "" {
		return debugRequest{}, fmt.Errorf("label is required for query_type=%q", queryType)
	}

	start, err := time.Parse(time.RFC3339, startRaw)
	if err != nil {
		return debugRequest{}, fmt.Errorf("invalid start: %w", err)
	}

	end, err := time.Parse(time.RFC3339, endRaw)
	if err != nil {
		return debugRequest{}, fmt.Errorf("invalid end: %w", err)
	}

	start = start.UTC()
	end = end.UTC()
	if end.Before(start) {
		return debugRequest{}, fmt.Errorf("end must be greater than or equal to start")
	}

	return debugRequest{
		start:     start,
		end:       end,
		selector:  selector,
		label:     label,
		needle:    needle,
		queryType: queryType,
	}, nil
}

// buildDebugHintQuery uses the same builders + newHintQuery as runVerificationCycle.
func buildDebugHintQuery(req debugRequest) (hintQuery, error) {
	switch req.queryType {
	case hintQueryTypeLabelFilter, hintQueryTypeSMLabelFilter:
		return newHintQuery(req.queryType, req.needle, buildLabelFilterHintQuery(req.selector, req.label, req.needle))
	case hintQueryTypeJSONLabelFilter:
		return newHintQuery(req.queryType, req.needle, buildJSONLabelFilterHintQuery(req.selector, req.label, req.needle))
	default:
		return newHintQuery(hintQueryTypeLineFilter, req.needle, buildLineFilterHintQuery(req.selector, req.needle))
	}
}

func buildDebugEntries(entries []logproto.Entry, hintRanges []hintprovider.HintTimeRange) []debugEntry {
	respEntries := make([]debugEntry, 0, len(entries))
	for _, entry := range entries {
		covered := verification.TimestampCovered(hintRanges, entry.Timestamp)
		coveredBy := firstCoveringRangeIndex(hintRanges, entry.Timestamp)
		respEntries = append(respEntries, debugEntry{
			Timestamp: entry.Timestamp,
			Line:      entry.Line,
			Covered:   covered,
			CoveredBy: coveredBy,
		})
	}
	return respEntries
}

func buildDebugHintRanges(entries []logproto.Entry, hintRanges []hintprovider.HintTimeRange) []debugHintRange {
	respRanges := make([]debugHintRange, 0, len(hintRanges))
	for i, hintRange := range hintRanges {
		entryCount := 0
		for _, entry := range entries {
			if verification.RangeCoversTimestamp(hintRange, entry.Timestamp) {
				entryCount++
			}
		}
		respRanges = append(respRanges, debugHintRange{
			Index:      i,
			Start:      hintRange.Start,
			End:        hintRange.End,
			Source:     hintRange.Source,
			EntryCount: entryCount,
		})
	}
	return respRanges
}

func firstCoveringRangeIndex(ranges []hintprovider.HintTimeRange, ts time.Time) *int {
	for i, candidate := range ranges {
		if verification.RangeCoversTimestamp(candidate, ts) {
			index := i
			return &index
		}
	}
	return nil
}
