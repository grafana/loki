package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/thanos-io/objstore"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var tracer = otel.Tracer("pkg/engine/internal/executor")

type Config struct {
	BatchSize int64
	Bucket    objstore.Bucket
	Metastore metastore.Metastore

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
		metastore:          cfg.Metastore,
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
	metastore metastore.Metastore

	getExternalInputs func(ctx context.Context, node physical.Node) []Pipeline

	mergePrefetchCount int
}

func (c *Context) execute(ctx context.Context, node physical.Node) Pipeline {
	// Start a new xcap.Region for this node.
	// Region is created in preorder traversal to maintain the parent-child relationship.
	ctx, nodeRegion := startRegionForNode(ctx, node)

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
		return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
			return newObservedPipeline(c.executeDataObjScan(ctx, n, nodeRegion))
		}, inputs)
	case *physical.PointersScan:
		return newObservedPipeline(c.executePointersScan(ctx, n, nodeRegion))
	case *physical.TopK:
		return newObservedPipeline(c.executeTopK(ctx, n, inputs, nodeRegion))
	case *physical.Limit:
		return newObservedPipeline(c.executeLimit(ctx, n, inputs, nodeRegion))
	case *physical.Filter:
		return newObservedPipeline(c.executeFilter(ctx, n, inputs, nodeRegion))
	case *physical.Projection:
		return newObservedPipeline(c.executeProjection(ctx, n, inputs, nodeRegion))
	case *physical.RangeAggregation:
		return newObservedPipeline(c.executeRangeAggregation(ctx, n, inputs, nodeRegion))
	case *physical.VectorAggregation:
		return newObservedPipeline(c.executeVectorAggregation(ctx, n, inputs, nodeRegion))
	case *physical.ColumnCompat:
		return newObservedPipeline(c.executeColumnCompat(ctx, n, inputs, nodeRegion))
	case *physical.Merge:
		return newObservedPipeline(c.executeMerge(ctx, n, inputs, nodeRegion))
	case *physical.Parallelize:
		return c.executeParallelize(ctx, n, inputs, nodeRegion)
	case *physical.ScanSet:
		return c.executeScanSet(ctx, n, nodeRegion)
	default:
		return errorPipeline(ctx, fmt.Errorf("invalid node type: %T", node))
	}
}

func (c *Context) executeDataObjScan(ctx context.Context, node *physical.DataObjScan, region *xcap.Region) Pipeline {
	if c.bucket == nil {
		return errorPipelineWithRegion(ctx, errors.New("no object store bucket configured"), region)
	}

	obj, err := dataobj.FromBucket(ctx, c.bucket, string(node.Location))
	if err != nil {
		return errorPipelineWithRegion(ctx, fmt.Errorf("creating data object: %w", err), region)
	}
	region.AddEvent("opened dataobj")

	var (
		streamsSection *streams.Section
		logsSection    *logs.Section
	)

	tenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return errorPipelineWithRegion(ctx, fmt.Errorf("missing org ID: %w", err), region)
	}

	for _, sec := range obj.Sections().Filter(streams.CheckSection) {
		if sec.Tenant != tenant {
			continue
		}

		if streamsSection != nil {
			return errorPipelineWithRegion(ctx, fmt.Errorf("multiple streams sections found in data object %q", node.Location), region)
		}

		var err error
		streamsSection, err = streams.Open(ctx, sec)
		if err != nil {
			return errorPipelineWithRegion(ctx, fmt.Errorf("opening streams section %q: %w", sec.Type, err), region)
		}
		region.AddEvent("opened streams section")
	}
	if streamsSection == nil {
		return errorPipelineWithRegion(ctx, fmt.Errorf("streams section not found in data object %q", node.Location), region)
	}

	for i, sec := range obj.Sections().Filter(logs.CheckSection) {
		if i != node.Section {
			continue
		}

		var err error
		logsSection, err = logs.Open(ctx, sec)
		if err != nil {
			return errorPipelineWithRegion(ctx, fmt.Errorf("opening logs section %q: %w", sec.Type, err), region)
		}
		region.AddEvent("opened logs section")
		break
	}
	if logsSection == nil {
		return errorPipelineWithRegion(ctx, fmt.Errorf("logs section %d not found in data object %q", node.Section, node.Location), region)
	}

	predicates := make([]logs.Predicate, 0, len(node.Predicates))

	for _, p := range node.Predicates {
		conv, err := buildLogsPredicate(p, logsSection.Columns())
		if err != nil {
			return errorPipelineWithRegion(ctx, err, region)
		}
		predicates = append(predicates, conv)
	}
	region.AddEvent("constructed predicate")

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

