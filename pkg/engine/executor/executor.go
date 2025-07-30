package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

var tracer = otel.Tracer("pkg/engine/executor")

type Config struct {
	BatchSize int64
	Bucket    objstore.Bucket

	DataobjScanPageCacheSize int64
}

func Run(ctx context.Context, cfg Config, plan *physical.Plan, logger log.Logger) Pipeline {
	c := &Context{
		plan:      plan,
		batchSize: cfg.BatchSize,
		bucket:    cfg.Bucket,
		logger:    logger,

		dataobjScanPageCacheSize: cfg.DataobjScanPageCacheSize,
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
	plan      *physical.Plan
	evaluator expressionEvaluator
	bucket    objstore.Bucket

	dataobjScanPageCacheSize int64
}

func (c *Context) execute(ctx context.Context, node physical.Node) Pipeline {
	children := c.plan.Children(node)
	inputs := make([]Pipeline, 0, len(children))
	for _, child := range children {
		inputs = append(inputs, c.execute(ctx, child))
	}

	switch n := node.(type) {
	case *physical.DataObjScan:
		return tracePipeline("physical.DataObjScan", c.executeDataObjScan(ctx, n))
	case *physical.SortMerge:
		return tracePipeline("physical.SortMerge", c.executeSortMerge(ctx, n, inputs))
	case *physical.Limit:
		return tracePipeline("physical.Limit", c.executeLimit(ctx, n, inputs))
	case *physical.Filter:
		return tracePipeline("physical.Filter", c.executeFilter(ctx, n, inputs))
	case *physical.Projection:
		return tracePipeline("physical.Projection", c.executeProjection(ctx, n, inputs))
	case *physical.RangeAggregation:
		return tracePipeline("physical.RangeAggregation", c.executeRangeAggregation(ctx, n, inputs))
	case *physical.VectorAggregation:
		return tracePipeline("physical.VectorAggregation", c.executeVectorAggregation(ctx, n, inputs))
	default:
		return errorPipeline(ctx, fmt.Errorf("invalid node type: %T", node))
	}
}

func (c *Context) executeDataObjScan(ctx context.Context, node *physical.DataObjScan) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeDataObjScan", trace.WithAttributes(
		attribute.String("location", string(node.Location)),
		attribute.Int("section", node.Section),
		attribute.Stringer("direction", node.Direction),
		attribute.Int("limit", int(node.Limit)),
		attribute.Int("num_stream_ids", len(node.StreamIDs)),
		attribute.Int("num_predicates", len(node.Predicates)),
		attribute.Int("num_projections", len(node.Projections)),
	))
	defer span.End()

	if c.bucket == nil {
		return errorPipeline(ctx, errors.New("no object store bucket configured"))
	}

	obj, err := dataobj.FromBucket(ctx, c.bucket, string(node.Location))
	if err != nil {
		return errorPipeline(ctx, fmt.Errorf("creating data object: %w", err))
	}
	span.AddEvent("opened dataobj")

	var (
		streamsSection *streams.Section
		logsSection    *logs.Section
	)

	for _, sec := range obj.Sections().Filter(streams.CheckSection) {
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
		if i != node.Section {
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
		conv, err := buildLogsPredicate(p, logsSection.Columns())
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
		StreamIDs:   node.StreamIDs,
		Predicates:  predicates,
		Projections: node.Projections,

		// TODO(rfratto): pass custom allocator
		Allocator: memory.DefaultAllocator,

		BatchSize: c.batchSize,
		CacheSize: int(c.dataobjScanPageCacheSize),
	})

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
	if node.Direction != physical.UNSORTED && (node.Direction != logsSortOrder(sortDirection) || sortType != logs.ColumnTypeTimestamp) {
		level.Debug(c.logger).Log("msg", "sorting logs section", "source_sort", sortType, "source_direction", sortDirection, "requested_sort", logs.ColumnTypeTimestamp, "requested_dir", node.Direction)

		pipeline, err = newTopkPipeline(topkOptions{
			Inputs: []Pipeline{pipeline},

			SortBy: []physical.ColumnExpression{
				&physical.ColumnExpr{
					Ref: types.ColumnRef{
						Column: types.ColumnNameBuiltinTimestamp,
						Type:   types.ColumnTypeBuiltin,
					},
				},
			},
			Ascending: node.Direction == physical.ASC,
			K:         int(node.Limit),

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

func logsSortOrder(dir logs.SortDirection) physical.SortOrder {
	switch dir {
	case logs.SortDirectionAscending:
		return physical.ASC
	case logs.SortDirectionDescending:
		return physical.DESC
	}

	return physical.UNSORTED
}

func (c *Context) executeSortMerge(ctx context.Context, sortmerge *physical.SortMerge, inputs []Pipeline) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeSortMerge", trace.WithAttributes(
		attribute.Stringer("order", sortmerge.Order),
		attribute.Int("num_inputs", len(inputs)),
	))
	if sortmerge.Column != nil {
		span.SetAttributes(attribute.Stringer("column", sortmerge.Column))
	}
	defer span.End()

	if len(inputs) == 0 {
		return emptyPipeline()
	}

	pipeline, err := NewSortMergePipeline(inputs, sortmerge.Order, sortmerge.Column, c.evaluator)
	if err != nil {
		return errorPipeline(ctx, err)
	}
	return pipeline
}

