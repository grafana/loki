package engine

import (
	"context"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
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

	tenantID string

	startTime time.Time

	capture    *xcap.Capture
	rootRegion *xcap.Region
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

// Prepare constucts a workflow from the given physical plan. The returned
// workflow must be closed to release resources.
func (q *query) Prepare(ctx context.Context, plan *physical.Plan, useAdmissionLanes bool) (*workflow.Workflow, error) {
	span := trace.SpanFromContext(ctx)
	timer := prometheus.NewTimer(q.engine.metrics.workflowPlanning)

	var maxRunningScanTasks int
	if useAdmissionLanes {
		maxRunningScanTasks = q.engine.limits.MaxScanTaskParallelism(q.tenantID)
	}

	opts := workflow.Options{
		ID:     q.id,
		Tenant: q.tenantID,
		Actor:  httpreq.ExtractActorPath(ctx),

		MaxRunningScanTasks:  maxRunningScanTasks,
		MaxRunningOtherTasks: 0,

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
	wf, err := workflow.New(opts, q.logger, q.engine.scheduler.inner, plan)
	if err != nil {
		level.Warn(q.logger).Log("msg", "failed to create workflow", "err", err)
		q.RecordError(ctx, err)
		return nil, ErrNotSupported
	}

	duration := timer.ObserveDuration()

	// The execution plan can be way more verbose than the physical plan, so we
	// only log it at debug level.
	level.Debug(q.logger).Log(
		"msg", "generated execution plan",
		"plan", workflow.Sprint(wf),
	)
	level.Info(q.logger).Log(
		"msg", "finished execution planning",
		"duration", duration.String(),
		"tasks", wf.Len(),
	)

	span.AddEvent("finished execution planning", trace.WithAttributes(attribute.Stringer("duration", duration)))
	return wf, nil
}

// RecordError records the query as having failed with the given error.
func (q *query) RecordError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())

	// TODO(rfratto): save somewhere else?
}

// Close finalizes the query and logs a summary line about the query's
// execution.
func (q *query) Close() {
	// TODO(rfratto): emit summary. This depends on having stats for known major
	// phases about queries that we reference here.
	q.rootRegion.End()
	q.capture.End()
}
