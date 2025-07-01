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

type rangeAggregationOptions struct {
	partitionBy []physical.ColumnExpression

	// start and end timestamps are equal for instant queries.
	startTs       time.Time     // start timestamp of the query
	endTs         time.Time     // end timestamp of the query
	rangeInterval time.Duration // range interval
	step          time.Duration // step used for range queries
}

// RangeAggregationPipeline is a pipeline that performs aggregations over a time window.
//
// 1. It reads from the input pipelines
// 2. Partitions the data by the specified columns
// 3. Applies the aggregation function on each partition
//
// Current version only supports counting for instant queries.
type RangeAggregationPipeline struct {
	state  state
	inputs []Pipeline

	aggregator *partitionAggregator
	evaluator  expressionEvaluator // used to evaluate column expressions
	opts       rangeAggregationOptions
}

func NewRangeAggregationPipeline(inputs []Pipeline, evaluator expressionEvaluator, opts rangeAggregationOptions) (*RangeAggregationPipeline, error) {
	return &RangeAggregationPipeline{
		inputs:     inputs,
		evaluator:  evaluator,
		aggregator: newPartitionAggregator(),
		opts:       opts,
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

	if r.state.batch != nil {
		r.state.batch.Release()
	}

	record, err := r.read()
	r.state = newState(record, err)

	if err != nil {
		return fmt.Errorf("run range aggregation: %w", err)
	}
	return nil
}

// TODOs:
// - Support implicit partitioning by all labels when partitionBy is empty
// - Use columnar access pattern. Current approach is row-based which does not benefit from the storage format.
// - Add toggle to return partial results on Read() call instead of returning only after exhausing all inputs.
func (r *RangeAggregationPipeline) read() (arrow.Record, error) {
	var (
		isTSInRange  func(t time.Time) bool
		tsColumnExpr = &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameBuiltinTimestamp,
				Type:   types.ColumnTypeBuiltin,
			},
		} // timestamp column expression

		// reused on each row read
		labelValues = make([]string, len(r.opts.partitionBy))
	)

	{
		evalTs := r.opts.endTs
		earliestTs := r.opts.endTs.Add(-r.opts.rangeInterval)
		isTSInRange = func(t time.Time) bool {
			// Aggregate entries that belong in [earliestTs, evalTs)
			return t.Compare(earliestTs) >= 0 && t.Compare(evalTs) < 0
		}
	}

	r.aggregator.Reset() // reset before reading new inputs
	inputsExhausted := false
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
				if !isTSInRange(tsCol.Value(row).ToTime(arrow.Nanosecond)) {
					continue
				}

				// reset label values and hash for each row
				clear(labelValues)
				for col, arr := range arrays {
					labelValues[col] = arr.Value(row)
				}
				r.aggregator.Add(labelValues)
			}
		}
	}

	if r.aggregator.NumOfPartitions() == 0 {
		return nil, EOF // no values to aggregate & reached EOF
	}

	// TODO: schema is same for each read call when partitionBy is defined, we can create it once and reuse.
	fields := make([]arrow.Field, 0, len(r.opts.partitionBy)+2)
	fields = append(fields,
		arrow.Field{
			Name:     types.ColumnNameBuiltinTimestamp,
			Type:     arrow.FixedWidthTypes.Timestamp_ns,
			Nullable: false,
			Metadata: datatype.ColumnMetadataBuiltinTimestamp,
		},
		arrow.Field{
			Name:     types.ColumnNameGeneratedValue,
			Type:     arrow.PrimitiveTypes.Int64,
			Nullable: false,
			Metadata: datatype.ColumnMetadata(types.ColumnTypeGenerated, datatype.Integer), // needs a new ColumnType, ColumnTypeComputed or Generated?
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
	for _, entry := range r.aggregator.entries {
		rb.Field(0).(*array.TimestampBuilder).Append(ts)
		rb.Field(1).(*array.Int64Builder).Append(entry.count)

		for col, val := range entry.labelValues {
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

type partitionAggregator struct {
	digest  *xxhash.Digest // used to compute key for each partition
	entries map[uint64]*partitionEntry
}

func newPartitionAggregator() *partitionAggregator {
	return &partitionAggregator{
		digest: xxhash.New(),
		// TODO: estimate size during planning
		entries: make(map[uint64]*partitionEntry),
	}
}

type partitionEntry struct {
	count       int64
	labelValues []string
}

func (a *partitionAggregator) Add(partitionLabelValues []string) {
	a.digest.Reset()

	for i, val := range partitionLabelValues {
		if i > 0 {
			_, _ = a.digest.Write([]byte{0}) // separator for label values
		}

		_, _ = a.digest.WriteString(val)
	}

	key := a.digest.Sum64()
	if entry, ok := a.entries[key]; ok {
		// TODO: handle hash collisions
		entry.count++
	} else {
		// create a new slice since partitionLabelValues is reused by the calling code
		labelValues := make([]string, len(partitionLabelValues))
		for i, v := range partitionLabelValues {
			// copy the value as this is backed by the arrow array data buffer.
			// We could retain the record to avoid this copy, but that would hold
			// all other columns in memory for as long as the query is evaluated.
			labelValues[i] = strings.Clone(v)
		}

		// TODO: add limits on number of partitions
		a.entries[key] = &partitionEntry{
			labelValues: labelValues,
			count:       1,
		}
	}
}

func (a *partitionAggregator) Reset() {
	a.digest.Reset()
	clear(a.entries)
}

func (a *partitionAggregator) NumOfPartitions() int {
	return len(a.entries)
}
