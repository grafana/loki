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

type groupKey struct {
	timestamp time.Time
	key       uint64
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
	points            map[groupKey]*groupState // holds the groupState for each point in time series
	digest            *xxhash.Digest           // used to compute key for each group
	operation         aggregationOperation     // aggregation type
	labels            map[string]arrow.Field   // combined list of all label fields for all sample values
	clonedLabelValues map[string]string        // cache of cloned strings to reduce allocations for repeated values

	// Track unique series across all timestamps to enforce maxSeries limit
	maxSeries    int                          // maximum number of unique series allowed (0 means no limit)
	uniqueSeries map[uint64]map[string]string // tracks unique series across all timestamps

	// cached values to avoid recomputing the key and labels for each sample value in a tight loop
	memoizedKey         uint64
	memoizedLabels      []arrow.Field
	memoizedLabelValues []string
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
		a.points = make(map[groupKey]*groupState, pointsSizeHint)
	} else {
		a.points = make(map[groupKey]*groupState)
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

// BindLabels caches the provided labels for the next Add call.
// If Add is called on an aggregator with Bound labels, the bound labels and values will be used instead of the provided labels and values.
// The returned unbind function must be called to release the cached labels and values and use the allocator for a new series.
func (a *aggregator) BindLabels(labels []arrow.Field, labelValues []string) func() {
	a.memoizedKey = a.computeKey(labels, labelValues)
	a.memoizedLabels = labels
	a.memoizedLabelValues = labelValues

	return a.unbindLabels
}

func (a *aggregator) unbindLabels() {
	a.memoizedKey = 0
	a.memoizedLabels = nil
	a.memoizedLabelValues = nil
}

func (a *aggregator) computeKey(labels []arrow.Field, labelValues []string) uint64 {
	a.digest.Reset()
	for i, label := range labels {
		if i > 0 {
			_, _ = a.digest.Write([]byte{0}) // separator
		}
		_, _ = a.digest.WriteString(label.Name)
		_, _ = a.digest.Write([]byte{'='})
		_, _ = a.digest.WriteString(labelValues[i])
	}
	return a.digest.Sum64()
}

// Add adds a new sample value to the aggregation for the given timestamp and grouping label values.
// It expects labelValues to be in the same order as the groupBy columns.
func (a *aggregator) Add(ts time.Time, value float64, labels []arrow.Field, labelValues []string) error {
	var key uint64
	switch {
	case a.memoizedLabels != nil:
		key = a.memoizedKey
		labels = a.memoizedLabels
		labelValues = a.memoizedLabelValues
	case len(labels) == 0:
		// Aggregation without grouping; all samples share key 0.
		key = 0
	case len(labels) != len(labelValues):
		panic("len(labels) != len(labelValues)")
	default:
		key = a.computeKey(labels, labelValues)
	}

	groupKey := groupKey{timestamp: ts, key: key}
	state, ok := a.points[groupKey]

	if ok {
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

		state = &groupState{
			value: value,
			count: int64(1),
		}
		a.points[groupKey] = state
	}
	return nil
}

func (a *aggregator) BuildRecord() (arrow.RecordBatch, error) {
	fields := make([]arrow.Field, 0, len(a.labels)+2)
	fields = append(fields,
		semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, false),
		semconv.FieldFromIdent(semconv.ColumnIdentValue, false),
	)
	for _, name := range slices.Sorted(maps.Keys(a.labels)) {
		fields = append(fields, a.labels[name])
	}
	schema := arrow.NewSchema(fields, nil)
	rb := array.NewRecordBuilder(memory.NewGoAllocator(), schema)

	// Emit aggregated results in sorted timestamp order.
	sortedKeys := slices.SortedFunc(maps.Keys(a.points), func(a, b groupKey) int {
		if c := a.timestamp.Compare(b.timestamp); c != 0 {
			return c
		}
		if a.key < b.key {
			return -1
		}
		if a.key > b.key {
			return 1
		}
		return 0
	})

	rb.Reserve(len(sortedKeys))

	for _, gk := range sortedKeys {
		entry := a.points[gk]
		tsValue, _ := arrow.TimestampFromTime(gk.timestamp, arrow.Nanosecond)

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

		series := a.uniqueSeries[gk.key]
		for i := 2; i < len(fields); i++ { // offset by 2 as the first 2 fields are timestamp and value
			builder := rb.Field(i)

			if v, ok := series[fields[i].Name]; ok {
				builder.(*array.StringBuilder).Append(v)
			} else {
				builder.(*array.StringBuilder).AppendNull()
			}
		}
	}

	return rb.NewRecordBatch(), nil
}

func (a *aggregator) Reset() {
	a.digest.Reset()
	clear(a.points)
	clear(a.uniqueSeries)
	clear(a.clonedLabelValues)
	a.unbindLabels()
}
