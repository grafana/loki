package correctness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand" //#nosec G404 -- Cycle sampling is not security-sensitive. -- nosemgrep: math-random-used
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	lokiclient "github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/httpreq"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

const (
	skipIngesterWindow   = "ingester_window"
	skipNoLabels         = "no_labels"
	skipNoLabelValues    = "no_label_values"
	skipNoLogs           = "no_logs"
	skipNoNeedle         = "no_entropy_needle"
	skipQueryUnsupported = "query_unsupported"
)

var (
	tokenCandidateRE = regexp.MustCompile(`[A-Za-z0-9._:/@%?-]{6,128}`)
	// jsonLabelNameRE is the subset of LogQL identifiers we will put after
	// `| json |`. Hyphenated JSON keys are skipped rather than quoted.
	jsonLabelNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Service runs continuous correctness checks between Loki query results and logline indexes.
type Service struct {
	services.Service

	cfg          Config
	indexStore   *store.Store
	hintProvider hintprovider.QueryHintProvider
	logger       log.Logger
	metrics      *Metrics
	lokiClients  []lokiEndpointClient

	startedAt time.Time
	nowFn     func() time.Time

	randMu sync.Mutex
	rand   *rand.Rand
}

type lokiEndpointClient struct {
	endpoint string
	client   lokiclient.Client
}

// CycleReport captures one verification cycle for operator diagnostics.
type CycleReport struct {
	Timestamp  time.Time `json:"timestamp"`
	RangeStart time.Time `json:"range_start"`
	RangeEnd   time.Time `json:"range_end"`
	Label      string    `json:"label"`
	Value      string    `json:"value"`
	Selector   string    `json:"selector"`
	Needle     string    `json:"needle"`
	HintQuery  string    `json:"hint_query"`
	// HintQueryType is "line_filter", "label_filter" (stream label),
	// "sm_label_filter" (structured metadata), or "json_label_filter"
	// (`| json | field="value"`).
	HintQueryType           string        `json:"hint_query_type"`
	CandidateTimeRanges     int           `json:"candidate_time_ranges"`
	TotalResults            int           `json:"total_results"`
	CoveredResults          int           `json:"covered_results"`
	TruePositives           int           `json:"true_positives"`
	FalsePositives          int           `json:"false_positives"`
	FalseNegatives          int           `json:"false_negatives"`
	HintDuration            time.Duration `json:"hint_duration"`
	AvgHintRangeSeconds     float64       `json:"avg_hint_range_seconds"`
	Correct                 bool          `json:"correct"`
	NeedleQueryTruncated    bool          `json:"needle_query_truncated"`
	OverlappingIndexIDs     []string      `json:"overlapping_index_ids,omitempty"`
	FalseNegativeTimestamps []time.Time   `json:"false_negative_timestamps,omitempty"`
	SkippedReason           string        `json:"skipped_reason,omitempty"`
	Error                   string        `json:"error,omitempty"`
}

// New creates a correctness verification service.
func New(indexStore *store.Store, cfg Config, logger log.Logger, reg prometheus.Registerer) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid correctness config: %w", err)
	}
	if indexStore == nil {
		return nil, fmt.Errorf("indexStore cannot be nil")
	}

	startedAt := time.Now().UTC()
	if !cfg.StartedAt.IsZero() {
		startedAt = cfg.StartedAt.UTC()
	}

	hintProvider, err := hintprovider.NewLoglineHintProvider(indexStore, cfg.NgramLength, 0, nil, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("create hint provider: %w", err)
	}

	s := &Service{
		cfg:          cfg,
		indexStore:   indexStore,
		hintProvider: hintProvider,
		logger:       logger,
		metrics:      NewMetrics(reg),
		lokiClients:  newLokiEndpointClients(cfg),
		startedAt:    startedAt,
		nowFn:        time.Now,
		rand:         rand.New(rand.NewSource(time.Now().UnixNano())), //#nosec G404 -- Cycle sampling is not security-sensitive. -- nosemgrep: math-random-used
	}
	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

func (s *Service) starting(ctx context.Context) error {
	level.Info(s.logger).Log(
		"msg", "correctness service starting",
		"loki_query_endpoints", strings.Join(s.cfg.LokiQueryEndpoints, ","),
		"query_interval", s.cfg.QueryInterval,
		"query_ingesters_within", s.cfg.QueryIngestersWithin,
		"query_range_min", s.cfg.QueryRangeMin,
		"query_range_max", s.cfg.QueryRangeMax,
		"max_lookback", s.cfg.MaxLookback,
		"started_at", s.startedAt,
	)
	if err := s.indexStore.StartPolling(ctx); err != nil {
		return fmt.Errorf("start store polling: %w", err)
	}
	return nil
}

