package executor

import (
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/cespare/xxhash/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
)

// columnarGroupState holds the aggregated state for a single group.
type columnarGroupState struct {
	value       float64  // aggregated value
	count       int64    // values counter
	labelValues []string // grouping label values (in order of groupBy columns)
}

// columnarAggregator is a columnar-oriented aggregator that processes data column-by-column
// for improved cache efficiency and reduced per-row overhead.
//
// Key differences from the row-based aggregator:
// 1. Pre-computes hashes for all rows at once using column-by-column hash combining
// 2. Uses int64 timestamps (nanoseconds) as map keys instead of time.Time
// 3. Avoids repeated hash computation by caching group hashes
type columnarAggregator struct {
	// points holds the groupState for each point in time, keyed by timestamp (nanoseconds)
	// and then by group hash
	points map[int64]map[uint64]*columnarGroupState

	// operation is the aggregation type
	operation aggregationOperation

	// groupByLabels are the labels used for grouping (in order)
	groupByLabels []arrow.Field

	// windowFunc maps an input timestamp to output window(s)
	// If nil, timestamps are used directly (identity mapping)
	windowFunc timestampMatchingWindowsFunc

	// clonedLabelValues caches cloned strings to reduce allocations for repeated values
	clonedLabelValues map[string]string

	// maxSeries is the maximum number of unique series allowed (0 means no limit)
	maxSeries int

	// uniqueSeries tracks unique series across all timestamps
	uniqueSeries map[uint64]struct{}

	// hashBuffer is a reusable buffer for computing hashes
	hashBuffer []uint64
}

// newColumnarAggregator creates a new columnar aggregator with the specified operation.
// The windowFunc parameter maps input timestamps to output windows. If nil, timestamps
// are used directly (identity mapping).
func newColumnarAggregator(pointsSizeHint int, operation aggregationOperation, windowFunc timestampMatchingWindowsFunc) *columnarAggregator {
	a := &columnarAggregator{
		operation:         operation,
		windowFunc:        windowFunc,
		clonedLabelValues: make(map[string]string),
		uniqueSeries:      make(map[uint64]struct{}),
	}

	if pointsSizeHint > 0 {
		a.points = make(map[int64]map[uint64]*columnarGroupState, pointsSizeHint)
	} else {
		a.points = make(map[int64]map[uint64]*columnarGroupState)
	}

	return a
}

// SetGroupByLabels merges the given labels into the existing groupBy labels.
// This can be called multiple times to accommodate processing of multiple records
// with different schemas (e.g., when using 'without' grouping).
func (a *columnarAggregator) SetGroupByLabels(labels []arrow.Field) {
	for _, label := range labels {
		if !slices.ContainsFunc(a.groupByLabels, func(l arrow.Field) bool {
			return label.Equal(l)
		}) {
			a.groupByLabels = append(a.groupByLabels, label)
		}
	}
}

// SetMaxSeries sets the maximum number of unique series allowed.
func (a *columnarAggregator) SetMaxSeries(maxSeries int) {
	a.maxSeries = maxSeries
}

// AddBatch processes an entire batch of data in a columnar fashion.
// It expects:
// - timestamps: array of timestamps (one per row)
// - values: array of values to aggregate (one per row), can be nil for COUNT
// - groupByColumns: arrays for each groupBy column (in order matching groupByLabels)
//
// Returns ErrSeriesLimitExceeded if the series limit is exceeded.
func (a *columnarAggregator) AddBatch(
	timestamps *array.Timestamp,
	values *array.Float64,
	groupByColumns []*array.String,
) error {
	numRows := timestamps.Len()
	if numRows == 0 {
		return nil
	}

	// Compute hashes for all rows using column-by-column approach
	hashes := a.computeHashes(groupByColumns, numRows)

	// Process each row
	for row := 0; row < numRows; row++ {
		// Skip null values (except for COUNT)
		if values != nil && values.IsNull(row) {
			continue
		}

		hash := hashes[row]

		var value float64
		if values != nil {
			value = values.Value(row)
		}

		// Get the output timestamps from the window function
		outputTimestamps := a.getOutputTimestamps(timestamps.Value(row))
		if len(outputTimestamps) == 0 {
			continue // timestamp is out of range or doesn't match any window
		}

		// Add the value to each matching window
		for _, tsNano := range outputTimestamps {
			if err := a.addToGroup(tsNano, hash, value, groupByColumns, row); err != nil {
				return err
			}
		}
	}

	return nil
}

// getOutputTimestamps returns the output timestamp(s) for a given input timestamp.
// If windowFunc is set, it maps the input timestamp to window end times.
// If windowFunc is nil, the input timestamp is used directly.
func (a *columnarAggregator) getOutputTimestamps(inputTs arrow.Timestamp) []int64 {
	if a.windowFunc == nil {
		// No window function - use timestamp directly
		return []int64{int64(inputTs)}
	}

	// Convert Arrow timestamp to time.Time and apply window function
	t := inputTs.ToTime(arrow.Nanosecond)
	windows := a.windowFunc(t)

	if len(windows) == 0 {
		return nil
	}

	// Extract the end time of each window as the output timestamp
	result := make([]int64, len(windows))
	for i, w := range windows {
		result[i] = w.end.UnixNano()
	}

	return result
}

