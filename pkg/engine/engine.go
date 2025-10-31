package engine

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
)

var (
	// ErrPlanningFailed is returned when query planning fails unexpectedly.
	// ErrPlanningFailed is not used for unimplemented features, which returns
	// [ErrNotSupported] instead.
	ErrPlanningFailed = errors.New("query planning failed unexpectedly")

	// ErrSchedulingFailed is returned when communication with the scheduler fails.
	ErrSchedulingFailed = errors.New("failed to schedule query")
)

// ExecutorConfig configures engine execution.
type ExecutorConfig struct {
	DataobjStorageLag   time.Duration `yaml:"dataobj_storage_lag" category:"experimental"`
	DataobjStorageStart flagext.Time  `yaml:"dataobj_storage_start" category:"experimental"`

	// Batch size of the v2 execution engine.
	BatchSize int `yaml:"batch_size" category:"experimental"`

	// MergePrefetchCount controls the number of inputs that are prefetched simultaneously by any Merge node.
	MergePrefetchCount int `yaml:"merge_prefetch_count" category:"experimental"`

	// RangeConfig determines how to optimize range reads in the V2 engine.
	RangeConfig rangeio.Config `yaml:"range_reads" category:"experimental" doc:"description=Configures how to read byte ranges from object storage when using the V2 engine."`
}

func (cfg *ExecutorConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.BatchSize, prefix+"batch-size", 100, "Experimental: Batch size of the next generation query engine.")
	f.IntVar(&cfg.MergePrefetchCount, prefix+"merge-prefetch-count", 0, "Experimental: The number of inputs that are prefetched simultaneously by any Merge node. A value of 0 means that only the currently processed input is prefetched, 1 means that only the next input is prefetched, and so on. A negative value means that all inputs are be prefetched in parallel.")
	cfg.RangeConfig.RegisterFlags(prefix+"range-reads.", f)

	f.DurationVar(&cfg.DataobjStorageLag, prefix+"dataobj-storage-lag", 1*time.Hour, "Amount of time until data objects are available.")
	f.Var(&cfg.DataobjStorageStart, prefix+"dataobj-storage-start", "Initial date when data objects became available. Format YYYY-MM-DD. If not set, assume data objects are always available no matter how far back.")
}

func (cfg *ExecutorConfig) ValidQueryRange() (time.Time, time.Time) {
	return time.Time(cfg.DataobjStorageStart).UTC(), time.Now().UTC().Add(-cfg.DataobjStorageLag)
}

// Params holds parameters for constructing a new [Engine].
type Params struct {
	Logger     log.Logger            // Logger for optional log messages.
	Registerer prometheus.Registerer // Registerer for optional metrics.

	Config          ExecutorConfig   // Config for the Engine.
	MetastoreConfig metastore.Config // Config for the Metastore.

	Scheduler *Scheduler      // Scheduler to manage the execution of tasks.
	Bucket    objstore.Bucket // Bucket to read stored data from.
	Limits    logql.Limits    // Limits to apply to engine queries.
}

// validate validates p and applies defaults.
func (p *Params) validate() error {
	if p.Logger == nil {
		p.Logger = log.NewNopLogger()
	}
	if p.Registerer == nil {
		p.Registerer = prometheus.NewRegistry()
	}
	if p.Scheduler == nil {
		return errors.New("scheduler is required")
	}
	if p.Config.BatchSize <= 0 {
		return fmt.Errorf("invalid batch size for query engine. must be greater than 0, got %d", p.Config.BatchSize)
	}
	return nil
}

// Engine defines parameters for executing queries.
type Engine struct {
	logger      log.Logger
	metrics     *metrics
	rangeConfig rangeio.Config

	scheduler *Scheduler      // Scheduler to manage the execution of tasks.
	bucket    objstore.Bucket // Bucket to read stored data from.
	limits    logql.Limits    // Limits to apply to engine queries.

	metastore metastore.Metastore
}

// New creates a new Engine.
func New(params Params) (*Engine, error) {
	if err := params.validate(); err != nil {
		return nil, err
	}

	e := &Engine{
		logger:      params.Logger,
		metrics:     newMetrics(params.Registerer),
		rangeConfig: params.Config.RangeConfig,

		scheduler: params.Scheduler,
		bucket:    params.Bucket,
		limits:    params.Limits,
	}

	if e.bucket != nil {
		indexBucket := e.bucket
		if params.MetastoreConfig.IndexStoragePrefix != "" {
			indexBucket = objstore.NewPrefixedBucket(e.bucket, params.MetastoreConfig.IndexStoragePrefix)
		}
		e.metastore = metastore.NewObjectMetastore(indexBucket, e.logger, params.Registerer)
	}

	return e, nil
}