func (s *Service) running(ctx context.Context) error {
	errBackoff := backoff.New(ctx, backoff.Config{
		MinBackoff: s.cfg.ErrorBackoffMin,
		MaxBackoff: s.cfg.ErrorBackoffMax,
	})

	for ctx.Err() == nil {
		s.metrics.cyclesTotal.Inc()
		cycleStart := time.Now()

		cycleCtx, cancel := context.WithTimeout(ctx, s.cfg.CycleTimeout)
		report, err := s.runVerificationCycle(cycleCtx)
		cancel()
		report.Timestamp = cycleStart.UTC()

		s.metrics.cycleDuration.Observe(time.Since(cycleStart).Seconds())
		if err != nil {
			report.Error = err.Error()
			s.metrics.cycleErrorsTotal.Inc()
			level.Error(s.logger).Log(
				"msg", "correctness cycle failed",
				"err", err,
				"selector", report.Selector,
				"needle", report.Needle,
				"range_start", report.RangeStart,
				"range_end", report.RangeEnd,
				"overlapping_indexes", len(report.OverlappingIndexIDs),
				"candidate_time_ranges", report.CandidateTimeRanges,
			)
			errBackoff.Wait()
			continue
		}

		errBackoff.Reset()

		if report.SkippedReason == "" {
			logLevel := level.Info(s.logger)
			if !report.Correct {
				logLevel = level.Warn(s.logger)
			}
			_ = logLevel.Log(
				"msg", "correctness cycle completed",
				"hint_query", report.HintQuery,
				"hint_query_type", report.HintQueryType,
				"range_start", report.RangeStart,
				"range_end", report.RangeEnd,
				"overlapping_indexes", len(report.OverlappingIndexIDs),
				"candidate_time_ranges", report.CandidateTimeRanges,
				"total_results", report.TotalResults,
				"covered_results", report.CoveredResults,
				"false_positives", report.FalsePositives,
				"false_negatives", report.FalseNegatives,
				"hint_duration", report.HintDuration,
				"avg_hint_range_seconds", fmt.Sprintf("%.1f", report.AvgHintRangeSeconds),
				"needle_query_truncated", report.NeedleQueryTruncated,
				"correct", report.Correct,
			)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.cfg.QueryInterval):
		}
	}

	return nil
}

func (s *Service) stopping(err error) error {
	level.Info(s.logger).Log("msg", "correctness service stopping", "err", err)
	return nil
}