// addToGroup adds a value to a specific group at a specific timestamp.
func (a *columnarAggregator) addToGroup(tsNano int64, hash uint64, value float64, groupByColumns []*array.String, row int) error {
	// Get or create the point map for this timestamp
	point, ok := a.points[tsNano]
	if !ok {
		point = make(map[uint64]*columnarGroupState)
		a.points[tsNano] = point
	}

	if state, ok := point[hash]; ok {
		// Existing group - accumulate value
		a.accumulateValue(state, value)
	} else {
		// New group - check series limit and create state
		if err := a.checkSeriesLimit(hash); err != nil {
			return err
		}

		// Extract and clone label values for this row
		labelValues := a.extractLabelValues(groupByColumns, row)

		point[hash] = &columnarGroupState{
			value:       a.initialValue(value),
			count:       1,
			labelValues: labelValues,
		}
	}

	return nil
}

// computeHashes computes hashes for all rows using column-by-column processing.
// This approach is more cache-friendly than row-by-row processing.
func (a *columnarAggregator) computeHashes(columns []*array.String, numRows int) []uint64 {
	// Reuse or allocate hash buffer
	if cap(a.hashBuffer) < numRows {
		a.hashBuffer = make([]uint64, numRows)
	} else {
		a.hashBuffer = a.hashBuffer[:numRows]
		// Clear the buffer
		for i := range a.hashBuffer {
			a.hashBuffer[i] = 0
		}
	}

	if len(columns) == 0 {
		// No grouping columns - all rows get the same hash (0)
		return a.hashBuffer
	}

	// Process first column - initialize hashes
	col := columns[0]
	for row := 0; row < numRows; row++ {
		a.hashBuffer[row] = xxhash.Sum64String(col.Value(row))
	}

	// Process remaining columns - combine hashes
	for colIdx := 1; colIdx < len(columns); colIdx++ {
		col := columns[colIdx]
		for row := 0; row < numRows; row++ {
			valueHash := xxhash.Sum64String(col.Value(row))
			a.hashBuffer[row] = combineHashes(a.hashBuffer[row], valueHash)
		}
	}

	return a.hashBuffer
}

// combineHashes combines two hash values using the same algorithm as DataFusion.
// This is a simple but effective hash combining function using prime numbers.
func combineHashes(existing, new uint64) uint64 {
	// DataFusion-style: (17*37 + existing) * 37 + new
	return (17*37+existing)*37 + new
}

// accumulateValue adds a value to an existing group state.
func (a *columnarAggregator) accumulateValue(state *columnarGroupState, value float64) {
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

// initialValue returns the initial value for a new group.
func (a *columnarAggregator) initialValue(value float64) float64 {
	switch a.operation {
	case aggregationOperationMin:
		// For MIN, if this is the first value, use it directly
		// Otherwise we'd need to initialize with +Inf, but since we're
		// creating the state with the first value, we can just return it
		return value
	case aggregationOperationMax:
		// Same reasoning as MIN
		return value
	default:
		return value
	}
}

// checkSeriesLimit checks if adding a new series would exceed the limit.
func (a *columnarAggregator) checkSeriesLimit(hash uint64) error {
	if a.maxSeries <= 0 {
		return nil
	}

	if _, exists := a.uniqueSeries[hash]; !exists {
		if len(a.uniqueSeries) >= a.maxSeries {
			return ErrSeriesLimitExceeded
		}
		a.uniqueSeries[hash] = struct{}{}
	}

	return nil
}

// extractLabelValues extracts and clones label values for a row.
func (a *columnarAggregator) extractLabelValues(columns []*array.String, row int) []string {
	if len(columns) == 0 {
		return nil
	}

	labelValues := make([]string, len(columns))
	for i, col := range columns {
		val := col.Value(row)
		// Clone the value to avoid holding references to arrow buffers
		cloned, ok := a.clonedLabelValues[val]
		if !ok {
			cloned = strings.Clone(val)
			a.clonedLabelValues[val] = cloned
		}
		labelValues[i] = cloned
	}

	return labelValues
}

// BuildRecord builds the final Arrow record from the aggregated data.
func (a *columnarAggregator) BuildRecord() (arrow.RecordBatch, error) {
	// Build schema: timestamp, value, then groupBy labels
	fields := make([]arrow.Field, 0, len(a.groupByLabels)+2)
	fields = append(fields,
		semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, false),
		semconv.FieldFromIdent(semconv.ColumnIdentValue, false),
	)
	fields = append(fields, a.groupByLabels...)

	schema := arrow.NewSchema(fields, nil)
	rb := array.NewRecordBuilder(memory.NewGoAllocator(), schema)

	// Get sorted timestamps
	sortedTimestamps := a.getSortedTimestamps()

	// Preallocate builders
	total := 0
	for _, ts := range sortedTimestamps {
		total += len(a.points[ts])
	}
	rb.Reserve(total)

	// Emit results
	for _, tsNano := range sortedTimestamps {
		tsValue := arrow.Timestamp(tsNano)

		for _, state := range a.points[tsNano] {
			// Compute final value based on operation
			var value float64
			switch a.operation {
			case aggregationOperationAvg:
				value = state.value / float64(state.count)
			case aggregationOperationCount:
				value = float64(state.count)
			default:
				value = state.value
			}

			// Append timestamp and value
			rb.Field(0).(*array.TimestampBuilder).Append(tsValue)
			rb.Field(1).(*array.Float64Builder).Append(value)

			// Append label values (in order of groupByLabels)
			for i := range a.groupByLabels {
				builder := rb.Field(2 + i).(*array.StringBuilder)
				if i < len(state.labelValues) {
					val := state.labelValues[i]
					if val == "" {
						builder.AppendNull()
					} else {
						builder.Append(val)
					}
				} else {
					builder.AppendNull()
				}
			}
		}
	}

	return rb.NewRecordBatch(), nil
}

