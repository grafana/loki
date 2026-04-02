package engine

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	gotrace "runtime/trace"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/units"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/deletion"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	lokiutil "github.com/grafana/loki/v3/pkg/util"
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

	// TasksResultCache configures the backing cache for task results.
	TasksResultCache TaskCacheConfig `yaml:"tasks_result_cache" category:"experimental"`
}

func (cfg *ExecutorConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.PrefetchBytes = flagext.Bytes(16 * units.KiB)

	f.IntVar(&cfg.BatchSize, prefix+"batch-size", 100, "Experimental: Batch size of the next generation query engine.")
	f.Var(&cfg.PrefetchBytes, prefix+"prefetch-bytes", "Experimental: Number of bytes to prefetch when opening a data object for decoding metadata and overlapping section reads. Clamps to at least 16KiB.")
	f.IntVar(&cfg.MergePrefetchCount, prefix+"merge-prefetch-count", 0, "Experimental: The number of inputs that are prefetched simultaneously by any Merge node. A value of 0 means that only the currently processed input is prefetched, 1 means that only the next input is prefetched, and so on. A negative value means that all inputs are be prefetched in parallel.")
	cfg.RangeConfig.RegisterFlags(prefix+"range-reads.", f)
	cfg.TasksResultCache.RegisterFlagsWithPrefix(prefix+"tasks-result-cache.", f)
}

// TaskCacheConfig extends resultscache.Config with additional task-cache-specific settings.
type TaskCacheConfig struct {
	resultscache.Config `yaml:",inline"`
	MaxCacheableSize    flagext.Bytes `yaml:"max_cacheable_size" category:"experimental"`
}

// RegisterFlagsWithPrefix registers flags for TaskCacheConfig with the given prefix.
func (cfg *TaskCacheConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.Config.RegisterFlagsWithPrefix(f, prefix)
	f.Var(&cfg.MaxCacheableSize, prefix+"max-cacheable-size",
		"Experimental: Maximum size for a task result to be cacheable. 0 means only empty responses are cached.")
}

