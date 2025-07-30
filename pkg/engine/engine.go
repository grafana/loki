package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/executor"
	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
)

var tracer = otel.Tracer("pkg/engine")

var ErrNotSupported = errors.New("feature not supported in new query engine")

// New creates a new instance of the query engine that implements the [logql.Engine] interface.
func New(opts logql.EngineOpts, bucket objstore.Bucket, limits logql.Limits, reg prometheus.Registerer, logger log.Logger) *QueryEngine {
	var ms metastore.Metastore
	if bucket != nil {
		metastoreBucket := objstore.NewPrefixedBucket(bucket, opts.CataloguePath)
		ms = metastore.NewObjectMetastore(metastoreBucket, logger, reg)
	}

	if opts.BatchSize <= 0 {
		panic(fmt.Sprintf("invalid batch size for query engine. must be greater than 0, got %d", opts.BatchSize))
	}

	return &QueryEngine{
		logger:    logger,
		metrics:   newMetrics(reg),
		limits:    limits,
		metastore: ms,
		bucket:    bucket,
		opts:      opts,
	}
}

// QueryEngine combines logical planning, physical planning, and execution to evaluate LogQL queries.
type QueryEngine struct {
	logger    log.Logger
	metrics   *metrics
	limits    logql.Limits
	metastore metastore.Metastore
	bucket    objstore.Bucket
	opts      logql.EngineOpts
}

// Query implements [logql.Engine].
func (e *QueryEngine) Query(params logql.Params) logql.Query {
	return &queryAdapter{
		engine: e,
		params: params,
	}
}

