package executor

import (
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

// window is a time interval where start is exclusive and end is inclusive
// Refer to [logql.batchRangeVectorIterator].
type window struct {
	start, end time.Time
}

// Contains returns if the timestamp t is within the bounds of the window.
// The window start is exclusive, the window end is inclusive.
func (w window) Contains(t time.Time) bool {
	return t.After(w.start) && !t.After(w.end)
}

// cmpWindowStartTime compares a window's lower bound to t for [slices.BinarySearchFunc].
func cmpWindowStartTime(w window, t time.Time) int {
	return w.start.Compare(t)
}

// cmpWindowEndTime compares a window's upper bound to t for [slices.BinarySearchFunc].
func cmpWindowEndTime(w window, t time.Time) int {
	return w.end.Compare(t)
}

// timestampMatchingWindowsFunc resolves matching range interval windows for a specific timestamp.
// The list can be empty if the timestamp is out of bounds or does not match any of the range windows.
type timestampMatchingWindowsFunc func(time.Time) []window

type columnarMatcherKind int

const (
	columnarMatcherNone columnarMatcherKind = iota
	columnarMatcherInstant
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

	aggregator          *aggregator
	windows             []window
	matcher             *matcherFactory
	columnarMatcher     columnarMatcherKind
	expandScratch       rangeAggExpandScratch
	windowsForTimestamp timestampMatchingWindowsFunc // function to find matching time windows for a given timestamp
	evaluator           *expressionEvaluator         // used to evaluate column expressions
	opts                rangeAggregationOptions
	identCache          *semconv.IdentifierCache
}

type rangeAggExpandScratch struct {
	mem        memory.Allocator
	outputTs   []time.Time
	values     []float64
	sourceRows []int
}

func (s *rangeAggExpandScratch) reset() {
	s.outputTs = s.outputTs[:0]
	s.values = s.values[:0]
	s.sourceRows = s.sourceRows[:0]
}

func (s *rangeAggExpandScratch) append(outTs time.Time, value float64, row int) {
	s.outputTs = append(s.outputTs, outTs)
	s.values = append(s.values, value)
	s.sourceRows = append(s.sourceRows, row)
}

func (s *rangeAggExpandScratch) finish() (*array.Timestamp, *array.Float64, *array.Int32) {
	if s.mem == nil {
		s.mem = memory.NewGoAllocator()
	}

	tsBuilder := array.NewTimestampBuilder(s.mem, &arrow.TimestampType{Unit: arrow.Nanosecond})
	valBuilder := array.NewFloat64Builder(s.mem)
	rowBuilder := array.NewInt32Builder(s.mem)

	for i := range s.outputTs {
		tsValue, _ := arrow.TimestampFromTime(s.outputTs[i], arrow.Nanosecond)
		tsBuilder.Append(tsValue)
		valBuilder.Append(s.values[i])
		rowBuilder.Append(int32(s.sourceRows[i]))
	}

	return tsBuilder.NewArray().(*array.Timestamp), valBuilder.NewArray().(*array.Float64), rowBuilder.NewArray().(*array.Int32)
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
	r.windows = windows
	r.columnarMatcher = r.detectColumnarMatcher()
	r.windowsForTimestamp = f.createMatcher(windows)

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
// - Add columnar ingest for without() grouping.
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

	useColumnarAddRecord := r.columnarMatcher != columnarMatcherNone
	var (
		labelValuesCache *labelValuesCache
		fieldsCache      *fieldsCache
	)
	if !useColumnarAddRecord {
		labelValuesCache = newLabelValuesCache()
		fieldsCache = newFieldsCache()
	}

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

			if useColumnarAddRecord {
				if err := r.addRecordColumnar(tsCol, valArr, arrays, groupingFields); err != nil {
					return nil, err
				}
				continue
			}

			for row := range int(record.NumRows()) {
				windows := r.windowsForTimestamp(tsCol.Value(row).ToTime(arrow.Nanosecond))
				if len(windows) == 0 {
					continue // out of range, skip this row
				}

				var value float64
				if r.opts.operation != types.RangeAggregationTypeCount {
					if valArr.IsNull(row) {
						continue
					}

					value = valArr.Value(row)
				}

				labelValues := labelValuesCache.getLabelValues(arrays, row)
				labels := fieldsCache.getFields(arrays, groupingFields, row)

				for _, w := range windows {
					if err := r.aggregator.Add(w.end, value, labels, labelValues); err != nil {
						return nil, err
					}
				}
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
	if r.opts.grouping.Without {
		return columnarMatcherNone
	}

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
func (r *rangeAggregationPipeline) windowEndForTimestamp(ts time.Time) (time.Time, bool) {
	if !r.matcher.bounds.Contains(ts) {
		return time.Time{}, false
	}

	switch r.columnarMatcher {
	case columnarMatcherInstant:
		if len(r.windows) == 0 {
			return time.Time{}, false
		}
		return r.windows[0].end, true
	case columnarMatcherAligned:
		startNs := r.matcher.start.UnixNano()
		stepNs := r.matcher.step.Nanoseconds()
		windowIndex := (ts.UnixNano() - startNs + stepNs - 1) / stepNs
		if windowIndex < 0 || windowIndex >= int64(len(r.windows)) {
			return time.Time{}, false
		}
		return r.windows[windowIndex].end, true
	case columnarMatcherGapped:
		startNs := r.matcher.start.UnixNano()
		stepNs := r.matcher.step.Nanoseconds()
		tNs := ts.UnixNano()
		windowIndex := (tNs - startNs + stepNs - 1) / stepNs
		if windowIndex >= int64(len(r.windows)) {
			return time.Time{}, false
		}
		if tNs > r.windows[windowIndex].start.UnixNano() {
			return r.windows[windowIndex].end, true
		}
		return time.Time{}, false
	default:
		return time.Time{}, false
	}
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

	countOp := r.opts.operation == types.RangeAggregationTypeCount

	for row := range int(tsCol.Len()) {
		ts := tsCol.Value(row).ToTime(arrow.Nanosecond)
		outTs, ok := r.windowEndForTimestamp(ts)
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
		ts := tsCol.Value(row).ToTime(arrow.Nanosecond)
		matching := overlappingWindowsForTimestamp(r.windows, r.matcher.bounds, ts)
		if len(matching) == 0 {
			continue
		}

		if !countOp && valCol.IsNull(row) {
			continue
		}

		var value float64
		if !countOp {
			value = valCol.Value(row)
		}

		for _, w := range matching {
			r.expandScratch.append(w.end, value, row)
		}
	}

	outputTs, values, sourceRows := r.expandScratch.finish()
	defer outputTs.Release()
	defer values.Release()
	defer sourceRows.Release()

	return r.aggregator.BatchAddSample(outputTs, values, sourceRows, labelCols, labelFields)
}

// overlappingWindowsForTimestamp returns all evaluation windows that contain t.
func overlappingWindowsForTimestamp(windows []window, bounds window, t time.Time) []window {
	if !bounds.Contains(t) {
		return nil
	}

	// Find the last window that could contain the timestamp.
	// We need the last window where t > window.start, i.e. the index before
	// the first window where t <= window.start. Use BinarySearchFunc with a
	// package-level cmp so we do not allocate a closure per call (unlike sort.Search).
	firstOOBIndex, _ := slices.BinarySearchFunc(windows, t, cmpWindowStartTime)

	windowIndex := firstOOBIndex - 1
	if windowIndex < 0 {
		return nil
	}

	// For every i in [0, windowIndex], t > windows[i].start (by definition of windowIndex).
	// Containment is therefore equivalent to t <= windows[i].end. Ends are non-decreasing
	// in i, so matching indices are always a suffix [low, windowIndex] of that prefix.
	prefix := windows[:windowIndex+1]
	low, _ := slices.BinarySearchFunc(prefix, t, cmpWindowEndTime)
	if low > windowIndex {
		return nil
	}
	return windows[low : windowIndex+1]
}

// Close closes the resources of the pipeline.
// The implementation must close all the of the pipeline's inputs.
func (r *rangeAggregationPipeline) Close() {
	r.aggregator.Reset()
	for _, input := range r.inputs {
		input.Close()
	}
}

func newMatcherFactoryFromOpts(opts rangeAggregationOptions) *matcherFactory {
	return &matcherFactory{
		start:    opts.startTs,
		step:     opts.step,
		interval: opts.rangeInterval,
		bounds: window{
			start: opts.startTs.Add(-opts.rangeInterval),
			end:   opts.endTs,
		},
	}
}

type matcherFactory struct {
	start    time.Time
	step     time.Duration
	interval time.Duration
	bounds   window
}

func (f *matcherFactory) createMatcher(windows []window) timestampMatchingWindowsFunc {
	switch {
	case f.step == 0:
		// For instant queries, step == 0, meaning that all samples fall into the one and same step.
		// A sample timestamp will always match the only time window available, unless the timestamp it out of range.
		return f.createExactMatcher(windows)
	case f.step == f.interval:
		// If the step is equal to the range interval (e.g. when used $__auto in Grafana), then a sample timestamp matches exactly one time window.
		return f.createAlignedMatcher(windows)
	case f.step > f.interval:
		// If the step is greater than the range interval, then a sample timestamp matches either one time window or no time window (and will be discarded).
		return f.createGappedMatcher(windows)
	case f.step < f.interval:
		// If the step is smaller than the range interval, then a sample timestamp matches either one or multiple time windows.
		return f.createOverlappingMatcher(windows)
	default:
		panic("invalid step and range interval")
	}
}

// createExactMatcher is used for instant queries.
// The function returns a matcher that always returns the first aggregation window from the given windows if the timestamp is not out of range.
// It is expected that len(windows) is exactly 1, but it is not enforced.
//
//	steps         |---------x-------|
//	interval      |---------x-------|
func (f *matcherFactory) createExactMatcher(windows []window) timestampMatchingWindowsFunc {
	return func(t time.Time) []window {
		if !f.bounds.Contains(t) {
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
	startNs := f.start.UnixNano()
	stepNs := f.step.Nanoseconds()

	return func(t time.Time) []window {
		if !f.bounds.Contains(t) {
			return nil // out of range
		}

		tNs := t.UnixNano()
		// valid timestamps for window i: t > startNs + (i-1) * intervalNs && t <= startNs + i * intervalNs
		windowIndex := (tNs - startNs + stepNs - 1) / stepNs // subtract 1ns because we are calculating 0-based indexes
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
	startNs := f.start.UnixNano()
	stepNs := f.step.Nanoseconds()

	return func(t time.Time) []window {
		if !f.bounds.Contains(t) {
			return nil // out of range
		}

		tNs := t.UnixNano()
		// For gapped windows, window i covers: (start + i*step - interval, start + i*step]
		windowIndex := (tNs - startNs + stepNs - 1) / stepNs // subtract 1ns because we are calculating 0-based indexes

		if windowIndex >= int64(len(windows)) {
			return nil // out of range when bounds do not fit exact number of steps
		}

		// Verify the timestamp is within the window (not in a gap)
		if tNs > windows[windowIndex].start.UnixNano() {
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
	return func(t time.Time) []window {
		return overlappingWindowsForTimestamp(windows, f.bounds, t)
	}
}