// IndexGatewayClient is a subset of the index gateway client used for
// resolving dataobj section references from TSDB indices.
type IndexGatewayClient interface {
	GetDataobjSections(ctx context.Context, in *logproto.GetDataobjSectionsRequest) (*logproto.GetDataobjSectionsResponse, error)
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

	// IndexGateway is an optional index gateway client for resolving dataobj
	// sections via TSDB. When set, the engine uses TSDBCatalog instead of
	// MetastoreCatalog for physical planning.
	IndexGateway IndexGatewayClient
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
	if p.Metastore == nil && p.IndexGateway == nil {
		return errors.New("either metastore or index gateway is required")
	}
	if p.Config.Executor.BatchSize <= 0 {
		return fmt.Errorf("invalid batch size for query engine. must be greater than 0, got %d", p.Config.Executor.BatchSize)
	}
	if p.Config.UseIndexGatewayPlanning && p.IndexGateway == nil {
		return errors.New("index gateway client is required when use-index-gateway-planning is enabled")
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

	metastore    metastore.Metastore
	indexGateway IndexGatewayClient

	dualResolveSem chan struct{}
}

// New creates a new Engine.
func New(params Params) (*Engine, error) {
	if err := params.validate(); err != nil {
		return nil, err
	}

	e := &Engine{
		logger:  params.Logger,
		metrics: newMetrics(params.Registerer),
		cfg:     params.Config,

		scheduler:    params.Scheduler,
		limits:       params.Limits,
		deleteGetter: params.DeleteGetter,

		metastore:    params.Metastore,
		indexGateway: params.IndexGateway,
	}

	if maxConc := params.Config.DualResolveMaxConcurrency; maxConc > 0 && e.indexGateway != nil {
		e.dualResolveSem = make(chan struct{}, maxConc)
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

	// Starts the execution capture for the query.
	// All recorded observations will be captured to it.
	ctx, capture := xcap.NewCapture(ctx, nil)
	defer capture.End()
	startTime := time.Now()

	ctx, task := gotrace.NewTask(ctx, "Engine.Execute")
	defer task.End()

	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return logqlmodel.Result{}, httpgrpc.Error(http.StatusBadRequest, err.Error())
	}

	cacheEnabled := cache.IsCacheConfigured(e.cfg.Executor.TasksResultCache.CacheConfig) && !params.CachingOptions().Disabled

	ctx, span := xcap.StartSpan(ctx, tracer, "Engine.Execute",
		trace.WithAttributes(
			attribute.String("type", string(logql.GetRangeType(params))),
			attribute.String("query", params.QueryString()),
			attribute.Stringer("start", params.Start()),
			attribute.Stringer("end", params.End()),
			attribute.Stringer("step", params.Step()),
			attribute.Stringer("length", params.End().Sub(params.Start())),
			attribute.StringSlice("shards", params.Shards()),
			attribute.Bool("cache_disabled", cacheEnabled),
		),
	)
	defer span.End()

	ctx = e.buildContext(ctx)
	logger := util_log.WithContext(ctx, e.logger)
	logger = log.With(logger, "engine", "v2")
	logger = injectQueryTags(ctx, logger)

	level.Info(logger).Log("msg", "starting query", "query", params.QueryString(), "shard", strings.Join(params.Shards(), ","))

	logicalPlan, durLogicalPlanning, err := e.buildLogicalPlan(ctx, logger, params)
	if err != nil {
		e.metrics.subqueries.WithLabelValues(statusNotImplemented).Inc()
		span.SetStatus(codes.Error, "failed to create logical plan")
		return logqlmodel.Result{}, ErrNotSupported
	}
	gotrace.Log(ctx, "logical_planning", "done")

	physicalPlan, durPhysicalPlanning, err := e.buildPhysicalPlan(ctx, tenantID, logger, params, logicalPlan, cacheEnabled)
	if err != nil {
		e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
		span.SetStatus(codes.Error, "failed to create physical plan")
		return logqlmodel.Result{}, ErrPlanningFailed
	}
	gotrace.Log(ctx, "physical_planning", "done")

	// Enable admission lanes only for log queries
	useAdmissionLanes := !isMetricQuery(params.GetExpression())

	wf, durWorkflowPlanning, err := e.buildWorkflow(ctx, tenantID, logger, physicalPlan, useAdmissionLanes, cacheEnabled)
	if err != nil {
		e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
		span.SetStatus(codes.Error, "failed to create execution plan")
		return logqlmodel.Result{}, ErrPlanningFailed
	}
	defer wf.Close()
	gotrace.Log(ctx, "workflow_planning", "done")

	pipeline, err := wf.Run(ctx)
	if err != nil {
		level.Error(logger).Log("msg", "failed to execute query", "err", err)

		e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
		span.SetStatus(codes.Error, "failed to execute query")
		return logqlmodel.Result{}, ErrSchedulingFailed
	}
	defer pipeline.Close()

	gotrace.Log(ctx, "collect_result", "start")
	builder, durExecution, err := e.collectResult(ctx, logger, params, pipeline)
	if err != nil {
		e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
		span.SetStatus(codes.Error, "error during query execution")
		return logqlmodel.Result{}, err
	}

	durFull := time.Since(startTime)
	logValues := []any{
		"msg", "finished executing",
		"query", params.QueryString(),
		"length", params.End().Sub(params.Start()).String(),
		"step", params.Step().String(),
		"duration_logical_planning", durLogicalPlanning,
		"duration_physical_planning", durPhysicalPlanning,
		"duration_workflow_planning", durWorkflowPlanning,
		"duration_execution", durExecution,
		"duration_full", durFull,
	}

	gotrace.Log(ctx, "collect_result", "done")

	// Close the pipeline to calculate the stats.
	pipeline.Close()

	span.SetStatus(codes.Ok, "")

	// explicitly call End() before exporting even though we have a defer above.
	// It is safe to call End() multiple times.
	span.End()
	capture.End()

	logValues = append(logValues, xcap.SummaryLogValues(capture)...)
	level.Info(logger).Log(
		logValues...,
	)

	// TODO: capture and report queue time
	md := metadata.FromContext(ctx)
	stats := capture.ToStatsSummary(durFull, 0, builder.Len())
	result := builder.Build(stats, md)

	logql.RecordRangeAndInstantQueryMetrics(ctx, logger, params, strconv.Itoa(http.StatusOK), stats, result.Data)
	return result, nil
}

// buildContext initializes a request-scoped context prior to execution.
func (e *Engine) buildContext(ctx context.Context) context.Context {
	metadataContext, ctx := metadata.NewContext(ctx)

	// Inject the range config into the context for any calls to
	// [rangeio.ReadRanges] to make use of.
	ctx = rangeio.WithConfig(ctx, &e.cfg.Executor.RangeConfig)

	metadataContext.AddWarning("Query was executed using the new experimental query engine and dataobj storage.")
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
func (e *Engine) buildLogicalPlan(ctx context.Context, logger log.Logger, params logql.Params) (*logical.Plan, time.Duration, error) {
	span := trace.SpanFromContext(ctx)
	timer := prometheus.NewTimer(e.metrics.logicalPlanning)

	var deleteReqs []*deletion.Request
	if e.deleteGetter != nil {
		var err error
		deleteReqs, err = deletion.DeletesForUser(ctx, params.Start(), params.End(), e.deleteGetter)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to get delete requests: %w", err)
		}
	}

	logicalPlan, err := logical.BuildPlanWithDeletes(ctx, params, deleteReqs)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to create logical plan", "err", err)
		span.RecordError(err)
		return nil, 0, ErrNotSupported
	}

	if err := logical.Optimize(logicalPlan); err != nil {
		level.Warn(logger).Log("msg", "failed to optimize logical plan", "err", err)
		span.RecordError(err)
		return nil, 0, ErrNotSupported
	}

	duration := timer.ObserveDuration()
	level.Info(logger).Log(
		"msg", "finished logical planning",
		"plan", logicalPlan.String(),
		"duration", duration.String(),
	)

	span.AddEvent("finished logical planning",
		trace.WithAttributes(
			attribute.Stringer("plan", logicalPlan),
			attribute.Stringer("duration", duration),
		),
	)
	return logicalPlan, duration, nil
}