func (s *Service) runVerificationCycle(ctx context.Context) (CycleReport, error) {
	now := s.nowFn().UTC()
	report := CycleReport{Timestamp: now}

	rangeStart, rangeEnd, skipReason := s.pickRandomRange(now)
	if skipReason != "" {
		report.SkippedReason = skipReason
		s.recordSkipped(skipReason)
		level.Info(s.logger).Log(
			"msg", "correctness cycle skipped",
			"reason", skipReason,
			"started_at", s.startedAt,
			"now", now,
			"query_ingesters_within", s.cfg.QueryIngestersWithin,
		)
		return report, nil
	}
	report.RangeStart = rangeStart
	report.RangeEnd = rangeEnd
	level.Info(s.logger).Log(
		"msg", "correctness cycle step",
		"step", "picked_range",
		"range_start", rangeStart,
		"range_end", rangeEnd,
	)

	label, value, err := s.pickRandomLabelValue(ctx, rangeStart, rangeEnd)
	if err != nil {
		if reason, ok := skipReasonFromErr(err); ok {
			report.SkippedReason = reason
			s.recordSkipped(reason)
			level.Info(s.logger).Log("msg", "correctness cycle skipped", "reason", reason)
			return report, nil
		}
		return report, err
	}

	selector := buildSelector(label, value)
	report.Label = label
	report.Value = value
	report.Selector = selector
	level.Info(s.logger).Log(
		"msg", "correctness cycle step",
		"step", "picked_label_value",
		"label", label,
		"value", value,
		"selector", selector,
	)

	logs, _, err := s.queryRange(ctx, selector, rangeStart, rangeEnd, s.cfg.LogQueryLimit)
	if err != nil {
		return report, err
	}
	level.Info(s.logger).Log(
		"msg", "correctness cycle step",
		"step", "fetched_sample_logs",
		"selector", selector,
		"log_count", len(logs),
	)
	if len(logs) == 0 {
		report.SkippedReason = skipNoLogs
		s.recordSkipped(skipNoLogs)
		level.Info(s.logger).Log(
			"msg", "correctness cycle skipped",
			"reason", skipNoLogs,
			"selector", selector,
			"range_start", rangeStart,
			"range_end", rangeEnd,
		)
		return report, nil
	}

	hq, ok := s.pickHintQuery(selector, label, value, logs)
	if !ok {
		report.SkippedReason = skipNoNeedle
		s.recordSkipped(skipNoNeedle)
		level.Info(s.logger).Log(
			"msg", "correctness cycle skipped",
			"reason", skipNoNeedle,
			"selector", selector,
			"range_start", rangeStart,
			"range_end", rangeEnd,
		)
		return report, nil
	}
	report.Needle = hq.needle
	report.HintQuery = hq.query
	report.HintQueryType = string(hq.queryType)
	level.Info(s.logger).Log(
		"msg", "correctness cycle step",
		"step", "picked_needle",
		"needle", hq.needle,
		"hint_query_type", hq.queryType,
		"hint_query", hq.query,
	)

	overlapping := s.indexStore.IndexesForRange(rangeStart, rangeEnd)
	ids := make([]string, len(overlapping))
	for i, m := range overlapping {
		ids[i] = m.ID()
	}
	report.OverlappingIndexIDs = ids

	resultEntries, _, err := s.queryRange(ctx, hq.query, rangeStart, rangeEnd, s.cfg.LogQueryLimit)
	if err != nil {
		return report, err
	}
	report.NeedleQueryTruncated = len(resultEntries) >= s.cfg.LogQueryLimit
	level.Info(s.logger).Log(
		"msg", "correctness cycle step",
		"step", "fetched_needle_results",
		"result_count", len(resultEntries),
		"truncated", report.NeedleQueryTruncated,
	)

	hintStart := time.Now()
	hints, _, err := s.hintProvider.ProvideHints(
		ctx,
		s.cfg.TenantID,
		hq.expr,
		model.TimeFromUnixNano(rangeStart.UnixNano()),
		model.TimeFromUnixNano(rangeEnd.UnixNano()),
	)
	hintDuration := time.Since(hintStart)
	if err != nil {
		if errors.Is(err, hintprovider.ErrUnsupported) {
			report.SkippedReason = skipQueryUnsupported
			s.recordSkipped(skipQueryUnsupported)
			level.Info(s.logger).Log(
				"msg", "correctness cycle skipped",
				"reason", skipQueryUnsupported,
				"selector", selector,
				"needle", hq.needle,
			)
			return report, nil
		}
		return report, err
	}

	var hintRanges []hintprovider.HintTimeRange
	if hints != nil {
		hintRanges = hints.TimeRanges
	}

	// Compute average hint range duration for sentinel detection.
	// A sentinel term produces a hint covering the full day (~86400s);
	// normal terms produce small, focused ranges.
	var avgHintRangeSeconds float64
	if len(hintRanges) > 0 {
		var totalCoverage time.Duration
		for _, hr := range hintRanges {
			totalCoverage += hr.End.Sub(hr.Start)
		}
		avgHintRangeSeconds = totalCoverage.Seconds() / float64(len(hintRanges))
	}

	level.Info(s.logger).Log(
		"msg", "correctness cycle step",
		"step", "fetched_hints",
		"hint_ranges", len(hintRanges),
		"hint_duration", hintDuration,
	)

	vr := verifyHints(hintRanges, protoEntries(resultEntries))

	report.AvgHintRangeSeconds = avgHintRangeSeconds
	report.CandidateTimeRanges = vr.HintRanges
	report.TotalResults = vr.TotalResults
	report.CoveredResults = vr.CoveredResults
	report.TruePositives = vr.CoveredResults
	report.FalsePositives = vr.FalsePositives
	report.FalseNegatives = vr.FalseNegatives
	report.FalseNegativeTimestamps = vr.FalseNegativeTimestamps
	report.HintDuration = hintDuration
	report.Correct = vr.Correct

	s.metrics.totalTests.Inc()

	// :) path
	if report.Correct {
		s.metrics.totalCorrectTests.Inc()
		s.metrics.lastSuccessfulTestTs.Set(float64(now.Unix()))

		return report, nil
	}

	// :( path
	s.metrics.totalIncorrectTests.Inc()
	s.metrics.lastIncorrectTestTs.Set(float64(now.Unix()))

	excludedCoveringFN := s.excludedIndexesCoveringFNs(report.FalseNegativeTimestamps, rangeStart, rangeEnd)

	// Log failure detail: which indexes were consulted, which timestamps were missed.
	level.Warn(s.logger).Log(
		"msg", "correctness failure detail",
		"hint_query", report.HintQuery,
		"overlapping_index_ids", strings.Join(report.OverlappingIndexIDs, ","),
		"false_negative_timestamps", formatTimestamps(report.FalseNegativeTimestamps),
		"excluded_covering_fn_count", len(excludedCoveringFN),
	)

	// Log per-index hint breakdown: which indexes contributed hints and which didn't.
	indexesWithHints, indexesWithoutHints := hintBreakdownByIndex(hintRanges, report.OverlappingIndexIDs)
	level.Warn(s.logger).Log(
		"msg", "correctness failure hint breakdown",
		"hint_query", report.HintQuery,
		"indexes_with_hints", strings.Join(indexesWithHints, ","),
		"indexes_without_hints", strings.Join(indexesWithoutHints, ","),
	)

	// Log full metadata for each overlapping index.
	for _, m := range overlapping {
		level.Warn(s.logger).Log(
			"msg", "correctness failure overlapping index",
			"hint_query", report.HintQuery,
			"index_id", m.ID(),
			"min_log_ts", m.MinLogTs,
			"max_log_ts", m.MaxLogTs,
			"min_rec_ts", m.MinRecordTs,
			"max_rec_ts", m.MaxRecordTs,
			"size_bytes", m.SizeBytes,
			"created_at", m.CreatedAt,
			"compacted_from", strings.Join(m.CompactedFrom, ","),
		)
	}

	for _, m := range excludedCoveringFN {
		level.Warn(s.logger).Log(
			"msg", "correctness failure FN covered by ingester-window index",
			"hint_query", report.HintQuery,
			"index_id", m.ID(),
			"min_log_ts", m.MinLogTs,
			"max_log_ts", m.MaxLogTs,
			"min_rec_ts", m.MinRecordTs,
			"max_rec_ts", m.MaxRecordTs,
			"created_at", m.CreatedAt,
			"compacted_from", strings.Join(m.CompactedFrom, ","),
		)
	}

	s.metrics.falseNegativesTotal.Add(float64(vr.FalseNegatives))
	s.metrics.falsePositivesTotal.Add(float64(vr.FalsePositives))

	return report, nil
}

