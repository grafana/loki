package executor

import (
	"math"
	"slices"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/cespare/xxhash/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
)

// groupKey is a composite key for the group map.
type groupKey struct {
	tsNano     int64
	seriesHash uint64
}

// columnarWindowMatchFunc resolves output timestamps for an entire batch of
// input timestamps. It reads validMask and sets validMask[i] = false for rows
// that don't match any window. For matched rows, outTs[i] is written with the
// resolved output timestamp (nanos). inputTs and outTs must have length >=
// numRows. nil means identity mode: output timestamps equal input timestamps.
type columnarWindowMatchFunc func(inputTs []int64, outTs []int64, validMask []bool, numRows int)

// columnarAggregator uses flat contiguous slices for accumulator state and
// processes data in separated phases for improved cache efficiency.
//
// Data layout:
//   - groupMap maps groupKey{outputTsNano, seriesHash} -> accumIdx
//   - values[accumIdx] and counts[accumIdx] hold accumulator state per group
//   - seriesMap stores label values ONCE per unique series hash
//
// Timestamps and series labels are reconstructed from groupMap keys during
// BuildRecord, avoiding redundant per-group metadata slices.
type columnarAggregator struct {
	maxSeries     int                     // 0 = unlimited
	groupByLabels []arrow.Field           // labels used for grouping (set once at construction)
	matchWindows  columnarWindowMatchFunc // nil = identity mode (input ts IS output ts)
	operation     aggregationOperation
	updateFunc    func(values []float64, counts []int64, groupIdxForRow []int, inputValues []float64) // typed accumulator, resolved once at init

	// Accumulator state (grows over the lifetime of the aggregator) ---
	// groupMap resolves a (timestamp, seriesHash) pair to an accumulator index.
	groupMap map[groupKey]int
	values   []float64 // accumulated value per group
	counts   []int64   // row count per group

	// Global series registry: label values stored ONCE per unique series hash.
	seriesMap         map[uint64][]string // seriesHash -> label values
	clonedLabelValues map[string]string   // dedup cache: original string -> cloned string (avoids holding arrow buffer refs)

	// resuable buffers for per-batch processing
	hashBuffer []uint64 // per-row series hashes (len = numRows)
	outTSBuf   []int64  // per-row resolved output timestamps (len = numRows)
	groupIdx   []int    // per-row resolved group index, -1 = skip (len = numRows)
	validMask  []bool   // per-row null and out of bounds filtering mask (len = numRows)
}

func newColumnarAggregator(pointsSizeHint int, operation aggregationOperation, groupByLabels []arrow.Field, matchWindows columnarWindowMatchFunc) *columnarAggregator {
	a := &columnarAggregator{
		seriesMap:         make(map[uint64][]string),
		operation:         operation,
		matchWindows:      matchWindows,
		groupByLabels:     groupByLabels,
		clonedLabelValues: make(map[string]string),

		// assuming each point gets atleast one sample.
		// these will resize as we come across more series per point.
		groupMap: make(map[groupKey]int, pointsSizeHint),
		values:   make([]float64, 0, pointsSizeHint),
		counts:   make([]int64, 0, pointsSizeHint),
	}
	a.updateFunc = a.resolveUpdateFunc(operation)
	return a
}

// SetMaxSeries sets the maximum number of unique series allowed.
func (a *columnarAggregator) SetMaxSeries(maxSeries int) {
	a.maxSeries = maxSeries
}

// AddBatch processes an entire batch of data using phase-separated columnar processing.
func (a *columnarAggregator) AddBatch(
	timestamps *array.Timestamp,
	values *array.Float64,
	groupByColumns []*array.String,
) error {
	numRows := timestamps.Len()
	if numRows == 0 {
		return nil
	}

	// compute hash for each row using groupBy column values.
	hashes := a.computeHashes(groupByColumns, numRows)

	// set validMask to filter out rows with null values in the value column.
	validCount := a.computeValidMask(values, numRows)
	if validCount == 0 {
		return nil
	}

	// resolves output timestamp for each row and updates validMask to exclude
	// rows that are out of bounds.
	//
	// for range queries, matchWindows will be used to determine the output timestamp.
	// for vector queries, this will be identity operation.
	outTs := a.resolveWindows(timestamps)

	// Phase 4: Group resolution — maps each valid row to a group index.
	// Writes into groupIdxForRow (dense, indexed by row; -1 = skip).
	if err := a.resolveGroups(outTs, hashes, groupByColumns, numRows); err != nil {
		return err
	}

	// Phase 5: Tight accumulator update over contiguous arrays.
	var valSlice []float64
	if values != nil {
		valSlice = values.Float64Values()
	}
	a.updateFunc(a.values, a.counts, a.groupIdx[:numRows], valSlice)

	return nil
}