// buildPhysicalPlan builds a physical plan from the given logical plan.
func (e *Engine) buildPhysicalPlan(ctx context.Context, tenantID string, logger log.Logger, params logql.Params, logicalPlan *logical.Plan, cacheEnabled bool) (*physical.Plan, time.Duration, error) {
	span := trace.SpanFromContext(ctx)
	timer := prometheus.NewTimer(e.metrics.physicalPlanning)

	var catalog physical.Catalog
	if e.cfg.UseIndexGatewayPlanning && e.indexGateway != nil && e.cfg.TSDBCoversQuery(params.Start()) {
		catalog = physical.NewTSDBCatalog(e.tsdbSectionsResolver(ctx, tenantID))
	} else {
		catalog = physical.NewMetastoreCatalog(e.metastoreSectionsResolver(ctx, tenantID, cacheEnabled))
	}

	plannerCtx := physical.NewContext(params.Start(), params.End())

	if e.cfg.EnforceQuerySeriesLimit {
		plannerCtx = plannerCtx.WithMaxQuerySeries(e.limits.MaxQuerySeries(ctx, tenantID))
	}

	planner := physical.NewPlanner(plannerCtx, catalog)
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

	physicalPlan, err = physical.WrapWithBatching(physicalPlan, e.cfg.Executor.BatchSize)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to wrap physical plan with batching", "err", err)
		span.RecordError(err)
		return nil, 0, ErrNotSupported
	}

	duration := timer.ObserveDuration()
	msSections := extractScanSections(physicalPlan)

	level.Info(logger).Log(
		"msg", "finished physical planning",
		"plan", physical.PrintAsTree(physicalPlan),
		"duration", duration.String(),
	)

	span.AddEvent("finished physical planning", trace.WithAttributes(attribute.Stringer("duration", duration)))

	e.maybeDualResolve(logger, tenantID, params, logicalPlan, plannerCtx, msSections, duration)

	return physicalPlan, duration, nil
}

