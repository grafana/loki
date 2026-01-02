package executor

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/cespare/xxhash/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
)

type groupState struct {
	value       float64       // aggregated value
	count       int64         // values counter
	labels      []arrow.Field // grouping labels
	labelValues []string      // grouping label values
}

type aggregationOperation int

const (
	aggregationOperationSum aggregationOperation = iota
	aggregationOperationMax
	aggregationOperationMin
	aggregationOperationCount
	aggregationOperationAvg
	aggregationOperationStddev
	aggregationOperationStdvar
	aggregationOperationBytes
)

// aggregator is used to aggregate sample values by a set of grouping keys for each point in time.
type aggregator struct {
	points    map[time.Time]map[uint64]*groupState // holds the groupState for each point in time series
	digest    *xxhash.Digest                       // used to compute key for each group
	operation aggregationOperation                 // aggregation type
	labels    []arrow.Field                        // combined list of all label fields for all sample values
}

// newAggregator creates a new aggregator with the specified grouping.
func newAggregator(pointsSizeHint int, operation aggregationOperation) *aggregator {
	a := aggregator{
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

// AddLabels merges a list of labels that all sample values will have combined. This can be done several times
// over the lifetime of an aggregator to accommodate processing of multiple records with different schemas.
func (a *aggregator) AddLabels(labels []arrow.Field) {
	for _, label := range labels {
		if !slices.ContainsFunc(a.labels, func(l arrow.Field) bool {
			return label.Equal(l)
		}) {
			a.labels = append(a.labels, label)
		}
	}
}

// Add adds a new sample value to the aggregation for the given timestamp and grouping label values.
// It expects labelValues to be in the same order as the groupBy columns.
func (a *aggregator) Add(ts time.Time, value float64, labels []arrow.Field, labelValues []string) {
	if len(labels) != len(labelValues) {
		panic("len(labels) != len(labelValues)")
	}

	point, ok := a.points[ts]
	if !ok {
		point = make(map[uint64]*groupState)
		a.points[ts] = point
	}

	var key uint64
	if len(labelValues) != 0 {
		a.digest.Reset()
		for i, val := range labelValues {
			if i > 0 {
				_, _ = a.digest.Write([]byte{0}) // separator
			}

			_, _ = a.digest.WriteString(labels[i].Name)
			_, _ = a.digest.Write([]byte("="))
			_, _ = a.digest.WriteString(val)
		}
		key = a.digest.Sum64()
	}

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
		case aggregationOperationAvg:
			state.value += value
		}

		state.count++
	} else {
		count := int64(1)

		if len(labels) == 0 {
			// special case: All values aggregated into a single group.
			point[key] = &groupState{
				value: value,
				count: count,
			}
			return
		}

		labelValuesCopy := make([]string, len(labelValues))
		for i, v := range labelValues {
			// copy the value as this is backed by the arrow array data buffer.
			// We could retain the record to avoid this copy, but that would hold
			// all other columns in memory for as long as the query is evaluated.
			labelValuesCopy[i] = strings.Clone(v)
		}

		// TODO: add limits on number of groups
		point[key] = &groupState{
			labels:      labels,
			labelValues: labelValuesCopy,
			value:       value,
			count:       count,
		}
	}
}

func (a *aggregator) BuildRecord() (arrow.RecordBatch, error) {
	fields := make([]arrow.Field, 0, len(a.labels)+2)
	fields = append(fields,
		semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, false),
		semconv.FieldFromIdent(semconv.ColumnIdentValue, false),
	)
	for _, label := range a.labels {
		fields = append(fields, arrow.Field{
			Name:     label.Name,
			Type:     label.Type,
			Nullable: true,
			Metadata: label.Metadata,
		})
	}

	schema := arrow.NewSchema(fields, nil)
	rb := array.NewRecordBuilder(memory.NewGoAllocator(), schema)

	// emit aggregated results in sorted order of timestamp
	for _, ts := range a.getSortedTimestamps() {
		tsValue, _ := arrow.TimestampFromTime(ts, arrow.Nanosecond)

		for _, entry := range a.points[ts] {
			var value float64
			switch a.operation {
			case aggregationOperationAvg:
				value = entry.value / float64(entry.count)
			case aggregationOperationCount:
				value = float64(entry.count)
			default:
				value = entry.value
			}

			rb.Field(0).(*array.TimestampBuilder).Append(tsValue)
			rb.Field(1).(*array.Float64Builder).Append(value)

			for i, label := range a.labels {
				builder := rb.Field(2 + i) // offset by 2 as the first 2 fields are timestamp and value

				j := slices.IndexFunc(entry.labels, func(l arrow.Field) bool {
					return l.Name == label.Name
				})
				if j == -1 {
					builder.(*array.StringBuilder).AppendNull()
				} else {
					// TODO: differentiate between null and actual empty string
					if entry.labelValues[j] == "" {
						builder.(*array.StringBuilder).AppendNull()
					} else {
						builder.(*array.StringBuilder).Append(entry.labelValues[j])
					}
				}
			}
		}
	}

	return rb.NewRecordBatch(), nil
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