func (a *columnarAggregator) computeValidMask(values *array.Float64, numRows int) int {
	if cap(a.validMask) < numRows {
		a.validMask = a.validMask[:0] // reset buffer
		a.validMask = slices.Grow(a.validMask, numRows)
	}
	a.validMask = a.validMask[:numRows]

	if values == nil {
		for i := range numRows {
			a.validMask[i] = true
		}
		return numRows
	}

	count := 0
	for i := range numRows {
		valid := !values.IsNull(i)
		a.validMask[i] = valid
		if valid {
			count++
		}
	}
	return count
}

// resolveWindows maps input timestamps to output timestamps for the entire
// batch. When matchWindows is nil (identity mode), output equals input. When
// set, it runs the columnar matcher which also updates validMask to exclude
// rows that don't fall into any window.
func (a *columnarAggregator) resolveWindows(timestamps *array.Timestamp) []int64 {
	numRows := timestamps.Len()

	if cap(a.outTSBuf) < numRows {
		a.outTSBuf = a.outTSBuf[:0] // reset buffer
		a.outTSBuf = slices.Grow(a.outTSBuf, numRows)
	}
	a.outTSBuf = a.outTSBuf[:numRows]

	tsValues := timestamps.Values()

	for i := range numRows {
		a.outTSBuf[i] = int64(tsValues[i])
	}

	if a.matchWindows == nil {
		return a.outTSBuf
	}

	// matchWindows updates a.outTSBuf in-place
	a.matchWindows(a.outTSBuf, a.outTSBuf, a.validMask, numRows)
	return a.outTSBuf
}

// resolveGroups maps each row to a group index, writing the result into the
// dense groupIdxForRow buffer. Invalid or unmatched rows are set to -1.
// This separates the cache-hostile map lookups from the subsequent tight
// accumulator update loop.
func (a *columnarAggregator) resolveGroups(
	outTs []int64,
	hashes []uint64,
	groupByColumns []*array.String,
	numRows int,
) error {
	if cap(a.groupIdx) < numRows {
		a.groupIdx = a.groupIdx[:0]
		a.groupIdx = slices.Grow(a.groupIdx, numRows)
	}
	a.groupIdx = a.groupIdx[:numRows]

	for row := range numRows {
		if !a.validMask[row] {
			a.groupIdx[row] = -1
			continue
		}

		h := hashes[row]
		key := groupKey{tsNano: outTs[row], seriesHash: h}

		// this still uses row operations for reading column values
		// when creating a new group.
		gIdx, err := a.getOrCreateGroup(key, groupByColumns, row)
		if err != nil {
			return err
		}

		a.groupIdx[row] = gIdx
	}

	return nil
}

// getOrCreateGroup looks up or creates a group, returning its accumulator index.
func (a *columnarAggregator) getOrCreateGroup(
	key groupKey,
	groupByColumns []*array.String,
	origRow int,
) (int, error) {
	if idx, ok := a.groupMap[key]; ok {
		return idx, nil
	}

	// Register series if new, enforcing the series limit.
	if _, exists := a.seriesMap[key.seriesHash]; !exists {
		if a.maxSeries > 0 && len(a.seriesMap) >= a.maxSeries {
			return 0, ErrSeriesLimitExceeded
		}
		a.seriesMap[key.seriesHash] = a.extractLabelValues(groupByColumns, origRow)
	}

	// Append new group accumulator state.
	groupIdx := len(a.values)
	a.groupMap[key] = groupIdx
	a.values = append(a.values, a.initialValue())
	a.counts = append(a.counts, 0)

	return groupIdx, nil
}

func (a *columnarAggregator) initialValue() float64 {
	switch a.operation {
	case aggregationOperationMin:
		return math.Inf(1)
	case aggregationOperationMax:
		return math.Inf(-1)
	default:
		return 0
	}
}

// computeHashes computes hashes for all rows using column-by-column processing.
// Each column is scanned sequentially using direct access to the Arrow byte and
// offset buffers, avoiding per-row Value() overhead (method dispatch, offset
// addition, string header construction).
func (a *columnarAggregator) computeHashes(columns []*array.String, numRows int) []uint64 {
	if cap(a.hashBuffer) < numRows {
		a.hashBuffer = a.hashBuffer[:0] // reset buffer
		a.hashBuffer = slices.Grow(a.hashBuffer, numRows)
	}
	a.hashBuffer = a.hashBuffer[:numRows]

	// TODO: handle no grouping in the AddBatch method. No need to enter this func.
	if len(columns) == 0 {
		return a.hashBuffer
	}

	offsets := columns[0].ValueOffsets()
	bytes := columns[0].ValueBytes()
	for row := range numRows {
		a.hashBuffer[row] = xxhash.Sum64(bytes[offsets[row]:offsets[row+1]])
	}

	for colIdx := 1; colIdx < len(columns); colIdx++ {
		offsets = columns[colIdx].ValueOffsets()
		bytes = columns[colIdx].ValueBytes()
		for row := range numRows {
			valueHash := xxhash.Sum64(bytes[offsets[row]:offsets[row+1]])
			a.hashBuffer[row] = combineHashes(a.hashBuffer[row], valueHash)
		}
	}

	return a.hashBuffer
}