func (c *Context) executePointersScan(ctx context.Context, node *physical.PointersScan, region *xcap.Region) Pipeline {
	if c.metastore == nil {
		return errorPipelineWithRegion(ctx, errors.New("no metastore configured"), region)
	}

	req, err := physical.CatalogRequestToMetastoreSectionsRequest(node.Selector, node.Predicates, node.Start, node.End)
	if err != nil {
		return errorPipelineWithRegion(ctx, fmt.Errorf("convert catalog request to metastore request: %w", err), region)
	}

	return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
		pipeline, err := newScanPointersPipeline(ctx, scanPointersOptions{
			metastore: c.metastore,
			location:  string(node.Location),
			req:       req,
			region:    region,
		})
		if err != nil {
			return errorPipelineWithRegion(ctx, err, region)
		}
		return pipeline
	}, nil)
}

func (c *Context) executeTopK(ctx context.Context, topK *physical.TopK, inputs []Pipeline, region *xcap.Region) Pipeline {
	if len(inputs) == 0 {
		return emptyPipelineWithRegion(region)
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
		return errorPipelineWithRegion(ctx, err, region)
	}

	return pipeline
}

func (c *Context) executeLimit(ctx context.Context, limit *physical.Limit, inputs []Pipeline, region *xcap.Region) Pipeline {
	if len(inputs) == 0 {
		return emptyPipelineWithRegion(region)
	}

	if len(inputs) > 1 {
		return errorPipelineWithRegion(ctx, fmt.Errorf("limit expects exactly one input, got %d", len(inputs)), region)
	}

	return NewLimitPipeline(inputs[0], limit.Skip, limit.Fetch, region)
}

func (c *Context) executeFilter(ctx context.Context, filter *physical.Filter, inputs []Pipeline, region *xcap.Region) Pipeline {
	if len(inputs) == 0 {
		return emptyPipelineWithRegion(region)
	}

	if len(inputs) > 1 {
		return errorPipelineWithRegion(ctx, fmt.Errorf("filter expects exactly one input, got %d", len(inputs)), region)
	}

	return NewFilterPipeline(filter, inputs[0], c.evaluator, region)
}

func (c *Context) executeProjection(ctx context.Context, proj *physical.Projection, inputs []Pipeline, region *xcap.Region) Pipeline {
	if len(inputs) == 0 {
		return emptyPipelineWithRegion(region)
	}

	if len(inputs) > 1 {
		// unsupported for now
		return errorPipelineWithRegion(ctx, fmt.Errorf("projection expects exactly one input, got %d", len(inputs)), region)
	}

	if len(proj.Expressions) == 0 {
		return errorPipelineWithRegion(ctx, fmt.Errorf("projection expects at least one expression, got 0"), region)
	}

	p, err := NewProjectPipeline(inputs[0], proj, &c.evaluator, region)
	if err != nil {
		return errorPipelineWithRegion(ctx, err, region)
	}
	return p
}

func (c *Context) executeRangeAggregation(ctx context.Context, plan *physical.RangeAggregation, inputs []Pipeline, region *xcap.Region) Pipeline {
	if len(inputs) == 0 {
		return emptyPipelineWithRegion(region)
	}

	pipeline, err := newRangeAggregationPipeline(inputs, c.evaluator, rangeAggregationOptions{
		grouping:      plan.Grouping,
		startTs:       plan.Start,
		endTs:         plan.End,
		rangeInterval: plan.Range,
		step:          plan.Step,
		operation:     plan.Operation,
	}, region)
	if err != nil {
		return errorPipelineWithRegion(ctx, err, region)
	}

	return pipeline
}

func (c *Context) executeVectorAggregation(ctx context.Context, plan *physical.VectorAggregation, inputs []Pipeline, region *xcap.Region) Pipeline {
	if len(inputs) == 0 {
		return emptyPipelineWithRegion(region)
	}

	pipeline, err := newVectorAggregationPipeline(inputs, plan.Grouping, c.evaluator, plan.Operation, region)
	if err != nil {
		return errorPipelineWithRegion(ctx, err, region)
	}

	return pipeline
}

func (c *Context) executeColumnCompat(ctx context.Context, compat *physical.ColumnCompat, inputs []Pipeline, region *xcap.Region) Pipeline {
	if len(inputs) == 0 {
		return emptyPipelineWithRegion(region)
	}

	if len(inputs) > 1 {
		return errorPipelineWithRegion(ctx, fmt.Errorf("columncompat expects exactly one input, got %d", len(inputs)), region)
	}

	return newColumnCompatibilityPipeline(compat, inputs[0], region)
}

func (c *Context) executeMerge(ctx context.Context, _ *physical.Merge, inputs []Pipeline, region *xcap.Region) Pipeline {
	if len(inputs) == 0 {
		return emptyPipelineWithRegion(region)
	}

	pipeline, err := newMergePipeline(inputs, c.mergePrefetchCount, region)
	if err != nil {
		return errorPipelineWithRegion(ctx, err, region)
	}

	return pipeline
}

