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

// windowMatchFunc resolves output timestamps for an entire batch of
// input timestamps. It reads validMask and sets validMask[i] = false for rows
// that don't match any window. For matched rows, outTs[i] is written with the
// resolved output timestamp (nanos).
type windowMatchFunc func(inputTs []int64, outTs []int64, validMask []bool, numRows int)

type columnarAggregatorOpts struct {
	operation     aggregationOperation
	groupByLabels []arrow.Field
	matchWindows  windowMatchFunc // nil = identity mode
	maxSeries     int             // 0 = unlimited
}

// columnarAggregator aggregates input data using the grouping labels, time windows
// and the aggregation operation.
//
// The time window a sample belongs to is determined by the matchWindows function.
// If matchWindows is nil, the output timestamp is the same as the input timestamp (identity mode).
//
// This version of the aggregator tries to perform columnar processing where possible.
// For example [updateSum] updates the aggregator state in tight loop over contiguous arrays.
//
// TODO:
//  1. A sample can belong to more than one window, the current implementations
//     only supports one output window per input sample.
//  2. Grouping **without** labels is not supported in this implementation.
//
// We can either extend this implementation to support the above cases, or look
// into dedicated aggregators if extending is affecting benchmarks.
type columnarAggregator struct {
	opts          columnarAggregatorOpts
	aggregateFunc func(values []float64, counts []int64, groupIdxForRow []int, inputValues []float64)

	// groupMap holds the mapping from groupKey to aggregator index.
	groupMap map[groupKey]int

	// aggregator state.
	values []float64 // accumulated value per group
	counts []int64   // row count per group

	// seriesMap holds label values for each unique series hash.
	seriesMap   map[uint64][]string // seriesHash -> label values
	labelValues map[string]string   // dedupe label values

	// reusable buffers for batch processing
	seriesHash []uint64 // per-row series hashes
	outTS      []int64  // per-row resolved output timestamps
	groupIdx   []int    // per-row resolved group index, -1 = skip
	validMask  []bool   // per-row null and out of bounds filtering mask

	columnValues []colBuffers // pre-fetched column offset/byte slices
	hashBuffer   []byte       // scratch buffer for hash computation
}

type colBuffers struct {
	offsets []int32
	bytes   []byte
}

func newColumnarAggregator(pointsSizeHint int, opts columnarAggregatorOpts) *columnarAggregator {
	a := &columnarAggregator{
		opts: opts,

		// assuming each point gets atleast one sample.
		groupMap: make(map[groupKey]int, pointsSizeHint),
		values:   make([]float64, 0, pointsSizeHint),
		counts:   make([]int64, 0, pointsSizeHint),

		seriesMap:   make(map[uint64][]string),
		labelValues: make(map[string]string),

		columnValues: make([]colBuffers, len(opts.groupByLabels)),
	}
	a.aggregateFunc = a.resolveUpdateFunc(opts.operation)
	return a
}

// AddBatch updates the aggregator state for the input batch.
// It assumes all columns are of the same length and that the
// groupByColumns are in the same order as opts.groupByLabels.
func (a *columnarAggregator) AddBatch(
	timestamps *array.Timestamp,
	values *array.Float64,
	groupByColumns []*array.String,
) error {
	numRows := timestamps.Len()
	if numRows == 0 {
		return nil
	}

	// Compute hash for each row using groupBy column values.
	//
	// TODO:
	// 1. This does not consider collisions where multiple series have
	//    the same hash. This needs to be addressed in a follow-up, which
	//    might impact the benchmarks and require row based access of label
	//    values.
	if len(groupByColumns) > 0 {
		a.computeHashes(groupByColumns, numRows)
	}

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
	a.resolveWindows(timestamps, numRows)

	// Group resolution — maps each valid row to a group index.
	if err := a.resolveGroups(groupByColumns, numRows); err != nil {
		return err
	}

	// update aggregator state for each group.
	var valSlice []float64
	if values != nil {
		valSlice = values.Float64Values()
	}
	a.aggregateFunc(a.values, a.counts, a.groupIdx[:numRows], valSlice)

	return nil
}

