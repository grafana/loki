package executor

import (
	"errors"
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

var ErrSeriesLimitExceeded = errors.New("maximum number of series limit exceeded")

type groupState struct {
	value float64 // aggregated value
	count int64   // values counter
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
	points            map[time.Time]map[uint64]*groupState // holds the groupState for each point in time series
	digest            *xxhash.Digest                       // used to compute key for each group
	operation         aggregationOperation                 // aggregation type
	labels            map[string]arrow.Field               // combined list of all label fields for all sample values
	clonedLabelValues map[string]string                    // cache of cloned strings to reduce allocations for repeated values

	// Track unique series across all timestamps to enforce maxSeries limit
	maxSeries    int                          // maximum number of unique series allowed (0 means no limit)
	uniqueSeries map[uint64]map[string]string // tracks unique series across all timestamps
}

// newAggregator creates a new aggregator with the specified grouping.
func newAggregator(pointsSizeHint int, operation aggregationOperation) *aggregator {
	a := aggregator{
		digest:            xxhash.New(),
		operation:         operation,
		clonedLabelValues: make(map[string]string),
		labels:            make(map[string]arrow.Field),
		uniqueSeries:      make(map[uint64]map[string]string),
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
		if _, ok := a.labels[label.Name]; !ok {
			a.labels[label.Name] = label
		}
	}
}

// SetMaxSeries sets the maximum number of unique series allowed.
func (a *aggregator) SetMaxSeries(maxSeries int) {
	a.maxSeries = maxSeries
}

// Add adds a new sample value to the aggregation for the given timestamp and grouping label values.
// It expects labelValues to be in the same order as the groupBy columns.
func (a *aggregator) Add(ts time.Time, value float64, labels []arrow.Field, labelValues []string) error {
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
		if series, exists := a.uniqueSeries[key]; !exists {
			// Check series limit before adding a new series
			if a.maxSeries > 0 && len(a.uniqueSeries) >= a.maxSeries {
				return ErrSeriesLimitExceeded
			}

			if len(labels) > 0 {
				series = make(map[string]string)
				for i, v := range labelValues {
					// copy the value as this is backed by the arrow array data buffer.
					// We could retain the record to avoid this copy, but that would hold
					// all other columns in memory for as long as the query is evaluated.
					cloned, ok := a.clonedLabelValues[v]
					if !ok {
						cloned = strings.Clone(v)
						a.clonedLabelValues[v] = cloned
					}
					series[labels[i].Name] = cloned
				}
			}

			a.uniqueSeries[key] = series
		}

		point[key] = &groupState{
			value: value,
			count: int64(1),
		}
	}
	return nil
}

func (a *aggregator) BuildRecord() (arrow.RecordBatch, error) {
	fields := make([]arrow.Field, 0, len(a.labels)+2)
	fields = append(fields,
		semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, false),
		semconv.FieldFromIdent(semconv.ColumnIdentValue, false),
	)
	for _, label := range a.labels {
		fields = append(fields, label)
	}
	schema := arrow.NewSchema(fields, nil)
	rb := array.NewRecordBuilder(memory.NewGoAllocator(), schema)

	// emit aggregated results in sorted order of timestamp
	sortedTimestamps := a.getSortedTimestamps()

	// preallocate all builders to the total amount of rows
	total := 0
	for _, ts := range sortedTimestamps {
		total += len(a.points[ts])
	}
	rb.Reserve(total)

	for _, ts := range sortedTimestamps {
		tsValue, _ := arrow.TimestampFromTime(ts, arrow.Nanosecond)

		for key, entry := range a.points[ts] {
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

			series := a.uniqueSeries[key]
			for i := 2; i < len(fields); i++ { // offset by 2 as the first 2 fields are timestamp and value
				builder := rb.Field(i)

				if v, ok := series[fields[i].Name]; ok {
					builder.(*array.StringBuilder).Append(v)
				} else {
					builder.(*array.StringBuilder).AppendNull()
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

	clear(a.uniqueSeries)
	clear(a.clonedLabelValues)
}

// getSortedTimestamps returns all timestamps in sorted order
func (a *aggregator) getSortedTimestamps() []time.Time {
	return slices.SortedFunc(maps.Keys(a.points), func(a, b time.Time) int {
		return a.Compare(b)
	})
}
