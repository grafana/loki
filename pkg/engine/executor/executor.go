package executor

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"iter"

	"github.com/apache/arrow/go/v18/arrow"
	"github.com/apache/arrow/go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

var (
	errNotImplemented = errors.New("pipeline not implemented")
)

type Pipeline = iter.Seq2[arrow.Record, error]

type Config struct {
	BatchSize int64 `yaml:"batch_size"`
}

func Run(ctx context.Context, cfg Config, plan *physical.Plan) iter.Seq2[arrow.Record, error] {
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
	return func(yield func(arrow.Record, error) bool) {
		yield(nil, err)
	}
}
func emptyPipeline() Pipeline {
	return func(yield func(arrow.Record, error) bool) {
		yield(nil, nil)
	}
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

func (c *Context) executeDataGenerator(ctx context.Context, n *dataGenerator) Pipeline {
	rows := int64(0)
	limit := int64(n.limit)

	return func(yield func(arrow.Record, error) bool) {
		for {
			// Stop once we reached the limit
			if rows >= limit {
				return
			}

			// Create a new batch
			batch := createBatch(rows, c.batchSize)
			rows += batch.NumRows()

			// Adjust batch size
			n := c.batchSize
			if rows > limit {
				n = rows - limit - 1
			}

			if !yield(batch.NewSlice(0, n), nil) {
				return
			}
		}
	}
}

func (c *Context) executeDataObjScan(ctx context.Context, n *physical.DataObjScan) Pipeline {
	return errorPipeline(errNotImplemented)
}

func (c *Context) executeSortMerge(ctx context.Context, n *physical.SortMerge, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}

	return func(yield func(arrow.Record, error) bool) {
		nextFn := make([]func() (arrow.Record, error, bool), len(inputs))
		stopFn := make([]func(), len(inputs))
		active := make([]bool, len(inputs))
		batches := make([]arrow.Record, len(inputs))
		rows := make([]int64, len(inputs))

		mh := &MinHeap{}
		heap.Init(mh)

		for i, input := range inputs {
			nextFn[i], stopFn[i] = iter.Pull2(input)
			active[i] = true
			defer stopFn[i]()

			// Pull first batch and load it into the heap
			next := nextFn[i]
			batch, err, ok := next()
			if !ok {
				active[i] = false
				continue
			}
			if err != nil {
				yield(nil, err)
				return
			}
			batches[i] = batch
			rows[i] = batch.NumRows()
			col := batch.Column(2) // assuming timestamp column is at index 2
			tsCol, ok := col.(*array.Uint64)
			if !ok {
				yield(nil, errors.New("column is not a timestamp column"))
				return
			}

			for j := 0; int64(j) < batch.NumRows(); j++ {
				row := Row{
					value:   tsCol.Value(j),
					rowIdx:  j,
					iterIdx: i,
				}
				fmt.Printf("Push(row) %+v\n", row)
				heap.Push(mh, row)
			}
		}

		for mh.Len() > 0 {
			row := heap.Pop(mh).(Row)
			i := row.iterIdx
			r := row.rowIdx

			rows[i]--

			fmt.Printf("yield(row) %+v\n", row)

			// TODO(chaudum): This yields single-row batches!!!
			if !yield(batches[row.iterIdx].NewSlice(int64(r), int64(r)+1), nil) {
				return
			}

			if rows[i] <= 0 {
				batches[i].Release()
				next := nextFn[i]
				batch, err, ok := next()
				if !ok {
					active[i] = false
					fmt.Printf("no more results from downstream %d\n", i)
					continue
				}
				if err != nil {
					yield(nil, err)
					return
				}

				batches[i] = batch
				rows[i] = batch.NumRows()
				fmt.Printf("new rows %d from input %d\n", rows[i], i)
				col := batch.Column(2) // assuming timestamp column is at index 2
				tsCol, ok := col.(*array.Uint64)
				if !ok {
					yield(nil, errors.New("column is not a timestamp column"))
					return
				}

				for j := 0; int64(j) < batch.NumRows(); j++ {
					row := Row{
						value:   tsCol.Value(j),
						rowIdx:  j,
						iterIdx: i,
					}
					fmt.Printf("Push(row) %+v\n", row)
					heap.Push(mh, row)
				}
			}
		}

	}
}

func (c *Context) executeLimit(ctx context.Context, n *physical.Limit, inputs []Pipeline) Pipeline {
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

	input := inputs[0]
	return func(yield func(arrow.Record, error) bool) {

		// Stop once we reached the limit
		if limit <= 0 {
			return
		}

		// TODO(chaudum): Skip yielding zero-lenght batches while offset > 0

		// Pull the next item from downstream
		input(func(batch arrow.Record, err error) bool {
			// If there's an error, pass it through
			if err != nil {
				return yield(nil, err)
			}

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
				return false
			}

			return yield(batch.NewSlice(start, end), nil)
		})
	}
}

func (c *Context) executeFilter(ctx context.Context, n *physical.Filter, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}
	if len(inputs) > 1 {
		return errorPipeline(fmt.Errorf("filter expects exactly one input, got %d", len(inputs)))
	}
	return inputs[0]
}

func (c *Context) executeProjection(ctx context.Context, n *physical.Projection, inputs []Pipeline) Pipeline {
	if len(inputs) == 0 {
		return emptyPipeline()
	}
	if len(inputs) > 1 {
		return errorPipeline(fmt.Errorf("projection expects exactly one input, got %d", len(inputs)))
	}
	return inputs[0]
}
