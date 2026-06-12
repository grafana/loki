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

	// batchRowKeys caches grouping keys by source row index during BatchAddSample.
	batchRowKeys         []uint64
	batchRowKeysComputed []bool
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

func (a *aggregator) pointForTimestamp(ts time.Time) map[uint64]*groupState {
	point, ok := a.points[ts]
	if !ok {
		point = make(map[uint64]*groupState)
		a.points[ts] = point
	}
	return point
}

// computeGroupKeyFromColumns computes a grouping key from the non-null label columns at row.
// Null columns are skipped, matching the sparse grouping semantics used by range and vector aggregators.
func (a *aggregator) computeGroupKeyFromColumns(labelCols []*array.String, labelFields []arrow.Field, row int) uint64 {
	a.digest.Reset()
	first := true
	for i, col := range labelCols {
		if col.IsNull(row) {
			continue
		}

		if !first {
			_, _ = a.digest.Write([]byte{0}) // separator
		}
		first = false

		_, _ = a.digest.WriteString(labelFields[i].Name)
		_, _ = a.digest.Write([]byte("="))
		_, _ = a.digest.WriteString(col.Value(row))
	}
	return a.digest.Sum64()
}

// ensureSeriesFromColumns registers a new unique series for key using label values read
// from Arrow columns at row. Null columns are skipped.
func (a *aggregator) ensureSeriesFromColumns(key uint64, labelCols []*array.String, labelFields []arrow.Field, row int) error {
	if _, exists := a.uniqueSeries[key]; exists {
		return nil
	}

	if a.maxSeries > 0 && len(a.uniqueSeries) >= a.maxSeries {
		return ErrSeriesLimitExceeded
	}

	var series map[string]string
	if len(labelFields) > 0 {
		series = make(map[string]string)
		for i, col := range labelCols {
			if col.IsNull(row) {
				continue
			}

			v := col.Value(row)
			cloned, ok := a.clonedLabelValues[v]
			if !ok {
				cloned = strings.Clone(v)
				a.clonedLabelValues[v] = cloned
			}
			series[labelFields[i].Name] = cloned
		}
	}

	a.uniqueSeries[key] = series
	return nil
}

// AddSample accumulates value at ts, grouping by the non-null label columns at row.
func (a *aggregator) AddSample(ts time.Time, value float64, labelCols []*array.String, labelFields []arrow.Field, row int) error {
	key := a.computeGroupKeyFromColumns(labelCols, labelFields, row)
	return a.addSampleWithKey(ts, key, value, labelCols, labelFields, row)
}

// BatchAddSample ingests samples from columnar arrays.
// Sample i aggregates at outputTs[i] with value values[i]. When sourceRows is nil,
// sample i uses label row i; otherwise label row sourceRows[i].
// values may be nil for count aggregations.
func (a *aggregator) BatchAddSample(
	outputTs *array.Timestamp,
	values *array.Float64,
	sourceRows *array.Int32,
	labelCols []*array.String,
	labelFields []arrow.Field,
) error {
	n := outputTs.Len()
	if values != nil && values.Len() != n {
		panic("BatchAddSample: outputTs and values length mismatch")
	}
	if sourceRows != nil && sourceRows.Len() != n {
		panic("BatchAddSample: outputTs and sourceRows length mismatch")
	}
	if n == 0 {
		return nil
	}

	a.prepareBatchRowKeys(a.maxSourceRow(sourceRows, labelCols, n) + 1)

	for i := range n {
		row := i
		if sourceRows != nil {
			if sourceRows.IsNull(i) {
				continue
			}
			row = int(sourceRows.Value(i))
		}

		if !a.batchRowKeysComputed[row] {
			a.batchRowKeys[row] = a.computeGroupKeyFromColumns(labelCols, labelFields, row)
			a.batchRowKeysComputed[row] = true
		}

		var value float64
		if values != nil {
			if values.IsNull(i) {
				continue
			}
			value = values.Value(i)
		}

		ts := outputTs.Value(i).ToTime(arrow.Nanosecond)
		if err := a.addSampleWithKey(ts, a.batchRowKeys[row], value, labelCols, labelFields, row); err != nil {
			return err
		}
	}

	return nil
}

func (a *aggregator) maxSourceRow(sourceRows *array.Int32, labelCols []*array.String, n int) int {
	if sourceRows != nil {
		maxRow := 0
		for i := range n {
			if sourceRows.IsNull(i) {
				continue
			}
			row := int(sourceRows.Value(i))
			if row > maxRow {
				maxRow = row
			}
		}
		return maxRow
	}

	if len(labelCols) > 0 {
		return labelCols[0].Len() - 1
	}

	return n - 1
}

func (a *aggregator) prepareBatchRowKeys(n int) {
	if cap(a.batchRowKeys) < n {
		a.batchRowKeys = make([]uint64, n)
		a.batchRowKeysComputed = make([]bool, n)
		return
	}

	a.batchRowKeys = a.batchRowKeys[:n]
	a.batchRowKeysComputed = a.batchRowKeysComputed[:n]
	clear(a.batchRowKeysComputed)
}

func (a *aggregator) addSampleWithKey(ts time.Time, key uint64, value float64, labelCols []*array.String, labelFields []arrow.Field, row int) error {
	point := a.pointForTimestamp(ts)

	if state, ok := point[key]; ok {
		a.accumulate(state, value)
		return nil
	}

	if err := a.ensureSeriesFromColumns(key, labelCols, labelFields, row); err != nil {
		return err
	}

	point[key] = &groupState{
		value: value,
		count: 1,
	}
	return nil
}

// accumulate updates an existing groupState for the configured aggregation operation.
func (a *aggregator) accumulate(state *groupState, value float64) {
	// TODO: handle hash collisions

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
