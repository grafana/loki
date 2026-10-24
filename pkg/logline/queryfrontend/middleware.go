package queryfrontend

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/zeebo/xxh3"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/loghttp"
	logline_query_limits "github.com/grafana/loki/v3/pkg/logline/queryfrontend/limits"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	logqlstats "github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	util_log "github.com/grafana/loki/v3/pkg/util/log"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logline/verification"
)

// Two-layer logline index middleware:
//
// 1. Prefetch MW (above SplitByInterval): parses query, kicks off async
//    logline index lookup, stores results + ingester cutoff in context.
//
// 2. Filter MW (below SplitByInterval, below cache): for each interval
//    sub-request, skips empty intervals or attaches the clipped hint ranges
//    to one downstream request. Queriers consume those ranges.
//
// SplitByInterval handles direction ordering, LIMIT early-exit, and
// interval-level caching. Filter does not change request bounds.
//
//	Prefetch MW → [Loki: limits → SplitByInterval → cache → shard → ...] → Filter MW → queriers

// LoglineIndexHeader enables logline index narrowing for a query.
// Accepted values:
//   - "dry_run" — opt in to logline dry-run verification
//   - "live" — override dry-run config and perform live query acceleration
//   - "off" — force-disable logline handling for this request
const LoglineIndexHeader = "X-Logline-Index"

// LoglineIndexHeaderValue is the string enum for X-Logline-Index values.
type LoglineIndexHeaderValue string

const (
	// LoglineIndexDryRun enables dry-run verification for a single request.
	LoglineIndexDryRun LoglineIndexHeaderValue = "dry_run"
	// LoglineIndexLive enables live query acceleration for a single request.
	LoglineIndexLive LoglineIndexHeaderValue = "live"
	// LoglineIndexOff force-disables logline handling for a single request,
	// overriding all other config and tenant settings.
	LoglineIndexOff LoglineIndexHeaderValue = "off"
)

const LoglineSkipCacheHeader = "X-Logline-Skip-Cache"

type hintPrefetchKeyType struct{}

// hintPrefetchResult holds pre-computed logline index lookup results, shared
// between the prefetch and filter middleware layers via context.
type hintPrefetchResult struct {
	ranges         []hintprovider.HintTimeRange // normalized, sorted by start
	err            error
	stats          *hintprovider.QueryStats
	queryBytes     uint64
	queryStart     time.Time     // original query start
	queryEnd       time.Time     // original query end
	ingesterCutoff time.Time     // data after this is in ingester window only
	minDate        time.Time     // data before this has no index coverage; zero if unknown
	lookupDuration time.Duration // wall time of the lookup, set before done is closed
	done           chan struct{} // closed when lookup completes
	wait           waitTracker   // time sub-requests spent waiting for done
}

// waitForLookup blocks until the lookup is done, ctx ends or timeout passes,
// and reports whether the timeout passed.
func (r *hintPrefetchResult) waitForLookup(ctx context.Context, timeout time.Duration) (timedOut bool, err error) {
	select {
	case <-r.done:
		return false, nil
	default:
	}

	r.wait.begin(time.Now())
	defer func() { r.wait.end(time.Now()) }()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-r.done:
		return false, nil
	case <-ctx.Done():
		return false, ctx.Err()
	case <-timer.C:
		return true, nil
	}
}

// waitTracker measures the wall time during which at least one sub-request
// waits for the hint lookup. Sub-requests wait in parallel and in waves, so
// this is the union of their wait intervals, not the sum.
type waitTracker struct {
	mu      sync.Mutex
	waiters int
	start   time.Time
	total   time.Duration
}

func (w *waitTracker) begin(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.waiters == 0 {
		w.start = now
	}
	w.waiters++
}

func (w *waitTracker) end(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.waiters--
	if w.waiters == 0 {
		w.total += now.Sub(w.start)
	}
}

// duration returns the wait time so far, including a wait still in progress.
func (w *waitTracker) duration(now time.Time) time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.waiters > 0 {
		return w.total + now.Sub(w.start)
	}
	return w.total
}

type dryRunLookupResult struct {
	ranges   []hintprovider.HintTimeRange
	err      error
	stats    *hintprovider.QueryStats
	duration time.Duration
	done     chan struct{}
}

// Values of the logline hintStatus statistic.
const (
	hintStatusOK             = "ok"
	hintStatusUnsupported    = "unsupported"
	hintStatusError          = "error"
	hintStatusCanceled       = "canceled"
	hintStatusIncomplete     = "incomplete"
	hintStatusIngesterWindow = "ingester_window"
)

// status reports how the lookup ended. It must be called after the query
// pipeline returns, so a lookup still running is reported as incomplete.
func (r *hintPrefetchResult) status() string {
	select {
	case <-r.done:
	default:
		return hintStatusIncomplete
	}
	switch {
	case r.err == nil && !r.ingesterCutoff.After(r.queryStart):
		return hintStatusIngesterWindow
	case r.err == nil:
		return hintStatusOK
	case errors.Is(r.err, hintprovider.ErrUnsupported):
		return hintStatusUnsupported
	case isCancel(r.err):
		return hintStatusCanceled
	default:
		return hintStatusError
	}
}

// Values of the logline outcome statistic, which tells whether the index
// skipped data or why it didn't.
const (
	outcomeDataSkipped          = "data_skipped"
	outcomeTinyDataSkipped      = "tiny_data_skipped"
	outcomeSkipRatioUnavailable = "skip_ratio_unavailable"
	outcomeTimeRangeNotIndexed  = "time_range_not_indexed"
	outcomeRecentDataOnly       = "recent_data_only"
	outcomeUnsupportedQuery     = "unsupported_query"
	outcomeQueryTooSmall        = "query_too_small"
	outcomeSizeEstimateFailed   = "size_estimate_failed"
	outcomeLookupFailed         = "lookup_failed"
	outcomeLookupTimeout        = "lookup_timeout"
	outcomeAnsweredBeforeLookup = "answered_before_lookup"
	outcomeCanceled             = "canceled"
)

// tinyDataSkippedRatio is the chunk filter ratio below which the index is
// reported as having skipped too little data to help.
const tinyDataSkippedRatio = 0.25

