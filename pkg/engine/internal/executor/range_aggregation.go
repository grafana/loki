package executor

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/assertions"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/xcap"
)

type rangeAggregationOptions struct {
	grouping physical.Grouping

	// start and end timestamps are equal for instant queries.
	startTs        time.Time     // start timestamp of the query
	endTs          time.Time     // end timestamp of the query
	rangeInterval  time.Duration // range interval
	step           time.Duration // step used for range queries
	operation      types.RangeAggregationType
	maxQuerySeries int // maximum number of unique series allowed
}

// rangeAggregationOperations holds the mapping of range aggregation types to operations for an aggregator.
var rangeAggregationOperations = map[types.RangeAggregationType]aggregationOperation{
	types.RangeAggregationTypeSum:   aggregationOperationSum,
	types.RangeAggregationTypeCount: aggregationOperationCount,
	types.RangeAggregationTypeMax:   aggregationOperationMax,
	types.RangeAggregationTypeMin:   aggregationOperationMin,
	types.RangeAggregationTypeAvg:   aggregationOperationAvg,
}

// window is a time interval where start is exclusive and end is inclusive.
// Refer to [logql.batchRangeVectorIterator]. Timestamps in int64 helpers are Unix nanoseconds.
type window struct {
	start, end time.Time
}

type columnarMatcherKind int

const (
	columnarMatcherInstant columnarMatcherKind = iota
	columnarMatcherAligned
	columnarMatcherGapped
	columnarMatcherOverlapping
)

// rangeAggregationPipeline is a pipeline that performs aggregations over a time window.
//
// 1. It reads from the input pipelines
// 2. Groups the data by the specified columns
// 3. Applies the aggregation function on each group
//
// Current version only supports counting for instant queries.
type rangeAggregationPipeline struct {
	inputs          []Pipeline
	inputsExhausted bool // indicates if all inputs are exhausted

	aggregator      *aggregator
	windowStarts    []int64
	windowEnds      []int64
	matcher         *matcherFactory
	columnarMatcher columnarMatcherKind
	expandScratch   rangeAggExpandScratch
	evaluator       *expressionEvaluator // used to evaluate column expressions
	opts            rangeAggregationOptions
	identCache      *semconv.IdentifierCache
}

type rangeAggExpandScratch struct {
	mem        memory.Allocator
	outputTs   []int64
	values     []float64
	sourceRows []int
}

func (s *rangeAggExpandScratch) reset() {
	s.outputTs = s.outputTs[:0]
	s.values = s.values[:0]
	s.sourceRows = s.sourceRows[:0]
}

func (s *rangeAggExpandScratch) append(outTs int64, value float64, row int, includeValue bool) {
	s.outputTs = append(s.outputTs, outTs)
	if includeValue {
		s.values = append(s.values, value)
	}
	s.sourceRows = append(s.sourceRows, row)
}

func (s *rangeAggExpandScratch) finish(includeValues bool) (*array.Timestamp, *array.Float64, *array.Int32) {
	if len(s.outputTs) == 0 {
		return nil, nil, nil
	}

	if s.mem == nil {
		s.mem = memory.NewGoAllocator()
	}

	tsBuilder := array.NewTimestampBuilder(s.mem, &arrow.TimestampType{Unit: arrow.Nanosecond})
	rowBuilder := array.NewInt32Builder(s.mem)

	for i, ts := range s.outputTs {
		tsBuilder.Append(arrow.Timestamp(ts))
		rowBuilder.Append(int32(s.sourceRows[i]))
	}

	ts := tsBuilder.NewTimestampArray()
	rows := rowBuilder.NewInt32Array()
	if !includeValues {
		return ts, nil, rows
	}

	valBuilder := array.NewFloat64Builder(s.mem)
	for _, value := range s.values {
		valBuilder.Append(value)
	}

	return ts, valBuilder.NewFloat64Array(), rows
}

func newRangeAggregationPipeline(inputs []Pipeline, evaluator *expressionEvaluator, opts rangeAggregationOptions) (*rangeAggregationPipeline, error) {
	r := &rangeAggregationPipeline{
		inputs:     inputs,
		evaluator:  evaluator,
		opts:       opts,
		identCache: semconv.NewIdentifierCache(),
	}
	r.init()
	return r, nil
}

