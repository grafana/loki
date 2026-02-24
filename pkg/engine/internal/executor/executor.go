package executor

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

var tracer = otel.Tracer("pkg/engine/internal/executor")

// RequestStreamFilterer creates a StreamFilterer for a given request context.
type RequestStreamFilterer interface {
	ForRequest(ctx context.Context) StreamFilterer
}

// StreamFilterer filters streams based on their labels.
type StreamFilterer interface {
	// ShouldFilter returns true if the stream should be filtered out.
	ShouldFilter(labels labels.Labels) bool
}

type Config struct {
	BatchSize int64
	Bucket    objstore.Bucket
	Metastore metastore.Metastore

	MergePrefetchCount int

	// GetExternalInputs is an optional function called for each node in the
	// plan. If GetExternalInputs returns a non-nil slice of Pipelines, they
	// will be used as inputs to the pipeline of node.
	GetExternalInputs func(ctx context.Context, node physical.Node) []Pipeline

	// StreamFilterer is an optional filterer that can filter streams based on their labels.
	// When set, streams are filtered before scanning.
	StreamFilterer RequestStreamFilterer `yaml:"-"`

	// TaskCacheIDCacheDataObj is an optional cache used to log task_cache_id hit/miss for DataObjScan.
	// TaskCacheIDCachePointers is an optional cache used to log task_cache_id hit/miss for PointersScan.
	// DataObjectLogsCache is an optional cache used to log data_object_cache_key hit/miss for DataObjScan.
	// DataObjectPointersCache is an optional cache used to log data_object_cache_key hit/miss for PointersScan.
	// When set, each scan does a fetch (log hit/miss) and on miss stores the key. No real data is cached.
	TaskCacheIDCacheDataObj  cache.Cache `yaml:"-"`
	TaskCacheIDCachePointers cache.Cache `yaml:"-"`
	DataObjectLogsCache      cache.Cache `yaml:"-"`
	DataObjectPointersCache  cache.Cache `yaml:"-"`

	// TaskResultCacheDataObj and TaskResultCachePointers are optional caches used to log
	// hit/miss for whole task pipelines keyed by the concatenation of all node cache keys.
	// DataObj is used for tasks containing a DataObjScan; Pointers for tasks containing a
	// PointersScan. No real data is cached; only tasks with scan nodes are eligible.
	TaskResultCacheDataObj  cache.Cache `yaml:"-"`
	TaskResultCachePointers cache.Cache `yaml:"-"`
}

func Run(ctx context.Context, cfg Config, plan *physical.Plan, logger log.Logger) Pipeline {
	c := &Context{
		plan:                     plan,
		batchSize:                cfg.BatchSize,
		mergePrefetchCount:       cfg.MergePrefetchCount,
		bucket:                   cfg.Bucket,
		metastore:                cfg.Metastore,
		logger:                   logger,
		evaluator:                newExpressionEvaluator(),
		getExternalInputs:        cfg.GetExternalInputs,
		streamFilterer:           cfg.StreamFilterer,
		taskCacheIDCacheDataObj:  cfg.TaskCacheIDCacheDataObj,
		taskCacheIDCachePointers: cfg.TaskCacheIDCachePointers,
		dataObjectLogsCache:      cfg.DataObjectLogsCache,
		dataObjectPointersCache:  cfg.DataObjectPointersCache,
		taskResultCacheDataObj:  cfg.TaskResultCacheDataObj,
		taskResultCachePointers: cfg.TaskResultCachePointers,
	}
	if plan == nil {
		return errorPipeline(ctx, errors.New("plan is nil"))
	}
	if key, scanType, ok := physical.PlanCacheKey(plan); ok {
		if scanType == physical.NodeTypePointersScan {
			c.logTaskCacheResult(ctx, logger, key, c.taskResultCachePointers)
		} else {
			c.logTaskCacheResult(ctx, logger, key, c.taskResultCacheDataObj)
		}
	}
	node, err := plan.Root()
	if err != nil {
		return errorPipeline(ctx, err)
	}

	return c.execute(ctx, node)
}

// Context is the execution context
type Context struct {
	batchSize int64

	logger    log.Logger
	plan      *physical.Plan
	evaluator *expressionEvaluator
	bucket    objstore.Bucket
	metastore metastore.Metastore

	getExternalInputs func(ctx context.Context, node physical.Node) []Pipeline

	mergePrefetchCount int

	streamFilterer RequestStreamFilterer

	taskCacheIDCacheDataObj  cache.Cache
	taskCacheIDCachePointers cache.Cache
	dataObjectLogsCache      cache.Cache
	dataObjectPointersCache  cache.Cache
	taskResultCacheDataObj  cache.Cache
	taskResultCachePointers cache.Cache
}

