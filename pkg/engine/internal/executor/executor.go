package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/thanos-io/objstore"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var tracer = otel.Tracer("pkg/engine/internal/executor")

type Config struct {
	BatchSize int64
	Bucket    objstore.Bucket

	MergePrefetchCount int
}

func Run(ctx context.Context, cfg Config, plan *physicalpb.Plan, logger log.Logger) Pipeline {
	c := &Context{
		plan:               plan,
		batchSize:          cfg.BatchSize,
		mergePrefetchCount: cfg.MergePrefetchCount,
		bucket:             cfg.Bucket,
		logger:             logger,
		evaluator:          newExpressionEvaluator(),
	}
	if plan == nil {
		return errorPipeline(ctx, errors.New("plan is nil"))
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
	plan      *physicalpb.Plan
	evaluator expressionEvaluator
	bucket    objstore.Bucket

	mergePrefetchCount int
}

func (c *Context) execute(ctx context.Context, node physicalpb.Node) Pipeline {
	children := c.plan.Children(node)
	inputs := make([]Pipeline, 0, len(children))
	for _, child := range children {
		inputs = append(inputs, c.execute(ctx, child))
	}

	switch n := node.(type) {
	case *physicalpb.DataObjScan:
		// DataObjScan reads from object storage to determine the full pipeline to
		// construct, making it expensive to call during planning time.
		//
		// TODO(rfratto): find a way to remove the logic from executeDataObjScan
		// which wraps the pipeline with a topk/limit without reintroducing
		// planning cost for thousands of scan nodes.
		return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
			return tracePipeline("physicalpb.DataObjScan", c.executeDataObjScan(ctx, n))
		}, inputs)

	case *physicalpb.Limit:
		return tracePipeline("physicalpb.Limit", c.executeLimit(ctx, n, inputs))
	case *physicalpb.Filter:
		return tracePipeline("physicalpb.Filter", c.executeFilter(ctx, n, inputs))
	case *physicalpb.Projection:
		return tracePipeline("physicalpb.Projection", c.executeProjection(ctx, n, inputs))
	case *physicalpb.AggregateRange:
		return tracePipeline("physicalpb.RangeAggregation", c.executeRangeAggregation(ctx, n, inputs))
	case *physicalpb.AggregateVector:
		return tracePipeline("physicalpb.VectorAggregation", c.executeVectorAggregation(ctx, n, inputs))
	case *physicalpb.Parse:
		return tracePipeline("physicalpb.ParseNode", c.executeParse(ctx, n, inputs))
	case *physicalpb.ColumnCompat:
		return tracePipeline("physicalpb.ColumnCompat", c.executeColumnCompat(ctx, n, inputs))
	case *physicalpb.Parallelize:
		return tracePipeline("physicalpb.Parallelize", c.executeParallelize(ctx, n, inputs))
	case *physicalpb.ScanSet:
		return tracePipeline("physicalpb.ScanSet", c.executeScanSet(ctx, n))
	case *physicalpb.TopK:
		return tracePipeline("physicalpb.TopK", c.executeTopK(ctx, n, inputs))
	default:
		return errorPipeline(ctx, fmt.Errorf("invalid node type: %T", node))
	}
}