// loglineOutcome derives the outcome of a query that went through the hint
// lookup from its final logline statistics. lookupTimedOut reports whether a
// sub-request stopped waiting for the lookup.
func loglineOutcome(l *logqlstats.Logline, lookupTimedOut bool) string {
	switch l.HintStatus {
	case hintStatusUnsupported:
		return outcomeUnsupportedQuery
	case hintStatusError:
		return outcomeLookupFailed
	case hintStatusIncomplete:
		if !lookupTimedOut {
			return outcomeAnsweredBeforeLookup
		}
		return outcomeLookupTimeout
	case hintStatusCanceled:
		return outcomeCanceled
	case hintStatusIngesterWindow:
		return outcomeRecentDataOnly
	}
	switch {
	case l.PlannedChunks <= 0:
		return outcomeSkipRatioUnavailable
	case l.SkippedRequests+l.NarrowedRequests == 0:
		return outcomeTimeRangeNotIndexed
	case l.ChunkFilterRatio < tinyDataSkippedRatio:
		return outcomeTinyDataSkipped
	default:
		return outcomeDataSkipped
	}
}

// attachLoglineStats sets the lookup-level logline statistics and the outcome
// on the final query response. The stats collector aliases the response's
// statistics, so these values reach the frontend metrics.go line.
func attachLoglineStats(resp queryrangebase.Response, result *hintPrefetchResult) {
	lokiResp, ok := resp.(*queryrange.LokiResponse)
	if !ok || result == nil {
		return
	}
	l := lokiResp.Statistics.LoglineStats()
	l.HintStatus = result.status()
	l.HintWaitTime = result.wait.duration(time.Now()).Nanoseconds()
	if l.HintStatus != hintStatusIncomplete {
		l.HintLookupTime = result.lookupDuration.Nanoseconds()
		l.HintRanges = int64(len(result.ranges))
		l.HintRangesDuration = indexedHintDuration(result.ranges, result.queryStart, result.queryEnd).Nanoseconds()
		if result.stats != nil {
			snap := result.stats.Snapshot()
			l.ObjectRequests = snap.ObjectStorageRequests
			l.IOBytes = snap.TotalIOBytes
			l.IOWait = snap.TotalIOWait.Nanoseconds()
		}
	}
	setLoglineChunkStats(&lokiResp.Statistics)
	l.Outcome = loglineOutcome(l, result.stats.Snapshot().PrefetchTimeouts > 0)
}

// setLoglineChunkStats copies the shard planning chunk counts and their ratio
// into the logline statistics.
func setLoglineChunkStats(s *logqlstats.Result) {
	l := s.LoglineStats()
	l.PlannedChunks = s.Index.ShardPlannedChunks
	l.ScannedChunks = s.Index.ShardPlannedChunksScanned
	l.ChunkFilterRatio, _ = s.Index.ChunkFilterRatio()
}

// indexedHintDuration sums the hint ranges clipped to [start, end], leaving out
// the pre-min-date passthrough range, which has no index coverage.
func indexedHintDuration(ranges []hintprovider.HintTimeRange, start, end time.Time) time.Duration {
	indexed := make([]hintprovider.HintTimeRange, 0, len(ranges))
	for _, r := range ranges {
		if !r.IsPassthrough() {
			indexed = append(indexed, r)
		}
	}
	return hintCoverageDuration(indexed, start, end)
}

func appendHintStats(logValues []any, stats *hintprovider.QueryStats) []any {
	if stats == nil {
		return logValues
	}
	snap := stats.Snapshot()
	return append(logValues,
		"object_requests", snap.ObjectStorageRequests,
		"header_reads", snap.HeaderReads,
		"metadata_reads", snap.MetadataReads,
		"term_dict_reads", snap.TermDictReads,
		"bitmap_reads", snap.BitmapReads,
		"header_cache_misses", snap.HeaderCacheMisses,
		"metadata_cache_misses", snap.MetadataCacheMisses,
		"io_wait", snap.TotalIOWait,
		"io_bytes", snap.TotalIOBytes,
		"peak_concurrency", snap.PeakConcurrency,
		"effective_concurrency", snap.EffectiveConcurrency,
		"index_queries_total", snap.IndexQueriesTotal,
		"index_queries_term_miss", snap.IndexQueriesTermMiss,
		"index_queries_empty_and", snap.IndexQueriesEmptyAnd,
		"index_queries_positive", snap.IndexQueriesPositive,
		"term_batches_processed_total", snap.TotalTermBatchesProcessed,
		"hint_cache_result", snap.HintCacheResult,
		"hint_cache_days_fetched", snap.HintCacheDaysFetched,
		"hint_cache_days_hit", snap.HintCacheDaysHit,
	)
}

func withHintPrefetch(ctx context.Context, r *hintPrefetchResult) context.Context {
	return context.WithValue(ctx, hintPrefetchKeyType{}, r)
}

func hintPrefetchFromContext(ctx context.Context) *hintPrefetchResult {
	v, _ := ctx.Value(hintPrefetchKeyType{}).(*hintPrefetchResult)
	return v
}

func intervalDuration(start, end time.Time) time.Duration {
	if !end.After(start) {
		return 0
	}
	return end.Sub(start)
}

// NewLoglinePrefetchMiddleware starts an async logline index lookup before
// SplitByInterval generates intervals.
func NewLoglinePrefetchMiddleware(
	inner *hintprovider.LoglineHintProvider,
	hintCache cache.Cache,
	cfg Config,
	limits logline_query_limits.Limits,
	metrics *Metrics,
	logger log.Logger,
	reg prometheus.Registerer,
) queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		hp := hintprovider.QueryHintProvider(&frontendHintAdapter{inner: inner, next: next})
		hp = hintprovider.NewCachingHintProvider(hp, hintCache, reg)
		return newLoglinePrefetchHandler(next, hp, cfg, limits, metrics, logger)
	})
}

func newLoglinePrefetchHandler(
	next queryrangebase.Handler,
	hp hintprovider.QueryHintProvider,
	cfg Config,
	limits logline_query_limits.Limits,
	metrics *Metrics,
	logger log.Logger,
) queryrangebase.Handler {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &loglinePrefetchHandler{
		next:                 next,
		hintProvider:         hp,
		defaultMode:          modeFromDryRun(cfg.DryRun),
		requireOptInHeader:   cfg.RequireOptInHeader,
		ngramLength:          cfg.NgramLength,
		hintTimeout:          cfg.HintTimeout,
		queryIngestersWithin: cfg.QueryIngestersWithin,
		limits:               limits,
		metrics:              metrics,
		logger:               logger,
	}
}

