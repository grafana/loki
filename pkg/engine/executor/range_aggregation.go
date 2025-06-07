package executor

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/cespare/xxhash/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

type partitionEntry struct {
	values []string
	count  int64
}

type partitionAggregator struct {
	entries map[uint64]*partitionEntry
}

func (a *partitionAggregator) add(key uint64, partitionValues []string) {
	if entry, ok := a.entries[key]; ok {
		// TODO: handle hash collisions
		entry.count++
	} else {
		// create a new slice since partitionValues is reused on each row read.
		values := make([]string, len(partitionValues))
		for i, v := range partitionValues {
			// copy the value as this is backed by the arrow array data buffer.
			// We could retain the record to avoid this copy, but that would hold
			// all other columns in memory for as long as the query is evaluated.
			values[i] = strings.Clone(v)
		}

		// TODO: add limits on number of partitions
		a.entries[key] = &partitionEntry{
			values: values,
			count:  1,
		}
	}
}

type rangeAggregationOptions struct {
	partitionBy []physical.ColumnExpression

	// start and end timestamps are equal for instant queries.
	startTs       time.Time      // start timestamp of the query
	endTs         time.Time      // end timestamp of the query
	rangeInterval time.Duration  // range interval
	step          *time.Duration // step used for range queries, nil for instant queries
}

// RangeAggregationPipeline is a pipeline that performs aggregations over a time window.
// - It reads from the input pipelines
// - Partitions the data by the specified columns
// - And counts the number of rows seen by each partition
// It is used to implement the `count_over_time` function in Loki queries.
// This version only support instant queries.
type RangeAggregationPipeline struct {
	state       state
	accumulator *partitionAggregator

	inputs    []Pipeline
	evaluator *expressionEvaluator
	opts      rangeAggregationOptions
}

func NewRangeAggregationPipeline(inputs []Pipeline, evaluator *expressionEvaluator, opts rangeAggregationOptions) (*RangeAggregationPipeline, error) {
	return &RangeAggregationPipeline{
		inputs:      inputs,
		evaluator:   evaluator,
		accumulator: &partitionAggregator{entries: make(map[uint64]*partitionEntry)}, // TODO: estimate size during planning
		opts:        opts,
	}, nil
}

// Read reads the next value into its state.
// It returns an error if reading fails or when the pipeline is exhausted. In this case, the function returns EOF.
// The implementation must retain the returned error in its state and return it with subsequent Value() calls.
func (r *RangeAggregationPipeline) Read() error {
	// if the state already has an error, do not attempt to read.
	if r.state.err != nil {
		return r.state.err
	}

	record, err := r.read()
	r.state = newState(record, err)

	if err != nil {
		return fmt.Errorf("read and aggregate inputs: %w", err)
	}
	return nil
}