func (c *Context) executeDataObjScan(ctx context.Context, node *physicalpb.DataObjScan) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeDataObjScan", trace.WithAttributes(
		attribute.String("location", node.Location),
		attribute.Int64("section", node.Section),
		attribute.Stringer("direction", node.SortOrder),
		attribute.Int("limit", int(node.Limit)),
		attribute.Int("num_stream_ids", len(node.StreamIds)),
		attribute.Int("num_predicates", len(node.Predicates)),
		attribute.Int("num_projections", len(node.Projections)),
	))
	defer span.End()

	if c.bucket == nil {
		return errorPipeline(ctx, errors.New("no object store bucket configured"))
	}

	obj, err := dataobj.FromBucket(ctx, c.bucket, node.Location)
	if err != nil {
		return errorPipeline(ctx, fmt.Errorf("creating data object: %w", err))
	}
	span.AddEvent("opened dataobj")

	var (
		streamsSection *streams.Section
		logsSection    *logs.Section
	)

	tenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return errorPipeline(ctx, fmt.Errorf("missing org ID: %w", err))
	}

	for _, sec := range obj.Sections().Filter(streams.CheckSection) {
		if sec.Tenant != tenant {
			continue
		}

		if streamsSection != nil {
			return errorPipeline(ctx, fmt.Errorf("multiple streams sections found in data object %q", node.Location))
		}

		var err error
		streamsSection, err = streams.Open(ctx, sec)
		if err != nil {
			return errorPipeline(ctx, fmt.Errorf("opening streams section %q: %w", sec.Type, err))
		}
		span.AddEvent("opened streams section")
		break
	}
	if streamsSection == nil {
		return errorPipeline(ctx, fmt.Errorf("streams section not found in data object %q", node.Location))
	}

	for i, sec := range obj.Sections().Filter(logs.CheckSection) {
		if int64(i) != node.Section {
			continue
		}

		var err error
		logsSection, err = logs.Open(ctx, sec)
		if err != nil {
			return errorPipeline(ctx, fmt.Errorf("opening logs section %q: %w", sec.Type, err))
		}
		span.AddEvent("opened logs section")
		break
	}
	if logsSection == nil {
		return errorPipeline(ctx, fmt.Errorf("logs section %d not found in data object %q", node.Section, node.Location))
	}

	predicates := make([]logs.Predicate, 0, len(node.Predicates))

	for _, p := range node.Predicates {
		conv, err := buildLogsPredicate(*p, logsSection.Columns())
		if err != nil {
			return errorPipeline(ctx, err)
		}
		predicates = append(predicates, conv)
	}
	span.AddEvent("constructed predicate")

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
		StreamIDs:   node.StreamIds,
		Predicates:  predicates,
		Projections: node.Projections,

		BatchSize: c.batchSize,
	}, log.With(c.logger, "location", node.Location, "section", node.Section))

	sortType, sortDirection, err := logsSection.PrimarySortOrder()
	if err != nil {
		level.Warn(c.logger).Log("msg", "could not determine primary sort order for logs section, forcing topk sort", "err", err)

		sortType = logs.ColumnTypeInvalid
		sortDirection = logs.SortDirectionUnspecified
	}

	// Wrap our pipeline to enforce expected sorting. We always emit logs in
	// timestamp-sorted order, so we need to sort if either the section doesn't
	// match the expected sort order or the requested sort type is not timestamp.
	//
	// If it's already sorted, we wrap by LimitPipeline to enforce the limit
	// given to the node (if defined).
	if node.SortOrder != physicalpb.SORT_ORDER_INVALID && (node.SortOrder != logsSortOrder(sortDirection) || sortType != logs.ColumnTypeTimestamp) {
		level.Debug(c.logger).Log("msg", "sorting logs section", "source_sort", sortType, "source_direction", sortDirection, "requested_sort", logs.ColumnTypeTimestamp, "requested_dir", node.SortOrder.String())

		pipeline, err = newTopkPipeline(topkOptions{
			Inputs: []Pipeline{pipeline},

			SortBy: []*physicalpb.ColumnExpression{
				{
					Name: types.ColumnNameBuiltinTimestamp,
					Type: physicalpb.COLUMN_TYPE_BUILTIN,
				},
			},
			Ascending: node.SortOrder == physicalpb.SORT_ORDER_ASCENDING,
			K:         int64(node.Limit),

			MaxUnused: int(c.batchSize) * 2,
		})
		if err != nil {
			return errorPipeline(ctx, err)
		}
		span.AddEvent("injected topk")
	} else if node.Limit > 0 {
		pipeline = NewLimitPipeline(pipeline, 0, node.Limit)
		span.AddEvent("injected limit")
	}

	return pipeline
}

func logsSortOrder(dir logs.SortDirection) physicalpb.SortOrder {
	switch dir {
	case logs.SortDirectionAscending:
		return physicalpb.SORT_ORDER_ASCENDING
	case logs.SortDirectionDescending:
		return physicalpb.SORT_ORDER_DESCENDING
	}

	return physicalpb.SORT_ORDER_INVALID
}

func (c *Context) executeTopK(ctx context.Context, topK *physicalpb.TopK, inputs []Pipeline) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeTopK", trace.WithAttributes(
		attribute.Int64("k", topK.K),
		attribute.Bool("ascending", topK.Ascending),
	))
	defer span.End()

	if topK.SortBy != nil {
		span.SetAttributes(attribute.Stringer("sort_by", topK.SortBy))
	}

	if len(inputs) == 0 {
		return emptyPipeline()
	}

	pipeline, err := newTopkPipeline(topkOptions{
		Inputs:     inputs,
		SortBy:     []*physicalpb.ColumnExpression{topK.SortBy},
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

func (c *Context) executeLimit(ctx context.Context, limit *physicalpb.Limit, inputs []Pipeline) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeLimit", trace.WithAttributes(
		attribute.Int("skip", int(limit.Skip)),
		attribute.Int("fetch", int(limit.Fetch)),
		attribute.Int("num_inputs", len(inputs)),
	))
	defer span.End()

	if len(inputs) == 0 {
		return emptyPipeline()
	}

	if len(inputs) > 1 {
		return errorPipeline(ctx, fmt.Errorf("limit expects exactly one input, got %d", len(inputs)))
	}

	return NewLimitPipeline(inputs[0], limit.Skip, limit.Fetch)
}

