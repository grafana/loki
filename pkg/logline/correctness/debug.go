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
	"github.com/grafana/loki/v3/pkg/logql/syntax"

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
	Samples            []debugSample    `json:"samples,omitempty"`
	HintRanges         []debugHintRange `json:"hint_ranges"`
	Summary            debugSummary     `json:"summary"`
}

type debugSample struct {
	Time    time.Time `json:"time"`
	Value   float64   `json:"value"`
	Covered bool      `json:"covered"`
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

	from, through := getRangeBounds(hq, req.start, req.end)
	overlapping := s.indexStore.IndexesForRange(from, through)

	res, err := s.fetchNeedleResults(r.Context(), hq, req.start, req.end)
	if err != nil {
		level.Error(s.logger).Log("msg", "correctness debug query failed", "query", hq.query, "err", err)
		http.Error(w, "failed to query Loki", http.StatusInternalServerError)
		return
	}

	hintStart := time.Now()
	hints, _, err := s.hintProvider.ProvideHints(
		r.Context(),
		s.cfg.TenantID,
		hq.expr,
		model.TimeFromUnixNano(from.UnixNano()),
		model.TimeFromUnixNano(through.UnixNano()),
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
	}
	vr := res.verify(hintRanges)
	if isMetricQuery(hq) {
		resp.Samples = buildDebugSamples(hq.expr, res.samples, hintRanges)
		resp.HintRanges = buildDebugMetricHintRanges(hq.expr, res.samples, hintRanges)
	} else {
		resp.Entries = buildDebugEntries(res.entries, hintRanges)
		resp.HintRanges = buildDebugHintRanges(res.entries, hintRanges)
	}
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
	case hintQueryTypeLineFilter, hintQueryTypeLabelFilter, hintQueryTypeSMLabelFilter, hintQueryTypeJSONLabelFilter,
		hintQueryTypeMetricRange, hintQueryTypeMetricInstant:
	default:
		return debugRequest{}, fmt.Errorf(
			"query_type must be %q, %q, %q, %q, %q, or %q",
			hintQueryTypeLineFilter, hintQueryTypeLabelFilter, hintQueryTypeSMLabelFilter, hintQueryTypeJSONLabelFilter,
			hintQueryTypeMetricRange, hintQueryTypeMetricInstant,
		)
	}

	label := query.Get("label")
	if queryType != hintQueryTypeLineFilter && !isMetricQuery(hintQuery{queryType: queryType}) && label == "" {
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

// buildDebugHintQuery uses the same builders as runVerificationCycle.
// A metric check wraps the line-filter needle with buildMetricQuery.
func buildDebugHintQuery(req debugRequest) (hintQuery, error) {
	switch req.queryType {
	case hintQueryTypeLabelFilter, hintQueryTypeSMLabelFilter:
		return newHintQuery(req.queryType, req.needle, buildLabelFilterHintQuery(req.selector, req.label, req.needle))
	case hintQueryTypeJSONLabelFilter:
		return newHintQuery(req.queryType, req.needle, buildJSONLabelFilterHintQuery(req.selector, req.label, req.needle))
	case hintQueryTypeMetricRange, hintQueryTypeMetricInstant:
		line, err := newHintQuery(hintQueryTypeLineFilter, req.needle, buildLineFilterHintQuery(req.selector, req.needle))
		if err != nil {
			return hintQuery{}, err
		}
		return buildMetricQuery(req.queryType, line)
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

func buildDebugSamples(expr syntax.Expr, samples []verification.MetricSample, hintRanges []hintprovider.HintTimeRange) []debugSample {
	group := debugMatcherGroup(expr)
	out := make([]debugSample, 0, len(samples))
	for _, sample := range samples {
		winStart, winEnd := group.Widen(sample.Time, sample.Time)
		out = append(out, debugSample{
			Time:    sample.Time,
			Value:   sample.Value,
			Covered: sample.Value > 0 && verification.RangesOverlap(hintRanges, winStart, winEnd),
		})
	}
	return out
}

func buildDebugMetricHintRanges(expr syntax.Expr, samples []verification.MetricSample, hintRanges []hintprovider.HintTimeRange) []debugHintRange {
	group := debugMatcherGroup(expr)
	respRanges := make([]debugHintRange, 0, len(hintRanges))
	for i, hintRange := range hintRanges {
		entryCount := 0
		for _, sample := range samples {
			if sample.Value <= 0 {
				continue
			}
			winStart, winEnd := group.Widen(sample.Time, sample.Time)
			if verification.RangesOverlap([]hintprovider.HintTimeRange{hintRange}, winStart, winEnd) {
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

func debugMatcherGroup(expr syntax.Expr) syntax.MatcherRange {
	groups, err := syntax.MatcherGroups(expr)
	if err != nil || len(groups) != 1 {
		return syntax.MatcherRange{}
	}
	return groups[0]
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