// TODOs:
// - Support implicit partitioning by all labels when partitionBy is empty
// - Use columnar access pattern. Current approach is row-based which does not benefit from the columnar storage format.
// - Add toggle to return partial results on Read() call instead of doing it only after exhausing all inputs.
func (r *RangeAggregationPipeline) read() (arrow.Record, error) {
	var isEntryInRange func(t time.Time) bool
	{
		evalTs := r.opts.endTs
		earliestTs := r.opts.endTs.Add(-r.opts.rangeInterval)
		isEntryInRange = func(t time.Time) bool {
			// Aggregate entries that belong in [earliestTs, evalTs)
			return t.Compare(earliestTs) >= 0 && t.Compare(evalTs) < 0
		}
	}

	var (
		// expr to extract the timestamp column from the input records
		tsColumnExpr = &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameBuiltinTimestamp,
				Type:   types.ColumnTypeBuiltin,
			},
		}

		// reused on each row read
		h         = xxhash.New()
		lblValues = make([]string, len(r.opts.partitionBy))
	)

	inputsExhausted := false
	r.accumulator.entries = make(map[uint64]*partitionEntry) // reset accumulator for each read
	for !inputsExhausted {
		inputsExhausted = true

		for _, input := range r.inputs {
			if err := input.Read(); err != nil {
				if errors.Is(err, EOF) {
					continue
				}

				return nil, err
			}
			inputsExhausted = false

			record, _ := input.Value()
			// We own this record for the duration of this loop iteration
			// and must release it when we're done processing it
			defer record.Release()

			// extract all the columns that are used for partitioning
			arrays := make([]*array.String, 0, len(r.opts.partitionBy))
			for _, columnExpr := range r.opts.partitionBy {
				vec, err := r.evaluator.eval(columnExpr, record)
				if err != nil {
					return nil, err
				}

				if vec.Type() != datatype.String {
					return nil, fmt.Errorf("unsupported datatype for partitioning %s", vec.Type())
				}

				arrays = append(arrays, vec.ToArray().(*array.String))
			}

			// extract timestamp column to check if the entry is in range
			vec, err := r.evaluator.eval(tsColumnExpr, record)
			if err != nil {
				return nil, err
			}

			tsCol := vec.ToArray().(*array.Timestamp)
			for row := range int(record.NumRows()) {
				if !isEntryInRange(tsCol.Value(row).ToTime(arrow.Nanosecond)) {
					continue
				}

				// reset label values and hash for each row
				clear(lblValues)
				h.Reset()

				for col, arr := range arrays {
					if col > 0 {
						_, _ = h.Write([]byte{0}) // separator
					}

					v := arr.Value(row)
					_, _ = h.WriteString(v)
					lblValues[col] = v
				}

				r.accumulator.add(h.Sum64(), lblValues)
			}
		}
	}

	if len(r.accumulator.entries) == 0 {
		return nil, EOF // exhausted all inputs and no entries found
	}

	// TODO: Schema is static when partitionBy is defined, we can create once and reuse it.
	fields := make([]arrow.Field, 0, len(r.opts.partitionBy)+2)
	fields = append(fields,
		arrow.Field{
			Name:     types.ColumnNameBuiltinTimestamp,
			Type:     arrow.FixedWidthTypes.Timestamp_ns,
			Nullable: false,
			Metadata: datatype.ColumnMetadataBuiltinTimestamp,
		},
		arrow.Field{
			Name:     "value",
			Type:     arrow.PrimitiveTypes.Int64,
			Nullable: false,
			Metadata: datatype.ColumnMetadata(types.ColumnTypeAmbiguous, datatype.Integer), // needs a new ColumnType, ColumnTypeComputed or Generated?
		},
	)

	for _, column := range r.opts.partitionBy {
		columnExpr, ok := column.(*physical.ColumnExpr)
		if !ok {
			panic(fmt.Sprintf("invalid column expression type %T", column))
		}

		fields = append(fields, arrow.Field{
			Name:     columnExpr.Ref.Column,
			Type:     arrow.BinaryTypes.String,
			Nullable: true,
			Metadata: datatype.ColumnMetadata(columnExpr.Ref.Type, datatype.String),
		})
	}
	schema := arrow.NewSchema(fields, nil)

	rb := array.NewRecordBuilder(memory.NewGoAllocator(), schema)
	defer rb.Release()

	ts, _ := arrow.TimestampFromTime(r.opts.endTs, arrow.Nanosecond)
	for _, entry := range r.accumulator.entries {
		rb.Field(0).(*array.TimestampBuilder).Append(ts)
		rb.Field(1).(*array.Int64Builder).Append(entry.count)

		for col, val := range entry.values {
			builder := rb.Field(col + 2) // offset by 2 as the first 2 fields are timestamp and value
			if val == "" {
				builder.(*array.StringBuilder).AppendNull()
			} else {
				builder.(*array.StringBuilder).Append(val)
			}
		}
	}

	return rb.NewRecord(), nil
}

// Value returns the current value in state.
func (r *RangeAggregationPipeline) Value() (arrow.Record, error) {
	return r.state.Value()
}

// Close closes the resources of the pipeline.
// The implementation must close all the of the pipeline's inputs.
func (r *RangeAggregationPipeline) Close() {
	// Release last batch
	if r.state.batch != nil {
		r.state.batch.Release()
	}

	for _, input := range r.inputs {
		input.Close()
	}
}

// Inputs returns the inputs of the pipeline.
func (r *RangeAggregationPipeline) Inputs() []Pipeline {
	return r.inputs
}

// Transport returns the type of transport of the implementation.
func (r *RangeAggregationPipeline) Transport() Transport {
	return Local
}
