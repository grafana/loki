package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/thanos-io/objstore"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor/xcap"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

var tracer = otel.Tracer("pkg/engine/internal/executor")

// regionName returns a unique name for a region based on the node type and ID.
// This ensures that each node, including partitioned nodes, has a unique region name.
func regionName(node physical.Node) string {
	return fmt.Sprintf("%s-%s", node.Type().String(), node.ID().String())
}

type Config struct {
	BatchSize int64
	Bucket    objstore.Bucket

	MergePrefetchCount int

	// GetExternalInputs is an optional function called for each node in the
	// plan. If GetExternalInputs returns a non-nil slice of Pipelines, they
	// will be used as inputs to the pipeline of node.
	GetExternalInputs func(ctx context.Context, node physical.Node) []Pipeline
}

func Run(ctx context.Context, cfg Config, plan *physical.Plan, logger log.Logger) Pipeline {
	c := &Context{
		plan:               plan,
		batchSize:          cfg.BatchSize,
		mergePrefetchCount: cfg.MergePrefetchCount,
		bucket:             cfg.Bucket,
		logger:             logger,
		evaluator:          newExpressionEvaluator(),
		getExternalInputs:  cfg.GetExternalInputs,
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

	getExternalInputs func(ctx context.Context, node physical.Node) []Pipeline

	mergePrefetchCount int
}

func (c *Context) execute(ctx context.Context, node physical.Node) Pipeline {
	children := c.plan.Children(node)
	inputs := make([]Pipeline, 0, len(children))

	var executeNodeFn func() Pipeline
	switch n := node.(type) {
	case *physical.DataObjScan:
		// DataObjScan reads from object storage to determine the full pipeline to
		// construct, making it expensive to call during planning time.
		//
		// TODO(rfratto): find a way to remove the logic from executeDataObjScan
		// which wraps the pipeline with a topk/limit without reintroducing
		// planning cost for thousands of scan nodes.

		var region *xcap.Region
		ctx, region = xcap.StartRegion(ctx, regionName(n), xcap.WithRegionAttributes(
			attribute.String("location", string(n.Location)),
			attribute.Int("section", n.Section),
			attribute.Int("num_stream_ids", len(n.StreamIDs)),
			attribute.Int("num_predicates", len(n.Predicates)),
			attribute.Int("num_projections", len(n.Projections)),
		))

		executeNodeFn = func() Pipeline {
			return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
				return newObservedPipeline(c.executeDataObjScan(ctx, n, region))
			}, inputs)
		}

	case *physical.TopK:
		var region *xcap.Region
		ctx, region = xcap.StartRegion(ctx, regionName(n), xcap.WithRegionAttributes(
			attribute.Int("k", n.K),
			attribute.Bool("ascending", n.Ascending),
			attribute.Bool("nulls_first", n.NullsFirst),
		))

		executeNodeFn = func() Pipeline {
			return newObservedPipeline(c.executeTopK(ctx, n, inputs, region))
		}
	case *physical.Limit:
		var region *xcap.Region
		ctx, region = xcap.StartRegion(ctx, regionName(n), xcap.WithRegionAttributes(
			attribute.Int("skip", int(n.Skip)),
			attribute.Int("fetch", int(n.Fetch)),
		))

		executeNodeFn = func() Pipeline {
			return newObservedPipeline(c.executeLimit(ctx, n, inputs, region))
		}
	case *physical.Filter:
		var region *xcap.Region
		ctx, region = xcap.StartRegion(ctx, regionName(n), xcap.WithRegionAttributes(
			attribute.Int("num_predicates", len(n.Predicates)),
		))

		executeNodeFn = func() Pipeline {
			return newObservedPipeline(c.executeFilter(ctx, n, inputs, region))
		}
	case *physical.Projection:
		var region *xcap.Region
		ctx, region = xcap.StartRegion(ctx, regionName(n), xcap.WithRegionAttributes(
			attribute.Int("num_expressions", len(n.Expressions)),
			attribute.Bool("all", n.All),
			attribute.Bool("drop", n.Drop),
			attribute.Bool("expand", n.Expand),
		))

		executeNodeFn = func() Pipeline {
			return newObservedPipeline(c.executeProjection(ctx, n, inputs, region))
		}
	case *physical.RangeAggregation:
		var region *xcap.Region
		ctx, region = xcap.StartRegion(ctx, regionName(n), xcap.WithRegionAttributes(
			attribute.String("operation", string(n.Operation)),
			attribute.Int64("start_ts", n.Start.UnixNano()),
			attribute.Int64("end_ts", n.End.UnixNano()),
			attribute.Int64("range_interval", int64(n.Range)),
			attribute.Int64("step", int64(n.Step)),
			attribute.Int("num_partition_by", len(n.PartitionBy)),
		))

		executeNodeFn = func() Pipeline {
			return newObservedPipeline(c.executeRangeAggregation(ctx, n, inputs, region))
		}
	case *physical.VectorAggregation:
		var region *xcap.Region
		ctx, region = xcap.StartRegion(ctx, regionName(n), xcap.WithRegionAttributes(
			attribute.String("operation", string(n.Operation)),
			attribute.Int("num_group_by", len(n.GroupBy)),
		))

		executeNodeFn = func() Pipeline {
			return newObservedPipeline(c.executeVectorAggregation(ctx, n, inputs, region))
		}
	case *physical.ColumnCompat:
		var region *xcap.Region
		ctx, region = xcap.StartRegion(ctx, regionName(n), xcap.WithRegionAttributes(
			attribute.String("src", n.Source.String()),
			attribute.String("dst", n.Destination.String()),
			attribute.String("collision", n.Collision.String()),
		))

		executeNodeFn = func() Pipeline {
			return newObservedPipeline(c.executeColumnCompat(ctx, n, inputs, region))
		}
	case *physical.Parallelize:
		executeNodeFn = func() Pipeline {
			return newObservedPipeline(c.executeParallelize(ctx, n, inputs))
		}
	case *physical.ScanSet:
		var region *xcap.Region
		ctx, region = xcap.StartRegion(ctx, regionName(n), xcap.WithRegionAttributes(
			attribute.Int("num_targets", len(n.Targets)),
			attribute.Int("num_predicates", len(n.Predicates)),
			attribute.Int("num_projections", len(n.Projections)),
		))

		executeNodeFn = func() Pipeline {
			return newObservedPipeline(c.executeScanSet(ctx, n, region))
		}
	default:
		executeNodeFn = func() Pipeline {
			return errorPipeline(ctx, fmt.Errorf("invalid node type: %T", node))

		}
	}

	for _, child := range children {
		inputs = append(inputs, c.execute(ctx, child))
	}

	if c.getExternalInputs != nil {
		inputs = append(inputs, c.getExternalInputs(ctx, node)...)
	}

	return executeNodeFn()
}

