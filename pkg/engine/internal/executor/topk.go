package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/engine/internal/arrowagg"
	"github.com/grafana/loki/v3/pkg/engine/internal/assertions"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

type topkOptions struct {
	// Input pipeliens to compute top K from.
	Inputs []Pipeline

	// SortBy is the list of columns to sort by, in order of precedence.
	SortBy     []physical.ColumnExpression
	Ascending  bool // Sorts lines in ascending order if true.
	NullsFirst bool // When true, considers NULLs < non-NULLs when sorting.
	K          int  // Number of top rows to compute.

	// MaxUnused determines the maximum number of unused rows to retain. An
	// unused row is any row from a retained record that does not contribute to
	// the current top K.
	//
	// After the number of unused rows exceeds this value, retained records are
	// compacted into a new record only containing the current used rows.
	MaxUnused int
}

// topkPipeline performs a topk (SORT + LIMIT) operation across several input
// pipelines.
type topkPipeline struct {
	inputs []Pipeline

	// TopK can be sorted by any set of columns, but sorting by timestamp only is a special case
	// due to possibility of short circuting.
	sortByTime bool
	callbacks  []ContributingTimeRangeChangedHandler

	batch *topkBatch

	computed bool
}

var _ Pipeline = (*topkPipeline)(nil)

// newTopkPipeline creates a new topkPipeline with the given options.
func newTopkPipeline(opts topkOptions) (*topkPipeline, error) {
	fields, err := exprsToFields(opts.SortBy)
	if err != nil {
		return nil, err
	}

	sortByTime := false
	if len(fields) == 1 {
		fieldIdent, err := semconv.ParseFQN(fields[0].Name)
		if err != nil {
			return nil, err
		}
		sortByTime = semconv.ColumnIdentTimestamp.Equal(fieldIdent)
	}

	return &topkPipeline{
		inputs:     opts.Inputs,
		sortByTime: sortByTime,
		batch: &topkBatch{
			Fields:        fields,
			Ascending:     opts.Ascending,
			NullsFirst:    opts.NullsFirst,
			K:             opts.K,
			MaxUnused:     opts.MaxUnused,
			labelsBuilder: labels.NewScratchBuilder(1),
		},
	}, nil
}

func exprsToFields(exprs []physical.ColumnExpression) ([]arrow.Field, error) {
	fields := make([]arrow.Field, 0, len(exprs))
	for _, expr := range exprs {
		expr, ok := expr.(*physical.ColumnExpr)
		if !ok {
			panic("topkPipeline only supports ColumnExpr expressions")
		}

		dt, err := guessLokiType(expr.Ref)
		if err != nil {
			return nil, err
		}

		ident := semconv.NewIdentifier(expr.Ref.Column, expr.Ref.Type, dt)
		fields = append(fields, semconv.FieldFromIdent(ident, true))
	}
	return fields, nil
}

func guessLokiType(ref types.ColumnRef) (types.DataType, error) {
	switch ref.Type {
	case types.ColumnTypeBuiltin:
		switch ref.Column {
		case types.ColumnNameBuiltinTimestamp:
			return types.Loki.Timestamp, nil
		case types.ColumnNameBuiltinMessage:
			return types.Loki.String, nil
		default:
			panic(fmt.Sprintf("unsupported builtin column type %s", ref))
		}
	case types.ColumnTypeGenerated:
		return types.Loki.Float, nil
	case types.ColumnTypeAmbiguous:
		// TODO(rfratto): It's not clear how topk should sort when there's an
		// ambiguous column reference, since ambiguous column references can
		// refer to multiple columns.
		return nil, fmt.Errorf("topkPipeline does not support ambiguous column types")
	default:
		return types.Loki.String, nil
	}
}

// Open opens all input pipelines.
func (p *topkPipeline) Open(ctx context.Context) error {
	return openInputsConcurrently(ctx, p.inputs)
}