// Execute executes the given query. Execute returns [ErrNotSupported] if params
// denotes a query that is not yet implemented in the new engine.
func (e *Engine) Execute(ctx context.Context, params logql.Params) (logqlmodel.Result, error) {
	// NOTE(rfratto): To simplify the API, Engine does not directly implement
	// [logql.Engine], whose interface definition is not useful to the V2
	// engine. As such, callers must define adapters to use Engine work as a
	// [logql.Engine].
	//
	// This pain point will eventually go away as remaining usages of
	// [logql.Engine] disappear.

	startTime := time.Now()

	ctx, span := tracer.Start(ctx, "Engine.Execute", trace.WithAttributes(
		attribute.String("type", string(logql.GetRangeType(params))),
		attribute.String("query", params.QueryString()),
		attribute.Stringer("start", params.Start()),
		attribute.Stringer("end", params.Start()),
		attribute.Stringer("step", params.Step()),
		attribute.Stringer("length", params.End().Sub(params.Start())),
		attribute.StringSlice("shards", params.Shards()),
	))
	defer span.End()

	ctx = e.buildContext(ctx)
	logger := util_log.WithContext(ctx, e.logger)
	logger = log.With(logger, "engine", "v2", "query", params.QueryString(), "shard", strings.Join(params.Shards(), ","))

	logicalPlan, durLogicalPlanning, err := e.buildLogicalPlan(ctx, logger, params)
	if err != nil {
		e.metrics.subqueries.WithLabelValues(statusNotImplemented).Inc()
		span.SetStatus(codes.Error, "failed to create logical plan")
		return logqlmodel.Result{}, ErrNotSupported
	}

	physicalPlan, durPhysicalPlanning, err := e.buildPhysicalPlan(ctx, logger, params, logicalPlan)
	if err != nil {
		e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
		span.SetStatus(codes.Error, "failed to create physical plan")
		return logqlmodel.Result{}, ErrPlanningFailed
	}

	wf, durWorkflowPlanning, err := e.buildWorkflow(ctx, logger, physicalPlan)
	if err != nil {
		e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
		span.SetStatus(codes.Error, "failed to create execution plan")
		return logqlmodel.Result{}, ErrPlanningFailed
	}

	pipeline, err := wf.Run(ctx)
	if err != nil {
		level.Error(logger).Log("msg", "failed to execute query", "err", err)

		e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
		span.SetStatus(codes.Error, "failed to execute query")
		return logqlmodel.Result{}, ErrSchedulingFailed
	}
	defer pipeline.Close()

	builder, durExecution, err := e.collectResult(ctx, logger, params, pipeline)
	if err != nil {
		e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
		span.SetStatus(codes.Error, "error during query execution")
		return logqlmodel.Result{}, err
	}

	durFull := time.Since(startTime)
	level.Info(logger).Log(
		"msg", "finished executing",
		"duration_logical_planning", durLogicalPlanning,
		"duration_physical_planning", durPhysicalPlanning,
		"duration_workflow_planning", durWorkflowPlanning,
		"duration_execution", durExecution,
		"duration_full", durFull,
	)

	queueTime, _ := ctx.Value(httpreq.QueryQueueTimeHTTPHeader).(time.Duration)
	statsCtx := stats.FromContext(ctx)
	statsCtx.AddQuerierExecTime(durFull)
	stats := statsCtx.Result(durFull, queueTime, builder.Len())
	md := metadata.FromContext(ctx)

	span.SetStatus(codes.Ok, "")
	result := builder.Build(stats, md)

	logql.RecordRangeAndInstantQueryMetrics(ctx, logger, params, strconv.Itoa(http.StatusOK), stats, result.Data)
	return result, nil
}

// buildContext initializes a request-scoped context prior to execution.
func (e *Engine) buildContext(ctx context.Context) context.Context {
	statsContext, ctx := stats.NewContext(ctx)
	metadataContext, ctx := metadata.NewContext(ctx)

	// Inject the range config into the context for any calls to
	// [rangeio.ReadRanges] to make use of.
	ctx = rangeio.WithConfig(ctx, &e.rangeConfig)

	statsContext.SetQueryUsedV2Engine()
	metadataContext.AddWarning("Query was executed using the new experimental query engine and dataobj storage.")
	return ctx
}