type loglinePrefetchHandler struct {
	next                 queryrangebase.Handler
	hintProvider         hintprovider.QueryHintProvider
	defaultMode          Mode
	requireOptInHeader   bool
	ngramLength          int
	hintTimeout          time.Duration
	dryRunInflight       atomic.Int32
	queryIngestersWithin time.Duration
	limits               logline_query_limits.Limits
	metrics              *Metrics
	logger               log.Logger
}

// Mode controls how logline filtering behaves for a tenant.
type Mode int

const (
	ModeUnset Mode = iota
	ModeOff
	ModeDryRun
	ModeLive
)

// modeFromLimit maps the limits_config value to a Mode. Loki validates the
// value when it loads the limits, so anything else is treated as unset.
func modeFromLimit(s string) Mode {
	switch s {
	case "off":
		return ModeOff
	case "dry_run":
		return ModeDryRun
	case "live":
		return ModeLive
	default:
		return ModeUnset
	}
}

func modeFromDryRun(dryRun bool) Mode {
	if dryRun {
		return ModeDryRun
	}
	return ModeLive
}

func resolveMode(header string, tenantMode, defaultMode Mode, requireOptInHeader bool) (Mode, bool) {
	switch LoglineIndexHeaderValue(header) {
	case LoglineIndexOff:
		return ModeOff, true
	case LoglineIndexLive:
		return ModeLive, false
	case LoglineIndexDryRun:
		return ModeDryRun, false
	}

	mode := defaultMode
	if tenantMode != ModeUnset {
		mode = tenantMode
	}

	if mode == ModeOff {
		return ModeOff, true
	}

	if tenantMode == ModeUnset && requireOptInHeader {
		return mode, true
	}

	return mode, false
}

func (h *loglinePrefetchHandler) getQueryBytes(ctx context.Context, expr syntax.Expr, from, through time.Time, stepMillis int64) (uint64, error) {
	matcherGroups, err := syntax.MatcherGroups(expr)
	if err != nil {
		return 0, err
	}
	// If there are zero matcher groups, query index stats for everything.
	if len(matcherGroups) == 0 {
		matcherGroups = append(matcherGroups, syntax.MatcherRange{})
	}

	ctx = queryrange.WithInternalRequest(ctx)

	var totalBytes uint64
	for _, group := range matcherGroups {
		adjustedFrom, adjustedThrough := metricLookupBounds(group, from, through, stepMillis)

		resp, err := h.next.Do(ctx, &logproto.IndexStatsRequest{
			From:     model.Time(adjustedFrom.UnixMilli()),
			Through:  model.Time(adjustedThrough.UnixMilli()),
			Matchers: syntax.MatchersString(group.Matchers),
		})
		if err != nil {
			return 0, err
		}

		casted, ok := resp.(*queryrange.IndexStatsResponse)
		if !ok {
			return 0, fmt.Errorf("expected *queryrange.IndexStatsResponse while querying index, got %T", resp)
		}

		if casted.Response != nil {
			totalBytes += casted.Response.Bytes
		}
	}

	return totalBytes, nil
}

// metricLookupBounds rounds onto the step, then widens by the range and offset.
// Prefetch runs before the splitter, which floors the start and rounds the end up.
// A zero interval or a zero step is only widened.
func metricLookupBounds(group syntax.MatcherRange, from, through time.Time, stepMillis int64) (time.Time, time.Time) {
	if group.Interval > 0 && stepMillis > 0 {
		from, through = alignMetricStep(from, through, stepMillis)
	}
	return group.Widen(from, through)
}

// alignMetricStep matches metricQuerySplitter.alignStartEnd: floor the start and
// round the end up to the next step.
func alignMetricStep(start, end time.Time, stepMillis int64) (time.Time, time.Time) {
	stepNs := stepMillis * int64(time.Millisecond)
	startNs := start.UnixNano()
	endNs := end.UnixNano()
	if mod := endNs % stepNs; mod != 0 {
		endNs += stepNs - mod
	}
	return time.Unix(0, startNs-startNs%stepNs).UTC(), time.Unix(0, endNs).UTC()
}

// dryRunMetricReport treats a positive sample as covered when any hint overlaps its lookback.
// The hint may not be the line that produced the sample. TotalEntries counts the positive
// steps whose lookback ended at or before the ingester cutoff. A miss records that whole lookback.
func dryRunMetricReport(expr syntax.Expr, promResp *queryrange.LokiPromResponse, hints []hintprovider.HintTimeRange, eligibleEnd time.Time) verification.Report {
	report := verification.Report{HintRanges: len(hints)}
	// For now, we don't check absent_over_time for correctness
	if isAbsentOverTimeAgg(expr) || promResp == nil || promResp.Response == nil {
		return report
	}
	group := metricMatcherGroup(expr)
	for _, stream := range promResp.Response.Data.Result {
		for _, sample := range stream.Samples {
			if sample.Value <= 0 {
				continue
			}
			step := time.UnixMilli(sample.TimestampMs)
			winStart, winEnd := group.Widen(step, step)
			if !eligibleEnd.IsZero() && winEnd.After(eligibleEnd) {
				continue
			}
			report.TotalEntries++
			if !rangesOverlap(hints, winStart, winEnd) {
				report.FalseNegativeRanges = append(report.FalseNegativeRanges, hintprovider.HintTimeRange{
					Start: winStart,
					End:   winEnd,
				})
				continue
			}
			report.CoveredEntries++
		}
	}
	report.FalseNegatives = report.TotalEntries - report.CoveredEntries
	return report
}

// metricMatcherGroup returns the single matcher group for a supported metric query.
func metricMatcherGroup(expr syntax.Expr) syntax.MatcherRange {
	groups, err := syntax.MatcherGroups(expr)
	if err != nil || len(groups) != 1 {
		return syntax.MatcherRange{}
	}
	return groups[0]
}