func (r *rangeAggregationPipeline) init() {
	windows := []window{}
	cur := r.opts.startTs
	for cur.Compare(r.opts.endTs) <= 0 {
		windows = append(windows, window{start: cur.Add(-r.opts.rangeInterval), end: cur})

		if r.opts.step == 0 {
			break
		}

		// advance to the next window using step
		cur = cur.Add(r.opts.step)
	}

	f := newMatcherFactoryFromOpts(r.opts)
	r.matcher = f
	r.columnarMatcher = r.detectColumnarMatcher()
	r.windowStarts = make([]int64, len(windows))
	r.windowEnds = make([]int64, len(windows))
	for i, w := range windows {
		r.windowStarts[i] = w.start.UnixNano()
		r.windowEnds[i] = w.end.UnixNano()
	}

	op, ok := rangeAggregationOperations[r.opts.operation]
	if !ok {
		panic(fmt.Sprintf("unknown range aggregation operation: %v", r.opts.operation))
	}

	r.aggregator = newAggregator(len(windows), op)
	r.aggregator.SetMaxSeries(r.opts.maxQuerySeries)
}

// Open opens all input pipelines.
func (r *rangeAggregationPipeline) Open(ctx context.Context) error {
	return openInputsConcurrently(ctx, r.inputs)
}

// Read reads the next value into its state.
// It returns an error if reading fails or when the pipeline is exhausted. In this case, the function returns EOF.
// The implementation must retain the returned error in its state and return it with subsequent Value() calls.
func (r *rangeAggregationPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if r.inputsExhausted {
		return nil, EOF
	}

	rec, err := r.read(ctx)

	assertions.CheckColumnDuplicates(rec)
	assertions.CheckLabelValuesDuplicates(rec)

	return rec, err
}

// TODOs:
// - Add toggle to return partial results on Read() call instead of returning only after exhausting all inputs.
func (r *rangeAggregationPipeline) read(ctx context.Context) (arrow.RecordBatch, error) {
	var (
		tsColumnExpr = &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameBuiltinTimestamp,
				Type:   types.ColumnTypeBuiltin,
			},
		} // timestamp column expression

		valColumnExpr = &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameGeneratedValue,
				Type:   types.ColumnTypeGenerated,
			},
		} // value column expression

		startedAt     = time.Now()
		inputReadTime time.Duration
	)

	r.aggregator.Reset() // reset before reading new inputs
	inputsExhausted := false
	for !inputsExhausted {
		inputsExhausted = true

		for _, input := range r.inputs {
			inputStart := time.Now()
			record, err := input.Read(ctx)
			inputReadTime += time.Since(inputStart)

			if err != nil {
				if errors.Is(err, EOF) {
					continue
				}
				return nil, err
			}

			inputsExhausted = false
			if record.NumRows() == 0 {
				// Nothing to process
				continue
			}

			assertions.CheckLabelValuesDuplicates(record)

			arrays, groupingFields, err := collectGroupingColumns(record, r.opts.grouping, r.evaluator, r.identCache)
			if err != nil {
				return nil, err
			}

			r.aggregator.AddLabels(groupingFields)

			// extract timestamp column to check if the entry is in range
			tsVec, err := r.evaluator.eval(tsColumnExpr, record)
			if err != nil {
				return nil, err
			}
			tsCol := tsVec.(*array.Timestamp)

			// no need to extract value column for COUNT aggregation
			var valArr *array.Float64
			if r.opts.operation != types.RangeAggregationTypeCount {
				valVec, err := r.evaluator.eval(valColumnExpr, record)
				if err != nil {
					return nil, err
				}
				valArr = valVec.(*array.Float64)
			}

			if err := r.addRecordColumnar(tsCol, valArr, arrays, groupingFields); err != nil {
				return nil, err
			}
		}
	}

	r.inputsExhausted = true

	rec, err := r.aggregator.BuildRecord()

	if region := xcap.RegionFromContext(ctx); region != nil {
		computeTime := time.Since(startedAt) - inputReadTime
		region.Record(xcap.StatPipelineExecDuration.Observe(computeTime.Seconds()))
	}

	return rec, err
}

func (r *rangeAggregationPipeline) detectColumnarMatcher() columnarMatcherKind {
	switch {
	case r.opts.step == 0:
		return columnarMatcherInstant
	case r.opts.step == r.opts.rangeInterval:
		return columnarMatcherAligned
	case r.opts.step > r.opts.rangeInterval:
		return columnarMatcherGapped
	default:
		return columnarMatcherOverlapping
	}
}

// windowEndForTimestamp maps an input sample timestamp to the end of its matching
// evaluation window for columnar ingest. Each sample maps to at most one window.
func (r *rangeAggregationPipeline) windowEndForTimestamp(t int64) (time.Time, bool) {
	m := r.matcher
	if !boundsContains(m.boundsStart, m.boundsEnd, t) {
		return time.Time{}, false
	}

	switch r.columnarMatcher {
	case columnarMatcherInstant:
		if len(r.windowEnds) == 0 {
			return time.Time{}, false
		}
		return time.Unix(0, r.windowEnds[0]), true
	case columnarMatcherAligned:
		windowIndex := (t - m.queryStart + m.step - 1) / m.step
		if windowIndex < 0 || windowIndex >= int64(len(r.windowEnds)) {
			return time.Time{}, false
		}
		return time.Unix(0, r.windowEnds[windowIndex]), true
	case columnarMatcherGapped:
		windowIndex := (t - m.queryStart + m.step - 1) / m.step
		if windowIndex >= int64(len(r.windowEnds)) {
			return time.Time{}, false
		}
		if t > r.windowStarts[windowIndex] {
			return time.Unix(0, r.windowEnds[windowIndex]), true
		}
		return time.Time{}, false
	default:
		return time.Time{}, false
	}
}

