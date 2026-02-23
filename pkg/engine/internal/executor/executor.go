package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
		streamFilterer:     cfg.StreamFilterer,
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
	evaluator *expressionEvaluator
	bucket    objstore.Bucket
	metastore metastore.Metastore

	getExternalInputs func(ctx context.Context, node physical.Node) []Pipeline

	mergePrefetchCount int

	streamFilterer RequestStreamFilterer
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
	}, log.With(c.logger, "location", string(node.Location), "section", node.Section))

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

func (c *Context) executePointersScan(ctx context.Context, node *physical.PointersScan) Pipeline {
	if c.metastore == nil {
		return errorPipeline(ctx, errors.New("no metastore configured"))
	}

	req, err := physical.CatalogRequestToMetastoreSectionsRequest(node.Selector, node.Predicates, node.Start, node.End)
	if err != nil {
		return errorPipeline(ctx, fmt.Errorf("convert catalog request to metastore request: %w", err))
	}

	return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
		pipeline, err := newScanPointersPipeline(ctx, scanPointersOptions{
			metastore: c.metastore,
			location:  string(node.Location),
			req:       req,
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
		)

	case *physical.PointersScan:
		attrs = append(attrs,
			attribute.String("location", string(n.Location)),
			attribute.Int("num_predicates", len(n.Predicates)),
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