func isAbsentOverTimeAgg(expr syntax.Expr) bool {
	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		return isAbsentOverTimeAgg(e.Left)
	case *syntax.RangeAggregationExpr:
		return e.Operation == syntax.OpRangeTypeAbsent
	default:
		return false
	}
}

func verifyDryRunHints(hintRanges []hintprovider.HintTimeRange, resp *queryrange.LokiResponse, eligibleEnd time.Time) verification.Report {
	if resp == nil {
		return verification.Report{HintRanges: len(hintRanges)}
	}
	var timestamps []time.Time
	for _, stream := range resp.Data.Result {
		for _, entry := range stream.Entries {
			if !eligibleEnd.IsZero() && entry.Timestamp.After(eligibleEnd) {
				continue
			}
			timestamps = append(timestamps, entry.Timestamp)
		}
	}
	return verification.VerifyEntries(hintRanges, timestamps)
}

type dryRunHintSummary struct {
	hintTotalSeconds   string
	earliestHintTime   string
	latestHintTime     string
	initialSkipSeconds string
	rangesChecksum     string
}

func summarizeDryRunHints(
	hintRanges []hintprovider.HintTimeRange,
	queryStart, queryEnd, eligibleEnd time.Time,
	direction logproto.Direction,
) dryRunHintSummary {
	start := queryStart.UTC()
	end := queryEnd.UTC()
	if end.Before(start) {
		end = start
	}

	if eligibleEnd.IsZero() {
		eligibleEnd = end
	}
	verifyEnd := eligibleEnd.UTC()
	if verifyEnd.Before(start) {
		verifyEnd = start
	}

	hasPassthrough := false
	for _, r := range hintRanges {
		if r.IsPassthrough() {
			hasPassthrough = true
			break
		}
	}
	if hasPassthrough {
		total := roundTo1Decimal(intervalDuration(start, end).Seconds())
		return dryRunHintSummary{
			hintTotalSeconds:   fmt.Sprintf("%.1f", total),
			earliestHintTime:   start.Format(time.RFC3339Nano),
			latestHintTime:     end.Format(time.RFC3339Nano),
			initialSkipSeconds: "0.0",
			rangesChecksum:     "0",
		}
	}

	hasher := xxh3.New()
	var (
		earliest   time.Time
		latest     time.Time
		total      float64
		hasClipped bool
	)
	for _, r := range hintRanges {
		rangeStart := r.Start.UTC()
		rangeEnd := r.End.UTC()
		if rangeEnd.Before(rangeStart) {
			rangeEnd = rangeStart
		}
		fmt.Fprintf(hasher, "%d,%d;", rangeStart.UnixNano(), rangeEnd.UnixNano())

		clippedStart := maxTime(rangeStart, start)
		clippedEnd := minTime(rangeEnd, verifyEnd)
		if clippedEnd.Before(clippedStart) {
			continue
		}
		total += clippedEnd.Sub(clippedStart).Seconds()
		if !hasClipped || clippedStart.Before(earliest) {
			earliest = clippedStart
		}
		if !hasClipped || clippedEnd.After(latest) {
			latest = clippedEnd
		}
		hasClipped = true
	}

	totalRounded := roundTo1Decimal(total)
	initialSkip := 0.0
	earliestStr := ""
	latestStr := ""
	if hasClipped {
		earliestStr = earliest.Format(time.RFC3339Nano)
		latestStr = latest.Format(time.RFC3339Nano)
		switch direction {
		case logproto.BACKWARD:
			initialSkip = intervalDuration(latest, end).Seconds()
		default:
			initialSkip = intervalDuration(start, earliest).Seconds()
		}
		initialSkip = roundTo1Decimal(initialSkip)
	}

	return dryRunHintSummary{
		hintTotalSeconds:   fmt.Sprintf("%.1f", totalRounded),
		earliestHintTime:   earliestStr,
		latestHintTime:     latestStr,
		initialSkipSeconds: fmt.Sprintf("%.1f", initialSkip),
		rangesChecksum:     fmt.Sprintf("%016x", hasher.Sum64()),
	}
}

func roundTo1Decimal(v float64) float64 {
	return math.Round(v*10) / 10
}