// Execute executes a LogQL query and returns its results or alternatively an error.
// The execution is done in three steps:
//  1. Create a logical plan from the provided query parameters.
//  2. Create a physical plan from the logical plan using information from the catalog.
//  3. Evaluate the physical plan with the executor.
func (e *QueryEngine) Execute(ctx context.Context, params logql.Params) (logqlmodel.Result, error) {
	ctx, span := tracer.Start(ctx, "QueryEngine.Execute", trace.WithAttributes(
		attribute.String("type", string(logql.GetRangeType(params))),
		attribute.String("query", params.QueryString()),
		attribute.Stringer("start", params.Start()),
		attribute.Stringer("end", params.Start()),
		attribute.Stringer("step", params.Step()),
		attribute.Stringer("length", params.End().Sub(params.Start())),
		attribute.StringSlice("shards", params.Shards()),
	))
	defer span.End()

	var (
		startTime = time.Now()

		durLogicalPlanning  time.Duration
		durPhysicalPlanning time.Duration
		durExecution        time.Duration
	)

	statsCtx, ctx := stats.NewContext(ctx)
	metadataCtx, ctx := metadata.NewContext(ctx)

	logger := utillog.WithContext(ctx, e.logger)
	logger = log.With(logger, "query", params.QueryString(), "shard", strings.Join(params.Shards(), ","), "engine", "v2")

	logicalPlan, err := func() (*logical.Plan, error) {
		_, span := tracer.Start(ctx, "QueryEngine.Execute.logicalPlan")
		defer span.End()

		timer := prometheus.NewTimer(e.metrics.logicalPlanning)
		logicalPlan, err := logical.BuildPlan(params)
		if err != nil {
			level.Warn(logger).Log("msg", "failed to create logical plan", "err", err)
			e.metrics.subqueries.WithLabelValues(statusNotImplemented).Inc()
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to create logical plan")
			return nil, ErrNotSupported
		}

		durLogicalPlanning = timer.ObserveDuration()
		level.Info(logger).Log(
			"msg", "finished logical planning",
			"plan", logicalPlan.String(),
			"duration", durLogicalPlanning.Seconds(),
		)
		span.SetStatus(codes.Ok, "")
		return logicalPlan, nil
	}()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create logical plan")
		return logqlmodel.Result{}, err
	}

	physicalPlan, err := func() (*physical.Plan, error) {
		ctx, span := tracer.Start(ctx, "QueryEngine.Execute.physicalPlan")
		defer span.End()

		timer := prometheus.NewTimer(e.metrics.physicalPlanning)

		catalogueType := physical.CatalogueTypeDirect
		if e.opts.CataloguePath != "" {
			catalogueType = physical.CatalogueTypeIndex
		}
		catalog := physical.NewMetastoreCatalog(ctx, e.metastore, catalogueType)
		planner := physical.NewPlanner(physical.NewContext(params.Start(), params.End()), catalog)
		plan, err := planner.Build(logicalPlan)
		if err != nil {
			level.Warn(logger).Log("msg", "failed to create physical plan", "err", err)
			e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to create physical plan")
			return nil, ErrNotSupported
		}

		plan, err = planner.Optimize(plan)
		if err != nil {
			level.Warn(logger).Log("msg", "failed to optimize physical plan", "err", err)
			e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to optimize physical plan")
			return nil, ErrNotSupported
		}

		durPhysicalPlanning = timer.ObserveDuration()
		level.Info(logger).Log(
			"msg", "finished physical planning",
			"plan", physical.PrintAsTree(plan),
			"duration", durPhysicalPlanning.Seconds(),
		)
		span.SetStatus(codes.Ok, "")
		return plan, nil
	}()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create physical plan")
		return logqlmodel.Result{}, err
	}

	builder, err := func() (ResultBuilder, error) {
		ctx, span := tracer.Start(ctx, "QueryEngine.Execute.Process")
		defer span.End()

		level.Info(logger).Log("msg", "start executing query with new engine")

		timer := prometheus.NewTimer(e.metrics.execution)

		cfg := executor.Config{
			BatchSize:                int64(e.opts.BatchSize),
			Bucket:                   e.bucket,
			DataobjScanPageCacheSize: int64(e.opts.DataobjScanPageCacheSize),
		}
		pipeline := executor.Run(ctx, cfg, physicalPlan, logger)
		defer pipeline.Close()

		var builder ResultBuilder
		switch params.GetExpression().(type) {
		case syntax.LogSelectorExpr:
			builder = newStreamsResultBuilder()
		case syntax.SampleExpr:
			// assume instant query since logical planning would fail for range queries.
			builder = newVectorResultBuilder()
		default:
			// should never happen as we already check the expression type in the logical planner
			panic(fmt.Sprintf("failed to execute. Invalid exprression type (%T)", params.GetExpression()))
		}

		if err := collectResult(ctx, pipeline, builder); err != nil {
			e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to build results")
			return nil, err
		}

		durExecution = timer.ObserveDuration()
		e.metrics.subqueries.WithLabelValues(statusSuccess).Inc()
		span.SetStatus(codes.Ok, "")
		return builder, nil
	}()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to build results")
		return logqlmodel.Result{}, err
	}

	durFull := time.Since(startTime)
	queueTime, _ := ctx.Value(httpreq.QueryQueueTimeHTTPHeader).(time.Duration)
	stats := statsCtx.Result(durFull, queueTime, builder.Len())

	level.Debug(logger).Log(
		"msg", "finished executing with new engine",
		"duration_logical_planning", durLogicalPlanning,
		"duration_physical_planning", durPhysicalPlanning,
		"duration_execution", durExecution,
		"duration_full", durFull,
	)

	metadataCtx.AddWarning("Query was executed using the new experimental query engine and dataobj storage.")
	span.SetStatus(codes.Ok, "")
	return builder.Build(stats, metadataCtx), nil
}

func collectResult(ctx context.Context, pipeline executor.Pipeline, builder ResultBuilder) error {
	for {
		if err := pipeline.Read(ctx); err != nil {
			if errors.Is(err, executor.EOF) {
				break
			}
			return err
		}

		rec, err := pipeline.Value()
		if err != nil {
			return err
		}

		builder.CollectRecord(rec)
		rec.Release()
	}
	return nil
}

var _ logql.Engine = (*QueryEngine)(nil)

// queryAdapter dispatches query execution to the wrapped engine.
type queryAdapter struct {
	params logql.Params
	engine *QueryEngine
}

// Exec implements [logql.Query].
func (q *queryAdapter) Exec(ctx context.Context) (logqlmodel.Result, error) {
	return q.engine.Execute(ctx, q.params)
}

var _ logql.Query = (*queryAdapter)(nil)
