package executor

import (
	"errors"
	"fmt"
	"maps"
	"slices"
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

// VectorAggregationPipeline is a pipeline that performs vector aggregations.
//
// It reads from the input pipeline, groups the data by specified columns,
// and applies the aggregation function on each group.
type VectorAggregationPipeline struct {
	state  state
	inputs []Pipeline

	aggregator *vectorAggregator
	evaluator  expressionEvaluator
	groupBy    []physical.ColumnExpression

	tsEval    evalFunc // used to evaluate the timestamp column
	valueEval evalFunc // used to evaluate the value column
}

func NewVectorAggregationPipeline(inputs []Pipeline, groupBy []physical.ColumnExpression, evaluator expressionEvaluator) (*VectorAggregationPipeline, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("vector aggregation expects at least one input")
	}

	return &VectorAggregationPipeline{
		inputs:     inputs,
		evaluator:  evaluator,
		groupBy:    groupBy,
		aggregator: newVectorAggregator(groupBy),
		tsEval: evaluator.newFunc(&physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameBuiltinTimestamp,
				Type:   types.ColumnTypeBuiltin,
			},
		}),
		valueEval: evaluator.newFunc(&physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameGeneratedValue,
				Type:   types.ColumnTypeGenerated,
			},
		}),
	}, nil
}

// Read reads the next value into its state.
func (v *VectorAggregationPipeline) Read() error {
	if v.state.err != nil {
		return v.state.err
	}

	// release previous batch before creating a new one
	if v.state.batch != nil {
		v.state.batch.Release()
	}

	record, err := v.read()
	v.state = newState(record, err)

	if err != nil {
		return fmt.Errorf("run vector aggregation: %w", err)
	}
	return nil
}

func (v *VectorAggregationPipeline) read() (arrow.Record, error) {
	var (
		labelValues = make([]string, len(v.groupBy))
	)

	v.aggregator.Reset() // reset before reading new inputs
	inputsExhausted := false
	for !inputsExhausted {
		inputsExhausted = true

		for _, input := range v.inputs {
			if err := input.Read(); err != nil {
				if errors.Is(err, EOF) {
					continue
				}

				return nil, err
			}

			inputsExhausted = false
			record, _ := input.Value()

			// extract timestamp column
			tsVec, err := v.tsEval(record)
			if err != nil {
				return nil, err
			}
			tsCol := tsVec.ToArray().(*array.Timestamp)

			// extract value column
			valueVec, err := v.valueEval(record)
			if err != nil {
				return nil, err
			}
			valueArr := valueVec.ToArray().(*array.Int64)

			// extract all the columns that are used for grouping
			arrays := make([]*array.String, 0, len(v.groupBy))
			for _, columnExpr := range v.groupBy {
				vec, err := v.evaluator.eval(columnExpr, record)
				if err != nil {
					return nil, err
				}

				if vec.Type() != datatype.Loki.String {
					return nil, fmt.Errorf("unsupported datatype for grouping %s", vec.Type())
				}

				arrays = append(arrays, vec.ToArray().(*array.String))
			}

			for row := range int(record.NumRows()) {
				// reset for each row
				clear(labelValues)
				for col, arr := range arrays {
					labelValues[col] = arr.Value(row)
				}

				v.aggregator.Add(tsCol.Value(row).ToTime(arrow.Nanosecond), valueArr.Value(row), labelValues)
			}
		}
	}

	if v.aggregator.NumOfPoints() == 0 {
		return nil, EOF // no values to aggregate & reached EOF
	}

	return v.aggregator.buildRecord()
}

// Value returns the current value in state.
func (v *VectorAggregationPipeline) Value() (arrow.Record, error) {
	return v.state.Value()
}

// Close closes the resources of the pipeline.
func (v *VectorAggregationPipeline) Close() {
	if v.state.batch != nil {
		v.state.batch.Release()
	}

	for _, input := range v.inputs {
		input.Close()
	}
}

// Inputs returns the inputs of the pipeline.
func (v *VectorAggregationPipeline) Inputs() []Pipeline {
	return v.inputs
}

// Transport returns the transport type of the pipeline.
func (v *VectorAggregationPipeline) Transport() Transport {
	return Local
}

type groupState struct {
	sum         int64
	labelValues []string
}