func (c *Context) execute(ctx context.Context, node physical.Node) Pipeline {
	children := c.plan.Children(node)
	inputs := make([]Pipeline, 0, len(children))
	for _, child := range children {
		inputs = append(inputs, c.execute(ctx, child))
	}

	if c.getExternalInputs != nil {
		inputs = append(inputs, c.getExternalInputs(ctx, node)...)
	}

	switch n := node.(type) {
	case *physical.DataObjScan:
		// DataObjScan reads from object storage to determine the full pipeline to
		// construct, making it expensive to call during planning time.
		//
		// TODO(rfratto): find a way to remove the logic from executeDataObjScan
		// which wraps the pipeline with a topk/limit without reintroducing
		// planning cost for thousands of scan nodes.
		return NewObservedPipeline(n.Type().String(), nodeAttributes(n), newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
			return c.executeDataObjScan(ctx, n)
		}, inputs))
	case *physical.PointersScan:
		return NewObservedPipeline(n.Type().String(), nodeAttributes(n), c.executePointersScan(ctx, n))
	case *physical.TopK:
		return NewObservedPipeline(n.Type().String(), nodeAttributes(n), c.executeTopK(ctx, n, inputs))
	case *physical.Limit:
		return NewObservedPipeline(n.Type().String(), nodeAttributes(n), c.executeLimit(ctx, n, inputs))
	case *physical.Filter:
		return NewObservedPipeline(n.Type().String(), nodeAttributes(n), c.executeFilter(ctx, n, inputs))
	case *physical.Projection:
		return NewObservedPipeline(n.Type().String(), nodeAttributes(n), c.executeProjection(ctx, n, inputs))
	case *physical.RangeAggregation:
		return NewObservedPipeline(n.Type().String(), nodeAttributes(n), c.executeRangeAggregation(ctx, n, inputs))
	case *physical.VectorAggregation:
		return NewObservedPipeline(n.Type().String(), nodeAttributes(n), c.executeVectorAggregation(ctx, n, inputs))
	case *physical.ColumnCompat:
		return NewObservedPipeline(n.Type().String(), nodeAttributes(n), c.executeColumnCompat(ctx, n, inputs))
	case *physical.Merge:
		return NewObservedPipeline(n.Type().String(), nodeAttributes(n), c.executeMerge(ctx, n, inputs))
	case *physical.Parallelize:
		return c.executeParallelize(ctx, n, inputs)
	case *physical.ScanSet:
		return c.executeScanSet(ctx, n)
	default:
		return errorPipeline(ctx, fmt.Errorf("invalid node type: %T", node))
	}
}

