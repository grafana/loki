package queryfrontend

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/zeebo/xxh3"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/querylimits"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logline/verification"
)

// Two-layer logline index middleware:
//
// 1. Prefetch MW (above SplitByInterval): parses query, kicks off async
//    logline index lookup, stores results + ingester cutoff in context.
//
// 2. Filter MW (below SplitByInterval, below cache): for each interval
//    sub-request, consults the prefetched hints to skip empty intervals
//    or narrow to at most k hint envelopes (k = ceil(interval/15m), cap 8).
//
// SplitByInterval handles direction ordering, LIMIT early-exit, and
// interval-level caching. We just help it skip or narrow intervals.
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

// envelopeTargetDuration is the target coverage of each narrowed envelope.
// k = ceil(incoming interval / envelopeTargetDuration), capped at
// maxEnvelopesPerInterval. Groups are formed by cutting the k-1 largest
// inter-hint gaps so distant clusters are not unioned into one scan.
const (
	envelopeTargetDuration  = 15 * time.Minute
	maxEnvelopesPerInterval = 8
)

type hintPrefetchKeyType struct{}
type shardPlanningRerunGuardKeyType struct{}

type shardPlanningDecision struct {
	eligible bool
	reason   string
	overlaps []hintprovider.HintTimeRange
}

type provisionalQueryResult struct {
	resp queryrangebase.Response
	err  error
}

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
	done           chan struct{} // closed when lookup completes

	// Per-query impact counters, updated from the filter layer and logged once
	// after the full query pipeline finishes.
	totalIntervals        atomic.Int64
	skippedIntervals      atomic.Int64
	narrowedIntervals     atomic.Int64
	passthroughIntervals  atomic.Int64
	originalDurationNanos atomic.Int64
	queryDurationNanos    atomic.Int64
}

type hintImpactSnapshot struct {
	totalIntervals       int64
	skippedIntervals     int64
	narrowedIntervals    int64
	passthroughIntervals int64
	originalDuration     time.Duration
	queryDuration        time.Duration
	timeReductionRatio   float64
}

type dryRunLookupResult struct {
	ranges   []hintprovider.HintTimeRange
	err      error
	stats    *hintprovider.QueryStats
	duration time.Duration
	done     chan struct{}
}

func (r *hintPrefetchResult) recordSkipped(intervalDuration time.Duration) {
	if r == nil {
		return
	}
	r.totalIntervals.Add(1)
	r.skippedIntervals.Add(1)
	r.originalDurationNanos.Add(intervalDuration.Nanoseconds())
}

func (r *hintPrefetchResult) recordNarrowed(originalDuration, queriedDuration time.Duration) {
	if r == nil {
		return
	}
	r.totalIntervals.Add(1)
	r.narrowedIntervals.Add(1)
	r.originalDurationNanos.Add(originalDuration.Nanoseconds())
	r.queryDurationNanos.Add(queriedDuration.Nanoseconds())
}

func (r *hintPrefetchResult) recordPassthrough(intervalDuration time.Duration) {
	if r == nil {
		return
	}
	r.totalIntervals.Add(1)
	r.passthroughIntervals.Add(1)
	r.originalDurationNanos.Add(intervalDuration.Nanoseconds())
	r.queryDurationNanos.Add(intervalDuration.Nanoseconds())
}

func (r *hintPrefetchResult) impactSnapshot() hintImpactSnapshot {
	if r == nil {
		return hintImpactSnapshot{}
	}

	originalNanos := r.originalDurationNanos.Load()
	queryNanos := r.queryDurationNanos.Load()
	ratio := 0.0
	if originalNanos > 0 {
		ratio = float64(originalNanos-queryNanos) / float64(originalNanos)
		if ratio < 0 {
			ratio = 0
		}
	}

	return hintImpactSnapshot{
		totalIntervals:       r.totalIntervals.Load(),
		skippedIntervals:     r.skippedIntervals.Load(),
		narrowedIntervals:    r.narrowedIntervals.Load(),
		passthroughIntervals: r.passthroughIntervals.Load(),
		originalDuration:     time.Duration(originalNanos),
		queryDuration:        time.Duration(queryNanos),
		timeReductionRatio:   ratio,
	}
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

func withShardPlanningRerunGuard(ctx context.Context) context.Context {
	return context.WithValue(ctx, shardPlanningRerunGuardKeyType{}, true)
}

func shardPlanningRerunGuardFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(shardPlanningRerunGuardKeyType{}).(bool)
	return v
}