func (h *loglinePrefetchHandler) doDryRun(
	ctx context.Context,
	req queryrangebase.Request,
	direction logproto.Direction,
	expr syntax.Expr,
	from, through time.Time,
	queryBytes uint64,
) (queryrangebase.Response, error) {
	logger := util_log.WithContext(ctx, h.logger)
	// A supported metric query has one log selector, so it has one matcher group.
	// A log query has one too, with a zero interval and offset, so Widen leaves it unchanged.
	readStart, readEnd := from, through
	if groups, err := syntax.MatcherGroups(expr); err == nil && len(groups) == 1 {
		readStart, readEnd = metricLookupBounds(groups[0], from, through, req.GetStep())
	}
	eligibleEnd := readEnd
	if h.queryIngestersWithin > 0 {
		cutoff := time.Now().Add(-h.queryIngestersWithin).UTC()
		if cutoff.After(readStart) && cutoff.Before(readEnd) {
			// An instant query has one sample, and its lookback ends at readEnd.
			// That sample would be left out of the report, so skip the lookup.
			if _, ok := req.(*queryrange.LokiInstantRequest); ok {
				return h.next.Do(ctx, req)
			}
			eligibleEnd = cutoff
		} else if !cutoff.After(readStart) {
			// Entire query is in the ingester window — nothing to verify.
			return h.next.Do(ctx, req)
		}
	}

	if !h.dryRunInflight.CompareAndSwap(0, 1) {
		if h.metrics != nil && h.metrics.dryRunSkippedInflight != nil {
			h.metrics.dryRunSkippedInflight.Inc()
		}
		level.Warn(logger).Log(
			"msg", "logline dry-run hint lookup skipped due to inflight limit",
			"query_hash", util.HashedQuery(req.GetQuery()),
			"limit", 1,
		)
		return h.next.Do(ctx, req)
	}
	defer h.dryRunInflight.Store(0)

	if h.metrics != nil && h.metrics.dryRunTotal != nil {
		h.metrics.dryRunTotal.Inc()
	}

	prefetchCtx, cancelPrefetch := context.WithTimeout(ctx, h.hintTimeout)
	defer cancelPrefetch()
	tenant, _ := user.ExtractOrgID(ctx)

	lookup := &dryRunLookupResult{
		stats: hintprovider.NewQueryStats(),
		done:  make(chan struct{}),
	}

	go func() {
		defer close(lookup.done)
		start := time.Now()
		hints, stats, hintErr := h.hintProvider.ProvideHints(
			prefetchCtx,
			tenant,
			expr,
			model.TimeFromUnixNano(readStart.UnixNano()),
			model.TimeFromUnixNano(eligibleEnd.UnixNano()),
		)
		lookup.duration = time.Since(start)
		if h.metrics != nil && h.metrics.hintProviderDuration != nil {
			h.metrics.hintProviderDuration.Observe(lookup.duration.Seconds())
		}
		if lookup.stats != nil {
			lookup.stats.SetWallTime(lookup.duration)
		}
		if stats != nil {
			if lookup.stats == nil {
				lookup.stats = hintprovider.NewQueryStats()
			}
			lookup.stats.Merge(stats)
		}
		if hintErr != nil {
			lookup.err = hintErr
			return
		}
		if hints != nil {
			lookup.ranges = hints.TimeRanges
			if h.metrics != nil && h.metrics.hintRangesReturned != nil {
				h.metrics.hintRangesReturned.Observe(float64(len(lookup.ranges)))
			}
		}
	}()

	queryStart := time.Now()
	resp, queryErr := h.next.Do(ctx, req)
	queryDuration := time.Since(queryStart)
	queryTimeout := ctx.Err() != nil

	// Bind common fields once so every branch inherits them.
	logger = log.With(logger,
		"query_hash", util.HashedQuery(req.GetQuery()),
		"query_timeout", queryTimeout,
		"query_start", from.Format(time.RFC3339Nano),
		"query_end", through.Format(time.RFC3339Nano),
		"query_duration", queryDuration,
		"query_bytes", queryBytes,
	)

	hintsDone := false
	select {
	case <-lookup.done:
		hintsDone = true
	default:
		cancelPrefetch()
	}

	if !hintsDone {
		if h.metrics != nil && h.metrics.dryRunIncomplete != nil {
			h.metrics.dryRunIncomplete.Inc()
		}
		level.Error(logger).Log("msg", "logline dry-run hint lookup incomplete")
		return resp, queryErr
	}

	// Hint lookup goroutine finished — but it may have been cancelled by
	// the parent context (client timeout). Treat context errors on the hint
	// lookup as incomplete when the query itself timed out.
	if lookup.err != nil {
		if queryTimeout && isCancel(lookup.err) {
			if h.metrics != nil && h.metrics.dryRunIncomplete != nil {
				h.metrics.dryRunIncomplete.Inc()
			}
			level.Info(logger).Log("msg", "logline dry-run hint lookup incomplete")
			return resp, queryErr
		}
		level.Error(logger).Log("msg", "logline dry-run hint lookup failed", "err", lookup.err)
		return resp, queryErr
	}

	hintSummary := summarizeDryRunHints(lookup.ranges, readStart, readEnd, eligibleEnd, direction)
	queryType, _ := logql.QueryType(expr)

	// Hint lookup succeeded. If the query timed out we can still report
	// the hint data — mark correct=true since the hints themselves were
	// valid but we had no (complete) result set to verify against.
	if queryTimeout {
		if h.metrics != nil && h.metrics.dryRunVerified != nil {
			h.metrics.dryRunVerified.WithLabelValues("correct").Inc()
		}
		logValues := []any{
			"msg", "logline dry-run verification",
			"query_type", queryType,
			"eligible_end", eligibleEnd.Format(time.RFC3339Nano),
			"hint_duration", lookup.duration,
			"hint_ranges", len(lookup.ranges),
			"hint_ranges_detail", hintprovider.FormatHintRanges(lookup.ranges),
			"hint_total_seconds", hintSummary.hintTotalSeconds,
			"earliest_hint_time", hintSummary.earliestHintTime,
			"latest_hint_time", hintSummary.latestHintTime,
			"initial_skip_seconds", hintSummary.initialSkipSeconds,
			"hint_ranges_checksum", hintSummary.rangesChecksum,
			"correct", true,
		}
		logValues = appendHintStats(logValues, lookup.stats)
		level.Info(logger).Log(logValues...)
		return resp, queryErr
	}

	if queryErr != nil {
		level.Warn(logger).Log("msg", "logline dry-run verification skipped due query error", "err", queryErr)
		return resp, queryErr
	}

	var vr verification.Report
	switch r := resp.(type) {
	case *queryrange.LokiPromResponse:
		vr = dryRunMetricReport(expr, r, lookup.ranges, eligibleEnd)
	case *queryrange.LokiResponse:
		vr = verifyDryRunHints(lookup.ranges, r, eligibleEnd)
	default:
		level.Warn(logger).Log(
			"msg", "logline dry-run verification skipped due non-Loki response",
			"response_type", fmt.Sprintf("%T", resp),
		)
		return resp, queryErr
	}
	correct := vr.Correct()
	label := "correct"
	if !correct {
		label = "false_negative"
	}
	if h.metrics != nil && h.metrics.dryRunVerified != nil {
		h.metrics.dryRunVerified.WithLabelValues(label).Inc()
	}

	logValues := []any{
		"msg", "logline dry-run verification",
		"query_type", queryType,
		"eligible_end", eligibleEnd.Format(time.RFC3339Nano),
		"hint_duration", lookup.duration,
		"hint_ranges", len(lookup.ranges),
		"hint_ranges_detail", hintprovider.FormatHintRanges(lookup.ranges),
		"hint_total_seconds", hintSummary.hintTotalSeconds,
		"earliest_hint_time", hintSummary.earliestHintTime,
		"latest_hint_time", hintSummary.latestHintTime,
		"initial_skip_seconds", hintSummary.initialSkipSeconds,
		"hint_ranges_checksum", hintSummary.rangesChecksum,
		"total_entries", vr.TotalEntries,
		"covered_entries", vr.CoveredEntries,
		"false_negatives", vr.FalseNegatives,
		"correct", correct,
	}
	if len(vr.FalseNegativeRanges) > 0 {
		logValues = append(logValues,
			"false_negative_ranges", hintprovider.FormatHintRanges(vr.FalseNegativeRanges),
		)
	}
	logValues = appendHintStats(logValues, lookup.stats)
	level.Info(logger).Log(logValues...)

	return resp, queryErr
}