func (c *Context) executeDataObjScan(ctx context.Context, node *physical.DataObjScan) Pipeline {
	span := trace.SpanFromContext(ctx)

	if c.bucket == nil {
		return errorPipeline(ctx, errors.New("no object store bucket configured"))
	}

	obj, err := dataobj.FromBucket(ctx, c.bucket, string(node.Location))
	if err != nil {
		return errorPipeline(ctx, fmt.Errorf("creating data object: %w", err))
	}
	span.AddEvent("opened dataobj")

	var (
		foundStreamsSection *dataobj.Section
		foundLogsSection    *dataobj.Section

		streamsSection *streams.Section
		logsSection    *logs.Section
	)

	tenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return errorPipeline(ctx, fmt.Errorf("missing org ID: %w", err))
	}

	var logsSectionIndex int
	for _, sec := range obj.Sections() {
		if sec.Tenant != tenant {
			if logs.CheckSection(sec) {
				logsSectionIndex++
			}
			continue
		}

		switch {
		case streams.CheckSection(sec):
			if foundStreamsSection != nil {
				return errorPipeline(ctx, fmt.Errorf("multiple streams sections found in data object %q", node.Location))
			}
			foundStreamsSection = sec

		case logs.CheckSection(sec):
			if logsSectionIndex == node.Section {
				foundLogsSection = sec
			}
			logsSectionIndex++
		}
	}

	if foundStreamsSection == nil {
		return errorPipeline(ctx, fmt.Errorf("streams section not found in data object %q", node.Location))
	} else if foundLogsSection == nil {
		return errorPipeline(ctx, fmt.Errorf("logs section %d not found in data object %q", node.Section, node.Location))
	}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var err error
		streamsSection, err = streams.Open(ctx, foundStreamsSection)
		if err != nil {
			return fmt.Errorf("opening streams section %q: %w", foundStreamsSection.Type, err)
		}
		span.AddEvent("opened streams section")
		return nil
	})

	g.Go(func() error {
		var err error
		logsSection, err = logs.Open(ctx, foundLogsSection)
		if err != nil {
			return fmt.Errorf("opening logs section %q: %w", foundLogsSection.Type, err)
		}
		span.AddEvent("opened logs section")
		return nil
	})

	if err := g.Wait(); err != nil {
		return errorPipeline(ctx, err)
	}

	// Filter streams if a filterer is configured
	streamsToMatch := node.StreamIDs
	if c.streamFilterer != nil {
		if filterer := c.streamFilterer.ForRequest(ctx); filterer != nil {
			streamsToMatch = c.filterStreamsByLabels(ctx, node.StreamIDs, streamsSection, filterer)
		}
	}

	predicates := make([]logs.Predicate, 0, len(node.Predicates))

	for _, p := range node.Predicates {
		conv, err := buildLogsPredicate(p, logsSection.Columns())
		if err != nil {
			return errorPipeline(ctx, err)
		}
		predicates = append(predicates, conv)
	}
	span.AddEvent("constructed predicate")

	logger := log.With(
		c.logger,
		"node_type", "DataObjScan",
		"location", string(node.Location),
		"section", node.Section,
		"start_ts", node.MaxTimeRange.Start.Format(time.RFC3339Nano),
		"end_ts", node.MaxTimeRange.End.Format(time.RFC3339Nano),
		"num_predicates", len(node.Predicates),
		"num_projections", len(node.Projections),
		"num_stream_ids", len(streamsToMatch),
	)
	logger = log.With(logger, "msg", "dataobjscan computing cache key")

	taskCacheID := node.TaskCacheIDWithLogger(logger)
	log.With(logger, "task_cache_id", taskCacheID)
	c.logTaskCacheResult(ctx, logger, taskCacheID, c.taskCacheIDCacheDataObj)
	c.logTaskCacheResult(ctx, logger, node.DataObjectCacheKey(), c.dataObjectLogsCache)

	var pipeline Pipeline = newDataobjScanPipeline(dataobjScanOptions{
		// TODO(rfratto): passing the streams section means that each DataObjScan
		// will read the entire streams section (for IDs being loaded), which is
		// going to be quite a bit of wasted effort.
		//
		// Longer term, there should be a dedicated plan node which handles joining
		// streams and log records based on StreamID, which is shared between all
		// sections in the same object.
		StreamsSection: streamsSection,

		LogsSection: logsSection,
		StreamIDs:   streamsToMatch,
		Predicates:  predicates,
		Projections: node.Projections,

		BatchSize: c.batchSize,
	}, logger)

	level.Info(logger).Log("msg", "dataobjscan pipeline ready")
	return pipeline
}

// filterStreamsByLabels filters stream IDs based on the StreamFilterer.
func (c *Context) filterStreamsByLabels(ctx context.Context, streamIDs []int64, streamsSection *streams.Section, filterer StreamFilterer) []int64 {
	if len(streamIDs) == 0 {
		return streamIDs
	}

	view := newStreamsView(streamsSection, &streamsViewOptions{
		StreamIDs: streamIDs,
		BatchSize: int(c.batchSize),
	})

	filtered := make([]int64, 0, len(streamIDs))
	if err := view.Open(ctx); err != nil {
		level.Error(c.logger).Log("msg", "failed to open streams view, filtering out all streams", "err", err)
		return filtered
	}

	for _, id := range streamIDs {
		lbls, err := view.Labels(ctx, id)
		if err != nil {
			level.Error(c.logger).Log("msg", "failed to get labels for stream, skipping", "stream_id", id, "err", err)
			continue
		}

		// Skip stream if it should be filtered out.
		if filterer.ShouldFilter(labels.New(lbls...)) {
			continue
		}

		filtered = append(filtered, id)
	}

	trace.SpanFromContext(ctx).AddEvent("filtered streams",
		trace.WithAttributes(
			attribute.Int("original", len(streamIDs)),
			attribute.Int("remaining", len(filtered)),
		),
	)
	return filtered
}

