package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

type Config struct {
	BatchSize int64
	Bucket    objstore.Bucket
}

func Run(ctx context.Context, cfg Config, plan *physical.Plan) Pipeline {
	c := &Context{
		plan:      plan,
		batchSize: cfg.BatchSize,
		bucket:    cfg.Bucket,
	}
	if plan == nil {
		return errorPipeline(errors.New("plan is nil"))
	}
	node, err := plan.Root()
	if err != nil {
		return errorPipeline(err)
	}
	return c.execute(ctx, node)
}

// Context is the execution context
type Context struct {
	batchSize int64
	plan      *physical.Plan
	evaluator expressionEvaluator
	bucket    objstore.Bucket
}

func (c *Context) execute(ctx context.Context, node physical.Node) Pipeline {
	children := c.plan.Children(node)
	inputs := make([]Pipeline, 0, len(children))
	for _, child := range children {
		inputs = append(inputs, c.execute(ctx, child))
	}

	switch n := node.(type) {
	case *physical.DataObjScan:
		return c.executeDataObjScan(ctx, n)
	case *physical.SortMerge:
		return c.executeSortMerge(ctx, n, inputs)
	case *physical.Limit:
		return c.executeLimit(ctx, n, inputs)
	case *physical.Filter:
		return c.executeFilter(ctx, n, inputs)
	case *physical.Projection:
		return c.executeProjection(ctx, n, inputs)
	case *physical.RangeAggregation:
		return c.executeRangeAggregation(ctx, n, inputs)
	case *physical.VectorAggregation:
		return c.executeVectorAggregation(ctx, n, inputs)
	default:
		return errorPipeline(fmt.Errorf("invalid node type: %T", node))
	}
}

func (c *Context) executeDataObjScan(ctx context.Context, node *physical.DataObjScan) Pipeline {
	if c.bucket == nil {
		return errorPipeline(errors.New("no object store bucket configured"))
	}

	predicates := make([]logs.RowPredicate, 0, len(node.Predicates))

	for _, p := range node.Predicates {
		conv, err := buildLogsPredicate(p)
		if err != nil {
			return errorPipeline(err)
		}
		predicates = append(predicates, conv)
	}

	obj, err := dataobj.FromBucket(ctx, c.bucket, string(node.Location))
	if err != nil {
		return errorPipeline(fmt.Errorf("creating data object: %w", err))
	}

	return newDataobjScanPipeline(ctx, dataobjScanOptions{
		Object:      obj,
		StreamIDs:   node.StreamIDs,
		Sections:    node.Sections,
		Predicates:  predicates,
		Projections: node.Projections,

		Direction: node.Direction,
		Limit:     node.Limit,

		batchSize: c.batchSize,
	})
}

func (c *Context) executeSortMerge(_ context.Context, sortmerge *physical.SortMerge, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	pipeline, err := NewSortMergePipeline(inputs, sortmerge.Order, sortmerge.Column, c.evaluator)
	if err != nil {
		return errorPipeline(err)
	}
	return pipeline
}

func (c *Context) executeLimit(_ context.Context, limit *physical.Limit, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	if len(inputs) > 1 {
		return errorPipeline(fmt.Errorf("limit expects exactly one input, got %d", len(inputs)))
	}

	return NewLimitPipeline(inputs[0], limit.Skip, limit.Fetch)
}

func (c *Context) executeFilter(_ context.Context, filter *physical.Filter, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	// TODO: support multiple inputs
	if len(inputs) > 1 {
		return errorPipeline(fmt.Errorf("filter expects exactly one input, got %d", len(inputs)))
	}

	return NewFilterPipeline(filter, inputs[0], c.evaluator)
}

func (c *Context) executeProjection(_ context.Context, proj *physical.Projection, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	if len(inputs) > 1 {
		// unsupported for now
		return errorPipeline(fmt.Errorf("projection expects exactly one input, got %d", len(inputs)))
	}

	if len(proj.Columns) == 0 {
		return errorPipeline(fmt.Errorf("projection expects at least one column, got 0"))
	}

	p, err := NewProjectPipeline(inputs[0], proj.Columns, &c.evaluator)
	if err != nil {
		return errorPipeline(err)
	}
	return p
}

func (c *Context) executeRangeAggregation(_ context.Context, plan *physical.RangeAggregation, inputs []Pipeline) Pipeline {
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
		return errorPipeline(err)
	}

	return pipeline
}

func (c *Context) executeVectorAggregation(_ context.Context, plan *physical.VectorAggregation, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	pipeline, err := NewVectorAggregationPipeline(inputs, plan.GroupBy, c.evaluator)
	if err != nil {
		return errorPipeline(err)
	}

	return pipeline
}