func withShardPlanningQueryLimits(ctx context.Context, strategy string) context.Context {
	limits := querylimits.QueryLimits{}
	if existing := querylimits.ExtractQueryLimitsFromContext(ctx); existing != nil {
		limits = *existing
	}
	limits.TSDBShardingStrategy = strategy
	return querylimits.InjectQueryLimitsIntoContext(ctx, limits)
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
	hp hintprovider.QueryHintProvider,
	cfg MiddlewareConfig,
	tenantSettings TenantSettings,
	metrics *Metrics,
	logger log.Logger,
) queryrangebase.Middleware {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	if tenantSettings == nil {
		tenantSettings = staticTenantSettings{}
	}
	if cfg.ShardPlanning == (ShardPlanningConfig{}) {
		cfg.ShardPlanning.Enabled = defaultShardPlanningEnabled
	}
	cfg.ShardPlanning.applyDefaults()
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return &loglinePrefetchHandler{
			next:                 next,
			hintProvider:         hp,
			defaultMode:          modeFromDryRun(cfg.DryRun),
			requireOptInHeader:   cfg.RequireOptInHeader,
			ngramLength:          cfg.NgramLength,
			hintTimeout:          cfg.HintTimeout,
			minQueryBytes:        cfg.MinQueryBytesForIndex,
			queryIngestersWithin: cfg.QueryIngestersWithin,
			shardPlanning:        cfg.ShardPlanning,
			tenantSettings:       tenantSettings,
			metrics:              metrics,
			logger:               logger,
		}
	})
}