// excludedIndexesCoveringFNs returns ingester-window-excluded indexes whose log
// span covers a false-negative timestamp. Metadata only — does not probe terms.
func (s *Service) excludedIndexesCoveringFNs(fnTimestamps []time.Time, rangeStart, rangeEnd time.Time) []store.Meta {
	if len(fnTimestamps) == 0 {
		return nil
	}
	var out []store.Meta
	for _, m := range s.indexStore.IndexesExcludedByIngesterWindow(rangeStart, rangeEnd) {
		if metaCoversAnyTimestamp(m, fnTimestamps) {
			out = append(out, m)
		}
	}
	return out
}

func metaCoversAnyTimestamp(m store.Meta, timestamps []time.Time) bool {
	for _, ts := range timestamps {
		if !ts.Before(m.MinLogTs) && !ts.After(m.MaxLogTs) {
			return true
		}
	}
	return false
}

func (s *Service) pickRandomRange(now time.Time) (time.Time, time.Time, string) {
	eligibleEnd := now.Add(-s.cfg.QueryIngestersWithin)
	// earliest = max(startedAt, now-maxLookback): started_at is the farthest
	// allowed lookback, but never sample beyond max_lookback (retention safety).
	earliest := s.startedAt
	if lookbackStart := now.Add(-s.cfg.MaxLookback); lookbackStart.After(earliest) {
		earliest = lookbackStart
	}
	if !eligibleEnd.After(earliest) {
		return time.Time{}, time.Time{}, skipIngesterWindow
	}

	rangeDuration := s.cfg.QueryRangeMin
	if s.cfg.QueryRangeMax > s.cfg.QueryRangeMin {
		delta := s.cfg.QueryRangeMax - s.cfg.QueryRangeMin
		rangeDuration += time.Duration(s.randInt63n(int64(delta) + 1))
	}

	maxStart := eligibleEnd.Add(-rangeDuration)
	if maxStart.Before(earliest) {
		return time.Time{}, time.Time{}, skipIngesterWindow
	}

	start := earliest
	if maxStart.After(earliest) {
		randomOffset := time.Duration(s.randInt63n(int64(maxStart.Sub(earliest)) + 1))
		start = earliest.Add(randomOffset)
	}
	end := start.Add(rangeDuration)
	return start, end, ""
}

func (s *Service) pickRandomLabelValue(ctx context.Context, start, end time.Time) (string, string, error) {
	labels, _, err := s.fetchLabels(ctx, start, end)
	if err != nil {
		return "", "", err
	}

	labels = filterQueryableLabels(labels)
	if len(labels) == 0 {
		return "", "", skipErr(skipNoLabels)
	}

	s.shuffleStrings(labels)
	for _, label := range labels {
		values, _, valuesErr := s.fetchLabelValues(ctx, label, start, end)
		if valuesErr != nil {
			return "", "", valuesErr
		}
		if len(values) == 0 {
			continue
		}
		value := values[s.randIntn(len(values))]
		return label, value, nil
	}

	return "", "", skipErr(skipNoLabelValues)
}

func (s *Service) fetchLabels(ctx context.Context, start, end time.Time) ([]string, string, error) {
	var lastErr error
	for _, idx := range s.shuffledClientIndexes() {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}

		c := s.lokiClients[idx]
		resp, err := c.client.ListLabelNames(true, start, end)
		if err != nil {
			lastErr = fmt.Errorf("endpoint %s list labels: %w", c.endpoint, err)
			continue
		}
		if resp == nil {
			lastErr = fmt.Errorf("endpoint %s returned nil label response", c.endpoint)
			continue
		}
		return resp.Data, c.endpoint, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no Loki clients configured")
	}
	return nil, "", lastErr
}

