package executor

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func NewFilterPipeline(filter *physical.Filter, input Pipeline, evaluator *expressionEvaluator, region *xcap.Region) *GenericPipeline {
	return newGenericPipelineWithRegion(func(ctx context.Context, inputs []Pipeline) (arrow.RecordBatch, error) {
		// Pull the next item from the input pipeline
		input := inputs[0]
		batch, err := input.Read(ctx)
		if err != nil {
			return nil, err
		}

		if batch.NumRows() == 0 {
			// Nothing to process, return an empty record with the same schema
			return batch, nil
		}

		cols := make([]*array.Boolean, 0, len(filter.Predicates))

		for i, pred := range filter.Predicates {
			vec, err := evaluator.eval(pred, batch)
			if err != nil {
				return nil, err
			}

			if vec.DataType().ID() != arrow.BOOL {
				return nil, fmt.Errorf("predicate %d returned non-boolean type %s", i, vec.DataType())
			}
			casted := vec.(*array.Boolean)
			cols = append(cols, casted)
		}

		return filterBatch(batch, func(i int) bool {
			for _, p := range cols {
				if !p.IsValid(i) || !p.Value(i) {
					return false
				}
			}
			return true
		}), nil
	}, region, input)
}

// This is a very inefficient approach which creates a new filtered batch from a
// pre-existing batch. Additionally, there is not plumbing in the arrow library
// to do this efficiently, meaning we have to do a lot of roundabout type coercion
// to ensure we can use the arrow builders.
//
// NB: One saving grace is that many filters will skip this step via predicate
// pushdown optimizations.
//
// We should re-think this approach.
func filterBatch(batch arrow.RecordBatch, include func(int) bool) arrow.RecordBatch {
	fields := batch.Schema().Fields()

	builders := make([]array.Builder, len(fields))
	additions := make([]func(int), len(fields))

	for i, field := range fields {
		switch field.Type.ID() {
		case arrow.BOOL:
			builder := array.NewBooleanBuilder(memory.DefaultAllocator)
			builder.Reserve(int(batch.NumRows()))
			builders[i] = builder
			additions[i] = func(offset int) {
				src := batch.Column(i).(*array.Boolean)
				builder.Append(src.Value(offset))
			}
		case arrow.STRING:
			builder := array.NewStringBuilder(memory.DefaultAllocator)
			builder.Reserve(int(batch.NumRows()))
			builders[i] = builder
			additions[i] = func(offset int) {
				src := batch.Column(i).(*array.String)
				builder.Append(src.Value(offset))
			}
		case arrow.UINT64:
			builder := array.NewUint64Builder(memory.DefaultAllocator)
			builder.Reserve(int(batch.NumRows()))
			builders[i] = builder
			additions[i] = func(offset int) {
				src := batch.Column(i).(*array.Uint64)
				builder.Append(src.Value(offset))
			}
		case arrow.INT64:
			builder := array.NewInt64Builder(memory.DefaultAllocator)
			builder.Reserve(int(batch.NumRows()))
			builders[i] = builder
			additions[i] = func(offset int) {
				src := batch.Column(i).(*array.Int64)
				builder.Append(src.Value(offset))
			}
		case arrow.FLOAT64:
			builder := array.NewFloat64Builder(memory.DefaultAllocator)
			builder.Reserve(int(batch.NumRows()))
			builders[i] = builder
			additions[i] = func(offset int) {
				src := batch.Column(i).(*array.Float64)
				builder.Append(src.Value(offset))
			}
		case arrow.TIMESTAMP:
			builder := array.NewTimestampBuilder(memory.DefaultAllocator, &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"})
			builder.Reserve(int(batch.NumRows()))
			builders[i] = builder
			additions[i] = func(offset int) {
				src := batch.Column(i).(*array.Timestamp)
				builder.Append(src.Value(offset))
			}
		default:
			panic(fmt.Sprintf("unimplemented type in filterBatch: %s", field.Type.Name()))
		}
	}

	var ct int64
	for i := 0; i < int(batch.NumRows()); i++ {
		if !include(i) {
			continue
		}

		for col, add := range additions {
			if batch.Column(col).IsNull(i) {
				builders[col].AppendNull()
				continue
			}

			add(i)
		}
		ct++
	}

	arrays := make([]arrow.Array, len(fields))
	for i, builder := range builders {
		arrays[i] = builder.NewArray()
	}

	return array.NewRecordBatch(batch.Schema(), arrays, ct)
}
