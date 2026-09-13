package executor

import (
	"errors"
	"maps"
	"math"
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

type interner struct {
	interned map[string]string
}

// Get returns an interned copy of the input string. It is safe to call with unsafe strings.
func (s *interner) Get(name string) string {
	if value, ok := s.interned[name]; ok {
		return value
	}
	cloned := strings.Clone(name)
	s.interned[cloned] = cloned
	return cloned
}

// Reset clears the interner and resets it to its initial empty state.
func (s *interner) Reset() {
	clear(s.interned)
}

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
	points    map[uint64][]groupState // holds the groupState for each point in time series
	digest    *xxhash.Digest          // used to compute key for each group
	operation aggregationOperation    // aggregation type
	labels    map[string]arrow.Field  // combined list of all label fields for all sample values
	from      int64
	to        int64
	step      int64

	// Track unique series across all timestamps to enforce maxSeries limit
	maxSeries    int                          // maximum number of unique series allowed (0 means no limit)
	uniqueSeries map[uint64]map[string]string // tracks unique series across all timestamps

	// Intern unique symbols to reduce memory usage
	interner *interner
}

// newAggregator creates a new aggregator with the specified grouping.
func newAggregator(operation aggregationOperation, from time.Time, to time.Time, step time.Duration) *aggregator {
	a := aggregator{
		digest:       xxhash.New(),
		operation:    operation,
		labels:       make(map[string]arrow.Field),
		uniqueSeries: make(map[uint64]map[string]string),
		interner:     &interner{interned: make(map[string]string, 16)}, // 16 is arbitrary initial capacity
		from:         from.UnixNano(),
		to:           to.UnixNano(),
		step:         step.Nanoseconds(),
		points:       make(map[uint64][]groupState),
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

func (a *aggregator) add(ts int64, value float64, key uint64) {
	timesForKey, ok := a.points[key]
	if !ok {
		timesForKey = make([]groupState, 1+(a.to-a.from)/a.step)
		a.points[key] = timesForKey
	}

	timeIndex := (ts - a.from) / a.step
	switch a.operation {
	case aggregationOperationSum:
		timesForKey[timeIndex].value += value
	case aggregationOperationMax:
		timesForKey[timeIndex].value = math.Max(timesForKey[timeIndex].value, value)
	case aggregationOperationMin:
		if timesForKey[timeIndex].count == 0 {
			timesForKey[timeIndex].value = value
		} else {
			timesForKey[timeIndex].value = math.Min(timesForKey[timeIndex].value, value)
		}
	case aggregationOperationAvg:
		timesForKey[timeIndex].value += value
	}

	timesForKey[timeIndex].count++
}

// AddN adds new sample values to the aggregation for the given timestamps and grouping labels.
// It expects labelValues to be in the same order as the groupBy columns.
func (a *aggregator) AddN(timestamps []time.Time, value float64, labels []arrow.Field, labelValues []string) error {
	if len(labels) != len(labelValues) {
		panic("len(labels) != len(labelValues)")
	}

	// Calculate the hash key for all windows
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

	// Record the unique series if its new
	if series, exists := a.uniqueSeries[key]; !exists {
		// Check series limit before adding a new series
		if a.maxSeries > 0 && len(a.uniqueSeries) >= a.maxSeries {
			return ErrSeriesLimitExceeded
		}

		if len(labels) > 0 {
			series = make(map[string]string)
			for i, v := range labelValues {
				// intern the name & value, as the parameters are owned by the caller and may be reused.
				// We could retain the record to avoid this copy, but that would hold
				// all other columns in memory for as long as the query is evaluated.
				internedLabel := a.interner.Get(labels[i].Name)
				internedValue := a.interner.Get(v)
				series[internedLabel] = internedValue
			}
		}

		a.uniqueSeries[key] = series
	}

	// Add all observations to the aggregation
	for _, ts := range timestamps {
		a.add(ts.UnixNano(), value, key)
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

	// emit aggregated results in sorted order of timestamp
	sortedTimestamps := a.getSortedTimestamps()

	// preallocate all builders to the total amount of rows
	total := 0
	total += len(a.points) * int((a.to-a.from)/a.step)
	rb.Reserve(total)

	// Iterate over the timestamps in order to produce a timestamp ordered output
	// It is more efficient to iterate over each series but then the output would not be ordered.
	for i, timestamp := range sortedTimestamps {
		for key, points := range a.points {
			entry := points[i]
			if entry.count == 0 {
				continue
			}

			tsValue := arrow.Timestamp(timestamp)

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
	a.interner.Reset()
}

// getSortedTimestamps returns all timestamps in sorted order
func (a *aggregator) getSortedTimestamps() []int64 {
	timestamps := make([]int64, 0, (a.to-a.from)/a.step)
	for i := a.from; i <= a.to; i += a.step {
		timestamps = append(timestamps, i)
	}
	return timestamps
}
