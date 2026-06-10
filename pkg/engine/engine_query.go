package engine

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	"github.com/grafana/loki/v3/pkg/xcap"
)

type query struct {
	id        ulid.ULID
	engine    *Engine
	logger    log.Logger
	queryType string
	useCache  bool

	tenantID  string
	userAgent string
	actorPath string

	startTime  time.Time
	finishTime time.Time

	capture    *xcap.Capture
	rootRegion *xcap.Region

	failed      bool
	failureType string
}

func (e *Engine) newQuery(ctx context.Context, logger log.Logger, queryType string, useCache bool) (*query, context.Context, error) {
	startTime := time.Now()

	ctx, capture := xcap.NewCapture(ctx, nil)
	ctx, region := xcap.StartRegion(ctx, "Query")

	id := ulid.Make()
	q := &query{
		id:        id,
		engine:    e,
		logger:    log.With(logger, "query_id", id),
		queryType: queryType,
		useCache:  useCache,

		userAgent: httpreq.ExtractHeader(ctx, "User-Agent"),
		actorPath: httpreq.ExtractHeader(ctx, httpreq.LokiActorPathHeader),

		startTime: startTime,

		capture:    capture,
		rootRegion: region,
	}
	if err := q.init(ctx); err != nil {
		// The query failed to initialize, close it and return the error.
		q.Close()
		return nil, ctx, err
	}
	return q, ctx, nil
}

func (q *query) init(ctx context.Context) error {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		q.RecordError(ctx, err)
		return httpgrpc.Error(http.StatusBadRequest, err.Error())
	}
	q.tenantID = tenantID
	return nil
}

// Logger returns the logger associated with the query. It has fields attached
// for query identification.
func (q *query) Logger() log.Logger { return q.logger }

// TenantID returns the tenant ID associated with the query.
func (q *query) TenantID() string { return q.tenantID }

// ID returns the unique identifier of the query.
func (q *query) ID() ulid.ULID { return q.id }

// Duration returns the current duration of the query.
func (q *query) Duration() time.Duration {
	if q.finishTime.IsZero() {
		return time.Since(q.startTime)
	}
	return q.finishTime.Sub(q.startTime)
}

// Prepare constructs a workflow from the given physical plan. The returned
// workflow must be closed to release resources.
func (q *query) Prepare(ctx context.Context, plan *physical.Plan) (*workflow.Workflow, error) {
	ctx, span := xcap.StartSpan(ctx, tracer, "query.Prepare")
	defer span.End()

	timer := prometheus.NewTimer(q.engine.metrics.planning.prepare.WithLabelValues(q.queryType))

	opts := workflow.Options{
		ID:     q.id,
		Tenant: q.tenantID,
		Actor:  httpreq.ExtractActorPath(ctx),

		CacheEnabled:                 q.useCache,
		MaxTaskCacheSize:             uint64(q.engine.cfg.Executor.TaskResultsCache.TaskResultMaxCacheableSize),
		MaxDataObjScanCacheSize:      uint64(q.engine.cfg.Executor.TaskResultsCache.DataObjScanResultMaxCacheableSize),
		CacheCompression:             q.engine.cfg.Executor.TaskResultsCache.Compression,
		PruneEmptyCachedTasks:        q.engine.cfg.Executor.TaskResultsCache.PruneEmptyCachedTasks,
		NonEmptyCachedTasksMaxSize:   uint64(q.engine.cfg.Executor.TaskResultsCache.PruneCachedTasksMaxSize),
		PruneCachedTasksFetchTimeout: q.engine.cfg.Executor.TaskResultsCache.PruneCachedTasksFetchTimeout,
		TaskCacheRegistry:            q.engine.taskCaches,

		DebugTasks:   q.engine.limits.DebugEngineTasks(q.tenantID),
		DebugStreams: q.engine.limits.DebugEngineStreams(q.tenantID),
	}
	wf, err := workflow.New(ctx, opts, q.logger, q.engine.scheduler.inner, plan)
	if err != nil {
		level.Warn(q.logger).Log("msg", "failed to create workflow", "err", err)
		q.RecordError(ctx, err)
		return nil, ErrNotSupported
	}

	duration := timer.ObserveDuration()
	span.Record(statPrepareDuration.Observe(int64(duration)))

	// The execution plan can be way more verbose than the physical plan, so we
	// only log it at debug level.
	level.Debug(q.logger).Log(
		"msg", "execution-plan-detail",
		"plan", workflow.Sprint(wf),
	)
	level.Info(q.logger).Log(
		"msg", "execution-plan-summary",
		"duration_ms", duration.Milliseconds(),
		"tasks", wf.Len(),
	)

	span.SetStatus(codes.Ok, "")
	return wf, nil
}