func (s *Service) fetchLabelValues(ctx context.Context, label string, start, end time.Time) ([]string, string, error) {
	var lastErr error
	for _, idx := range s.shuffledClientIndexes() {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}

		c := s.lokiClients[idx]
		resp, err := c.client.ListLabelValues(label, true, start, end)
		if err != nil {
			lastErr = fmt.Errorf("endpoint %s list label values %q: %w", c.endpoint, label, err)
			continue
		}
		if resp == nil {
			lastErr = fmt.Errorf("endpoint %s returned nil label response", c.endpoint)
			continue
		}
		return resp.Data, c.endpoint, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no Loki clients configured")
	}
	return nil, "", lastErr
}

// queryEntry is one Loki result plus the stream labels it arrived with.
// Stream labels are dropped from logproto.Entry; we keep them so
// pickJSONField can skip keys | json will not overwrite (name → name_extracted).
type queryEntry struct {
	logproto.Entry
	StreamLabels loghttp.LabelSet
}

func protoEntries(logs []queryEntry) []logproto.Entry {
	out := make([]logproto.Entry, len(logs))
	for i := range logs {
		out[i] = logs[i].Entry
	}
	return out
}

func (s *Service) queryRange(
	ctx context.Context,
	query string,
	start, end time.Time,
	limit int,
) ([]queryEntry, string, error) {
	if end.Before(start) {
		end = start
	}
	var lastErr error
	for _, idx := range s.shuffledClientIndexes() {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}

		c := s.lokiClients[idx]
		resp, err := c.client.QueryRange(query, limit, start, end, logproto.BACKWARD, 0, 0, true)
		if err != nil {
			lastErr = fmt.Errorf("endpoint %s query range: %w", c.endpoint, err)
			continue
		}
		entries, err := queryResponseEntries(resp)
		if err != nil {
			lastErr = fmt.Errorf("endpoint %s decode query range response: %w", c.endpoint, err)
			continue
		}
		return entries, c.endpoint, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no Loki clients configured")
	}
	return nil, "", lastErr
}

func (s *Service) recordSkipped(reason string) {
	s.metrics.cycleSkippedTotal.WithLabelValues(reason).Inc()
}

func (s *Service) shuffledClientIndexes() []int {
	indices := make([]int, len(s.lokiClients))
	for i := range indices {
		indices[i] = i
	}
	s.randMu.Lock()
	s.rand.Shuffle(len(indices), func(i, j int) {
		indices[i], indices[j] = indices[j], indices[i]
	})
	s.randMu.Unlock()
	return indices
}

func queryResponseEntries(resp *loghttp.QueryResponse) ([]queryEntry, error) {
	if resp == nil {
		return nil, fmt.Errorf("nil query response")
	}
	if resp.Status != string(loghttp.QueryStatusSuccess) {
		return nil, fmt.Errorf("loki query status %q", resp.Status)
	}

	if resp.Data.Result == nil {
		return []queryEntry{}, nil
	}

	streams, ok := resp.Data.Result.(loghttp.Streams)
	if !ok {
		return nil, fmt.Errorf("unexpected Loki query result type: %T", resp.Data.Result)
	}

	entries := make([]queryEntry, 0, len(streams))
	for _, stream := range streams {
		for _, entry := range stream.Entries {
			entries = append(entries, queryEntry{
				Entry: logproto.Entry{
					Timestamp:          entry.Timestamp.UTC(),
					Line:               entry.Line,
					StructuredMetadata: logproto.FromLabelsToLabelAdapters(entry.StructuredMetadata),
				},
				StreamLabels: stream.Labels,
			})
		}
	}
	return entries, nil
}

func newLokiEndpointClients(cfg Config) []lokiEndpointClient {
	minBackoff := int(cfg.ErrorBackoffMin / time.Second)
	maxBackoff := int(cfg.ErrorBackoffMax / time.Second)
	if minBackoff < 1 {
		minBackoff = 1
	}
	if maxBackoff < minBackoff {
		maxBackoff = minBackoff
	}

	result := make([]lokiEndpointClient, 0, len(cfg.LokiQueryEndpoints))
	for _, endpoint := range cfg.LokiQueryEndpoints {
		client := &lokiclient.DefaultClient{
			Address: endpoint,
			OrgID:   cfg.TenantID,
			Retries: 0,
			BackoffConfig: lokiclient.BackoffConfig{
				MinBackoff: minBackoff,
				MaxBackoff: maxBackoff,
			},
			Tripperware: func(next http.RoundTripper) http.RoundTripper {
				return &timeoutRoundTripper{
					next:    next,
					timeout: cfg.RequestTimeout,
				}
			},
		}
		result = append(result, lokiEndpointClient{
			endpoint: endpoint,
			client:   client,
		})
	}
	return result
}

type timeoutRoundTripper struct {
	next    http.RoundTripper
	timeout time.Duration
}