type vectorAggregator struct {
	groupBy []physical.ColumnExpression          // columns to group by
	digest  *xxhash.Digest                       // used to compute key for each group
	points  map[time.Time]map[uint64]*groupState // holds the groupState for each point in time series
}

func newVectorAggregator(groupBy []physical.ColumnExpression) *vectorAggregator {
	return &vectorAggregator{
		groupBy: groupBy,
		digest:  xxhash.New(),
		points:  make(map[time.Time]map[uint64]*groupState),
	}
}

func (a *vectorAggregator) Add(ts time.Time, value int64, labelValues []string) {
	point, ok := a.points[ts]
	if !ok {
		point = make(map[uint64]*groupState)
		a.points[ts] = point
	}

	a.digest.Reset()
	for i, val := range labelValues {
		if i > 0 {
			_, _ = a.digest.Write([]byte{0}) // separator
		}

		_, _ = a.digest.WriteString(val)
	}
	key := a.digest.Sum64()

	if state, ok := point[key]; ok {
		// TODO: handle hash collisions
		state.sum += value
	} else {
		// create a new slice since labelValues is reused by the calling code
		labelValuesCopy := make([]string, len(labelValues))
		for i, v := range labelValues {
			// copy the value as this is backed by the arrow array data buffer.
			// We could retain the record to avoid this copy, but that would hold
			// all other columns in memory for as long as the query is evaluated.
			labelValuesCopy[i] = strings.Clone(v)
		}

		// TODO: add limits on number of groups
		point[key] = &groupState{
			labelValues: labelValuesCopy,
			sum:         value,
		}
	}
}

func (a *vectorAggregator) buildRecord() (arrow.Record, error) {
	fields := make([]arrow.Field, 0, len(a.groupBy)+2)
	fields = append(fields,
		arrow.Field{
			Name:     types.ColumnNameBuiltinTimestamp,
			Type:     datatype.Arrow.Timestamp,
			Nullable: false,
			Metadata: datatype.ColumnMetadataBuiltinTimestamp,
		},
		arrow.Field{
			Name:     types.ColumnNameGeneratedValue,
			Type:     datatype.Arrow.Integer,
			Nullable: false,
			Metadata: datatype.ColumnMetadata(types.ColumnTypeGenerated, datatype.Loki.Integer),
		},
	)

	for _, column := range a.groupBy {
		colExpr, ok := column.(*physical.ColumnExpr)
		if !ok {
			panic(fmt.Sprintf("invalid column expression type %T", column))
		}

		fields = append(fields, arrow.Field{
			Name:     colExpr.Ref.Column,
			Type:     datatype.Arrow.String,
			Nullable: true,
			Metadata: datatype.ColumnMetadata(colExpr.Ref.Type, datatype.Loki.String),
		})
	}

	schema := arrow.NewSchema(fields, nil)
	rb := array.NewRecordBuilder(memory.NewGoAllocator(), schema)
	defer rb.Release()

	// emit aggregated results in sorted order of timestamp
	for _, ts := range a.GetSortedTimestamps() {
		entries := a.GetEntriesForTimestamp(ts)
		tsValue, _ := arrow.TimestampFromTime(ts, arrow.Nanosecond)

		for _, entry := range entries {
			rb.Field(0).(*array.TimestampBuilder).Append(tsValue)
			rb.Field(1).(*array.Int64Builder).Append(entry.sum)

			for col, val := range entry.labelValues {
				builder := rb.Field(col + 2) // offset by 2 as the first 2 fields are timestamp and value
				if val == "" {
					builder.(*array.StringBuilder).AppendNull()
				} else {
					builder.(*array.StringBuilder).Append(val)
				}
			}
		}
	}

	return rb.NewRecord(), nil
}

func (a *vectorAggregator) Reset() {
	clear(a.points)
}

func (a *vectorAggregator) NumOfPoints() int {
	return len(a.points)
}

// GetSortedTimestamps returns all timestamps in sorted order
func (a *vectorAggregator) GetSortedTimestamps() []time.Time {
	return slices.SortedFunc(maps.Keys(a.points), func(a, b time.Time) int {
		return a.Compare(b)
	})
}

// GetEntriesForTimestamp returns all entries for a given timestamp
func (a *vectorAggregator) GetEntriesForTimestamp(ts time.Time) map[uint64]*groupState {
	return a.points[ts]
}