// maybeDualResolve kicks off an async index-gateway comparison if dual-resolve
// is enabled and there is capacity available. It never blocks the caller.
func (e *Engine) maybeDualResolve(
	logger log.Logger,
	tenantID string,
	params logql.Params,
	logicalPlan *logical.Plan,
	plannerCtx *physical.Context,
	msSections []resolvedSection,
	msDuration time.Duration,
) {
	if e.dualResolveSem == nil {
		return
	}
	if !e.cfg.TSDBCoversQuery(params.Start()) {
		return
	}

	select {
	case e.dualResolveSem <- struct{}{}:
		go func() {
			defer func() { <-e.dualResolveSem }()
			defer func() {
				if r := recover(); r != nil {
					level.Error(logger).Log("msg", "panic in dual-resolve goroutine", "panic", fmt.Sprintf("%v", r))
				}
			}()

			resolveCtx, cancel := context.WithTimeout(user.InjectOrgID(context.Background(), tenantID), 2*time.Minute)
			defer cancel()

			start := time.Now()
			catalog := physical.NewTSDBCatalog(e.tsdbSectionsResolver(resolveCtx, tenantID))
			p := physical.NewPlanner(plannerCtx, catalog)
			igwPlan, igwErr := p.Build(logicalPlan)
			igwDuration := time.Since(start)

			var cmp comparisonResult
			if igwErr == nil {
				igwSections := extractScanSections(igwPlan)
				cmp = compareSections(msSections, igwSections)
			} else {
				cmp.MsCount = len(msSections)
			}

			level.Info(logger).Log(
				"msg", "dual-resolve comparison",
				"query", params.QueryString(),
				"query_length", params.End().Sub(params.Start()),
				"metastore_sections", cmp.MsCount,
				"metastore_duration", msDuration,
				"index_gateway_sections", cmp.IgwCount,
				"index_gateway_duration", igwDuration,
				"index_gateway_error", fmt.Sprintf("%v", igwErr),
				"igw_superset_of_ms", cmp.IgwSupersetOfMs,
				"missing_from_igw", len(cmp.MissingFromIgw),
				"stream_id_mismatches", len(cmp.StreamMismatches),
				"time_mismatches", len(cmp.TimeMismatches),
			)

			if len(cmp.MissingFromIgw) > 0 {
				limit := min(len(cmp.MissingFromIgw), 5)
				level.Warn(logger).Log(
					"msg", "dual-resolve: sections missing from index-gateway",
					"query", params.QueryString(),
					"count", len(cmp.MissingFromIgw),
					"first_few", fmt.Sprintf("%v", cmp.MissingFromIgw[:limit]),
				)
			}
			if len(cmp.StreamMismatches) > 0 {
				limit := min(len(cmp.StreamMismatches), 5)
				level.Warn(logger).Log(
					"msg", "dual-resolve: stream ID mismatches",
					"query", params.QueryString(),
					"count", len(cmp.StreamMismatches),
					"first_few", fmt.Sprintf("%v", cmp.StreamMismatches[:limit]),
				)
			}
			if len(cmp.TimeMismatches) > 0 {
				limit := min(len(cmp.TimeMismatches), 5)
				level.Warn(logger).Log(
					"msg", "dual-resolve: time range mismatches",
					"query", params.QueryString(),
					"count", len(cmp.TimeMismatches),
					"first_few", fmt.Sprintf("%v", cmp.TimeMismatches[:limit]),
				)
			}
		}()
	default:
		e.metrics.dualResolveDropped.Inc()
	}
}

type sectionKey struct {
	Location string
	Section  int
}