// rfc3339NanoInKey matches RFC3339Nano-style timestamps in cache key strings (e.g. 2026-02-19T07:15:30Z or with subsecond).
var rfc3339NanoInKey = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z`)

// dataObjScanKeyHasBothTsClamped returns true when key is a DataObjScan cache key and all
// timestamps in the key are the same (1 distinct) or only max_start and max_end (2 distinct).
// Used as a heuristic: both GTE and LT predicates were clamped to max_time_range.
func dataObjScanKeyHasBothTsClamped(key string) bool {
	if !strings.HasPrefix(key, "DataObjScan ") {
		return false
	}
	seen := make(map[string]struct{})
	for _, m := range rfc3339NanoInKey.FindAllString(key, -1) {
		seen[m] = struct{}{}
	}
	return len(seen) == 1 || len(seen) == 2
}

// logTaskCacheResult checks the given task cache for key (fetch, store on miss) and logs hit or miss.
// key is the readable task cache key string; the cache backend is called with cache.HashKey(key).
func (c *Context) logTaskCacheResult(ctx context.Context, logger log.Logger, key string, taskCache cache.Cache) {
	result := "miss"
	var err error
	var cacheKey string
	if taskCache != nil {
		cacheKey = cache.HashKey(key)
		// For some reason looks like all DOs ctx are cancelled here. I'm using a background context to avoid the issue.
		ctx := context.Background()
		found, _, missing, fetchErr := taskCache.Fetch(ctx, []string{cacheKey})
		if fetchErr != nil {
			err = fetchErr
			level.Warn(logger).Log("msg", "task_cache_id mock fetch failed", "err", fetchErr)
		} else if len(found) > 0 {
			result = "hit"
		} else if len(missing) > 0 {
			err = taskCache.Store(ctx, []string{cacheKey}, [][]byte{{}})
			if err != nil {
				level.Warn(logger).Log("msg", "task_cache_id mock store failed", "err", err)
			} else {
				level.Debug(logger).Log("msg", "stored in task cache", "cache", taskCache.GetCacheType(), "cache_key", cacheKey)
			}
		}
	}
	var cacheType stats.CacheType
	if taskCache != nil {
		cacheType = taskCache.GetCacheType()
	}
	kvs := []any{"msg", "result from task cache", "result", result, "cache", cacheType, "cache_key", cacheKey, "key", key}
	if dataObjScanKeyHasBothTsClamped(key) {
		kvs = append(kvs, "both_ts_clamped", "true")
	}
	// if ctxErr := ctx.Err(); ctxErr != nil {
	// 	kvs = append(kvs, "context_canceled", ctxErr, "context_cause", context.Cause(ctx))
	// }
	if err != nil {
		kvs = append(kvs, "err", err)
	}
	level.Info(logger).Log(kvs...)
}

func (c *Context) executePointersScan(ctx context.Context, node *physical.PointersScan) Pipeline {
	selectorStr := ""
	if node.Selector != nil {
		selectorStr = node.Selector.String()
	}
	logger := log.With(
		c.logger,
		"node_type", "PointersScan",
		"location", string(node.Location),
		"selector", selectorStr,
		"start_ts", node.Start.Format(time.RFC3339Nano),
		"end_ts", node.End.Format(time.RFC3339Nano),
		"max_time_range_start", node.GetMaxTimeRange().Start.Format(time.RFC3339Nano),
		"max_time_range_end", node.GetMaxTimeRange().End.Format(time.RFC3339Nano),
		"num_predicates", len(node.Predicates),
		"task_cache_id", node.TaskCacheID(),
	)

	c.logTaskCacheResult(ctx, logger, node.TaskCacheID(), c.taskCacheIDCachePointers)
	c.logTaskCacheResult(ctx, logger, node.DataObjectCacheKey(), c.dataObjectPointersCache)

	if c.metastore == nil {
		return errorPipeline(ctx, errors.New("no metastore configured"))
	}

	req, err := physical.CatalogRequestToMetastoreSectionsRequest(node.Selector, node.Predicates, node.Start, node.End)
	if err != nil {
		return errorPipeline(ctx, fmt.Errorf("convert catalog request to metastore request: %w", err))
	}

	level.Info(logger).Log("msg", "pointersscan pipeline ready")

	return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
		pipeline, err := newScanPointersPipeline(ctx, scanPointersOptions{
			metastore: c.metastore,
			location:  string(node.Location),
			req:       req,
			logger:    logger,
		})
		if err != nil {
			return errorPipeline(ctx, err)
		}
		return pipeline
	}, nil)
}

func (c *Context) executeTopK(ctx context.Context, topK *physical.TopK, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	pipeline, err := newTopkPipeline(topkOptions{
		Inputs:     inputs,
		SortBy:     []physical.ColumnExpression{topK.SortBy},
		Ascending:  topK.Ascending,
		NullsFirst: topK.NullsFirst,
		K:          topK.K,
		MaxUnused:  int(c.batchSize) * 2,
	})
	if err != nil {
		return errorPipeline(ctx, err)
	}

	return pipeline
}

func (c *Context) executeLimit(ctx context.Context, limit *physical.Limit, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	if len(inputs) > 1 {
		return errorPipeline(ctx, fmt.Errorf("limit expects exactly one input, got %d", len(inputs)))
	}

	return NewLimitPipeline(inputs[0], limit.Skip, limit.Fetch)
}

func (c *Context) executeFilter(ctx context.Context, filter *physical.Filter, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	if len(inputs) > 1 {
		return errorPipeline(ctx, fmt.Errorf("filter expects exactly one input, got %d", len(inputs)))
	}

	return NewFilterPipeline(filter, inputs[0], c.evaluator)
}

func (c *Context) executeProjection(ctx context.Context, proj *physical.Projection, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	if len(inputs) > 1 {
		// unsupported for now
		return errorPipeline(ctx, fmt.Errorf("projection expects exactly one input, got %d", len(inputs)))
	}

	if len(proj.Expressions) == 0 {
		return errorPipeline(ctx, fmt.Errorf("projection expects at least one expression, got 0"))
	}

	p, err := NewProjectPipeline(inputs[0], proj, c.evaluator)
	if err != nil {
		return errorPipeline(ctx, err)
	}
	return p
}

func (c *Context) executeRangeAggregation(ctx context.Context, plan *physical.RangeAggregation, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	pipeline, err := newRangeAggregationPipeline(inputs, c.evaluator, rangeAggregationOptions{
		grouping:       plan.Grouping,
		startTs:        plan.Start,
		endTs:          plan.End,
		rangeInterval:  plan.Range,
		step:           plan.Step,
		operation:      plan.Operation,
		maxQuerySeries: plan.MaxQuerySeries,
	})
	if err != nil {
		return errorPipeline(ctx, err)
	}

	return pipeline
}

func (c *Context) executeVectorAggregation(ctx context.Context, plan *physical.VectorAggregation, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	pipeline, err := newVectorAggregationPipeline(inputs, c.evaluator, vectorAggregationOptions{
		grouping:       plan.Grouping,
		operation:      plan.Operation,
		maxQuerySeries: plan.MaxQuerySeries,
	})
	if err != nil {
		return errorPipeline(ctx, err)
	}

	return pipeline
}

func (c *Context) executeColumnCompat(ctx context.Context, compat *physical.ColumnCompat, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	if len(inputs) > 1 {
		return errorPipeline(ctx, fmt.Errorf("columncompat expects exactly one input, got %d", len(inputs)))
	}

	return newColumnCompatibilityPipeline(compat, inputs[0])
}

func (c *Context) executeMerge(ctx context.Context, _ *physical.Merge, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	pipeline, err := newMergePipeline(inputs, c.mergePrefetchCount)
	if err != nil {
		return errorPipeline(ctx, err)
	}

	return pipeline
}

func (c *Context) executeParallelize(ctx context.Context, _ *physical.Parallelize, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	} else if len(inputs) > 1 {
		return errorPipeline(ctx, fmt.Errorf("parallelize expects exactly one input, got %d", len(inputs)))
	}

	// Parallelize is a hint node to the scheduler for parallel execution. If we
	// see an Parallelize node in the plan, we ignore it and immediately
	// propagate up the input.
	return inputs[0]
}

func (c *Context) executeScanSet(ctx context.Context, set *physical.ScanSet) Pipeline {
	// ScanSet typically gets partitioned by the scheduler into multiple scan
	// nodes.
	//
	// However, for locally testing unpartitioned pipelines, we still support
	// running a ScanSet. In this case, we treat internally execute it as a
	// Merge on top of multiple sequential scans.

	var targets []Pipeline
	for _, target := range set.Targets {
		switch target.Type {
		case physical.ScanTypeDataObject:
			// Make sure projections and predicates get passed down to the
			// individual scan.
			partition := target.DataObject
			partition.Predicates = set.Predicates
			partition.Projections = set.Projections

			targets = append(targets, NewObservedPipeline(partition.Type().String(), nodeAttributes(partition), newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
				return c.executeDataObjScan(ctx, partition)
			}, nil)))
		case physical.ScanTypePointers:
			partition := target.Pointers
			targets = append(targets, NewObservedPipeline(partition.Type().String(), nodeAttributes(partition), c.executePointersScan(ctx, partition)))
		default:
			return errorPipeline(ctx, fmt.Errorf("unrecognized ScanSet target %s", target.Type))
		}
	}
	if len(targets) == 0 {
		return emptyPipeline()
	}

	pipeline, err := newMergePipeline(targets, c.mergePrefetchCount)
	if err != nil {
		return errorPipeline(ctx, err)
	}

	return pipeline
}

// nodeAttributes returns OTel span attributes relevant to the given physical
// plan node type.
func nodeAttributes(n physical.Node) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("node_id", n.ID().String()),
	}

	switch n := n.(type) {
	case *physical.DataObjScan:
		attrs = append(attrs,
			attribute.String("location", string(n.Location)),
			attribute.Int("section", n.Section),
			attribute.Int("num_stream_ids", len(n.StreamIDs)),
			attribute.Int("num_predicates", len(n.Predicates)),
			attribute.Int("num_projections", len(n.Projections)),
			attribute.String("start_ts", n.MaxTimeRange.Start.Format(time.RFC3339Nano)),
			attribute.String("end_ts", n.MaxTimeRange.End.Format(time.RFC3339Nano)),
			attribute.String("task_cache_id", n.TaskCacheID()),
		)

	case *physical.PointersScan:
		attrs = append(attrs,
			attribute.String("location", string(n.Location)),
			attribute.String("selector", n.Selector.String()),
			attribute.Int("num_predicates", len(n.Predicates)),
			attribute.String("start_ts", n.Start.Format(time.RFC3339Nano)),
			attribute.String("end_ts", n.End.Format(time.RFC3339Nano)),
			attribute.String("task_cache_id", n.TaskCacheID()),
		)

	case *physical.TopK:
		attrs = append(attrs,
			attribute.Int("k", n.K),
			attribute.Bool("ascending", n.Ascending),
			attribute.Bool("nulls_first", n.NullsFirst),
		)
		if n.SortBy != nil {
			attrs = append(attrs, attribute.Stringer("sort_by", n.SortBy))
		}

	case *physical.Limit:
		attrs = append(attrs,
			attribute.Int("skip", int(n.Skip)),
			attribute.Int("fetch", int(n.Fetch)),
		)

	case *physical.Filter:
		attrs = append(attrs,
			attribute.Int("num_predicates", len(n.Predicates)),
		)

	case *physical.Projection:
		attrs = append(attrs,
			attribute.Int("num_expressions", len(n.Expressions)),
			attribute.Bool("all", n.All),
			attribute.Bool("drop", n.Drop),
			attribute.Bool("expand", n.Expand),
		)

	case *physical.RangeAggregation:
		attrs = append(attrs,
			attribute.String("operation", string(rune(n.Operation))),
			attribute.Int64("start_ts", n.Start.UnixNano()),
			attribute.Int64("end_ts", n.End.UnixNano()),
			attribute.Int64("range_interval", int64(n.Range)),
			attribute.Int64("step", int64(n.Step)),
			attribute.Int("num_grouping", len(n.Grouping.Columns)),
			attribute.Bool("grouping_without", n.Grouping.Without),
		)

	case *physical.VectorAggregation:
		attrs = append(attrs,
			attribute.String("operation", string(rune(n.Operation))),
			attribute.Int("num_grouping", len(n.Grouping.Columns)),
			attribute.Bool("grouping_without", n.Grouping.Without),
		)

	case *physical.ColumnCompat:
		collisionStrs := make([]string, len(n.Collisions))
		for i, ct := range n.Collisions {
			collisionStrs[i] = ct.String()
		}
		attrs = append(attrs,
			attribute.String("src", n.Source.String()),
			attribute.String("dst", n.Destination.String()),
			attribute.String("collisions", fmt.Sprintf("[%s]", strings.Join(collisionStrs, ", "))),
		)

	case *physical.ScanSet:
		attrs = append(attrs,
			attribute.Int("num_targets", len(n.Targets)),
			attribute.Int("num_predicates", len(n.Predicates)),
			attribute.Int("num_projections", len(n.Projections)),
		)
	default:
		// do nothing.
	}

	return attrs
}
