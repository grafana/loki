package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

type Config struct {
	BatchSize int64 `yaml:"batch_size"`
}

func Run(ctx context.Context, cfg Config, plan *physical.Plan) Pipeline {
	c := &Context{
		plan:      plan,
		batchSize: cfg.BatchSize,
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
	default:
		return errorPipeline(fmt.Errorf("invalid node type: %T", node))
	}
}

func (c *Context) executeDataObjScan(_ context.Context, _ *physical.DataObjScan) Pipeline {
	return errorPipeline(errNotImplemented)
}

func (c *Context) executeSortMerge(_ context.Context, _ *physical.SortMerge, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	return errorPipeline(errNotImplemented)
}

func (c *Context) executeLimit(_ context.Context, _ *physical.Limit, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	if len(inputs) > 1 {
		return errorPipeline(fmt.Errorf("limit expects exactly one input, got %d", len(inputs)))
	}

	return errorPipeline(errNotImplemented)
}

func (c *Context) executeFilter(_ context.Context, _ *physical.Filter, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	if len(inputs) > 1 {
		return errorPipeline(fmt.Errorf("filter expects exactly one input, got %d", len(inputs)))
	}

	return errorPipeline(errNotImplemented)
}

func (c *Context) executeProjection(_ context.Context, proj *physical.Projection, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	if len(inputs) > 1 {
		return errorPipeline(fmt.Errorf("projection expects exactly one input, got %d", len(inputs)))
	}

	if len(proj.Columns) == 0 {
		return errorPipeline(fmt.Errorf("projection expects at least one column, got 0"))
	}

	return errorPipeline(errNotImplemented)
}