func (h *loglinePrefetchHandler) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	switch req.(type) {
	case *queryrange.LokiRequest, *queryrange.LokiInstantRequest, *queryrange.DetectedFieldsRequest:
	default:
		return h.next.Do(ctx, req)
	}
	logger := util_log.WithContext(ctx, h.logger)
	if httpreq.ExtractHeader(ctx, LoglineSkipCacheHeader) != "" {
		ctx = hintprovider.WithSkipCache(ctx)
		level.Debug(logger).Log("msg", "hint cache skipped by request header", "header", LoglineSkipCacheHeader)
	}

	header := httpreq.ExtractHeader(ctx, LoglineIndexHeader)
	tenant, _ := user.ExtractOrgID(ctx)
	// A multi-tenant request (a|b) has no entry of its own in the runtime
	// overrides, so it gets the limits_config defaults.
	tenantMode := modeFromLimit(h.limits.LoglineQueryMode(tenant))
	minQueryBytes := h.limits.LoglineQueryMinQueryBytesForIndex(tenant)
	mode, passthrough := resolveMode(header, tenantMode, h.defaultMode, h.requireOptInHeader)
	if passthrough {
		return h.next.Do(ctx, req)
	}
	// Dry-run verifies hints against the log lines or samples in the response.
	// A detected fields response has neither, so the lookup would be wasted.
	if _, ok := req.(*queryrange.DetectedFieldsRequest); ok && mode != ModeLive {
		return h.next.Do(ctx, req)
	}

	expr, err := syntax.ParseExpr(req.GetQuery())
	if err != nil {
		return h.next.Do(ctx, req)
	}

	if _, isMetric := expr.(syntax.SampleExpr); isMetric {
		if mode != ModeDryRun || len(hintprovider.SupportedMetricQuery(expr, h.ngramLength)) == 0 {
			return h.next.Do(ctx, req)
		}
		// A log selector on the instant endpoint is rejected by the querier.
		// Return it here so this path does not look up hints for that request.
	} else if _, isInstant := req.(*queryrange.LokiInstantRequest); isInstant {
		return h.next.Do(ctx, req)
	}

	// Shared eligibility: supported query shape.
	// When ngramLength is not configured (<=0), skip the early check and let
	// ProvideHints handle support detection internally.
	if h.ngramLength > 0 && len(hintprovider.SupportedQuery(expr, h.ngramLength)) == 0 {
		return h.nextWithOutcome(ctx, req, mode, outcomeUnsupportedQuery)
	}

	// Shared eligibility: stats-based byte threshold.
	from := req.GetStart().UTC()
	through := req.GetEnd().UTC()
	queryBytes := uint64(0)
	if minQueryBytes > 0 {
		queryBytes, err = h.getQueryBytes(ctx, expr, from, through, req.GetStep())
		if err != nil {
			level.Warn(logger).Log(
				"msg", "failed to get query stats for logline gating; skipping hint lookup",
				"err", err,
				"threshold_bytes", minQueryBytes,
			)
			return h.nextWithOutcome(ctx, req, mode, outcomeSizeEstimateFailed)
		}
		if queryBytes < uint64(minQueryBytes) {
			if h.metrics != nil && h.metrics.hintSkippedSmallQuery != nil {
				h.metrics.hintSkippedSmallQuery.Inc()
			}
			level.Debug(logger).Log(
				"msg", "query below logline index threshold; skipping hint lookup",
				"query_bytes", queryBytes,
				"threshold_bytes", minQueryBytes,
			)
			return h.nextWithOutcome(ctx, req, mode, outcomeQueryTooSmall)
		}
	}

	var direction logproto.Direction
	switch r := req.(type) {
	case *queryrange.LokiRequest:
		direction = r.GetDirection()
	case *queryrange.LokiInstantRequest:
		direction = r.GetDirection()
	}

	if mode == ModeDryRun {
		return h.doDryRun(ctx, req, direction, expr, from, through, queryBytes)
	}

	ingesterCutoff := through
	if h.queryIngestersWithin > 0 {
		cutoff := time.Now().Add(-h.queryIngestersWithin).UTC()
		if cutoff.After(from) && cutoff.Before(through) {
			ingesterCutoff = cutoff
		} else if !cutoff.After(from) {
			// Entire query is in the ingester window. Store this so filter middleware
			// can pass through without waiting for object storage hints.
			ingesterCutoff = from
		}
	}

	result := &hintPrefetchResult{
		queryStart:     from,
		queryEnd:       through,
		queryBytes:     queryBytes,
		ingesterCutoff: ingesterCutoff,
		minDate:        h.minDate(),
		stats:          hintprovider.NewQueryStats(),
		done:           make(chan struct{}),
	}

	// Pass-through sub-requests don't wait for the lookup, so the query can
	// return before it finishes.
	prefetchCtx, cancelPrefetch := context.WithCancel(ctx)
	defer cancelPrefetch()
	if ingesterCutoff.After(maxTime(from, result.minDate)) {
		go h.lookupHints(prefetchCtx, logger, tenant, expr, result)
	} else {
		// Every sub-request passes through, so nothing reads the lookup. When
		// the stored part of the query predates the index, report the range the
		// lookup would have returned so stats treat the query as not indexed.
		if ingesterCutoff.After(from) {
			result.ranges = []hintprovider.HintTimeRange{{
				End:    result.minDate,
				Source: hintprovider.HintSourcePreMinDate,
			}}
		}
		close(result.done)
	}

	ctx = withHintPrefetch(ctx, result)
	defer h.observeHintWait(result)
	resp, err := h.next.Do(ctx, req)
	if err != nil {
		return resp, err
	}
	attachLoglineStats(resp, result)
	return resp, nil
}

// observeHintWait records the hint wait of a live query. Call it once per
// query, including queries that fail, so the histogram counts every lookup.
func (h *loglinePrefetchHandler) observeHintWait(result *hintPrefetchResult) {
	wait := result.wait.duration(time.Now())
	h.metrics.hintWaitDuration.WithLabelValues(result.status()).Observe(wait.Seconds())
}