func (c *Context) executeParallelize(ctx context.Context, _ *physical.Parallelize, inputs []Pipeline, region *xcap.Region) Pipeline {
	if len(inputs) == 0 {
		return emptyPipelineWithRegion(region)
	} else if len(inputs) > 1 {
		return errorPipelineWithRegion(ctx, fmt.Errorf("parallelize expects exactly one input, got %d", len(inputs)), region)
	}

	// Parallelize is a hint node to the scheduler for parallel execution. If we
	// see an Parallelize node in the plan, we ignore it and immediately
	// propagate up the input.
	if region != nil {
		region.End()
	}
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

			nodeCtx, partitionRegion := startRegionForNode(ctx, partition)

			targets = append(targets, newLazyPipeline(func(_ context.Context, _ []Pipeline) Pipeline {
				return newObservedPipeline(c.executeDataObjScan(nodeCtx, partition, partitionRegion))
			}, nil))
		case physical.ScanTypePointers:
			partition := target.Pointers
			nodeCtx, partitionRegion := startRegionForNode(ctx, partition)
			targets = append(targets, c.executePointersScan(nodeCtx, partition, partitionRegion))
		default:
			return errorPipelineWithRegion(ctx, fmt.Errorf("unrecognized ScanSet target %s", target.Type), region)
		}
	}
	if len(targets) == 0 {
		return emptyPipelineWithRegion(region)
	}

	pipeline, err := newMergePipeline(targets, c.mergePrefetchCount, region)
	if err != nil {
		return errorPipelineWithRegion(ctx, err, region)
	}

	return pipeline
}

// startRegionForNode starts xcap.Region for the given physical plan node.
// It internally calls xcap.StartRegion with attributes relevant to the node type.
func startRegionForNode(ctx context.Context, n physical.Node) (context.Context, *xcap.Region) {
	// Include node ID in the region attributes to retain a link to the physical plan node.
	attributes := []attribute.KeyValue{
		attribute.String("node_id", n.ID().String()),
	}

	switch n := n.(type) {
	case *physical.DataObjScan:
		attributes = append(attributes,
			attribute.String("location", string(n.Location)),
			attribute.Int("section", n.Section),
			attribute.Int("num_stream_ids", len(n.StreamIDs)),
			attribute.Int("num_predicates", len(n.Predicates)),
			attribute.Int("num_projections", len(n.Projections)),
		)

	case *physical.PointersScan:
		attributes = append(attributes,
			attribute.String("location", string(n.Location)),
			attribute.Int("num_predicates", len(n.Predicates)),
		)

	case *physical.TopK:
		attributes = append(attributes,
			attribute.Int("k", n.K),
			attribute.Bool("ascending", n.Ascending),
			attribute.Bool("nulls_first", n.NullsFirst),
		)
		if n.SortBy != nil {
			attributes = append(attributes, attribute.Stringer("sort_by", n.SortBy))
		}

	case *physical.Limit:
		attributes = append(attributes,
			attribute.Int("skip", int(n.Skip)),
			attribute.Int("fetch", int(n.Fetch)),
		)

	case *physical.Filter:
		attributes = append(attributes,
			attribute.Int("num_predicates", len(n.Predicates)),
		)

	case *physical.Projection:
		attributes = append(attributes,
			attribute.Int("num_expressions", len(n.Expressions)),
			attribute.Bool("all", n.All),
			attribute.Bool("drop", n.Drop),
			attribute.Bool("expand", n.Expand),
		)

	case *physical.RangeAggregation:
		attributes = append(attributes,
			attribute.String("operation", string(rune(n.Operation))),
			attribute.Int64("start_ts", n.Start.UnixNano()),
			attribute.Int64("end_ts", n.End.UnixNano()),
			attribute.Int64("range_interval", int64(n.Range)),
			attribute.Int64("step", int64(n.Step)),
			attribute.Int("num_grouping", len(n.Grouping.Columns)),
			attribute.Bool("grouping_without", n.Grouping.Without),
		)

	case *physical.VectorAggregation:
		attributes = append(attributes,
			attribute.String("operation", string(rune(n.Operation))),
			attribute.Int("num_grouping", len(n.Grouping.Columns)),
			attribute.Bool("grouping_without", n.Grouping.Without),
		)

	case *physical.ColumnCompat:
		collisionStrs := make([]string, len(n.Collisions))
		for i, ct := range n.Collisions {
			collisionStrs[i] = ct.String()
		}
		attributes = append(attributes,
			attribute.String("src", n.Source.String()),
			attribute.String("dst", n.Destination.String()),
			attribute.String("collisions", fmt.Sprintf("[%s]", strings.Join(collisionStrs, ", "))),
		)

	case *physical.ScanSet:
		attributes = append(attributes,
			attribute.Int("num_targets", len(n.Targets)),
			attribute.Int("num_predicates", len(n.Predicates)),
			attribute.Int("num_projections", len(n.Projections)),
		)
	default:
		// do nothing.
	}

	return xcap.StartRegion(ctx, n.Type().String(), xcap.WithRegionAttributes(attributes...))
}
