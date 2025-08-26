package executor

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

func NewFilterPipeline(filter *physical.Filter, input Pipeline, evaluator expressionEvaluator) *GenericPipeline {
	return newGenericPipeline(Local, func(ctx context.Context, inputs []Pipeline) state {
		// Pull the next item from the input pipeline
		input := inputs[0]
		err := input.Read(ctx)
		if err != nil {
			return failureState(err)
		}

		batch, err := input.Value()
		if err != nil {
			return failureState(err)
		}

		cols := make([]*array.Boolean, 0, len(filter.Predicates))
		defer func() {
			for _, col := range cols {
				// boolean filters are only used for filtering; they're not returned
				// and must be released
				// TODO: verify this once the evaluator implementation is fleshed out
				col.Release()
			}
		}()

		for i, pred := range filter.Predicates {
			res, err := evaluator.eval(pred, batch)
			if err != nil {
				return failureState(err)
			}
			data := res.ToArray()
			if data.DataType().ID() != arrow.BOOL {
				return failureState(fmt.Errorf("predicate %d returned non-boolean type %s", i, data.DataType()))
			}
			casted := data.(*array.Boolean)
			cols = append(cols, casted)
		}

		filtered := filterBatch(batch, func(i int) bool {
			for _, p := range cols {
				if !p.IsValid(i) || !p.Value(i) {
					return false
				}
			}
			return true
		})

		return successState(filtered)

	}, input)
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
func filterBatch(batch arrow.Record, include func(int) bool) arrow.Record {
	mem := memory.NewGoAllocator()
	fields := batch.Schema().Fields()

	builders := make([]array.Builder, len(fields))
	defer func() {
		for _, b := range builders {
			if b != nil {
				b.Release()
			}
		}
	}()

	additions := make([]func(int), len(fields))

	for i, field := range fields {

		switch field.Type.ID() {
		case arrow.BOOL:
			builder := array.NewBooleanBuilder(mem)
			builders[i] = builder
			additions[i] = func(offset int) {
				src := batch.Column(i).(*array.Boolean)
				builder.Append(src.Value(offset))
			}

		case arrow.STRING:
			builder := array.NewStringBuilder(mem)
			builders[i] = builder
			additions[i] = func(offset int) {
				src := batch.Column(i).(*array.String)
				builder.Append(src.Value(offset))
			}

		case arrow.UINT64:
			builder := array.NewUint64Builder(mem)
			builders[i] = builder
			additions[i] = func(offset int) {
				src := batch.Column(i).(*array.Uint64)
				builder.Append(src.Value(offset))
			}

		case arrow.INT64:
			builder := array.NewInt64Builder(mem)
			builders[i] = builder
			additions[i] = func(offset int) {
				src := batch.Column(i).(*array.Int64)
				builder.Append(src.Value(offset))
			}

		case arrow.FLOAT64:
			builder := array.NewFloat64Builder(mem)
			builders[i] = builder
			additions[i] = func(offset int) {
				src := batch.Column(i).(*array.Float64)
				builder.Append(src.Value(offset))
			}

		case arrow.TIMESTAMP:
			builder := array.NewTimestampBuilder(mem, &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"})
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
		if include(i) {
			for _, add := range additions {
				add(i)
			}
			ct++
		}
	}

	schema := arrow.NewSchema(fields, nil)
	arrays := make([]arrow.Array, len(fields))
	for i, builder := range builders {
		arrays[i] = builder.NewArray()
	}

	return array.NewRecord(schema, arrays, ct)
}