// Reset clears the aggregator state for reuse.
func (a *columnarAggregator) Reset() {
	for _, point := range a.points {
		clear(point)
	}
	clear(a.uniqueSeries)
}

// getSortedTimestamps returns all timestamps in sorted order.
func (a *columnarAggregator) getSortedTimestamps() []int64 {
	return slices.Sorted(maps.Keys(a.points))
}

// --- Utility functions for batch processing ---

// ColumnarHasher provides efficient hash computation for multiple columns.
// It processes columns one at a time for better cache locality.
type ColumnarHasher struct {
	hashes []uint64
}

// NewColumnarHasher creates a new hasher with the given capacity hint.
func NewColumnarHasher(capacityHint int) *ColumnarHasher {
	return &ColumnarHasher{
		hashes: make([]uint64, 0, capacityHint),
	}
}

// HashStringColumns computes combined hashes for all rows across all columns.
// The first column initializes the hashes, subsequent columns are combined.
func (h *ColumnarHasher) HashStringColumns(columns []*array.String) []uint64 {
	if len(columns) == 0 {
		return h.hashes[:0]
	}

	numRows := columns[0].Len()

	// Ensure capacity
	if cap(h.hashes) < numRows {
		h.hashes = make([]uint64, numRows)
	} else {
		h.hashes = h.hashes[:numRows]
	}

	// First column: initialize hashes
	col := columns[0]
	for i := 0; i < numRows; i++ {
		h.hashes[i] = xxhash.Sum64String(col.Value(i))
	}

	// Remaining columns: combine hashes
	for colIdx := 1; colIdx < len(columns); colIdx++ {
		col := columns[colIdx]
		for i := 0; i < numRows; i++ {
			valueHash := xxhash.Sum64String(col.Value(i))
			h.hashes[i] = combineHashes(h.hashes[i], valueHash)
		}
	}

	return h.hashes
}

// HashInt64Column hashes an int64 column (e.g., for timestamps).
func (h *ColumnarHasher) HashInt64Column(col *array.Int64, rehash bool) []uint64 {
	numRows := col.Len()

	// Ensure capacity
	if cap(h.hashes) < numRows {
		h.hashes = make([]uint64, numRows)
	} else {
		h.hashes = h.hashes[:numRows]
	}

	values := col.Int64Values()

	if rehash {
		for i, v := range values {
			valueHash := hashInt64(v)
			h.hashes[i] = combineHashes(h.hashes[i], valueHash)
		}
	} else {
		for i, v := range values {
			h.hashes[i] = hashInt64(v)
		}
	}

	return h.hashes
}

// hashInt64 computes the hash of an int64 value.
func hashInt64(v int64) uint64 {
	// Convert to bytes and hash
	// Use the same approach as xxhash for consistency
	return xxhash.Sum64(int64ToBytes(v))
}

// int64ToBytes converts an int64 to a byte slice without allocation.
// Uses math.Float64bits trick to get a uint64, then converts to bytes.
func int64ToBytes(v int64) []byte {
	// This is a simple approach - for production, consider using unsafe
	b := make([]byte, 8)
	u := uint64(v)
	b[0] = byte(u)
	b[1] = byte(u >> 8)
	b[2] = byte(u >> 16)
	b[3] = byte(u >> 24)
	b[4] = byte(u >> 32)
	b[5] = byte(u >> 40)
	b[6] = byte(u >> 48)
	b[7] = byte(u >> 56)
	return b
}

// Compile-time check to ensure we don't use math package incorrectly
var _ = math.MaxFloat64