func (c *Context) executeDataObjScan(ctx context.Context, node *physical.DataObjScan, region *xcap.Region) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeDataObjScan", trace.WithAttributes(
		attribute.String("location", string(node.Location)),
		attribute.Int("section", node.Section),
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

		BatchSize: c.batchSize,
	}, log.With(c.logger, "location", string(node.Location), "section", node.Section), region)

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

func (c *Context) executeTopK(ctx context.Context, topK *physical.TopK, inputs []Pipeline, region *xcap.Region) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeTopK", trace.WithAttributes(
		attribute.Int("k", topK.K),
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
		SortBy:     []physical.ColumnExpression{topK.SortBy},
		Ascending:  topK.Ascending,
		NullsFirst: topK.NullsFirst,
		K:          topK.K,
		MaxUnused:  int(c.batchSize) * 2,
		Region:     region,
	})
	if err != nil {
		return errorPipeline(ctx, err)
	}

	return pipeline
}

func (c *Context) executeLimit(ctx context.Context, limit *physical.Limit, inputs []Pipeline, region *xcap.Region) Pipeline {
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

	return NewLimitPipeline(inputs[0], limit.Skip, limit.Fetch, region)
}

func (c *Context) executeFilter(ctx context.Context, filter *physical.Filter, inputs []Pipeline, region *xcap.Region) Pipeline {
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

	return NewFilterPipeline(filter, inputs[0], c.evaluator, region)
}

func (c *Context) executeProjection(ctx context.Context, proj *physical.Projection, inputs []Pipeline, region *xcap.Region) Pipeline {
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

	p, err := NewProjectPipeline(inputs[0], proj, &c.evaluator, region)
	if err != nil {
		return errorPipeline(ctx, err)
	}
	return p
}

func (c *Context) executeRangeAggregation(ctx context.Context, plan *physical.RangeAggregation, inputs []Pipeline, region *xcap.Region) Pipeline {
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

	pipeline, err := newRangeAggregationPipeline(inputs, c.evaluator, rangeAggregationOptions{
		partitionBy:   plan.PartitionBy,
		startTs:       plan.Start,
		endTs:         plan.End,
		rangeInterval: plan.Range,
		step:          plan.Step,
		operation:     plan.Operation,
	}, region)
	if err != nil {
		return errorPipeline(ctx, err)
	}

	return pipeline
}

func (c *Context) executeVectorAggregation(ctx context.Context, plan *physical.VectorAggregation, inputs []Pipeline, region *xcap.Region) Pipeline {
	ctx, span := tracer.Start(ctx, "Context.executeVectorAggregation", trace.WithAttributes(
		attribute.Int("num_group_by", len(plan.GroupBy)),
		attribute.Int("num_inputs", len(inputs)),
	))
	defer span.End()

	if len(inputs) == 0 {
		return emptyPipeline()
	}

	pipeline, err := newVectorAggregationPipeline(inputs, plan.GroupBy, c.evaluator, plan.Operation, region)
	if err != nil {
		return errorPipeline(ctx, err)
	}

	return pipeline
}

func (c *Context) executeColumnCompat(ctx context.Context, compat *physical.ColumnCompat, inputs []Pipeline, region *xcap.Region) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	if len(inputs) > 1 {
		return errorPipeline(ctx, fmt.Errorf("columncompat expects exactly one input, got %d", len(inputs)))
	}

	return newColumnCompatibilityPipeline(compat, inputs[0], region)
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

func (c *Context) executeScanSet(ctx context.Context, set *physical.ScanSet, region *xcap.Region) Pipeline {
	// ScanSet typically gets partitioned by the scheduler into multiple scan
	// nodes.
	//
	// However, for locally testing unpartitioned pipelines, we still supprt
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

			targets = append(targets, newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
				var dataObjRegion *xcap.Region
				ctx, dataObjRegion = xcap.StartRegion(ctx, regionName(partition), xcap.WithRegionAttributes(
					attribute.String("location", string(partition.Location)),
					attribute.Int("section", partition.Section),
					attribute.Int("num_stream_ids", len(partition.StreamIDs)),
					attribute.Int("num_predicates", len(partition.Predicates)),
					attribute.Int("num_projections", len(partition.Projections)),
				))
				return newObservedPipeline(c.executeDataObjScan(ctx, partition, dataObjRegion))
			}, nil))
		default:
			return errorPipeline(ctx, fmt.Errorf("unrecognized ScanSet target %s", target.Type))
		}
	}
	if len(targets) == 0 {
		return emptyPipeline()
	}

	pipeline, err := newMergePipeline(targets, c.mergePrefetchCount, region)
	if err != nil {
		return errorPipeline(ctx, err)
	}

	return pipeline
}