func (k sectionKey) String() string {
	return fmt.Sprintf("%s/%d", k.Location, k.Section)
}

type resolvedSection struct {
	Key       sectionKey
	StreamIDs []int64
	TimeRange physical.TimeRange
}

// extractScanSections walks a physical plan and returns details of every
// DataObject scan target, including location, section index, and stream IDs.
func extractScanSections(plan *physical.Plan) []resolvedSection {
	var sections []resolvedSection
	for _, root := range plan.Roots() {
		_ = plan.DFSWalk(root, func(n physical.Node) error {
			if ss, ok := n.(*physical.ScanSet); ok {
				for _, t := range ss.Targets {
					if t.Type == physical.ScanTypeDataObject && t.DataObject != nil {
						sections = append(sections, resolvedSection{
							Key: sectionKey{
								Location: string(t.DataObject.Location),
								Section:  t.DataObject.Section,
							},
							StreamIDs: t.DataObject.StreamIDs,
							TimeRange: t.DataObject.MaxTimeRange,
						})
					}
				}
			}
			return nil
		}, dag.PreOrderWalk)
	}
	return sections
}

// countScanTargets counts the number of ScanTypeDataObject targets in a plan.
func countScanTargets(plan *physical.Plan) int {
	return len(extractScanSections(plan))
}

// comparisonResult holds the outcome of comparing metastore and IGW sections.
type comparisonResult struct {
	MsCount          int
	IgwCount         int
	IgwSupersetOfMs  bool
	MissingFromIgw   []sectionKey
	StreamMismatches []sectionKey
	TimeMismatches   []sectionKey
}

// compareSections checks whether the index-gateway resolved sections are a
// superset of the metastore sections, and whether stream IDs and time ranges
// match for sections present in both.
func compareSections(msSections, igwSections []resolvedSection) comparisonResult {
	type igwEntry struct {
		StreamIDs []int64
		TimeRange physical.TimeRange
	}

	igwMap := make(map[sectionKey]igwEntry, len(igwSections))
	for _, s := range igwSections {
		ids := make([]int64, len(s.StreamIDs))
		copy(ids, s.StreamIDs)
		slices.Sort(ids)
		igwMap[s.Key] = igwEntry{StreamIDs: ids, TimeRange: s.TimeRange}
	}

	var (
		missingFromIgw   []sectionKey
		streamMismatches []sectionKey
		timeMismatches   []sectionKey
	)

	for _, ms := range msSections {
		entry, found := igwMap[ms.Key]
		if !found {
			missingFromIgw = append(missingFromIgw, ms.Key)
			continue
		}
		msIDs := make([]int64, len(ms.StreamIDs))
		copy(msIDs, ms.StreamIDs)
		slices.Sort(msIDs)
		if !slices.Equal(msIDs, entry.StreamIDs) {
			streamMismatches = append(streamMismatches, ms.Key)
		}
		if !ms.TimeRange.Start.Truncate(time.Millisecond).Equal(entry.TimeRange.Start.Truncate(time.Millisecond)) ||
			!ms.TimeRange.End.Truncate(time.Millisecond).Equal(entry.TimeRange.End.Truncate(time.Millisecond)) {
			timeMismatches = append(timeMismatches, ms.Key)
		}
	}

	return comparisonResult{
		MsCount:          len(msSections),
		IgwCount:         len(igwSections),
		IgwSupersetOfMs:  len(missingFromIgw) == 0,
		MissingFromIgw:   missingFromIgw,
		StreamMismatches: streamMismatches,
		TimeMismatches:   timeMismatches,
	}
}

