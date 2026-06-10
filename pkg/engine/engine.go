package engine

import (
	"context"
	"errors"
	"flag"
	"fmt"
	gotrace "runtime/trace"
	"strings"
	"time"

	"github.com/alecthomas/units"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/engine/internal/deletion"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/schedulerstat"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var (
	// ErrPlanningFailed is returned when query planning fails unexpectedly.
	// ErrPlanningFailed is not used for unimplemented features, which returns
	// [ErrNotSupported] instead.
	ErrPlanningFailed = errors.New("query planning failed unexpectedly")

	// ErrSchedulingFailed is returned when communication with the scheduler fails.
	ErrSchedulingFailed = errors.New("failed to schedule query")
)

var tracer = otel.Tracer("pkg/engine")

// Re-export internal types for external use.
type (
	// RequestStreamFilterer creates a StreamFilterer for a given request context.
	RequestStreamFilterer = executor.RequestStreamFilterer

	// StreamFilterer filters streams based on their labels.
	StreamFilterer = executor.StreamFilterer
)

// ExecutorConfig configures engine execution.
type ExecutorConfig struct {
	// Batch size of the v2 execution engine.
	BatchSize int `yaml:"batch_size" category:"experimental"`

	// PrefetchBytes controls the number of bytes read ahead from a data object
	// when opening it.
	PrefetchBytes flagext.Bytes `yaml:"prefetch_bytes" category:"experimental"`

	// MergePrefetchCount controls the number of inputs that are prefetched simultaneously by any Merge node.
	MergePrefetchCount int `yaml:"merge_prefetch_count" category:"experimental"`

	// RangeConfig determines how to optimize range reads in the V2 engine.
	RangeConfig rangeio.Config `yaml:"range_reads" category:"experimental" doc:"description=Configures how to read byte ranges from object storage when using the V2 engine."`

	// StreamFilterer is an optional filterer that can filter streams based on their labels.
	// When set, streams are filtered before scanning.
	StreamFilterer executor.RequestStreamFilterer `yaml:"-"`

	// TaskResultsCache configures the backing cache for task results.
	TaskResultsCache TaskCacheConfig `yaml:"task_results_cache" category:"experimental"`
}

func (cfg *ExecutorConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.PrefetchBytes = flagext.Bytes(16 * units.KiB)

	f.IntVar(&cfg.BatchSize, prefix+"batch-size", 100, "Experimental: Batch size of the next generation query engine.")
	f.Var(&cfg.PrefetchBytes, prefix+"prefetch-bytes", "Experimental: Number of bytes to prefetch when opening a data object for decoding metadata and overlapping section reads. Clamps to at least 16KiB.")
	f.IntVar(&cfg.MergePrefetchCount, prefix+"merge-prefetch-count", 0, "Experimental: The number of inputs that are prefetched simultaneously by any Merge node. A value of 0 means that only the currently processed input is prefetched, 1 means that only the next input is prefetched, and so on. A negative value means that all inputs are be prefetched in parallel.")
	cfg.RangeConfig.RegisterFlags(prefix+"range-reads.", f)
	cfg.TaskResultsCache.RegisterFlagsWithPrefix(prefix+"task-results-cache.", f)
}

// TaskCacheConfig extends resultscache.Config with additional task-cache-specific settings.
type TaskCacheConfig struct {
	resultscache.Config               `yaml:",inline"`
	TaskResultMaxCacheableSize        flagext.Bytes `yaml:"task_result_max_cacheable_size" category:"experimental"`
	DataObjScanResultMaxCacheableSize flagext.Bytes `yaml:"dataobjscan_result_max_cacheable_size" category:"experimental"`
	PruneEmptyCachedTasks             bool          `yaml:"prune_empty_cached_tasks" category:"experimental"`
	PruneCachedTasksMaxSize           flagext.Bytes `yaml:"prune_cached_tasks_max_size" category:"experimental"`
	PruneCachedTasksFetchTimeout      time.Duration `yaml:"prune_cached_tasks_fetch_timeout" category:"experimental"`
}

// RegisterFlagsWithPrefix registers flags for TaskCacheConfig with the given prefix.
func (cfg *TaskCacheConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.Config.RegisterFlagsWithPrefix(f, prefix)
	f.Var(&cfg.TaskResultMaxCacheableSize, prefix+"task-result-max-cacheable-size",
		"Experimental: Maximum size for a task result to be cacheable. 0 means only empty responses are cached.")
	f.Var(&cfg.DataObjScanResultMaxCacheableSize, prefix+"dataobjscan-result-max-cacheable-size",
		"Experimental: Maximum size for a DataObjScan result to be cacheable. 0 means only empty responses are cached.")
	f.BoolVar(&cfg.PruneEmptyCachedTasks, prefix+"prune-empty-cached-tasks", false,
		"Experimental: When enabled, the scheduler checks cached results at plan time and prunes tasks whose cached result is known to be empty.")
	f.Var(&cfg.PruneCachedTasksMaxSize, prefix+"prune-cached-tasks-max-size",
		"Experimental: Maximum total size of non-empty cached task results embedded in task assignments. "+
			"Results that would exceed the budget are skipped (smaller results that fit are still included). "+
			"0 disables non-empty task pruning.")
	f.DurationVar(&cfg.PruneCachedTasksFetchTimeout, prefix+"prune-cached-tasks-fetch-timeout", time.Second,
		"Experimental: Timeout for cache fetch operations during cached-task pruning at plan time. 0 disables the timeout.")
}

// Params holds parameters for constructing a new [Engine].
type Params struct {
	Logger     log.Logger            // Logger for optional log messages.
	Registerer prometheus.Registerer // Registerer for optional metrics.

	Config Config // Config for the Engine.

	Scheduler    *Scheduler          // Scheduler to manage the execution of tasks.
	Metastore    metastore.Metastore // Metastore to access the indexes
	Limits       logql.Limits        // Limits to apply to engine queries.
	DeleteGetter deletion.Getter     // DeleteGetter to fetch delete requests for query-time filtering.
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
	if p.Metastore == nil {
		return errors.New("metastore is required")
	}
	if p.Config.Executor.BatchSize <= 0 {
		return fmt.Errorf("invalid batch size for query engine. must be greater than 0, got %d", p.Config.Executor.BatchSize)
	}
	return nil
}

// Engine defines parameters for executing queries.
type Engine struct {
	logger  log.Logger
	metrics *metrics
	cfg     Config

	scheduler    *Scheduler      // Scheduler to manage the execution of tasks.
	limits       logql.Limits    // Limits to apply to engine queries.
	deleteGetter deletion.Getter // DeleteGetter to fetch delete requests for query-time filtering.

	metastore  metastore.Metastore
	taskCaches executor.TaskCacheRegistry
}

// New creates a new Engine.
func New(params Params) (*Engine, error) {
	if err := params.validate(); err != nil {
		return nil, err
	}

	var taskCaches executor.TaskCacheRegistry
	if params.Config.Executor.TaskResultsCache.PruneEmptyCachedTasks {
		var err error
		taskCaches, err = executor.NewTaskCacheRegistry(params.Config.Executor.TaskResultsCache.Config, params.Registerer, params.Logger)
		if err != nil {
			return nil, fmt.Errorf("creating task cache registry: %w", err)
		}
	}

	e := &Engine{
		logger:  params.Logger,
		metrics: newMetrics(params.Registerer),
		cfg:     params.Config,

		scheduler:    params.Scheduler,
		limits:       params.Limits,
		deleteGetter: params.DeleteGetter,

		metastore:  params.Metastore,
		taskCaches: taskCaches,
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

	cacheEnabled := cache.IsCacheConfigured(e.cfg.Executor.TaskResultsCache.CacheConfig) && !params.CachingOptions().Disabled

	ctx = e.buildContext(ctx)
	logger := util_log.WithContext(ctx, e.logger)
	logger = log.With(logger, "engine", "v2")
	logger = injectQueryTags(ctx, logger)

	q, ctx, err := e.newQuery(ctx, logger, logqlQueryType(params.GetExpression()), cacheEnabled)
	if err != nil {
		return logqlmodel.Result{}, err
	}
	defer q.Close()

	// We save the pre-query logger so we can pass it to buildPhysicalPlan,
	// which creates a subquery.
	engineLogger := logger
	logger = q.Logger()

	level.Info(logger).Log(
		"msg", "logql-query-start",

		"type", string(logql.GetRangeType(params)),
		"query", params.QueryString(),
		"start", params.Start().Format(time.RFC3339Nano),
		"end", params.End().Format(time.RFC3339Nano),
		"step_ms", params.Step().Milliseconds(),
		"length_ms", params.End().Sub(params.Start()).Milliseconds(),
		"shards", strings.Join(params.Shards(), ","),
		"cache_enabled", cacheEnabled,
	)

	e.metrics.observeLogQLShape(q.queryType, logqlShapeOf(params.QueryString(), params.GetExpression()))

	ctx, task := gotrace.NewTask(ctx, "Engine.Execute")
	defer task.End()

	ctx, span := xcap.StartSpan(ctx, tracer, "Engine.Execute",
		trace.WithAttributes(
			attribute.Stringer("query_id", q.id),
			attribute.String("type", string(logql.GetRangeType(params))),
			attribute.String("query", params.QueryString()),
			attribute.Stringer("start", params.Start()),
			attribute.Stringer("end", params.End()),
			attribute.Stringer("step", params.Step()),
			attribute.Stringer("length", params.End().Sub(params.Start())),
			attribute.StringSlice("shards", params.Shards()),
			attribute.Bool("cache_enabled", cacheEnabled),
		),
	)
	defer span.End()

	logicalPlan, err := e.buildLogicalPlan(ctx, q, params)
	if err != nil {
		e.metrics.query.subqueries.WithLabelValues(statusNotImplemented, q.queryType).Inc()
		e.metrics.query.stageFailures.WithLabelValues(stageLogicalPlanning, statusNotImplemented, q.queryType).Inc()
		q.RecordError(ctx, errors.New("failed to create logical plan"))
		return logqlmodel.Result{}, ErrNotSupported
	}
	gotrace.Log(ctx, "logical_planning", "done")

	catalog := physical.NewMetastoreCatalog(e.metastoreSectionsResolver(ctx, q, engineLogger, cacheEnabled))
	physicalPlan, err := e.buildPhysicalPlan(ctx, q, params, catalog, logicalPlan)
	if err != nil {
		e.metrics.query.subqueries.WithLabelValues(statusFailure, q.queryType).Inc()
		e.metrics.query.stageFailures.WithLabelValues(stagePhysicalPlanning, statusFailure, q.queryType).Inc()
		q.RecordError(ctx, errors.New("failed to create physical plan"))
		return logqlmodel.Result{}, ErrPlanningFailed
	}
	gotrace.Log(ctx, "physical_planning", "done")

	// Enable admission lanes only for log queries
	useAdmissionLanes := !isMetricQuery(params.GetExpression())

	wf, err := q.Prepare(ctx, physicalPlan, useAdmissionLanes)
	if err != nil {
		e.metrics.query.subqueries.WithLabelValues(statusFailure, q.queryType).Inc()
		e.metrics.query.stageFailures.WithLabelValues(stagePrepare, statusFailure, q.queryType).Inc()
		q.RecordError(ctx, errors.New("failed to create execution plan"))
		return logqlmodel.Result{}, ErrPlanningFailed
	}
	defer wf.Close()
	gotrace.Log(ctx, "workflow_planning", "done")

	pipeline, err := wf.Run(ctx)
	if err != nil {
		level.Error(logger).Log("msg", "failed to execute query", "err", err)

		e.metrics.query.subqueries.WithLabelValues(statusFailure, q.queryType).Inc()
		e.metrics.query.stageFailures.WithLabelValues(stageExecution, statusFailure, q.queryType).Inc()
		q.RecordError(ctx, errors.New("failed to execute query"))
		return logqlmodel.Result{}, ErrSchedulingFailed
	}
	defer pipeline.Close()

	gotrace.Log(ctx, "collect_result", "start")
	builder, err := e.execute(ctx, q, params, pipeline)
	if err != nil {
		e.metrics.query.subqueries.WithLabelValues(statusFailure, q.queryType).Inc()
		e.metrics.query.stageFailures.WithLabelValues(stageExecution, statusFailure, q.queryType).Inc()
		q.RecordError(ctx, errors.New("error during query execution"))
		return logqlmodel.Result{}, err
	}
	gotrace.Log(ctx, "collect_result", "done")

	// Close the pipeline to calculate the stats.
	pipeline.Close()
	span.SetStatus(codes.Ok, "")

	// explicitly call End() before exporting even though we have a defer above.
	// It is safe to call End() multiple times.
	span.End()
	q.Close()

	totalQueueTime := xcap.Value[int64](q.capture, schedulerstat.TaskQueueDuration)
	stats := q.capture.ToStatsSummary(q.Duration(), time.Duration(totalQueueTime), builder.Len())
	result := builder.Build(stats, metadata.FromContext(ctx))
	return result, nil
}

func logqlQueryType(expr syntax.Expr) string {
	_, ok := expr.(syntax.SampleExpr)
	if ok {
		return "metrics"
	}
	return "logs"
}

// buildContext initializes a request-scoped context prior to execution.
func (e *Engine) buildContext(ctx context.Context) context.Context {
	metadataContext, ctx := metadata.NewContext(ctx)

	// Inject the range config into the context for any calls to
	// [rangeio.ReadRanges] to make use of.
	ctx = rangeio.WithConfig(ctx, &e.cfg.Executor.RangeConfig)

	metadataContext.AddWarning("Query was executed using the next-generation Loki query engine.")
	return ctx
}

// injectQueryTags adds query tags as key-value pairs from the context into the
// given logger, if they have been defined via [httpreq.InjectQueryTags].
// Otherwise, the original logger is returned unmodified.
func injectQueryTags(ctx context.Context, logger log.Logger) log.Logger {
	tags := httpreq.ExtractQueryTagsFromContext(ctx)
	if len(tags) == 0 {
		return logger
	}
	return log.With(logger, httpreq.TagsToKeyValues(tags)...)
}

// isMetricQuery returns true if the given expression is a metric query,
// false if it is a log query.
func isMetricQuery(expr syntax.Expr) bool {
	_, ok := expr.(syntax.SampleExpr)
	return ok
}

// buildLogicalPlan builds a logical plan from the given params.
func (e *Engine) buildLogicalPlan(ctx context.Context, q *query, params logql.Params) (*logical.Plan, error) {
	ctx, span := xcap.StartSpan(ctx, tracer, "Engine.buildLogicalPlan",
		trace.WithAttributes(attribute.Stringer("query_id", q.id)),
	)
	defer span.End()

	timer := prometheus.NewTimer(e.metrics.planning.logical.WithLabelValues(q.queryType))

	var deleteReqs []*deletion.Request
	if e.deleteGetter != nil {
		var err error
		deleteReqs, err = deletion.DeletesForUser(ctx, params.Start(), params.End(), e.deleteGetter)
		if err != nil {
			q.RecordError(ctx, err)
			return nil, fmt.Errorf("failed to get delete requests: %w", err)
		}
	}

	logicalPlan, err := logical.BuildPlanWithDeletes(ctx, params, deleteReqs)
	if err != nil {
		level.Warn(q.Logger()).Log("msg", "failed to create logical plan", "err", err)
		q.RecordError(ctx, err)
		return nil, ErrNotSupported
	}

	var opt logical.Optimizer
	err = opt.Optimize(logicalPlan)
	passes := opt.Report()
	e.metrics.recordLogicalPasses(passes)
	if err != nil {
		level.Warn(q.Logger()).Log("msg", "failed to optimize logical plan", "err", err)
		q.RecordError(ctx, err)
		return nil, ErrNotSupported
	}

	duration := timer.ObserveDuration()
	span.Record(statLogicalPlanDuration.Observe(int64(duration)))

	level.Info(q.logger).Log(
		"msg", "logical-plan-summary",

		"plan", logicalPlan.String(),
		"duration_ms", duration.Milliseconds(),
		"passes_fired", encodeLogicalPasses(passes),
	)
	span.SetAttributes(
		attribute.Stringer("plan", logicalPlan),
	)

	span.SetStatus(codes.Ok, "")
	return logicalPlan, nil
}

// buildPhysicalPlan builds a physical plan from the given logical plan.
func (e *Engine) buildPhysicalPlan(ctx context.Context, q *query, params logql.Params, catalog *physical.MetastoreCatalog, logicalPlan *logical.Plan) (*physical.Plan, error) {
	ctx, span := xcap.StartSpan(ctx, tracer, "Engine.buildPhysicalPlan",
		trace.WithAttributes(attribute.Stringer("query_id", q.id)),
	)
	defer span.End()

	timer := prometheus.NewTimer(e.metrics.planning.physical.WithLabelValues(q.queryType))

	// TODO(rfratto): It feels strange that we need to past the start/end time
	// to the physical planner. Isn't it already represented by the logical
	// plan?
	plannerCtx := physical.NewContext(params.Start(), params.End())

	// Get the tenant's MaxQuerySeries limit and pass it to the planner context if enforcement is enabled
	if e.cfg.EnforceQuerySeriesLimit {
		plannerCtx = plannerCtx.WithMaxQuerySeries(e.limits.MaxQuerySeries(ctx, q.TenantID()))
	}

	planner := physical.NewPlanner(plannerCtx, catalog)
	physicalPlan, err := planner.Build(logicalPlan)
	if err != nil {
		level.Warn(q.Logger()).Log("msg", "failed to create physical plan", "err", err)
		q.RecordError(ctx, err)
		return nil, ErrNotSupported
	}

	var rules map[string]bool
	{
		optimizeStart := time.Now()

		physicalPlan, err = planner.Optimize(physicalPlan)
		if err != nil {
			level.Warn(q.Logger()).Log("msg", "failed to optimize physical plan", "err", err)
			q.RecordError(ctx, err)
			return nil, ErrNotSupported
		}
		rules = planner.FiredRules()
		e.metrics.recordPhysicalRules(rules)

		optimizeDuration := time.Since(optimizeStart)
		span.Record(statPhysicalOptimizeDuration.Observe(int64(optimizeDuration)))
	}

	physicalPlan, err = physical.WrapWithBatching(physicalPlan, e.cfg.Executor.BatchSize)
	if err != nil {
		level.Warn(q.Logger()).Log("msg", "failed to wrap physical plan with batching", "err", err)
		q.RecordError(ctx, err)
		return nil, ErrNotSupported
	}

	duration := timer.ObserveDuration()
	span.Record(statPhysicalPlanDuration.Observe(int64(duration)))

	e.metrics.observePhysicalShape(physicalPlanShapeOf(physicalPlan))

	printPhysicalPlanSummary(q, physicalPlan, duration, rules)
	span.SetAttributes(
		attribute.String("plan", physical.PrintAsTree(physicalPlan)),
	)

	span.SetStatus(codes.Ok, "")
	return physicalPlan, nil
}

// printExecutionSummary prints a general summary of the execution, including
// timing and cache information.
func printPhysicalPlanSummary(q *query, plan *physical.Plan, duration time.Duration, rules map[string]bool) {
	var (
		indexQueryDuration, _ = q.capture.Value(statPhysicalIndexQueryDuration).Int64()
		optimizeDuration, _   = q.capture.Value(statPhysicalOptimizeDuration).Int64()

		otherDuration = calculateResidual(duration, indexQueryDuration, optimizeDuration)
	)

	level.Info(q.Logger()).Log(
		"msg", "physical-plan-summary",

		// Plan stats.
		"duration_ms", duration.Milliseconds(),
		"plan_node_count", plan.Len(),

		// Timing info
		"duration_index_query_ms", time.Duration(indexQueryDuration).Milliseconds(),
		"duration_optimize_ms", time.Duration(optimizeDuration).Milliseconds(),
		"duration_other_ms", otherDuration.Milliseconds(),

		"dominant_phase", calculateDominantPhase(map[string]time.Duration{
			"index_query": time.Duration(indexQueryDuration),
			"optimize":    time.Duration(optimizeDuration),
			"other":       otherDuration,
		}),

		"rules_fired", encodePhysicalRules(rules),

		// Large plans result in log line truncation, retain the other
		// kvs by moving this to the end.
		"plan", physical.PrintAsTree(plan),
	)
}

func (e *Engine) metastoreSectionsResolver(ctx context.Context, parent *query, logger log.Logger, cacheEnabled bool) physical.MetastoreSectionsResolver {
	planner := physical.NewMetastorePlanner(e.metastore, e.cfg.Executor.BatchSize)
	logger = log.With(logger, "subcomponent", "metastore")

	if parent != nil {
		logger = log.With(logger, "parent_query_id", parent.ID())
	}

	return func(selector physical.Expression, predicates []physical.Expression, start time.Time, end time.Time) ([]*metastore.DataobjSectionDescriptor, error) {
		q, ctx, err := e.newQuery(ctx, logger, "index", cacheEnabled)
		if err != nil {
			return nil, fmt.Errorf("creating index query: %w", err)
		}
		defer func() {
			q.Close()

			// Record how long this query took to execute on the parent query.
			if parent != nil {
				queryTime := q.Duration()
				parent.rootRegion.Record(statPhysicalIndexQueryDuration.Observe(int64(queryTime)))
			}
		}()

		var plan *physical.Plan
		{
			timer := prometheus.NewTimer(e.metrics.planning.physical.WithLabelValues(q.queryType))

			plan, err = planner.Plan(ctx, selector, predicates, start, end)
			if err != nil {
				q.RecordError(ctx, err)
				return nil, fmt.Errorf("index query: build plan: %w", err)
			}

			duration := timer.ObserveDuration()
			q.rootRegion.Record(statPhysicalPlanDuration.Observe(int64(duration)))

			// Index queries plan via planner.Plan and don't run the rule-based
			// optimizer, so there are no rule firings to report here.
			printPhysicalPlanSummary(q, plan, duration, nil)
		}

		// Disable admission lanes for metastore queries
		useAdmissionLanes := false
		wf, err := q.Prepare(ctx, plan, useAdmissionLanes)
		if err != nil {
			q.RecordError(ctx, err)
			return nil, fmt.Errorf("index query: build workflow: %w", err)
		}
		defer wf.Close()

		pipeline, err := wf.Run(ctx)
		if err != nil {
			q.RecordError(ctx, err)
			return nil, fmt.Errorf("index query: run workflow: %w", err)
		}
		reader := executor.TranslateEOF(pipeline)
		defer reader.Close()

		if err := reader.Open(ctx); err != nil {
			q.RecordError(ctx, err)
			return nil, fmt.Errorf("index query: open pipeline: %w", err)
		}

		var resp metastore.CollectSectionsResponse
		{
			timer := prometheus.NewTimer(e.metrics.execution.duration.WithLabelValues(q.queryType))

			resp, err = e.metastore.CollectSections(ctx, metastore.CollectSectionsRequest{
				// externalize EOFs returned by executor pipelines (executor.EOF -> io.EOF)
				// because metastore is not aware about executor implementation details
				Reader: reader,
			})
			if err != nil {
				q.RecordError(ctx, err)
				return nil, fmt.Errorf("index query: collect sections: %w", err)
			}

			executionDuration := timer.ObserveDuration()
			q.rootRegion.Record(statExecutionDuration.Observe(int64(executionDuration)))

			printExecutionSummary(q, executionDuration)
		}

		sectionsResolved := len(resp.SectionsResponse.Sections)
		printMetastoreLocalitySummary(q, sectionsResolved)

		if parent != nil {
			parent.rootRegion.Record(xcap.StatMetastoreSectionsResolved.Observe(int64(sectionsResolved)))
		}

		return resp.SectionsResponse.Sections, nil
	}
}

func printMetastoreLocalitySummary(q *query, sectionsResolved int) {
	var (
		tocTables          = xcap.Value[int64](q.capture, metastore.StatMetastoreTocTables)
		indexObjects       = xcap.Value[int64](q.capture, xcap.StatMetastoreIndexObjects)
		sectionsOpened     = xcap.Value[int64](q.capture, metastore.StatMetastorePointerSectionsOpened)
		sectionsProductive = xcap.Value[int64](q.capture, metastore.StatMetastorePointerSectionsProductive)

		pagesTotal    = xcap.ValueFromRegion[int64](q.capture, postings.RegionPrefix, dataobj.StatPostingsColumnNamePagesTotal)
		pagesRelevant = xcap.ValueFromRegion[int64](q.capture, postings.RegionPrefix, dataobj.StatPostingsColumnNameRelevantPages)
		pageRuns      = xcap.ValueFromRegion[int64](q.capture, postings.RegionPrefix, dataobj.StatPostingsColumnNamePageRuns)
	)

	level.Info(q.Logger()).Log(
		"msg", "metastore-locality-summary",
		"toc_tables", tocTables,
		"index_objects", indexObjects,
		"index_sections_opened", sectionsOpened,
		"index_sections_productive", sectionsProductive,
		"logs_sections_resolved", sectionsResolved,
		"postings_column_name_pages_total", pagesTotal,
		"postings_column_name_pages_relevant", pagesRelevant,
		"postings_column_name_page_runs", pageRuns,
	)
}

// execute reads from the pipeline and produces a result.
func (e *Engine) execute(ctx context.Context, q *query, params logql.Params, pipeline executor.Pipeline) (_ ResultBuilder, err error) {
	ctx, span := xcap.StartSpan(ctx, tracer, "Engine.execute",
		trace.WithAttributes(attribute.Stringer("query_id", q.id)),
	)
	defer span.End()

	// Classify any non-EOF failure returned from result collection. EOF is
	// handled inline and never escapes as an error, so a non-nil err here is
	// always a real failure.
	defer func() {
		if err != nil {
			e.metrics.execution.resultErrors.WithLabelValues(resultErrorClass(err)).Inc()
		}
	}()

	executeStart := time.Now()
	timer := prometheus.NewTimer(e.metrics.execution.duration.WithLabelValues(q.queryType))

	var builder ResultBuilder
	switch params.GetExpression().(type) {
	case syntax.LogSelectorExpr:
		encodingFlags := httpreq.ExtractEncodingFlagsFromCtx(ctx)
		builder = newStreamsResultBuilder(params.Direction(), encodingFlags.Has(httpreq.FlagCategorizeLabels))
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

	if err := pipeline.Open(ctx); err != nil {
		return nil, err
	}

	var (
		totalReadBatchTime    time.Duration
		totalProcessBatchTime time.Duration
		totalBatches          int64
		totalBytes            int64

		gotFirstBatch    bool
		timeToFirstBatch time.Duration
	)

	for {
		startTime := time.Now()

		rec, err := pipeline.Read(ctx)
		totalReadBatchTime += time.Since(startTime)
		if err != nil {
			if errors.Is(err, executor.EOF) {
				break
			}

			level.Warn(q.Logger()).Log(
				"msg", "error during execution",
				"err", err,
			)
			return builder, err
		}

		if !gotFirstBatch {
			gotFirstBatch = true
			timeToFirstBatch = time.Since(executeStart)
		}
		totalBatches++
		totalBytes += recordBatchBytes(rec)

		startTime = time.Now()
		builder.CollectRecord(rec)
		totalProcessBatchTime += time.Since(startTime)
	}

	duration := timer.ObserveDuration()
	span.Record(statExecutionDuration.Observe(int64(duration)))
	span.Record(statReadBatchDuration.Observe(int64(totalReadBatchTime)))
	span.Record(statProcessBatchDuration.Observe(int64(totalProcessBatchTime)))

	// Result-collection observability, observed once per query.
	e.metrics.execution.resultRows.Observe(float64(builder.Len()))
	e.metrics.execution.timeToFirstBatch.WithLabelValues(q.queryType).Observe(timeToFirstBatch.Seconds())
	e.metrics.execution.resultGetBatch.Observe(totalReadBatchTime.Seconds())
	e.metrics.execution.resultProcess.Observe(totalProcessBatchTime.Seconds())
	e.metrics.execution.resultSizeBytes.Observe(float64(totalBytes))
	e.metrics.execution.batchesAccumulated.Add(float64(totalBatches))
	e.metrics.execution.bytesAccumulated.Add(float64(totalBytes))

	// Mirror the per-query result stats onto the capture so they can be
	// surfaced on the execution-summary log line.
	span.Record(statResultRows.Observe(int64(builder.Len())))
	span.Record(statTimeToFirstBatch.Observe(int64(timeToFirstBatch)))
	span.Record(statResultSizeBytes.Observe(totalBytes))

	printExecutionSummary(q, duration)
	printLogLocalitySummary(q)

	span.SetStatus(codes.Ok, "")
	return builder, nil
}

// recordBatchBytes returns the total in-memory size in bytes of all column
// buffers in a RecordBatch.
func recordBatchBytes(rec arrow.RecordBatch) int64 {
	var n int64
	for i := 0; i < int(rec.NumCols()); i++ {
		n += int64(rec.Column(i).Data().SizeInBytes())
	}
	return n
}

// resultErrorClass classifies a result-collection error into one of a small,
// bounded set of label values to keep metric cardinality fixed. Unrecognized
// errors are reported as "other".
func resultErrorClass(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	default:
		return "other"
	}
}

// printExecutionSummary prints a general summary of the execution, including
// timing and cache information.
//
// Stats that are not reported in the capture are left empty.
func printExecutionSummary(q *query, duration time.Duration) {
	var (
		readBatchDuration, _    = q.capture.Value(statReadBatchDuration).Int64()
		processBatchDuration, _ = q.capture.Value(statProcessBatchDuration).Int64()
		otherDuration           = calculateResidual(duration, readBatchDuration, processBatchDuration)

		logSections = xcap.Value[int64](q.capture, xcap.StatMetastoreSectionsResolved)

		resultRows, _       = q.capture.Value(statResultRows).Int64()
		timeToFirstBatch, _ = q.capture.Value(statTimeToFirstBatch).Int64()
		resultSizeBytes, _  = q.capture.Value(statResultSizeBytes).Int64()

		tasksPruned           = xcap.Value[int64](q.capture, workflow.StatPrunedTasks)
		tasksPlanned          = xcap.Value[int64](q.capture, scheduler.StatPlannedTasks)
		tasksQueued           = xcap.Value[int64](q.capture, scheduler.StatQueuedTasks)
		tasksAssigned         = xcap.Value[int64](q.capture, scheduler.StatAssignedTasks)
		tasksExecuted         = xcap.Value[int64](q.capture, scheduler.StatExecutedTasks)
		tasksFailed           = xcap.Value[int64](q.capture, scheduler.StatFailedTasks)
		tasksCanceledPending  = xcap.Value[int64](q.capture, scheduler.StatCanceledPendingTasks)
		tasksCanceledQueued   = xcap.Value[int64](q.capture, scheduler.StatCanceledQueuedTasks)
		tasksCanceledAssigned = xcap.Value[int64](q.capture, scheduler.StatCanceledAssignedTasks)
		tasksTotal            = tasksPruned + tasksPlanned
		totalTasksCanceled    = tasksCanceledPending + tasksCanceledQueued + tasksCanceledAssigned

		// tasksExecuted, tasksFailed, and totalTasksCanceled tracks the number
		// of tasks that reached a terminal state. The remainder from
		// tasksPlanned determines the number of tasks where information got
		// lost.
		tasksUnkownStatus = (tasksPlanned - tasksExecuted - tasksFailed - totalTasksCanceled)
	)

	level.Info(q.Logger()).Log(
		"msg", "execution-summary",

		// Stats
		"duration_ms", duration.Milliseconds(),

		// Timing info
		"duration_read_batch_ms", time.Duration(readBatchDuration).Milliseconds(),
		"duration_process_batch_ms", time.Duration(processBatchDuration).Milliseconds(),
		"duration_other_ms", otherDuration.Milliseconds(),

		// Result info
		"result_rows", resultRows,
		"time_to_first_batch_ms", time.Duration(timeToFirstBatch).Milliseconds(),
		"result_size_bytes", resultSizeBytes,

		"dominant_phase", calculateDominantPhase(map[string]time.Duration{
			"read_batch":    time.Duration(readBatchDuration),
			"process_batch": time.Duration(processBatchDuration),
			"other":         otherDuration,
		}),

		// Cache information
		"task_neg_result_cache_hits", xcap.Value[int64](q.capture, workflow.StatNegativeCacheHits),
		"task_pos_result_cache_hits", xcap.Value[int64](q.capture, workflow.StatPositiveCacheHits),

		"log_sections_resolved", logSections,

		// Task fan-out
		"tasks_total", tasksTotal,
		"tasks_pruned", tasksPruned,
		"tasks_planned", tasksPlanned,
		"tasks_queued", tasksQueued,
		"tasks_assigned", tasksAssigned,
		"tasks_executed", tasksExecuted,
		"tasks_failed", tasksFailed,
		"tasks_canceled_pending", tasksCanceledPending,
		"tasks_canceled_queued", tasksCanceledQueued,
		"tasks_canceled_assigned", tasksCanceledAssigned,
		"tasks_unknown_status", tasksUnkownStatus,
	)
}

func printLogLocalitySummary(q *query) {
	var (
		rowsTotal           = xcap.ValueFromRegion[int64](q.capture, logs.RegionPrefix, xcap.StatDatasetMaxRows)
		relevantRows        = xcap.ValueFromRegion[int64](q.capture, logs.RegionPrefix, dataobj.StatStreamRelevantRows)
		streamPagesTotal    = xcap.ValueFromRegion[int64](q.capture, logs.RegionPrefix, dataobj.StatStreamPagesTotal)
		streamRelevantPages = xcap.ValueFromRegion[int64](q.capture, logs.RegionPrefix, dataobj.StatStreamRelevantPages)
	)

	level.Info(q.Logger()).Log(
		"msg", "logs-locality-summary",
		"dataset_rows_total", rowsTotal,
		"stream_relevant_rows", relevantRows,
		"stream_pages_total", streamPagesTotal,
		"stream_relevant_pages", streamRelevantPages,
	)
}