func (c *Context) executeFilter(ctx context.Context, filter *physicalpb.Filter, inputs []Pipeline) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeFilter", trace.WithAttributes(
		attribute.Int("num_inputs", len(inputs)),
	))
	defer span.End()

	if len(inputs) == 0 {
		return emptyPipeline()
	}

	if len(inputs) > 1 {
		return errorPipeline(ctx, fmt.Errorf("filter expects exactly one input, got %d", len(inputs)))
	}

	return NewFilterPipeline(filter, inputs[0], c.evaluator)
}

func (c *Context) executeProjection(ctx context.Context, proj *physicalpb.Projection, inputs []Pipeline) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeProjection", trace.WithAttributes(
		attribute.Int("num_expressions", len(proj.Expressions)),
		attribute.Int("num_inputs", len(inputs)),
	))
	defer span.End()

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

	p, err := NewProjectPipeline(inputs[0], proj, &c.evaluator)
	if err != nil {
		return errorPipeline(ctx, err)
	}
	return p
}

func (c *Context) executeRangeAggregation(ctx context.Context, plan *physicalpb.AggregateRange, inputs []Pipeline) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeRangeAggregation", trace.WithAttributes(
		attribute.Int("num_partition_by", len(plan.PartitionBy)),
		attribute.Int64("start_ts", plan.StartUnixNanos),
		attribute.Int64("end_ts", plan.EndUnixNanos),
		attribute.Int64("range_interval", plan.RangeNs),
		attribute.Int64("step", plan.StepNs),
		attribute.Int("num_inputs", len(inputs)),
	))
	defer span.End()

	if len(inputs) == 0 {
		return emptyPipeline()
	}

	pipeline, err := newRangeAggregationPipeline(inputs, c.evaluator, rangeAggregationOptions{
		partitionBy:   plan.PartitionBy,
		startTs:       time.Unix(0, plan.StartUnixNanos),
		endTs:         time.Unix(0, plan.EndUnixNanos),
		rangeInterval: time.Duration(plan.RangeNs),
		step:          time.Duration(plan.StepNs),
		operation:     plan.Operation,
	})
	if err != nil {
		return errorPipeline(ctx, err)
	}

	return pipeline
}

func (c *Context) executeVectorAggregation(ctx context.Context, plan *physicalpb.AggregateVector, inputs []Pipeline) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeVectorAggregation", trace.WithAttributes(
		attribute.Int("num_group_by", len(plan.GroupBy)),
		attribute.Int("num_inputs", len(inputs)),
	))
	defer span.End()

	if len(inputs) == 0 {
		return emptyPipeline()
	}

	pipeline, err := newVectorAggregationPipeline(inputs, plan.GroupBy, c.evaluator, plan.Operation)
	if err != nil {
		return errorPipeline(ctx, err)
	}

	return pipeline
}

func (c *Context) executeParse(ctx context.Context, parse *physicalpb.Parse, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	if len(inputs) > 1 {
		return errorPipeline(ctx, fmt.Errorf("parse expects exactly one input, got %d", len(inputs)))
	}

	return NewParsePipeline(parse, inputs[0])
}

func (c *Context) executeColumnCompat(ctx context.Context, compat *physicalpb.ColumnCompat, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	if len(inputs) > 1 {
		return errorPipeline(ctx, fmt.Errorf("columncompat expects exactly one input, got %d", len(inputs)))
	}

	return newColumnCompatibilityPipeline(compat, inputs[0])
}

func (c *Context) executeParallelize(ctx context.Context, _ *physicalpb.Parallelize, inputs []Pipeline) Pipeline {
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

func (c *Context) executeScanSet(ctx context.Context, set *physicalpb.ScanSet) Pipeline {
	// ScanSet typically gets partitioned by the scheduler into multiple scan
	// nodes.
	//
	// However, for locally testing unpartitioned pipelines, we still supprt
	// running a ScanSet. In this case, we treat internally execute it as a
	// Merge on top of multiple sequential scans.

	var targets []Pipeline

	for _, target := range set.Targets {
		switch target.Type {
		case physicalpb.SCAN_TYPE_DATA_OBJECT:
			// Make sure projections and predicates get passed down to the
			// individual scan.
			partition := target.DataObject
			partition.Predicates = set.Predicates
			partition.Projections = set.Projections

			targets = append(targets, newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
				return tracePipeline("physical.DataObjScan", c.executeDataObjScan(ctx, partition))
			}, nil))
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