func (e *Engine) metastoreSectionsResolver(ctx context.Context, tenantID string, cacheEnabled bool) physical.MetastoreSectionsResolver {
	planner := physical.NewMetastorePlanner(e.metastore, e.cfg.Executor.BatchSize)
	return func(selector physical.Expression, predicates []physical.Expression, start time.Time, end time.Time) ([]*metastore.DataobjSectionDescriptor, error) {
		ctx, span := xcap.StartSpan(ctx, tracer, "engine.metastoreResolver")
		defer span.End()

		plan, err := planner.Plan(ctx, selector, predicates, start, end)
		if err != nil {
			return nil, fmt.Errorf("metastore: build plan: %w", err)
		}

		// Disable admission lanes for metastore queries
		useAdmissionLanes := false

		wf, _, err := e.buildWorkflow(ctx, tenantID, e.logger, plan, useAdmissionLanes, cacheEnabled)
		if err != nil {
			return nil, fmt.Errorf("metastore: build workflow: %w", err)
		}
		defer wf.Close()

		pipeline, err := wf.Run(ctx)
		if err != nil {
			return nil, fmt.Errorf("metastore: run workflow: %w", err)
		}
		reader := executor.TranslateEOF(pipeline)
		defer reader.Close()

		if err := reader.Open(ctx); err != nil {
			return nil, fmt.Errorf("metastore: open pipeline: %w", err)
		}

		resp, err := e.metastore.CollectSections(ctx, metastore.CollectSectionsRequest{
			// externalize EOFs returned by executor pipelines (executor.EOF -> io.EOF)
			// because metastore is not aware about executor implementation details
			Reader: reader,
		})
		if err != nil {
			return nil, fmt.Errorf("metastore: collect sections: %w", err)
		}

		// close to report the stats
		reader.Close()
		return resp.SectionsResponse.Sections, nil
	}
}

// tsdbSectionsResolver returns a resolver function that queries the index
// gateway for data object sections matching the given matchers/time range.
// When TSDBSplitInterval is configured, the time range is split into smaller
// sub-ranges that are queried concurrently and the results merged.
func (e *Engine) tsdbSectionsResolver(ctx context.Context, tenantID string) physical.TSDBSectionsResolver {
	return func(matchers string, start, end time.Time) ([]physical.DataObjSections, error) {
		if interval := e.cfg.TSDBSplitInterval; interval > 0 {
			return e.tsdbSplitResolve(ctx, matchers, start, end, interval)
		}
		return e.tsdbFetchSections(ctx, matchers, start, end)
	}
}

// tsdbFetchSections performs a single GetDataobjSections RPC for the given range.
func (e *Engine) tsdbFetchSections(ctx context.Context, matchers string, start, end time.Time) ([]physical.DataObjSections, error) {
	resp, err := e.indexGateway.GetDataobjSections(ctx, &logproto.GetDataobjSectionsRequest{
		From:     model.TimeFromUnixNano(start.UnixNano()),
		Through:  model.TimeFromUnixNano(end.UnixNano()),
		Matchers: matchers,
	})
	if err != nil {
		return nil, fmt.Errorf("index gateway GetDataobjSections: %w", err)
	}

	result := make([]physical.DataObjSections, 0, len(resp.Sections))
	for _, s := range resp.Sections {
		tr, err := physical.NewTimeRange(s.MinTime.Time(), s.MaxTime.Time())
		if err != nil {
			return nil, fmt.Errorf("invalid time range in section ref: %w", err)
		}
		result = append(result, physical.DataObjSections{
			Location:  physical.DataObjLocation(s.Path),
			Streams:   s.StreamIds,
			Sections:  []int{int(s.SectionId)},
			TimeRange: tr,
		})
	}
	return result, nil
}