// buildLogicalPlan builds a logical plan from the given params.
func (e *Engine) buildLogicalPlan(ctx context.Context, logger log.Logger, params logql.Params) (*logical.Plan, time.Duration, error) {
	span := trace.SpanFromContext(ctx)
	timer := prometheus.NewTimer(e.metrics.logicalPlanning)

	logicalPlan, err := logical.BuildPlan(params)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to create logical plan", "err", err)
		span.RecordError(err)
		return nil, 0, ErrNotSupported
	}

	duration := timer.ObserveDuration()
	level.Info(logger).Log(
		"msg", "finished logical planning",
		"plan", logicalPlan.String(),
		"duration", duration.String(),
	)

	span.AddEvent("finished logical planning", trace.WithAttributes(
		attribute.Stringer("plan", logicalPlan),
		attribute.Stringer("duration", duration),
	))
	return logicalPlan, duration, nil
}

// buildPhysicalPlan builds a physical plan from the given logical plan.
func (e *Engine) buildPhysicalPlan(ctx context.Context, logger log.Logger, params logql.Params, logicalPlan *logical.Plan) (*physical.Plan, time.Duration, error) {
	span := trace.SpanFromContext(ctx)
	timer := prometheus.NewTimer(e.metrics.physicalPlanning)

	// TODO(rfratto): To improve the performance of the physical planner, we
	// may want to parallelize metastore lookups across scheduled tasks as well.
	catalog := physical.NewMetastoreCatalog(ctx, e.metastore)

	// TODO(rfratto): It feels strange that we need to past the start/end time
	// to the physical planner. Isn't it already represented by the logical
	// plan?
	planner := physical.NewPlanner(physical.NewContext(params.Start(), params.End()), catalog)
	physicalPlan, err := planner.Build(logicalPlan)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to create physical plan", "err", err)
		span.RecordError(err)
		return nil, 0, ErrNotSupported
	}

	physicalPlan, err = planner.Optimize(physicalPlan)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to optimize physical plan", "err", err)
		span.RecordError(err)
		return nil, 0, ErrNotSupported
	}

	duration := timer.ObserveDuration()
	level.Info(logger).Log(
		"msg", "finished physical planning",
		"plan", physical.PrintAsTree(physicalPlan),
		"duration", duration.String(),
	)

	span.AddEvent("finished physical planning", trace.WithAttributes(
		attribute.String("plan", physical.PrintAsTree(physicalPlan)),
		attribute.Stringer("duration", duration),
	))
	return physicalPlan, duration, nil
}

// buildWorkflow builds a workflow from the given physical plan.
func (e *Engine) buildWorkflow(ctx context.Context, logger log.Logger, physicalPlan *physical.Plan) (*workflow.Workflow, time.Duration, error) {
	tenantID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("extracting tenant ID: %w", err)
	}

	span := trace.SpanFromContext(ctx)
	timer := prometheus.NewTimer(e.metrics.workflowPlanning)

	wf, err := workflow.New(logger, tenantID, e.scheduler.inner, physicalPlan)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to create workflow", "err", err)
		span.RecordError(err)
		return nil, 0, ErrNotSupported
	}

	duration := timer.ObserveDuration()

	// The execution plan can be way more verbose than the physical plan, so we
	// only log it at debug level.
	level.Debug(logger).Log(
		"msg", "generated execution plan",
		"plan", workflow.Sprint(wf),
	)
	level.Info(logger).Log(
		"msg", "finished execution planning",
		"duration", duration.String(),
	)

	span.AddEvent("finished execution planning", trace.WithAttributes(
		attribute.String("plan", workflow.Sprint(wf)),
		attribute.Stringer("duration", duration),
	))
	return wf, duration, nil
}

// collectResult processes the results of the execution plan.
func (e *Engine) collectResult(ctx context.Context, logger log.Logger, params logql.Params, pipeline executor.Pipeline) (ResultBuilder, time.Duration, error) {
	span := trace.SpanFromContext(ctx)
	timer := prometheus.NewTimer(e.metrics.execution)

	var builder ResultBuilder
	switch params.GetExpression().(type) {
	case syntax.LogSelectorExpr:
		builder = newStreamsResultBuilder(params.Direction())
	case syntax.SampleExpr:
		if params.Step() > 0 {
			builder = newMatrixResultBuilder()
		} else {
			builder = newVectorResultBuilder()
		}
	default:
		// This should never trigger since we already checked the expression
		// type in the logical planner.
		panic(fmt.Sprintf("invalid expression type %T", params.GetExpression()))
	}

	for {
		rec, err := pipeline.Read(ctx)
		if err != nil {
			if errors.Is(err, executor.EOF) {
				break
			}

			level.Warn(logger).Log(
				"msg", "error during execution",
				"err", err,
			)
			return builder, time.Duration(0), err
		}

		builder.CollectRecord(rec)
		rec.Release()
	}

	duration := timer.ObserveDuration()

	// We don't log an event here because a final event will be logged by the
	// caller noting that execution completed.
	span.AddEvent("finished execution", trace.WithAttributes(
		attribute.Stringer("duration", duration),
	))
	return builder, duration, nil
}