type loglinePrefetchHandler struct {
	next                 queryrangebase.Handler
	hintProvider         hintprovider.QueryHintProvider
	defaultMode          Mode
	requireOptInHeader   bool
	ngramLength          int
	hintTimeout          time.Duration
	dryRunInflight       atomic.Int32
	minQueryBytes        int64
	queryIngestersWithin time.Duration
	shardPlanning        ShardPlanningConfig
	tenantSettings       TenantSettings
	metrics              *Metrics
	logger               log.Logger
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

func (h *loglinePrefetchHandler) getQueryBytes(ctx context.Context, expr syntax.Expr, from, through time.Time) (uint64, error) {
	matcherGroups, err := syntax.MatcherGroups(expr)
	if err != nil {
		return 0, err
	}
	// If there are zero matcher groups, query index stats for everything.
	if len(matcherGroups) == 0 {
		matcherGroups = append(matcherGroups, syntax.MatcherRange{})
	}

	start := model.Time(from.UnixMilli())
	end := model.Time(through.UnixMilli())

	var totalBytes uint64
	for _, group := range matcherGroups {
		diff := group.Interval + group.Offset
		adjustedFrom := start.Add(-diff)
		adjustedThrough := end.Add(-group.Offset)

		resp, err := h.next.Do(ctx, &logproto.IndexStatsRequest{
			From:     adjustedFrom,
			Through:  adjustedThrough,
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

func (h *loglinePrefetchHandler) observeShardPlanning(result, reason string) {
	if h == nil || h.metrics == nil || h.metrics.shardPlanningTotal == nil {
		return
	}
	h.metrics.shardPlanningTotal.WithLabelValues(result, reason).Inc()
}

func (h *loglinePrefetchHandler) shardPlanningDecision(result *hintPrefetchResult, from, through time.Time) shardPlanningDecision {
	if result == nil {
		return shardPlanningDecision{reason: "hint_error"}
	}
	if result.err != nil {
		if errors.Is(result.err, hintprovider.ErrUnsupported) {
			return shardPlanningDecision{reason: "unsupported"}
		}
		return shardPlanningDecision{reason: "hint_error"}
	}
	if through.After(result.ingesterCutoff) {
		return shardPlanningDecision{reason: "ingester_window"}
	}
	for _, hint := range result.ranges {
		if hint.IsPassthrough() {
			return shardPlanningDecision{reason: "passthrough_range"}
		}
	}

	overlaps := rangesOverlapping(result.ranges, from, through)

	var cumulative time.Duration
	for _, hint := range overlaps {
		start := maxTime(hint.Start, from)
		end := minTime(hint.End, through)
		cumulative += intervalDuration(start, end)
	}

	queryDuration := intervalDuration(from, through)
	if queryDuration == 0 {
		return shardPlanningDecision{reason: "time_reduction_too_small", overlaps: overlaps}
	}
	timeReductionRatio := float64(queryDuration-cumulative) / float64(queryDuration)
	if timeReductionRatio < 0 {
		timeReductionRatio = 0
	}
	if timeReductionRatio < h.shardPlanning.MinTimeReductionRatio {
		return shardPlanningDecision{reason: "time_reduction_too_small", overlaps: overlaps}
	}

	return shardPlanningDecision{eligible: true, reason: "eligible", overlaps: overlaps}
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

// hintRangesTotalSeconds returns the unclipped sum of non-passthrough hint
// range durations, formatted to one decimal place for log output.
func hintRangesTotalSeconds(ranges []hintprovider.HintTimeRange) string {
	var total float64
	for _, r := range ranges {
		if r.IsPassthrough() {
			continue
		}
		total += intervalDuration(r.Start.UTC(), r.End.UTC()).Seconds()
	}
	return fmt.Sprintf("%.1f", roundTo1Decimal(total))
}

func (h *loglinePrefetchHandler) doDryRun(
	ctx context.Context,
	req queryrangebase.Request,
	lokiReq *queryrange.LokiRequest,
	expr syntax.Expr,
	from, through time.Time,
	queryBytes uint64,
) (queryrangebase.Response, error) {
	logger := util_log.WithContext(ctx, h.logger)
	eligibleEnd := through
	if h.queryIngestersWithin > 0 {
		cutoff := time.Now().Add(-h.queryIngestersWithin).UTC()
		if cutoff.After(from) && cutoff.Before(through) {
			eligibleEnd = cutoff
		} else if !cutoff.After(from) {
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
			"query_hash", util.HashedQuery(lokiReq.Query),
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
			model.TimeFromUnixNano(from.UnixNano()),
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
		"query_hash", util.HashedQuery(lokiReq.Query),
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

	hintSummary := summarizeDryRunHints(lookup.ranges, from, through, eligibleEnd, lokiReq.Direction)

	// Hint lookup succeeded. If the query timed out we can still report
	// the hint data — mark correct=true since the hints themselves were
	// valid but we had no (complete) result set to verify against.
	if queryTimeout {
		if h.metrics != nil && h.metrics.dryRunVerified != nil {
			h.metrics.dryRunVerified.WithLabelValues("correct").Inc()
		}
		logValues := []any{
			"msg", "logline dry-run verification",
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

	lokiResp, ok := resp.(*queryrange.LokiResponse)
	if !ok {
		level.Warn(logger).Log(
			"msg", "logline dry-run verification skipped due non-Loki response",
			"response_type", fmt.Sprintf("%T", resp),
		)
		return resp, queryErr
	}

	vr := verifyDryRunHints(lookup.ranges, lokiResp, eligibleEnd)
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
	logValues = appendHintStats(logValues, lookup.stats)
	level.Info(logger).Log(logValues...)

	return resp, queryErr
}

func (h *loglinePrefetchHandler) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	lokiReq, ok := req.(*queryrange.LokiRequest)
	if !ok {
		return h.next.Do(ctx, req)
	}
	logger := util_log.WithContext(ctx, h.logger)
	if shardPlanningRerunGuardFromContext(ctx) {
		h.observeShardPlanning("fallback", "rerun_guard")
		return h.next.Do(ctx, req)
	}
	if httpreq.ExtractHeader(ctx, LoglineSkipCacheHeader) != "" {
		ctx = hintprovider.WithSkipCache(ctx)
		level.Debug(logger).Log("msg", "hint cache skipped by request header", "header", LoglineSkipCacheHeader)
	}

	header := httpreq.ExtractHeader(ctx, LoglineIndexHeader)
	tenant, _ := user.ExtractOrgID(ctx)
	tenantMode := ModeUnset
	minQueryBytes := h.minQueryBytes
	if h.tenantSettings != nil {
		tenantMode = h.tenantSettings.Mode(tenant)
		if tenantMinQueryBytes, ok := h.tenantSettings.MinQueryBytesForIndex(tenant); ok {
			minQueryBytes = tenantMinQueryBytes
		}
	}
	mode, passthrough := resolveMode(header, tenantMode, h.defaultMode, h.requireOptInHeader)
	if passthrough {
		return h.next.Do(ctx, req)
	}

	expr, err := syntax.ParseExpr(lokiReq.Query)
	if err != nil {
		return h.next.Do(ctx, req)
	}

	// Metric queries (SampleExpr) return numeric series, not log entries,
	// so there is nothing to narrow or verify.
	if _, ok := expr.(syntax.SampleExpr); ok {
		return h.next.Do(ctx, req)
	}

	// Shared eligibility: supported query shape.
	// When ngramLength is not configured (<=0), skip the early check and let
	// ProvideHints handle support detection internally.
	if h.ngramLength > 0 && len(hintprovider.SupportedQuery(expr, h.ngramLength)) == 0 {
		return h.next.Do(ctx, req)
	}

	// Shared eligibility: stats-based byte threshold.
	from := lokiReq.StartTs.UTC()
	through := lokiReq.EndTs.UTC()
	queryBytes := uint64(0)
	if minQueryBytes > 0 {
		queryBytes, err = h.getQueryBytes(ctx, expr, from, through)
		if err != nil {
			level.Warn(logger).Log(
				"msg", "failed to get query stats for logline gating; skipping hint lookup",
				"err", err,
				"threshold_bytes", minQueryBytes,
			)
			return h.next.Do(ctx, req)
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
			return h.next.Do(ctx, req)
		}
	}

	if mode == ModeDryRun {
		return h.doDryRun(ctx, req, lokiReq, expr, from, through, queryBytes)
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
		stats:          hintprovider.NewQueryStats(),
		done:           make(chan struct{}),
	}

	prefetchCtx := ctx
	go func() {
		defer close(result.done)

		start := time.Now()
		defer func() {
			dur := time.Since(start)
			h.metrics.hintProviderDuration.Observe(dur.Seconds())
			if isCancel(result.err) {
				level.Info(logger).Log("msg", "hint prefetch canceled", "duration", dur)
				return
			}
			if result.stats != nil {
				result.stats.SetWallTime(dur)
				snap := result.stats.Snapshot()
				level.Info(logger).Log(
					"msg", "hint prefetch completed",
					"duration", dur,
					"query_bytes", result.queryBytes,
					"ranges", len(result.ranges),
					"hint_ranges_detail", hintprovider.FormatHintRanges(result.ranges),
					"hint_total_seconds", hintRangesTotalSeconds(result.ranges),
					"err", result.err,
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
					"prefetch_calls", snap.PrefetchCalls,
					"prefetch_timeouts", snap.PrefetchTimeouts,
					"index_queries_total", snap.IndexQueriesTotal,
					"index_queries_term_miss", snap.IndexQueriesTermMiss,
					"index_queries_empty_and", snap.IndexQueriesEmptyAnd,
					"index_queries_positive", snap.IndexQueriesPositive,
					"term_batches_processed_total", snap.TotalTermBatchesProcessed,
					"hint_cache_result", snap.HintCacheResult,
					"hint_cache_days_fetched", snap.HintCacheDaysFetched,
					"hint_cache_days_hit", snap.HintCacheDaysHit,
				)
				return
			}
			level.Info(logger).Log(
				"msg", "hint prefetch completed",
				"duration", dur,
				"query_bytes", result.queryBytes,
				"ranges", len(result.ranges),
				"hint_ranges_detail", hintprovider.FormatHintRanges(result.ranges),
				"hint_total_seconds", hintRangesTotalSeconds(result.ranges),
				"err", result.err,
			)
		}()

		eligibleEnd := ingesterCutoff
		if !eligibleEnd.After(from) {
			return
		}

		eligibleFrom := model.TimeFromUnixNano(from.UnixNano())
		eligibleThrough := model.TimeFromUnixNano(eligibleEnd.UnixNano())

		hints, stats, err := h.hintProvider.ProvideHints(prefetchCtx, tenant, expr, eligibleFrom, eligibleThrough)
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
	}()

	ctx = withHintPrefetch(ctx, result)
	if !h.shardPlanning.Enabled {
		resp, err := h.next.Do(ctx, req)
		if err != nil {
			return resp, err
		}

		h.logHintImpact(logger, resp, result)
		return resp, nil
	}

	// Shard planning races hint prefetch against a provisional query. If hints
	// finish first and narrow enough, cancel the in-flight query and rerun with
	// power_of_two sharding. Otherwise keep the provisional query result.
	provisionalQueryCtx, cancelProvisionalQuery := context.WithCancel(ctx)
	defer cancelProvisionalQuery()
	provisionalQueryDone := make(chan provisionalQueryResult, 1)
	go func() {
		resp, err := h.next.Do(provisionalQueryCtx, req)
		provisionalQueryDone <- provisionalQueryResult{resp: resp, err: err}
	}()

	select {
	case provisional := <-provisionalQueryDone:
		h.observeShardPlanning("provisional_query", "query_finished_first")
		if provisional.err != nil {
			return provisional.resp, provisional.err
		}
		h.logHintImpact(logger, provisional.resp, result)
		return provisional.resp, nil
	case <-result.done:
		select {
		case provisional := <-provisionalQueryDone:
			h.observeShardPlanning("provisional_query", "query_finished_first")
			if provisional.err != nil {
				return provisional.resp, provisional.err
			}
			h.logHintImpact(logger, provisional.resp, result)
			return provisional.resp, nil
		default:
		}

		decision := h.shardPlanningDecision(result, from, through)
		if !decision.eligible {
			h.observeShardPlanning("fallback", decision.reason)
			provisional := <-provisionalQueryDone
			if provisional.err != nil {
				return provisional.resp, provisional.err
			}
			h.logHintImpact(logger, provisional.resp, result)
			return provisional.resp, nil
		}

		// Hints finished first with enough time reduction — cancel the provisional
		// query and rerun with power_of_two sharding.
		h.observeShardPlanning("cancel_then_rerun", decision.reason)
		cancelProvisionalQuery()

		rerunCtx := withShardPlanningRerunGuard(ctx)
		rerunCtx = withShardPlanningQueryLimits(rerunCtx, shardPlanningStrategyPowerOfTwo)
		resp, err := h.next.Do(rerunCtx, req)
		if err != nil {
			return resp, err
		}
		h.logHintImpact(logger, resp, result)
		return resp, nil
	}
}

func (h *loglinePrefetchHandler) logHintImpact(logger log.Logger, resp queryrangebase.Response, result *hintPrefetchResult) {
	if result == nil {
		return
	}

	select {
	case <-result.done:
	default:
		return
	}

	if result.err != nil {
		return
	}

	impact := result.impactSnapshot()
	logValues := []any{
		"msg", "query hint impact",
		"query_start", result.queryStart.Format(time.RFC3339Nano),
		"query_end", result.queryEnd.Format(time.RFC3339Nano),
		"query_range", result.queryEnd.Sub(result.queryStart),
		"hint_ranges", len(result.ranges),
		"total_intervals", impact.totalIntervals,
		"skipped_intervals", impact.skippedIntervals,
		"narrowed_intervals", impact.narrowedIntervals,
		"passthrough_intervals", impact.passthroughIntervals,
		"original_duration", impact.originalDuration,
		"queried_duration", impact.queryDuration,
		"time_reduction_ratio", impact.timeReductionRatio,
	}

	if lokiResp, ok := resp.(*queryrange.LokiResponse); ok {
		totalChunksRef := lokiResp.Statistics.Querier.Store.GetTotalChunksRef() +
			lokiResp.Statistics.Ingester.Store.GetTotalChunksRef()
		totalChunksDownloaded := lokiResp.Statistics.Querier.Store.GetTotalChunksDownloaded() +
			lokiResp.Statistics.Ingester.Store.GetTotalChunksDownloaded()
		totalDecompressedBytes := lokiResp.Statistics.Querier.Store.Chunk.GetDecompressedBytes() +
			lokiResp.Statistics.Ingester.Store.Chunk.GetDecompressedBytes()
		totalDecompressedLines := lokiResp.Statistics.Querier.Store.Chunk.GetDecompressedLines() +
			lokiResp.Statistics.Ingester.Store.Chunk.GetDecompressedLines()

		logValues = append(logValues,
			"total_chunks_ref", totalChunksRef,
			"total_chunks_downloaded", totalChunksDownloaded,
			"decompressed_bytes", totalDecompressedBytes,
			"decompressed_lines", totalDecompressedLines,
			"total_entries_returned", lokiResp.Statistics.Summary.GetTotalEntriesReturned(),
		)
	}

	level.Info(logger).Log(logValues...)
}

// NewLoglineFilterMiddleware intercepts each interval sub-request from
// SplitByInterval and narrows/skips it using prefetched hints.
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
	originalDuration := intervalDuration(intervalStart, intervalEnd)

	timer := time.NewTimer(h.hintTimeout)
	defer timer.Stop()
	select {
	case <-result.done:
		result.stats.ObservePrefetchCall(false)
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		result.stats.ObservePrefetchCall(true)
		result.recordPassthrough(originalDuration)
		h.metrics.hintPassthrough.WithLabelValues("timeout").Inc()
		level.Warn(logger).Log("msg", "hint prefetch timeout, falling back to passthrough", "timeout", h.hintTimeout)
		return h.next.Do(ctx, req)
	}

	if result.err != nil {
		result.recordPassthrough(originalDuration)
		if errors.Is(result.err, hintprovider.ErrUnsupported) {
			h.metrics.hintPassthrough.WithLabelValues("unsupported").Inc()
		} else {
			h.metrics.hintPassthrough.WithLabelValues("error").Inc()
			level.Warn(logger).Log("msg", "hint provider error, passing through", "err", result.err)
		}
		return h.next.Do(ctx, req)
	}

	if intervalEnd.After(result.ingesterCutoff) {
		result.recordPassthrough(originalDuration)
		h.metrics.passthroughSubRequests.WithLabelValues("ingester_window").Inc()
		return h.next.Do(ctx, req)
	}

	overlapping := rangesOverlapping(result.ranges, intervalStart, intervalEnd)
	if len(overlapping) == 0 {
		result.recordSkipped(originalDuration)
		h.metrics.hintSubRequests.WithLabelValues("skipped").Inc()
		return emptyLokiResponse(lokiReq), nil
	}

	groups := groupHintEnvelopes(overlapping, intervalStart, intervalEnd, envelopeBudget(originalDuration))
	if len(groups) == 0 {
		result.recordSkipped(originalDuration)
		h.metrics.hintSubRequests.WithLabelValues("skipped").Inc()
		return emptyLokiResponse(lokiReq), nil
	}

	if len(overlapping) == 1 && overlapping[0].IsPassthrough() {
		result.recordPassthrough(originalDuration)
		h.metrics.passthroughSubRequests.WithLabelValues("pre_min_date").Inc()
		return h.next.Do(ctx, req.WithStartEnd(groups[0].Start, groups[0].End))
	}

	var queried time.Duration
	for _, g := range groups {
		queried += intervalDuration(g.Start, g.End)
	}
	result.recordNarrowed(originalDuration, queried)
	h.metrics.hintSubRequests.WithLabelValues("narrowed").Inc()

	if len(groups) == 1 {
		return h.next.Do(ctx, req.WithStartEnd(groups[0].Start, groups[0].End))
	}

	responses := make([]queryrangebase.Response, 0, len(groups))
	for _, g := range groups {
		resp, err := h.next.Do(ctx, req.WithStartEnd(g.Start, g.End))
		if err != nil {
			return nil, err
		}
		responses = append(responses, resp)
	}
	return queryrange.DefaultCodec.MergeResponse(responses...)
}

type hintEnvelope struct {
	Start time.Time
	End   time.Time
}

func envelopeBudget(interval time.Duration) int {
	if interval <= 0 {
		return 1
	}
	k := int((interval + envelopeTargetDuration - 1) / envelopeTargetDuration)
	if k < 1 {
		return 1
	}
	if k > maxEnvelopesPerInterval {
		return maxEnvelopesPerInterval
	}
	return k
}

// groupHintEnvelopes partitions start-sorted hints into at most maxGroups
// envelopes by cutting the largest inter-hint gaps. Overlapping or touching
// hints (gap <= 0) are never split. Each envelope is clipped to [intervalStart, intervalEnd].
func groupHintEnvelopes(ranges []hintprovider.HintTimeRange, intervalStart, intervalEnd time.Time, maxGroups int) []hintEnvelope {
	clipped := make([]hintprovider.HintTimeRange, 0, len(ranges))
	for _, r := range ranges {
		start := maxTime(r.Start, intervalStart)
		end := minTime(r.End, intervalEnd)
		if !end.After(start) {
			continue
		}
		clipped = append(clipped, hintprovider.HintTimeRange{Start: start, End: end})
	}
	if len(clipped) == 0 {
		return nil
	}
	if maxGroups < 1 {
		maxGroups = 1
	}

	n := len(clipped)
	cutCount := maxGroups - 1
	if cutCount > n-1 {
		cutCount = n - 1
	}

	type rankedGap struct {
		after int
		d     time.Duration
	}
	ranked := make([]rankedGap, 0, n-1)
	for i := 0; i < n-1; i++ {
		d := clipped[i+1].Start.Sub(clipped[i].End)
		if d < 0 {
			d = 0
		}
		ranked = append(ranked, rankedGap{after: i, d: d})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].d != ranked[j].d {
			return ranked[i].d > ranked[j].d
		}
		return ranked[i].after < ranked[j].after
	})

	cutAfter := make([]bool, n-1)
	cuts := 0
	for _, g := range ranked {
		if cuts >= cutCount {
			break
		}
		if g.d <= 0 {
			continue
		}
		cutAfter[g.after] = true
		cuts++
	}

	var groups []hintEnvelope
	runStart := 0
	for i := 0; i < n; i++ {
		if i < n-1 && !cutAfter[i] {
			continue
		}
		end := clipped[runStart].End
		for j := runStart + 1; j <= i; j++ {
			if clipped[j].End.After(end) {
				end = clipped[j].End
			}
		}
		groups = append(groups, hintEnvelope{Start: clipped[runStart].Start, End: end})
		runStart = i + 1
	}
	return groups
}

func isCancel(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
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