func (c *Context) executeLimit(ctx context.Context, limit *physical.Limit, inputs []Pipeline) Pipeline {
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

func (c *Context) executeFilter(ctx context.Context, filter *physical.Filter, inputs []Pipeline) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeFilter", trace.WithAttributes(
		attribute.Int("num_inputs", len(inputs)),
	))
	defer span.End()

	if len(inputs) == 0 {
		return emptyPipeline()
	}

	// TODO: support multiple inputs
	if len(inputs) > 1 {
		return errorPipeline(ctx, fmt.Errorf("filter expects exactly one input, got %d", len(inputs)))
	}

	return NewFilterPipeline(filter, inputs[0], c.evaluator)
}

func (c *Context) executeProjection(ctx context.Context, proj *physical.Projection, inputs []Pipeline) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeProjection", trace.WithAttributes(
		attribute.Int("num_columns", len(proj.Columns)),
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

	if len(proj.Columns) == 0 {
		return errorPipeline(ctx, fmt.Errorf("projection expects at least one column, got 0"))
	}

	p, err := NewProjectPipeline(inputs[0], proj.Columns, &c.evaluator)
	if err != nil {
		return errorPipeline(ctx, err)
	}
	return p
}

func (c *Context) executeRangeAggregation(ctx context.Context, plan *physical.RangeAggregation, inputs []Pipeline) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeRangeAggregation", trace.WithAttributes(
		attribute.Int("num_partition_by", len(plan.PartitionBy)),
		attribute.Int64("start_ts", plan.Start.UnixNano()),
		attribute.Int64("end_ts", plan.End.UnixNano()),
		attribute.Int64("range_interval", int64(plan.Range)),
		attribute.Int64("step", int64(plan.Step)),
		attribute.Int("num_inputs", len(inputs)),
	))
	defer span.End()

	if len(inputs) == 0 {
		return emptyPipeline()
	}

	pipeline, err := NewRangeAggregationPipeline(inputs, c.evaluator, rangeAggregationOptions{
		partitionBy:   plan.PartitionBy,
		startTs:       plan.Start,
		endTs:         plan.End,
		rangeInterval: plan.Range,
		step:          plan.Step,
	})
	if err != nil {
		return errorPipeline(ctx, err)
	}

	return pipeline
}

func (c *Context) executeVectorAggregation(ctx context.Context, plan *physical.VectorAggregation, inputs []Pipeline) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeVectorAggregation", trace.WithAttributes(
		attribute.Int("num_group_by", len(plan.GroupBy)),
		attribute.Int("num_inputs", len(inputs)),
	))
	defer span.End()

	if len(inputs) == 0 {
		return emptyPipeline()
	}

	pipeline, err := NewVectorAggregationPipeline(inputs, plan.GroupBy, c.evaluator)
	if err != nil {
		return errorPipeline(ctx, err)
	}

	return pipeline
}