func (t *timeoutRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Don't return early when timeout is off: every request still needs the
	// categorize-labels header below. Use a no-op cancel so the rest of the
	// function can always call cancel safely.
	cancel := func() {}
	ctx := req.Context()
	if t.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, t.timeout)
	}

	// categorize-labels returns SM in the entry's third object; without it Loki
	// folds SM into stream labels and StructuredMetadata stays empty.
	req = req.Clone(ctx)
	req.Header.Set(httpreq.LokiEncodingFlagsHeader, string(httpreq.FlagCategorizeLabels))

	resp, err := t.next.RoundTrip(req)
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = &cancelOnCloseReadCloser{
		ReadCloser: resp.Body,
		cancel:     cancel,
	}
	return resp, nil
}

type cancelOnCloseReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *cancelOnCloseReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.cancel()
	return err
}

func (s *Service) randInt63n(limit int64) int64 {
	if limit <= 0 {
		return 0
	}
	s.randMu.Lock()
	defer s.randMu.Unlock()
	return s.rand.Int63n(limit)
}

func (s *Service) randIntn(limit int) int {
	if limit <= 0 {
		return 0
	}
	s.randMu.Lock()
	defer s.randMu.Unlock()
	return s.rand.Intn(limit)
}

func (s *Service) shuffleStrings(values []string) {
	s.randMu.Lock()
	defer s.randMu.Unlock()
	s.rand.Shuffle(len(values), func(i, j int) {
		values[i], values[j] = values[j], values[i]
	})
}

// usableHintValue reports whether a value is long enough for n-gram lookup.
func usableHintValue(value string, ngramLength int) bool {
	return len(value) >= ngramLength
}

