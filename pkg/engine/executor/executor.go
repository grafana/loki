package executor

import (
	"container/heap"
	"context"
	"errors"
	"fmt"

	"github.com/apache/arrow/go/v18/arrow"
	"github.com/apache/arrow/go/v18/arrow/array"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

var (
	errNotImplemented = errors.New("pipeline not implemented")
)

type Config struct {
	BatchSize int64 `yaml:"batch_size"`
}

func Run(ctx context.Context, cfg Config, plan *physical.Plan) Pipeline {
	c := &Context{
		plan:      plan,
		batchSize: cfg.BatchSize,
	}
	node, err := plan.Root()
	if err != nil {
		errorPipeline(err)
	}
	return c.execute(ctx, node)
}

func errorPipeline(err error) Pipeline {
	return newGenericPipeline(Local, func(_ []Pipeline) State {
		return State{err: err}
	})
}

func emptyPipeline() Pipeline {
	return newGenericPipeline(Local, func(_ []Pipeline) State {
		return Exhausted
	})
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
	case *dataGenerator:
		return c.executeDataGenerator(ctx, n)
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

func (c *Context) executeDataGenerator(_ context.Context, n *dataGenerator) Pipeline {
	rows := int64(0)
	limit := n.limit

	return newGenericPipeline(Local, func(_ []Pipeline) State {
		// Stop once we reached the limit
		if rows >= limit {
			return Exhausted
		}

		// Create a new batch
		batch := createBatch(rows, c.batchSize)
		rows += batch.NumRows()

		// Adjust batch size
		n := c.batchSize
		if rows > limit {
			n = rows - limit - 1
		}

		rec := batch.NewSlice(0, n)

		return success(rec)
	})
}

func (c *Context) executeDataObjScan(_ context.Context, _ *physical.DataObjScan) Pipeline {
	return errorPipeline(errNotImplemented)
}

func (c *Context) executeSortMerge(_ context.Context, n *physical.SortMerge, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	mh := MinHeap()
	heap.Init(mh)

	return &HeapSortMerge{
		inputs:     inputs,
		heap:       mh,
		batchSize:  c.batchSize,
		columnEval: n.Column.Evaluate,
	}
}

func (c *Context) executeLimit(_ context.Context, n *physical.Limit, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}
	if len(inputs) > 1 {
		return errorPipeline(fmt.Errorf("limit expects exactly one input, got %d", len(inputs)))
	}

	// We gradually reduce offset and limit as we process more records, as the
	// offset and limit may cross record boundaries.
	var (
		offset = int64(n.Skip)
		limit  = int64(n.Fetch)
	)

	return newGenericPipeline(Local, func(inputs []Pipeline) State {
		// Stop once we reached the limit
		if limit <= 0 {
			return Exhausted
		}

		// TODO(chaudum): Skip yielding zero-length batches while offset > 0

		// Pull the next item from downstream
		input := inputs[0]
		err := input.Read()
		if err != nil {
			return state(input.Value())
		}
		batch, _ := input.Value()

		// We want to slice batch so it only contains the rows we're looking for
		// accounting for both the limit and offset.
		// We constrain the start and end to be within the bounds of the record.
		var (
			start  = min(offset, batch.NumRows())
			end    = min(start+limit, batch.NumRows())
			length = end - start
		)

		offset -= start
		limit -= length

		if length <= 0 && offset <= 0 {
			return Exhausted
		}

		rec := batch.NewSlice(start, end)
		return success(rec)
	}, inputs...)
}

func (c *Context) executeFilter(_ context.Context, _ *physical.Filter, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}
	if len(inputs) > 1 {
		return errorPipeline(fmt.Errorf("filter expects exactly one input, got %d", len(inputs)))
	}
	return inputs[0]
}

func (c *Context) executeProjection(_ context.Context, proj *physical.Projection, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}
	if len(inputs) > 1 {
		return errorPipeline(fmt.Errorf("projection expects exactly one input, got %d", len(inputs)))
	}

	// If no columns are specified, pass through the input pipeline
	if len(proj.Columns) == 0 {
		return errorPipeline(fmt.Errorf("projection expects at least one column, got 0"))
	}

	// Get the column names from the projection expressions
	columnNames := make([]string, len(proj.Columns))
	for i, col := range proj.Columns {
		if colExpr, ok := col.(*physical.ColumnExpr); ok {
			columnNames[i] = colExpr.Ref.Column
		} else {
			return errorPipeline(fmt.Errorf("projection column %d is not a column expression", i))
		}
	}

	return newGenericPipeline(Local, func(inputs []Pipeline) State {
		// Pull the next item from the input pipeline
		input := inputs[0]
		err := input.Read()
		if err != nil {
			return state(nil, err)
		}

		batch, err := input.Value()
		if err != nil {
			return state(nil, err)
		}

		// Project the columns
		indices := make([]int, 0, len(columnNames))
		projectedColumns := make([]arrow.Array, 0, len(columnNames))
		projectedNames := make([]string, 0, len(columnNames))

		// Find the index of each projected column in the input batch
		for _, name := range columnNames {
			for i := 0; i < int(batch.NumCols()); i++ {
				if batch.ColumnName(i) == name {
					indices = append(indices, i)
					projectedColumns = append(projectedColumns, batch.Column(i))
					projectedNames = append(projectedNames, name)
					break
				}
			}
		}

		// Create a new schema with only the projected columns
		fields := make([]arrow.Field, len(projectedColumns))
		for i, name := range projectedNames {
			fields[i] = arrow.Field{Name: name, Type: projectedColumns[i].DataType()}
		}
		schema := arrow.NewSchema(fields, nil)

		// Create a new record with only the projected columns
		projected := array.NewRecord(schema, projectedColumns, batch.NumRows())
		return success(projected)
	}, inputs...)
}