// computeHashes computes a hash for each row by building a composite key
// from all groupBy column values (separated by a null byte) and hashing it
// with xxhash.Sum64.
//
// It accesses Arrow byte and offset buffers directly to avoid per-row
// Value() overhead.
func (a *columnarAggregator) computeHashes(arrays []*array.String, numRows int) {
	if cap(a.seriesHash) < numRows {
		a.seriesHash = a.seriesHash[:0]
		a.seriesHash = slices.Grow(a.seriesHash, numRows)
	}
	a.seriesHash = a.seriesHash[:numRows]

	for i, arr := range arrays {
		a.columnValues[i] = colBuffers{
			offsets: arr.ValueOffsets(),
			bytes:   arr.ValueBytes(),
		}
	}

	// avoid keyBuf alloc and copy.
	if len(arrays) == 1 {
		offsets := a.columnValues[0].offsets
		bytes := a.columnValues[0].bytes

		for row := range numRows {
			a.seriesHash[row] = xxhash.Sum64(bytes[offsets[row]:offsets[row+1]])
		}

		return
	}

	for row := range numRows {
		a.hashBuffer = a.hashBuffer[:0]
		for i, col := range a.columnValues {
			if i > 0 {
				a.hashBuffer = append(a.hashBuffer, 0)
			}
			a.hashBuffer = append(a.hashBuffer, col.bytes[col.offsets[row]:col.offsets[row+1]]...)
		}
		a.seriesHash[row] = xxhash.Sum64(a.hashBuffer)
	}
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
func (a *columnarAggregator) resolveWindows(timestamps *array.Timestamp, numRows int) {
	if cap(a.outTS) < numRows {
		a.outTS = a.outTS[:0] // reset buffer
		a.outTS = slices.Grow(a.outTS, numRows)
	}
	a.outTS = a.outTS[:numRows]

	tsValues := timestamps.Values()
	for i := range numRows {
		a.outTS[i] = int64(tsValues[i])
	}

	if a.opts.matchWindows == nil {
		return
	}

	// matchWindows updates a.outTSBuf in-place
	a.opts.matchWindows(a.outTS, a.outTS, a.validMask, numRows)
}

// resolveGroups maps each row to a group index, writing the result into the
// groupIdxForRow buffer.
func (a *columnarAggregator) resolveGroups(
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

		key := groupKey{tsNano: a.outTS[row]}
		if len(groupByColumns) > 0 {
			key.seriesHash = a.seriesHash[row]
		}

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

// getOrCreateGroup looks up or creates a group, returning its aggregator index.
func (a *columnarAggregator) getOrCreateGroup(
	key groupKey,
	groupByColumns []*array.String,
	origRow int,
) (int, error) {
	if idx, ok := a.groupMap[key]; ok {
		return idx, nil
	}

	// Register series if new, enforcing the series limit.
	if len(groupByColumns) > 0 {
		if _, exists := a.seriesMap[key.seriesHash]; !exists {
			if a.opts.maxSeries > 0 && len(a.seriesMap) >= a.opts.maxSeries {
				return 0, ErrSeriesLimitExceeded
			}
			a.seriesMap[key.seriesHash] = a.extractLabelValues(groupByColumns, origRow)
		}
	}

	// Append new group aggregator state.
	groupIdx := len(a.values)
	a.groupMap[key] = groupIdx
	a.values = append(a.values, a.initialValue())
	a.counts = append(a.counts, 0)

	return groupIdx, nil
}

func (a *columnarAggregator) initialValue() float64 {
	switch a.opts.operation {
	case aggregationOperationMin:
		return math.Inf(1)
	case aggregationOperationMax:
		return math.Inf(-1)
	default:
		return 0
	}
}

func (a *columnarAggregator) extractLabelValues(columns []*array.String, row int) []string {
	if len(columns) == 0 {
		return nil
	}

	labelValues := make([]string, len(columns))
	for i, col := range columns {
		val := col.Value(row)
		cloned, ok := a.labelValues[val]
		if !ok {
			cloned = strings.Clone(val)
			a.labelValues[cloned] = cloned
		}
		labelValues[i] = cloned
	}

	return labelValues
}

func (a *columnarAggregator) resolveUpdateFunc(op aggregationOperation) func([]float64, []int64, []int, []float64) {
	// TODO: we currently update both values and counts for all operations
	// even though count is not always used. we can optimize this further.
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
		panic("unsupported aggregation operation")
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
	fields := make([]arrow.Field, 0, len(a.opts.groupByLabels)+2)
	fields = append(fields,
		semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, false),
		semconv.FieldFromIdent(semconv.ColumnIdentValue, false),
	)
	fields = append(fields, a.opts.groupByLabels...)

	schema := arrow.NewSchema(fields, nil)
	rb := array.NewRecordBuilder(memory.NewGoAllocator(), schema)
	rb.Reserve(len(a.groupMap))

	tsBuilder := rb.Field(0).(*array.TimestampBuilder)
	valBuilder := rb.Field(1).(*array.Float64Builder)

	labelBuilders := make([]*array.StringBuilder, len(a.opts.groupByLabels))
	for i := range a.opts.groupByLabels {
		labelBuilders[i] = rb.Field(2 + i).(*array.StringBuilder)
	}

	for key, accumIdx := range a.groupMap {
		tsBuilder.Append(arrow.Timestamp(key.tsNano))
		valBuilder.Append(a.finalValue(accumIdx))
		a.appendLabels(labelBuilders, key.seriesHash)
	}

	return rb.NewRecordBatch(), nil
}

func (a *columnarAggregator) finalValue(gIdx int) float64 {
	switch a.opts.operation {
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

func (a *columnarAggregator) appendLabels(lblBuilders []*array.StringBuilder, seriesHash uint64) {
	labels := a.seriesMap[seriesHash]

	for i := range a.opts.groupByLabels {
		builder := lblBuilders[i]
		val := labels[i]

		if val == "" {
			builder.AppendNull()
		} else {
			builder.Append(val)
		}
	}
}

// Reset clears the aggregator state for reuse.
func (a *columnarAggregator) Reset() {
	a.values = a.values[:0]
	a.counts = a.counts[:0]
	clear(a.groupMap)
	clear(a.seriesMap)
	clear(a.labelValues)
}