func filterQueryableLabels(labels []string) []string {
	if len(labels) == 0 {
		return nil
	}
	out := make([]string, 0, len(labels))
	seen := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if !hintprovider.UsableLabelName(label) {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	return out
}

func buildSelector(label, value string) string {
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	return fmt.Sprintf(`{%s="%s"}`, label, escaped)
}

type hintQueryType string

const (
	hintQueryTypeLineFilter      hintQueryType = "line_filter"
	hintQueryTypeLabelFilter     hintQueryType = "label_filter"
	hintQueryTypeSMLabelFilter   hintQueryType = "sm_label_filter"
	hintQueryTypeJSONLabelFilter hintQueryType = "json_label_filter"
)

// hintQuery is a validated query used to fetch Loki results and Logline hints.
// Construct only via newHintQuery, which guarantees expr is the parse of query.
type hintQuery struct {
	needle    string
	queryType hintQueryType
	query     string
	expr      syntax.Expr
}

func newHintQuery(qt hintQueryType, needle, query string) (hintQuery, error) {
	expr, err := syntax.ParseExpr(query)
	if err != nil {
		return hintQuery{}, err
	}
	return hintQuery{needle: needle, queryType: qt, query: query, expr: expr}, nil
}

func buildLineFilterHintQuery(selector, needle string) string {
	return fmt.Sprintf("%s |= %s", selector, strconv.Quote(needle))
}

func buildLabelFilterHintQuery(selector, label, value string) string {
	return fmt.Sprintf("%s | %s=%s", selector, label, strconv.Quote(value))
}

func buildJSONLabelFilterHintQuery(selector, label, value string) string {
	return fmt.Sprintf("%s | json | %s=%s", selector, label, strconv.Quote(value))
}

// pickHintQuery chooses among SM, stream-label, post-parser JSON, and line
// filters with equal weight. The chosen path is tried first; on failure
// (ineligible value / parse error / no JSON field) we fall back to a
// line-filter needle. Loki matches the label/SM/JSON key; Logline only looks
// up value n-grams.
func (s *Service) pickHintQuery(
	selector, label, value string,
	logs []queryEntry,
) (hintQuery, bool) {
	switch s.randIntn(4) {
	case 0:
		if hq, ok := s.trySMLabelFilterHintQuery(selector, logs); ok {
			return hq, true
		}
	case 1:
		if hq, ok := s.tryLabelFilterHintQuery(selector, label, value); ok {
			return hq, true
		}
	case 2:
		if hq, ok := s.tryJSONLabelFilterHintQuery(selector, logs); ok {
			return hq, true
		}
	}
	return s.tryLineFilterHintQuery(selector, logs)
}

// trySMLabelFilterHintQuery builds a label-filter hint from the first usable SM
// pair on sampled logs. Length eligibility is checked in pickStructuredMetadata.
// Failure falls back to line (not stream label).
func (s *Service) trySMLabelFilterHintQuery(selector string, logs []queryEntry) (hintQuery, bool) {
	smLabel, smValue, ok := pickStructuredMetadata(protoEntries(logs), s.cfg.NgramLength)
	if !ok {
		return hintQuery{}, false
	}
	return s.labelFilterHintQuery(hintQueryTypeSMLabelFilter, selector, smLabel, smValue)
}

// tryLabelFilterHintQuery builds a stream label-filter hint when the value is long
// enough for n-gram lookup.
func (s *Service) tryLabelFilterHintQuery(selector, label, value string) (hintQuery, bool) {
	if !usableHintValue(value, s.cfg.NgramLength) {
		return hintQuery{}, false
	}
	return s.labelFilterHintQuery(hintQueryTypeLabelFilter, selector, label, value)
}

// tryJSONLabelFilterHintQuery builds `{sel} | json | field="value"` from a
// top-level JSON string on a sampled line. Failure falls back to line.
func (s *Service) tryJSONLabelFilterHintQuery(selector string, logs []queryEntry) (hintQuery, bool) {
	field, value, ok := pickJSONField(logs, s.cfg.NgramLength)
	if !ok {
		return hintQuery{}, false
	}
	hq, err := newHintQuery(hintQueryTypeJSONLabelFilter, value, buildJSONLabelFilterHintQuery(selector, field, value))
	if err != nil {
		level.Warn(s.logger).Log(
			"msg", "failed to build json-label-filter hint query; falling back to line filter",
			"label", field,
			"err", err,
		)
		return hintQuery{}, false
	}
	return hq, true
}

func (s *Service) tryLineFilterHintQuery(selector string, logs []queryEntry) (hintQuery, bool) {
	needle, _, ok := pickNeedle(protoEntries(logs), s.cfg.NgramLength)
	if !ok {
		return hintQuery{}, false
	}
	hq, err := newHintQuery(hintQueryTypeLineFilter, needle, buildLineFilterHintQuery(selector, needle))
	if err != nil {
		level.Warn(s.logger).Log(
			"msg", "failed to build line-filter hint query",
			"needle", needle,
			"err", err,
		)
		return hintQuery{}, false
	}
	return hq, true
}

// labelFilterHintQuery builds and parses a label-filter hint query. Callers must
// supply a value that already passes usableHintValue. Warns and returns false
// only on parse failure.
func (s *Service) labelFilterHintQuery(qt hintQueryType, selector, label, value string) (hintQuery, bool) {
	hq, err := newHintQuery(qt, value, buildLabelFilterHintQuery(selector, label, value))
	if err != nil {
		level.Warn(s.logger).Log(
			"msg", "failed to build label-filter hint query; falling back to line filter",
			"label", label,
			"hint_query_type", qt,
			"err", err,
		)
		return hintQuery{}, false
	}
	return hq, true
}

// pickJSONField returns the first top-level JSON string field on a sampled
// line that is eligible as a `| json | field="value"` needle. Ineligible
// keys/values are skipped; if none remain, the JSON path fails and
// pickHintQuery falls back to line filter.
func pickJSONField(entries []queryEntry, ngramLength int) (string, string, bool) {
	for _, entry := range entries {
		reserved := reservedParserKeys(entry.StreamLabels, entry.StructuredMetadata)
		if field, value, ok := jsonFieldFromLine(entry.Line, ngramLength, reserved); ok {
			return field, value, true
		}
	}
	return "", "", false
}

// reservedParserKeys is the set of names | json will not overwrite. Loki
// extracts those as name_extracted, so | name="..." still filters the
// stream/SM label.
func reservedParserKeys(stream loghttp.LabelSet, sm []logproto.LabelAdapter) map[string]struct{} {
	out := make(map[string]struct{}, len(stream)+len(sm))
	for name := range stream {
		if name != "" {
			out[name] = struct{}{}
		}
	}
	for _, m := range sm {
		if m.Name != "" {
			out[m.Name] = struct{}{}
		}
	}
	return out
}

func jsonFieldFromLine(line string, ngramLength int, reserved map[string]struct{}) (string, string, bool) {
	dec, ok := jsonObjectDecoder(line)
	if !ok {
		return "", "", false
	}

	seen := make(map[string]struct{})
	for dec.More() {
		key, raw, ok := decodeObjectEntry(dec)
		if !ok {
			return "", "", false
		}
		if _, exists := seen[key]; exists {
			continue // Loki | json keeps the first occurrence
		}
		seen[key] = struct{}{}

		if !validJSONKey(key, reserved) {
			continue
		}
		value, ok := raw.(string) // JSON string, not object/array/number/bool/null
		if !ok || !validJSONValue(value, line, ngramLength) {
			continue
		}
		return key, value, true
	}
	return "", "", false
}

// jsonObjectDecoder starts a JSON decoder on line and consumes the opening
// `{`. False if the line is not a JSON object (array, string, invalid, …).
func jsonObjectDecoder(line string) (*json.Decoder, bool) {
	dec := json.NewDecoder(strings.NewReader(line))
	tok, err := dec.Token()
	if err != nil {
		return nil, false
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, false
	}
	return dec, true
}

// decodeObjectEntry reads the next `"key": value` pair. False if the JSON is
// truncated or malformed (the same cases Unmarshal would reject).
func decodeObjectEntry(dec *json.Decoder) (string, any, bool) {
	keyTok, err := dec.Token()
	if err != nil {
		return "", nil, false
	}
	key, ok := keyTok.(string)
	if !ok {
		return "", nil, false
	}
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return "", nil, false
	}
	return key, raw, true
}

