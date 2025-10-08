package executor

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/cespare/xxhash/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

type groupState struct {
	value       float64  // aggregated value
	labelValues []string // grouping label values
}

type aggregationOperation int

const (
	aggregationOperationSum aggregationOperation = iota
	aggregationOperationMax
	aggregationOperationMin
	aggregationOperationCount
)

// aggregator is used to aggregate sample values by a set of grouping keys for each point in time.
type aggregator struct {
	groupBy   []physical.ColumnExpression          // columns to group by
	points    map[time.Time]map[uint64]*groupState // holds the groupState for each point in time series
	digest    *xxhash.Digest                       // used to compute key for each group
	operation aggregationOperation                 // aggregation type
}

// newAggregator creates a new aggregator with the specified groupBy columns.
func newAggregator(groupBy []physical.ColumnExpression, pointsSizeHint int, operation aggregationOperation) *aggregator {
	a := aggregator{
		groupBy:   groupBy,
		digest:    xxhash.New(),
		operation: operation,
	}

	if pointsSizeHint > 0 {
		a.points = make(map[time.Time]map[uint64]*groupState, pointsSizeHint)
	} else {
		a.points = make(map[time.Time]map[uint64]*groupState)
	}

	return &a
}

// Add adds a new sample value to the aggregation for the given timestamp and grouping label values.
// It expects labelValues to be in the same order as the groupBy columns.
func (a *aggregator) Add(ts time.Time, value float64, labelValues []string) {
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

		// accumulate value based on aggregation type
		switch a.operation {
		case aggregationOperationSum:
			state.value += value
		case aggregationOperationMax:
			if value > state.value {
				state.value = value
			}
		case aggregationOperationMin:
			if value < state.value {
				state.value = value
			}
		case aggregationOperationCount:
			state.value = state.value + 1
		}
	} else {
		// create a new slice since labelValues is reused by the calling code
		labelValuesCopy := make([]string, len(labelValues))
		for i, v := range labelValues {
			// copy the value as this is backed by the arrow array data buffer.
			// We could retain the record to avoid this copy, but that would hold
			// all other columns in memory for as long as the query is evaluated.
			labelValuesCopy[i] = strings.Clone(v)
		}

		state = &groupState{
			labelValues: labelValuesCopy,
		}

		// set initial values based on aggregation type
		switch a.operation {
		case aggregationOperationSum, aggregationOperationMax, aggregationOperationMin:
			state.value = value
		case aggregationOperationCount:
			state.value = 1
		}

		// TODO: add limits on number of groups
		point[key] = state
	}
}

func (a *aggregator) BuildRecord() (arrow.Record, error) {
	fields := make([]arrow.Field, 0, len(a.groupBy)+2)
	fields = append(fields,
		arrow.Field{
			Name:     types.ColumnNameBuiltinTimestamp,
			Type:     types.Arrow.Timestamp,
			Nullable: false,
			Metadata: types.ColumnMetadataBuiltinTimestamp,
		},
		arrow.Field{
			Name:     types.ColumnNameGeneratedValue,
			Type:     types.Arrow.Float,
			Nullable: false,
			Metadata: types.ColumnMetadata(types.ColumnTypeGenerated, types.Loki.Float),
		},
	)

	for _, column := range a.groupBy {
		colExpr, ok := column.(*physical.ColumnExpr)
		if !ok {
			panic(fmt.Sprintf("invalid column expression type %T", column))
		}

		fields = append(fields, arrow.Field{
			Name:     colExpr.Ref.Column,
			Type:     types.Arrow.String,
			Nullable: true,
			Metadata: types.ColumnMetadata(colExpr.Ref.Type, types.Loki.String),
		})
	}

	schema := arrow.NewSchema(fields, nil)
	rb := array.NewRecordBuilder(memory.NewGoAllocator(), schema)
	defer rb.Release()

	// emit aggregated results in sorted order of timestamp
	for _, ts := range a.getSortedTimestamps() {
		tsValue, _ := arrow.TimestampFromTime(ts, arrow.Nanosecond)

		for _, entry := range a.points[ts] {
			rb.Field(0).(*array.TimestampBuilder).Append(tsValue)
			rb.Field(1).(*array.Float64Builder).Append(entry.value)

			for col, val := range entry.labelValues {
				builder := rb.Field(col + 2) // offset by 2 as the first 2 fields are timestamp and value
				// TODO: differentiate between null and actual empty string
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

func (a *aggregator) Reset() {
	a.digest.Reset()
	// keep the timestamps but clear the aggregated values
	for _, point := range a.points {
		clear(point)
	}
}

// getSortedTimestamps returns all timestamps in sorted order
func (a *aggregator) getSortedTimestamps() []time.Time {
	return slices.SortedFunc(maps.Keys(a.points), func(a, b time.Time) int {
		return a.Compare(b)
	})
}