func boundsContains(start, end, t int64) bool {
	return t > start && t <= end
}

// addRecordColumnar ingests an input batch using a columnar loop for by() grouping.
func (r *rangeAggregationPipeline) addRecordColumnar(
	tsCol *array.Timestamp,
	valCol *array.Float64,
	labelCols []*array.String,
	labelFields []arrow.Field,
) error {
	if r.columnarMatcher == columnarMatcherOverlapping {
		return r.addRecordOverlapping(tsCol, valCol, labelCols, labelFields)
	}

	return r.addRecordSingle(tsCol, valCol, labelCols, labelFields)
}

// addRecordSingle ingests an input batch when each row maps to at most one output window.
func (r *rangeAggregationPipeline) addRecordSingle(
	tsCol *array.Timestamp,
	valCol *array.Float64,
	labelCols []*array.String,
	labelFields []arrow.Field,
) error {
	countOp := r.opts.operation == types.RangeAggregationTypeCount

	for row := range int(tsCol.Len()) {
		outTs, ok := r.windowEndForTimestamp(int64(tsCol.Value(row)))
		if !ok {
			continue
		}

		if !countOp && valCol.IsNull(row) {
			continue
		}

		var value float64
		if !countOp {
			value = valCol.Value(row)
		}

		if err := r.aggregator.AddSample(outTs, value, labelCols, labelFields, row); err != nil {
			return err
		}
	}

	return nil
}

func (r *rangeAggregationPipeline) batchAddScratch(countOp bool, labelCols []*array.String, labelFields []arrow.Field) error {
	outputTs, values, sourceRows := r.expandScratch.finish(!countOp)
	if outputTs == nil {
		return nil
	}
	defer outputTs.Release()
	defer sourceRows.Release()
	if values != nil {
		defer values.Release()
	}

	return r.aggregator.BatchAddSample(outputTs, values, sourceRows, labelCols, labelFields)
}

// addRecordOverlapping ingests an input batch for overlapping range queries with by() grouping.
// Each row may contribute to multiple evaluation windows.
func (r *rangeAggregationPipeline) addRecordOverlapping(
	tsCol *array.Timestamp,
	valCol *array.Float64,
	labelCols []*array.String,
	labelFields []arrow.Field,
) error {
	countOp := r.opts.operation == types.RangeAggregationTypeCount
	r.expandScratch.reset()

	for row := range int(tsCol.Len()) {
		t := int64(tsCol.Value(row))
		low, high, ok := overlappingWindowRangeForTimestamp(r.windowStarts, r.windowEnds, r.matcher.boundsStart, r.matcher.boundsEnd, t)
		if !ok {
			continue
		}

		if !countOp && valCol.IsNull(row) {
			continue
		}

		var value float64
		if !countOp {
			value = valCol.Value(row)
		}

		for i := low; i <= high; i++ {
			r.expandScratch.append(r.windowEnds[i], value, row, !countOp)
		}
	}

	return r.batchAddScratch(countOp, labelCols, labelFields)
}

// overlappingWindowRangeForTimestamp returns the inclusive index range of evaluation
// windows that contain t. ok is false when t is out of bounds or matches no window.
func overlappingWindowRangeForTimestamp(windowStarts, windowEnds []int64, boundsStart, boundsEnd, t int64) (low, high int, ok bool) {
	if !boundsContains(boundsStart, boundsEnd, t) {
		return 0, 0, false
	}

	// Find the last window where t > window.start, i.e. the index before the first
	// window where start >= t.
	firstOOBIndex, _ := slices.BinarySearchFunc(windowStarts, t, cmp.Compare)

	windowIndex := firstOOBIndex - 1
	if windowIndex < 0 {
		return 0, 0, false
	}

	// For every i in [0, windowIndex], t > windows[i].start. Containment is therefore
	// equivalent to t <= windows[i].end. Ends are non-decreasing in i, so matching
	// indices are always a suffix [low, windowIndex].
	low, _ = slices.BinarySearchFunc(windowEnds[:windowIndex+1], t, cmp.Compare)
	if low > windowIndex {
		return 0, 0, false
	}

	return low, windowIndex, true
}