// Read computes the topk as the next record. Read blocks until all input
// pipelines have been fully read and the top K rows have been computed.
func (p *topkPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if !p.computed {
		rec, err := p.compute(ctx)
		p.computed = true

		assertions.CheckColumnDuplicates(rec)
		assertions.CheckLabelValuesDuplicates(rec)

		return rec, err
	}
	return nil, EOF
}

func (p *topkPipeline) compute(ctx context.Context) (arrow.RecordBatch, error) {
	var currentHeapMin time.Time

NextInput:
	for _, in := range p.inputs {
		for {
			rec, err := in.Read(ctx)
			if err != nil && errors.Is(err, EOF) {
				continue NextInput
			} else if err != nil {
				return nil, err
			}

			if rec.NumRows() == 0 {
				// Nothing to process
				continue
			}

			p.batch.Put(rec)

			// Short circuiting is possible only when the heap is full and it is sorted by timestamp.
			if p.sortByTime && p.batch.IsFull() {
				// We can safely assume there is 1 timestamp column and 1 row
				heapMin := p.batch.Peek().Column(0).(*array.Timestamp).Value(0).ToTime(arrow.Nanosecond)

				if p.batch.Ascending {
					// bottom k
					if currentHeapMin.IsZero() || heapMin.Before(currentHeapMin) {
						currentHeapMin = heapMin
						p.notifyAll(currentHeapMin, true)
					}
				} else {
					// top k
					if currentHeapMin.IsZero() || heapMin.After(currentHeapMin) {
						currentHeapMin = heapMin
						p.notifyAll(currentHeapMin, false)
					}
				}
			}
		}
	}

	compacted := p.batch.Compact()
	if compacted == nil {
		return nil, EOF
	}
	return reverseSameTimestampRows(compacted), nil
}

// reverseSameTimestampRows returns a copy of rec with the same overall row
// order, except that runs of consecutive rows sharing the same timestamp are
// reversed. This is a deliberately hacky/inefficient implementation used to
// test a hypothesis: it rebuilds the record one row at a time.
func reverseSameTimestampRows(rec arrow.RecordBatch) arrow.RecordBatch {
	if rec == nil || rec.NumRows() <= 1 {
		return rec
	}

	// Locate the timestamp column.
	tsCol := -1
	for i := 0; i < int(rec.NumCols()); i++ {
		ident, err := semconv.ParseFQN(rec.Schema().Field(i).Name)
		if err != nil {
			continue
		}
		if semconv.ColumnIdentTimestamp.Equal(ident) {
			tsCol = i
			break
		}
	}
	if tsCol < 0 {
		return rec // No timestamp column; nothing to reverse.
	}

	ts, ok := rec.Column(tsCol).(*array.Timestamp)
	if !ok {
		return rec
	}

	numRows := int(rec.NumRows())

	// Build the permutation: walk runs of equal timestamps and reverse each run.
	order := make([]int, 0, numRows)
	for start := 0; start < numRows; {
		end := start + 1
		for end < numRows && ts.Value(end) == ts.Value(start) {
			end++
		}
		for i := end - 1; i >= start; i-- {
			order = append(order, i)
		}
		start = end
	}

	// Rebuild the record one row at a time in the new order.
	compactor := arrowagg.NewRecords(memory.DefaultAllocator)
	for _, row := range order {
		compactor.AppendSlice(rec, int64(row), int64(row+1))
	}
	reordered, err := compactor.Aggregate()
	if err != nil {
		return rec
	}
	return reordered
}

// Close closes the resources of the pipeline.
func (p *topkPipeline) Close() {
	p.batch.Reset()
	for _, in := range p.inputs {
		in.Close()
	}
}

// SubscribeToTimeRangeChanges implements ContributingTimeRangeChangedNotifier
func (p *topkPipeline) SubscribeToTimeRangeChanges(callback ContributingTimeRangeChangedHandler) {
	p.callbacks = append(p.callbacks, callback)
}

func (p *topkPipeline) notifyAll(ts time.Time, lessThan bool) {
	for _, callback := range p.callbacks {
		callback(ts, lessThan)
	}
}