// combineHashes combines two hash values.
// TODO: document how this work?
func combineHashes(existing, new uint64) uint64 {
	return (17*37+existing)*37 + new
}

func (a *columnarAggregator) extractLabelValues(columns []*array.String, row int) []string {
	if len(columns) == 0 {
		return nil
	}

	labelValues := make([]string, len(columns))
	for i, col := range columns {
		val := col.Value(row)
		cloned, ok := a.clonedLabelValues[val]
		if !ok {
			cloned = strings.Clone(val)
			a.clonedLabelValues[cloned] = cloned
		}
		labelValues[i] = cloned
	}

	return labelValues
}

func (a *columnarAggregator) resolveUpdateFunc(op aggregationOperation) func([]float64, []int64, []int, []float64) {
	switch op {
	case aggregationOperationSum, aggregationOperationAvg:
		return updateSum
	case aggregationOperationCount:
		return updateCount
	case aggregationOperationMax:
		return updateMax
	case aggregationOperationMin:
		return updateMin
	default:
		return updateSum
	}
}

func updateSum(values []float64, counts []int64, groupIdxForRow []int, inputValues []float64) {
	for i, gIdx := range groupIdxForRow {
		if gIdx < 0 {
			continue
		}
		values[gIdx] += inputValues[i]
		counts[gIdx]++
	}
}

func updateCount(_ []float64, counts []int64, groupIdxForRow []int, _ []float64) {
	for _, gIdx := range groupIdxForRow {
		if gIdx < 0 {
			continue
		}
		counts[gIdx]++
	}
}

func updateMax(values []float64, counts []int64, groupIdxForRow []int, inputValues []float64) {
	for i, gIdx := range groupIdxForRow {
		if gIdx < 0 {
			continue
		}
		if inputValues[i] > values[gIdx] {
			values[gIdx] = inputValues[i]
		}
		counts[gIdx]++
	}
}

func updateMin(values []float64, counts []int64, groupIdxForRow []int, inputValues []float64) {
	for i, gIdx := range groupIdxForRow {
		if gIdx < 0 {
			continue
		}
		if inputValues[i] < values[gIdx] {
			values[gIdx] = inputValues[i]
		}
		counts[gIdx]++
	}
}

// BuildRecord builds the final Arrow record from the aggregated data.
func (a *columnarAggregator) BuildRecord() (arrow.RecordBatch, error) {
	fields := make([]arrow.Field, 0, len(a.groupByLabels)+2)
	fields = append(fields,
		semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, false),
		semconv.FieldFromIdent(semconv.ColumnIdentValue, false),
	)
	fields = append(fields, a.groupByLabels...)

	schema := arrow.NewSchema(fields, nil)
	rb := array.NewRecordBuilder(memory.NewGoAllocator(), schema)
	rb.Reserve(len(a.groupMap))

	tsBuilder := rb.Field(0).(*array.TimestampBuilder)
	valBuilder := rb.Field(1).(*array.Float64Builder)

	for key, accumIdx := range a.groupMap {
		tsBuilder.Append(arrow.Timestamp(key.tsNano))
		valBuilder.Append(a.finalValue(accumIdx))
		a.appendLabels(rb, key.seriesHash)
	}

	return rb.NewRecordBatch(), nil
}

func (a *columnarAggregator) finalValue(gIdx int) float64 {
	switch a.operation {
	case aggregationOperationAvg:
		if a.counts[gIdx] == 0 {
			return 0
		}
		return a.values[gIdx] / float64(a.counts[gIdx])
	case aggregationOperationCount:
		return float64(a.counts[gIdx])
	default:
		return a.values[gIdx]
	}
}

func (a *columnarAggregator) appendLabels(rb *array.RecordBuilder, seriesHash uint64) {
	labels := a.seriesMap[seriesHash]

	for i := range a.groupByLabels {
		builder := rb.Field(2 + i).(*array.StringBuilder)
		if i < len(labels) {
			val := labels[i]
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

// Reset clears the aggregator state for reuse.
func (a *columnarAggregator) Reset() {
	clear(a.groupMap)
	a.values = a.values[:0]
	a.counts = a.counts[:0]
	clear(a.seriesMap)
}