// timestampMatchingWindowsFunc resolves matching range interval windows for a specific timestamp.
// The list can be empty if the timestamp is out of bounds or does not match any of the range windows.
type timestampMatchingWindowsFunc func(time.Time) []window

// Close closes the resources of the pipeline.
// The implementation must close all the of the pipeline's inputs.
func (r *rangeAggregationPipeline) Close() {
	r.aggregator.Reset()
	for _, input := range r.inputs {
		input.Close()
	}
}

func newMatcherFactoryFromOpts(opts rangeAggregationOptions) *matcherFactory {
	bounds := window{
		start: opts.startTs.Add(-opts.rangeInterval),
		end:   opts.endTs,
	}

	return &matcherFactory{
		start:       opts.startTs,
		bounds:      bounds,
		boundsStart: bounds.start.UnixNano(),
		boundsEnd:   bounds.end.UnixNano(),
		queryStart:  opts.startTs.UnixNano(),
		step:        opts.step.Nanoseconds(),
	}
}

type matcherFactory struct {
	start       time.Time // retained for tests
	bounds      window    // retained for tests
	boundsStart int64
	boundsEnd   int64
	queryStart  int64
	step        int64
}

// createExactMatcher is used for instant queries.
// The function returns a matcher that always returns the first aggregation window from the given windows if the timestamp is not out of range.
// It is expected that len(windows) is exactly 1, but it is not enforced.
//
//	steps         |---------x-------|
//	interval      |---------x-------|
func (f *matcherFactory) createExactMatcher(windows []window) timestampMatchingWindowsFunc {
	return func(t time.Time) []window {
		if !boundsContains(f.boundsStart, f.boundsEnd, t.UnixNano()) {
			return nil // out of range
		}
		if len(windows) == 0 {
			return nil
		}
		return windows[0:1]
	}
}

// createAlignedMatcher is used for range queries.
// The function returns a matcher that always returns exactly one aggregation window that matches the timestamp if the timestamp is not out of range.
//
//	steps         |-----|---x-|-----|
//	interval                  |-----|
//	interval            |---x-|
//	interval      |-----|
func (f *matcherFactory) createAlignedMatcher(windows []window) timestampMatchingWindowsFunc {
	return func(t time.Time) []window {
		ts := t.UnixNano()
		if !boundsContains(f.boundsStart, f.boundsEnd, ts) {
			return nil // out of range
		}

		// valid timestamps for window i: t > start + (i-1) * interval && t <= start + i * interval
		windowIndex := (ts - f.queryStart + f.step - 1) / f.step // subtract 1ns because we are calculating 0-based indexes
		return windows[windowIndex : windowIndex+1]
	}
}

// createGappedMatcher is used for range queries.
// The function returns a matcher that either returns exactly one aggregation window that matches the timestamp, or none,
// if the timestamp is out of bounds or within bounds, but is within a "gap" between the end of an interval and the beginning of the next interval.
//
//	steps         |-----|---x-|-----|
//	interval                     |--|
//	interval               |x-|
//	interval         |--|
func (f *matcherFactory) createGappedMatcher(windows []window) timestampMatchingWindowsFunc {
	windowStarts := make([]int64, len(windows))
	for i, w := range windows {
		windowStarts[i] = w.start.UnixNano()
	}

	return func(t time.Time) []window {
		ts := t.UnixNano()
		if !boundsContains(f.boundsStart, f.boundsEnd, ts) {
			return nil // out of range
		}

		// For gapped windows, window i covers: (start + i*step - interval, start + i*step]
		windowIndex := (ts - f.queryStart + f.step - 1) / f.step // subtract 1ns because we are calculating 0-based indexes

		if windowIndex >= int64(len(windows)) {
			return nil // out of range when bounds do not fit exact number of steps
		}

		// Verify the timestamp is within the window (not in a gap)
		if ts > windowStarts[windowIndex] {
			return windows[windowIndex : windowIndex+1]
		}

		return nil // timestamp is in a gap
	}
}

// createOverlappingMatcher is used for range queries.
// The function returns a matcher that returns one or more aggregation windows that match the timestamp, if the timestamp is not out of range.
//
//	steps         |-----|---x-|-----|
//	interval               |x-------|
//	interval         |------x-|
//	interval   |--------|
func (f *matcherFactory) createOverlappingMatcher(windows []window) timestampMatchingWindowsFunc {
	windowStarts := make([]int64, len(windows))
	windowEnds := make([]int64, len(windows))
	for i, w := range windows {
		windowStarts[i] = w.start.UnixNano()
		windowEnds[i] = w.end.UnixNano()
	}

	return func(t time.Time) []window {
		low, high, ok := overlappingWindowRangeForTimestamp(windowStarts, windowEnds, f.boundsStart, f.boundsEnd, t.UnixNano())
		if !ok {
			return nil
		}
		return windows[low : high+1]
	}
}