// lookupHints looks up hint ranges for [result.queryStart, result.ingesterCutoff]
// and closes result.done when it finishes.
func (h *loglinePrefetchHandler) lookupHints(
	ctx context.Context,
	logger log.Logger,
	tenant string,
	expr syntax.Expr,
	result *hintPrefetchResult,
) {
	defer close(result.done)

	start := time.Now()
	defer func() {
		dur := time.Since(start)
		result.lookupDuration = dur
		if isCancel(result.err) {
			h.metrics.hintLookupsCanceled.Inc()
			level.Info(logger).Log("msg", "hint prefetch canceled", "duration", dur)
			return
		}
		h.metrics.hintProviderDuration.Observe(dur.Seconds())
		// The metrics.go line carries the status, lookup time and range
		// count and duration. This line keeps the remaining detail.
		logValues := []any{
			"msg", "hint prefetch completed",
			"query_bytes", result.queryBytes,
			"hint_ranges_detail", hintprovider.FormatHintRanges(result.ranges),
			"err", result.err,
		}
		if result.stats != nil {
			result.stats.SetWallTime(dur)
			snap := result.stats.Snapshot()
			logValues = append(logValues,
				"prefetch_calls", snap.PrefetchCalls,
				"prefetch_timeouts", snap.PrefetchTimeouts,
			)
		}
		level.Debug(logger).Log(appendHintStats(logValues, result.stats)...)
	}()

	eligibleFrom := model.TimeFromUnixNano(result.queryStart.UnixNano())
	eligibleThrough := model.TimeFromUnixNano(result.ingesterCutoff.UnixNano())

	hints, stats, err := h.hintProvider.ProvideHints(ctx, tenant, expr, eligibleFrom, eligibleThrough)
	if stats != nil {
		result.stats.Merge(stats)
	}
	if err != nil {
		result.err = err
		return
	}
	if hints != nil {
		result.ranges = hints.TimeRanges // already normalized+sorted
		h.metrics.hintRangesReturned.Observe(float64(len(result.ranges)))
	}
}

// minDate returns the first date the index covers, or zero when the hint
// provider doesn't report one.
func (h *loglinePrefetchHandler) minDate() time.Time {
	if p, ok := h.hintProvider.(interface{ MinDate() time.Time }); ok {
		return p.MinDate().UTC()
	}
	return time.Time{}
}

// nextWithOutcome runs the query without a hint lookup. In live mode it records
// why the lookup was skipped.
func (h *loglinePrefetchHandler) nextWithOutcome(ctx context.Context, req queryrangebase.Request, mode Mode, outcome string) (queryrangebase.Response, error) {
	resp, err := h.next.Do(ctx, req)
	if mode != ModeLive || err != nil {
		return resp, err
	}
	if lokiResp, ok := resp.(*queryrange.LokiResponse); ok {
		setLoglineChunkStats(&lokiResp.Statistics)
		lokiResp.Statistics.LoglineStats().Outcome = outcome
	}
	return resp, nil
}

// NewLoglineFilterMiddleware intercepts each interval sub-request from
// SplitByInterval and either skips it or attaches clipped hint ranges.
func NewLoglineFilterMiddleware(
	hintTimeout time.Duration,
	metrics *Metrics,
	logger log.Logger,
) queryrangebase.Middleware {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return &loglineFilterHandler{
			next:        next,
			hintTimeout: hintTimeout,
			metrics:     metrics,
			logger:      logger,
		}
	})
}

type loglineFilterHandler struct {
	next        queryrangebase.Handler
	hintTimeout time.Duration
	metrics     *Metrics
	logger      log.Logger
}

func (h *loglineFilterHandler) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	lokiReq, ok := req.(*queryrange.LokiRequest)
	if !ok {
		return h.next.Do(ctx, req)
	}
	logger := util_log.WithContext(ctx, h.logger)

	result := hintPrefetchFromContext(ctx)
	if result == nil {
		return h.next.Do(ctx, req)
	}

	intervalStart := lokiReq.StartTs.UTC()
	intervalEnd := lokiReq.EndTs.UTC()

	// A metric split reads before its start. Overlap and clipping use that window
	// so a lookback match is not treated as a miss. Log queries widen by zero.
	expr, _ := syntax.ParseExpr(lokiReq.Query)
	readStart, readEnd := intervalStart, intervalEnd
	if groups, err := syntax.MatcherGroups(expr); err == nil && len(groups) == 1 {
		readStart, readEnd = groups[0].Widen(intervalStart, intervalEnd)
	}

	// These intervals pass through whatever the lookup returns, so they run
	// without waiting for it.
	if readEnd.After(result.ingesterCutoff) {
		h.metrics.passthroughSubRequests.WithLabelValues("ingester_window").Inc()
		return h.next.Do(ctx, req)
	}
	if !readEnd.After(result.minDate) {
		h.metrics.passthroughSubRequests.WithLabelValues("pre_min_date").Inc()
		return h.next.Do(ctx, req)
	}

	timedOut, err := result.waitForLookup(ctx, h.hintTimeout)
	if err != nil {
		return nil, err
	}
	result.stats.ObservePrefetchCall(timedOut)
	if timedOut {
		h.metrics.hintPassthrough.WithLabelValues("timeout").Inc()
		level.Warn(logger).Log("msg", "hint prefetch timeout, falling back to passthrough", "timeout", h.hintTimeout)
		return h.next.Do(ctx, req)
	}

	if result.err != nil {
		if errors.Is(result.err, hintprovider.ErrUnsupported) {
			h.metrics.hintPassthrough.WithLabelValues("unsupported").Inc()
		} else {
			h.metrics.hintPassthrough.WithLabelValues("error").Inc()
			level.Warn(logger).Log("msg", "hint provider error, passing through", "err", result.err)
		}
		return h.next.Do(ctx, req)
	}

	overlapping := rangesOverlapping(result.ranges, readStart, readEnd)
	if len(overlapping) == 0 {
		h.metrics.hintSubRequests.WithLabelValues("skipped").Inc()
		return skippedResponse(lokiReq, expr), nil
	}

	hints := clipHintRangesForInterval(overlapping, readStart, readEnd)
	if len(hints) == 0 {
		h.metrics.hintSubRequests.WithLabelValues("skipped").Inc()
		return skippedResponse(lokiReq, expr), nil
	}

	// A pre-min-date-only interval has no indexed coverage. Leave hints off so
	// the querier scans the interval normally.
	if passthroughCoversInterval(overlapping, hints, readStart, readEnd) {
		h.metrics.passthroughSubRequests.WithLabelValues("pre_min_date").Inc()
		return h.next.Do(ctx, req)
	}

	h.metrics.hintSubRequests.WithLabelValues("narrowed").Inc()
	resp, err := h.next.Do(ctx, withHintRanges(lokiReq, hints))
	// The count travels on the sub-request's response, so the split and shard
	// merges add it up and discarded responses (retries) drop it.
	if err == nil {
		switch r := resp.(type) {
		case *queryrange.LokiResponse:
			r.Statistics.LoglineStats().NarrowedRequests++
		case *queryrange.LokiPromResponse:
			r.Statistics.LoglineStats().NarrowedRequests++
		}
	}
	return resp, err
}

