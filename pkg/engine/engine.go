package engine

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/executor"
	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
)

var (
	ErrNotSupported = errors.New("feature not supported in new query engine")
)

// New creates a new instance of the query engine that implements the [logql.Engine] interface.
func New(opts logql.EngineOpts, bucket objstore.Bucket, limits logql.Limits, reg prometheus.Registerer, logger log.Logger) *QueryEngine {

	var ms metastore.Metastore
	if bucket != nil {
		ms = metastore.NewObjectMetastore(bucket, logger)
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
	start := time.Now()

	logger := utillog.WithContext(ctx, e.logger)
	logger = log.With(logger, "query", params.QueryString(), "shard", strings.Join(params.Shards(), ","), "engine", "v2")

	t := time.Now() // start stopwatch for logical planning
	logicalPlan, err := logical.BuildPlan(params)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to create logical plan", "err", err)
		e.metrics.subqueries.WithLabelValues(statusNotImplemented).Inc()
		return logqlmodel.Result{}, ErrNotSupported
	}
	e.metrics.logicalPlanning.Observe(time.Since(t).Seconds())
	durLogicalPlanning := time.Since(t)

	level.Info(logger).Log(
		"msg", "finished logical planning",
		"plan", base64.StdEncoding.EncodeToString([]byte(logicalPlan.String())),
		"duration", durLogicalPlanning.Seconds(),
	)

	t = time.Now() // start stopwatch for physical planning
	statsCtx, ctx := stats.NewContext(ctx)
	catalog := physical.NewMetastoreCatalog(ctx, e.metastore)
	planner := physical.NewPlanner(physical.NewContext(params.Start(), params.End()), catalog)
	plan, err := planner.Build(logicalPlan)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to create physical plan", "err", err)
		e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
		return logqlmodel.Result{}, ErrNotSupported
	}
	plan, err = planner.Optimize(plan)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to optimize physical plan", "err", err)
		e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
		return logqlmodel.Result{}, ErrNotSupported
	}
	e.metrics.physicalPlanning.Observe(time.Since(t).Seconds())
	durPhysicalPlanning := time.Since(t)

	level.Info(logger).Log(
		"msg", "finished physical planning",
		"plan", base64.StdEncoding.EncodeToString([]byte(physical.PrintAsTree(plan))),
		"duration", durLogicalPlanning.Seconds(),
	)

	level.Info(logger).Log(
		"msg", "start executing query with new engine",
	)

	t = time.Now() // start stopwatch for execution
	cfg := executor.Config{
		BatchSize: int64(e.opts.BatchSize),
		Bucket:    e.bucket,
	}
	pipeline := executor.Run(ctx, cfg, plan)
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
		return logqlmodel.Result{}, err
	}

	builder.SetStats(statsCtx.Result(time.Since(start), 0, builder.Len()))

	e.metrics.subqueries.WithLabelValues(statusSuccess).Inc()
	e.metrics.execution.Observe(time.Since(t).Seconds())
	durExecution := time.Since(t)

	level.Debug(logger).Log(
		"msg", "finished executing with new engine",
		"duration_logical_planning", durLogicalPlanning,
		"duration_physical_planning", durPhysicalPlanning,
		"duration_execution", durExecution,
	)

	return builder.Build(), nil
}

func collectResult(_ context.Context, pipeline executor.Pipeline, builder ResultBuilder) error {
	for {
		if err := pipeline.Read(); err != nil {
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