// tsdbSplitResolve splits [start, end] into sub-ranges of the given interval,
// fetches each sub-range concurrently, and deduplicates the combined results.
func (e *Engine) tsdbSplitResolve(ctx context.Context, matchers string, start, end time.Time, interval time.Duration) ([]physical.DataObjSections, error) {
	var mu sync.Mutex
	var batches [][]physical.DataObjSections

	g, gCtx := errgroup.WithContext(ctx)
	if conc := e.cfg.TSDBSplitConcurrency; conc > 0 {
		g.SetLimit(conc)
	}

	lokiutil.ForInterval(interval, start, end, false, func(splitStart, splitEnd time.Time) {
		g.Go(func() error {
			sections, err := e.tsdbFetchSections(gCtx, matchers, splitStart, splitEnd)
			if err != nil {
				return err
			}
			mu.Lock()
			batches = append(batches, sections)
			mu.Unlock()
			return nil
		})
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return mergeDataObjSections(batches), nil
}

// mergeDataObjSections deduplicates sections across multiple batches.
// Sections sharing the same (Location, SectionId) are merged: stream IDs are
// unioned and the time range is widened to cover both entries.
func mergeDataObjSections(batches [][]physical.DataObjSections) []physical.DataObjSections {
	type mergeKey struct {
		location physical.DataObjLocation
		section  int
	}

	seen := make(map[mergeKey]int) // key -> index in result
	var result []physical.DataObjSections

	for _, batch := range batches {
		for _, ds := range batch {
			for _, sec := range ds.Sections {
				key := mergeKey{location: ds.Location, section: sec}
				if idx, ok := seen[key]; ok {
					existing := &result[idx]
					existing.TimeRange = existing.TimeRange.Merge(ds.TimeRange)
					existing.Streams = unionSortedInt64(existing.Streams, ds.Streams)
				} else {
					seen[key] = len(result)
					result = append(result, physical.DataObjSections{
						Location:  ds.Location,
						Streams:   ds.Streams,
						Sections:  []int{sec},
						TimeRange: ds.TimeRange,
					})
				}
			}
		}
	}
	return result
}

// unionSortedInt64 returns the sorted union of two int64 slices.
func unionSortedInt64(a, b []int64) []int64 {
	set := make(map[int64]struct{}, len(a)+len(b))
	for _, v := range a {
		set[v] = struct{}{}
	}
	for _, v := range b {
		set[v] = struct{}{}
	}
	out := make([]int64, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}

// buildWorkflow builds a workflow from the given physical plan.
func (e *Engine) buildWorkflow(ctx context.Context, tenantID string, logger log.Logger, physicalPlan *physical.Plan, useAdmissionLanes bool, cacheEnabled bool) (*workflow.Workflow, time.Duration, error) {
	span := trace.SpanFromContext(ctx)
	timer := prometheus.NewTimer(e.metrics.workflowPlanning)

	maxRunningScanTasks := 0
	// Set max running tasks limits on when admission lanes are enabled
	if useAdmissionLanes {
		maxRunningScanTasks = e.limits.MaxScanTaskParallelism(tenantID)
	}

	opts := workflow.Options{
		Tenant: tenantID,
		Actor:  httpreq.ExtractActorPath(ctx),

		MaxRunningScanTasks:  maxRunningScanTasks,
		MaxRunningOtherTasks: 0,

		CacheEnabled:     cacheEnabled,
		MaxCacheableSize: uint64(e.cfg.Executor.TasksResultCache.MaxCacheableSize),

		DebugTasks:   e.limits.DebugEngineTasks(tenantID),
		DebugStreams: e.limits.DebugEngineStreams(tenantID),
	}
	wf, err := workflow.New(opts, logger, e.scheduler.inner, physicalPlan)
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
		"tasks", wf.Len(),
	)

	span.AddEvent("finished execution planning", trace.WithAttributes(attribute.Stringer("duration", duration)))
	return wf, duration, nil
}

// collectResult processes the results of the execution plan.
func (e *Engine) collectResult(ctx context.Context, logger log.Logger, params logql.Params, pipeline executor.Pipeline) (ResultBuilder, time.Duration, error) {
	span := trace.SpanFromContext(ctx)
	timer := prometheus.NewTimer(e.metrics.execution)
	encodingFlags := httpreq.ExtractEncodingFlagsFromCtx(ctx)

	var builder ResultBuilder
	switch params.GetExpression().(type) {
	case syntax.LogSelectorExpr:
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
		return nil, time.Duration(0), err
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
	span.AddEvent("finished execution",
		trace.WithAttributes(attribute.Stringer("duration", duration)),
	)
	return builder, duration, nil
}