func skippedResponse(req *queryrange.LokiRequest, expr syntax.Expr) queryrangebase.Response {
	if _, ok := expr.(syntax.SampleExpr); ok {
		return skippedMetricResponse()
	}
	return skippedLokiResponse(req)
}

func skippedLokiResponse(req *queryrange.LokiRequest) *queryrange.LokiResponse {
	resp := emptyLokiResponse(req)
	resp.Statistics.LoglineStats().SkippedRequests = 1
	return resp
}

func skippedMetricResponse() *queryrange.LokiPromResponse {
	resp := emptyMetricResponse()
	resp.Statistics.LoglineStats().SkippedRequests = 1
	return resp
}

// withHintRanges clones the request and sets hint ranges. The caller's
// request, including its bounds, is left unchanged.
func withHintRanges(req *queryrange.LokiRequest, hints []logproto.HintTimeRange) *queryrange.LokiRequest {
	clone := *req
	clone.HintRanges = hints
	return &clone
}

// clipHintRangesForInterval clips ranges to the half-open interval, converts a
// pre-min-date sentinel into that overlap, then sorts and merges. Touching
// ranges merge, matching querier hint consumption.
func clipHintRangesForInterval(ranges []hintprovider.HintTimeRange, start, end time.Time) []logproto.HintTimeRange {
	clipped := make([]logproto.HintTimeRange, 0, len(ranges))
	for _, r := range ranges {
		rangeStart := r.Start
		if r.IsPassthrough() || rangeStart.Before(start) {
			rangeStart = start
		}
		rangeEnd := r.End
		if rangeEnd.After(end) {
			rangeEnd = end
		}
		if !rangeStart.Before(rangeEnd) {
			continue
		}
		clipped = append(clipped, logproto.HintTimeRange{Start: rangeStart, End: rangeEnd})
	}
	sort.Slice(clipped, func(i, j int) bool {
		if clipped[i].Start.Equal(clipped[j].Start) {
			return clipped[i].End.Before(clipped[j].End)
		}
		return clipped[i].Start.Before(clipped[j].Start)
	})

	merged := clipped[:0]
	for _, current := range clipped {
		if len(merged) == 0 || current.Start.After(merged[len(merged)-1].End) {
			merged = append(merged, current)
			continue
		}
		if current.End.After(merged[len(merged)-1].End) {
			merged[len(merged)-1].End = current.End
		}
	}
	return merged
}

// passthroughCoversInterval reports that every overlapping hint is a
// pre-min-date sentinel and together they cover the whole interval.
func passthroughCoversInterval(ranges []hintprovider.HintTimeRange, hints []logproto.HintTimeRange, start, end time.Time) bool {
	if len(hints) != 1 || !end.After(start) {
		return false
	}
	for _, r := range ranges {
		if !r.IsPassthrough() {
			return false
		}
	}
	return !hints[0].Start.After(start) && !hints[0].End.Before(end)
}

func hintRangesDuration(hints []logproto.HintTimeRange) time.Duration {
	var queried time.Duration
	for _, hint := range hints {
		queried += intervalDuration(hint.Start, hint.End)
	}
	return queried
}

func hintCoverageDuration(ranges []hintprovider.HintTimeRange, start, end time.Time) time.Duration {
	return hintRangesDuration(clipHintRangesForInterval(ranges, start, end))
}

func isCancel(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// rangesOverlap reports whether any half-open hint [Start, End) overlaps (start, end].
// A hint that only ends at start shares no time with the window.
// Assumes ranges are sorted by Start. It does not allocate.
func rangesOverlap(ranges []hintprovider.HintTimeRange, start, end time.Time) bool {
	if len(ranges) == 0 {
		return false
	}
	idx := sort.Search(len(ranges), func(i int) bool {
		return ranges[i].End.After(start)
	})
	return idx < len(ranges) && !ranges[idx].Start.After(end)
}

// rangesOverlapping returns hint ranges that overlap [start, end].
// Assumes ranges are sorted by Start.
func rangesOverlapping(ranges []hintprovider.HintTimeRange, start, end time.Time) []hintprovider.HintTimeRange {
	if len(ranges) == 0 {
		return nil
	}

	idx := sort.Search(len(ranges), func(i int) bool {
		return !ranges[i].End.Before(start)
	})

	var result []hintprovider.HintTimeRange
	for i := idx; i < len(ranges); i++ {
		if ranges[i].Start.After(end) {
			break
		}
		result = append(result, ranges[i])
	}
	return result
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func emptyLokiResponse(req *queryrange.LokiRequest) *queryrange.LokiResponse {
	return &queryrange.LokiResponse{
		Status:    "success",
		Direction: req.Direction,
		Limit:     req.Limit,
		Version:   uint32(loghttp.GetVersion(req.Path)),
		Data: queryrange.LokiData{
			ResultType: loghttp.ResultTypeStream,
			Result:     []logproto.Stream{},
		},
	}
}

func emptyMetricResponse() *queryrange.LokiPromResponse {
	return &queryrange.LokiPromResponse{
		Response: &queryrangebase.PrometheusResponse{
			Status: loghttp.QueryStatusSuccess,
			Data: queryrangebase.PrometheusData{
				ResultType: loghttp.ResultTypeMatrix,
				Result:     []queryrangebase.SampleStream{},
			},
		},
	}
}