// RecordError records the query as having failed with the given error.
func (q *query) RecordError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())

	switch {
	case errors.Is(err, context.Canceled):
		q.failureType = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		q.failureType = "timeout"
	default:
		q.failureType = "fail"
	}
	q.failed = true
}

// Close finalizes the query and logs a summary line about the query's
// execution.
func (q *query) Close() {
	if !q.finishTime.IsZero() {
		// Already closed.
		return
	}

	q.finishTime = time.Now()
	q.rootRegion.End()
	q.capture.End()

	status := "success"
	if q.failed {
		status = q.failureType
	}

	var (
		logicalPlanDuration, _  = q.capture.Value(statLogicalPlanDuration).Int64()
		physicalPlanDuration, _ = q.capture.Value(statPhysicalPlanDuration).Int64()
		prepareDuration, _      = q.capture.Value(statPrepareDuration).Int64()
		executeDuration, _      = q.capture.Value(statExecutionDuration).Int64()

		otherDuration = calculateResidual(q.finishTime.Sub(q.startTime), logicalPlanDuration, physicalPlanDuration, prepareDuration, executeDuration)
	)

	m := q.engine.metrics

	// The residual histogram mirrors the duration_other_ms field on the
	// query-summary line below and is observed for every query.
	m.query.other.WithLabelValues(q.queryType).Observe(otherDuration.Seconds())

	// Fan-out histograms count tasks per query, matching how printExecutionSummary
	// computes them: tasksPlanned is the count after pruning, and the pre-pruning
	// total is tasksPruned + tasksPlanned.
	//
	// Queries that fail during logical or physical planning never generate any
	// tasks, so observing them would flood the histograms with zeros that say
	// nothing about fan-out. We only observe once at least one task was generated;
	// a query that generated tasks but had them all pruned legitimately records a
	// tasks_per_query of zero.
	var (
		tasksPlanned   = xcap.Value[int64](q.capture, scheduler.StatPlannedTasks)
		tasksPruned    = xcap.Value[int64](q.capture, workflow.StatPrunedTasks)
		tasksGenerated = tasksPruned + tasksPlanned
	)
	if tasksGenerated > 0 {
		m.execution.tasksPerQuery.WithLabelValues(q.queryType).Observe(float64(tasksPlanned))
		m.execution.tasksGenerated.WithLabelValues(q.queryType).Observe(float64(tasksGenerated))
	}

	q.logger.Log(
		"msg", "query-summary",

		"user_agent", q.userAgent,
		"actor_path", q.actorPath,
		"query_type", q.queryType,

		"submitted_at", q.startTime.Format(time.RFC3339Nano),
		"completed_at", q.finishTime.Format(time.RFC3339Nano),
		"duration_ms", q.finishTime.Sub(q.startTime).Milliseconds(),

		"status", status,

		// "duration_cache_check_ms", "", // See comment on query_result_cache_hit for why this is disabled.
		"duration_logical_plan_ms", time.Duration(logicalPlanDuration).Milliseconds(),
		"duration_physical_plan_ms", time.Duration(physicalPlanDuration).Milliseconds(),
		"duration_prepare_ms", time.Duration(prepareDuration).Milliseconds(),
		"duration_execute_ms", time.Duration(executeDuration).Milliseconds(),
		"duration_other_ms", otherDuration.Milliseconds(),

		"dominant_phase", calculateDominantPhase(map[string]time.Duration{
			"logical":  time.Duration(logicalPlanDuration),
			"physical": time.Duration(physicalPlanDuration),
			"prepare":  time.Duration(prepareDuration),
			"execute":  time.Duration(executeDuration),
			"other":    otherDuration,
		}),

		// TODO(rfratto): Move caching logic into the engine so we can track
		// cache hits/misses/latency.
		"query_result_cache_hit", "false",
	)
}

// calculateResidual finds the amount of time in dur that is not accounted for
// in the given stats.
//
// Each stat must be an int64 value.
func calculateResidual(dur time.Duration, stats ...int64) time.Duration {
	var total time.Duration
	for _, s := range stats {
		total += time.Duration(s)
	}
	return dur - total
}

func calculateDominantPhase(phases map[string]time.Duration) string {
	var (
		maxPhase    string
		maxDuration time.Duration
	)
	for phase, duration := range phases {
		if duration > maxDuration {
			maxPhase = phase
			maxDuration = duration
		}
	}
	return maxPhase
}