// validJSONKey reports whether key can be used unquoted in `| json | key=...`.
// Empty, `__*`, and hyphenated names are skipped; names that already exist as
// stream/SM labels are skipped (`| json` would extract those as name_extracted).
func validJSONKey(key string, reserved map[string]struct{}) bool {
	if !hintprovider.UsableLabelName(key) || !jsonLabelNameRE.MatchString(key) {
		return false
	}
	_, taken := reserved[key]
	return !taken
}

// validJSONValue reports whether value can be used as a `| json | field="value"`
// needle:
//  1. long enough for n-grams
//  2. IsVerbatimLineLiteral: no `"`, `\`, or control bytes (unescape-unsafe)
//  3. strings.Contains: unescaped value is a contiguous substring of the line
func validJSONValue(value, line string, ngramLength int) bool {
	return usableHintValue(value, ngramLength) &&
		hintprovider.IsVerbatimLineLiteral(value) &&
		strings.Contains(line, value)
}

// pickStructuredMetadata returns the first SM key/value from sampled entries
// eligible for a label-filter hint. Filtering during the scan means an
// ineligible pair skips to the next SM key; if none are eligible, the SM path
// fails and pickHintQuery falls back to line filter.
func pickStructuredMetadata(entries []logproto.Entry, ngramLength int) (string, string, bool) {
	for _, entry := range entries {
		for _, m := range entry.StructuredMetadata {
			if !hintprovider.UsableLabelName(m.Name) || !usableHintValue(m.Value, ngramLength) {
				continue
			}
			return m.Name, m.Value, true
		}
	}
	return "", "", false
}

func pickNeedle(entries []logproto.Entry, ngramLength int) (string, logproto.Entry, bool) {
	bestNeedle := ""
	bestEntry := logproto.Entry{}
	bestScore := -1.0

	fallbackNeedle := ""
	fallbackEntry := logproto.Entry{}
	fallbackScore := -1.0

	for _, entry := range entries {
		tokens := tokenCandidateRE.FindAllString(entry.Line, -1)
		for _, token := range tokens {
			if !usableHintValue(token, ngramLength) {
				continue
			}

			entropy := shannonEntropy(token)
			score := entropy * float64(len(token))

			if score > fallbackScore {
				fallbackScore = score
				fallbackNeedle = token
				fallbackEntry = entry
			}

			if entropy >= 2.5 && score > bestScore {
				bestScore = score
				bestNeedle = token
				bestEntry = entry
			}
		}
	}

	if bestNeedle != "" {
		return bestNeedle, bestEntry, true
	}
	if fallbackNeedle != "" {
		return fallbackNeedle, fallbackEntry, true
	}
	return "", logproto.Entry{}, false
}

func shannonEntropy(value string) float64 {
	if value == "" {
		return 0
	}
	var counts [256]int
	for i := 0; i < len(value); i++ {
		counts[value[i]]++
	}

	length := float64(len(value))
	entropy := 0.0
	for _, count := range counts {
		if count == 0 {
			continue
		}
		p := float64(count) / length
		entropy -= p * math.Log2(p)
	}
	return entropy
}

type cycleSkipError string

func (e cycleSkipError) Error() string {
	return string(e)
}

func skipErr(reason string) error {
	return cycleSkipError(reason)
}

func skipReasonFromErr(err error) (string, bool) {
	var skip cycleSkipError
	if errors.As(err, &skip) {
		return skip.Error(), true
	}
	return "", false
}

// formatTimestamps formats a slice of timestamps as a comma-separated RFC3339Nano string.
func formatTimestamps(timestamps []time.Time) string {
	parts := make([]string, len(timestamps))
	for i, ts := range timestamps {
		parts[i] = ts.Format(time.RFC3339Nano)
	}
	return strings.Join(parts, ",")
}

// hintBreakdownByIndex extracts index IDs from hint range sources and partitions
// overlapping index IDs into those that contributed hints and those that didn't.
func hintBreakdownByIndex(hintRanges []hintprovider.HintTimeRange, overlappingIDs []string) (withHints, withoutHints []string) {
	contributing := make(map[string]struct{})
	for _, r := range hintRanges {
		// Source format: "index=<id>,doc=<n>,min=<ts>,max=<ts>" or merged with ";"
		for _, segment := range strings.Split(r.Source, ";") {
			if idx := strings.TrimPrefix(segment, "index="); idx != segment {
				if comma := strings.Index(idx, ","); comma >= 0 {
					idx = idx[:comma]
				}
				contributing[idx] = struct{}{}
			}
		}
	}

	for _, id := range overlappingIDs {
		if _, ok := contributing[id]; ok {
			withHints = append(withHints, id)
		} else {
			withoutHints = append(withoutHints, id)
		}
	}
	return withHints, withoutHints
}
