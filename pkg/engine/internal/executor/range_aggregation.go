package executor

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/xcap"
)

type rangeAggregationOptions struct {
	grouping physical.Grouping

	// start and end timestamps are equal for instant queries.
	startTs       time.Time     // start timestamp of the query
	endTs         time.Time     // end timestamp of the query
	rangeInterval time.Duration // range interval
	step          time.Duration // step used for range queries
	operation     types.RangeAggregationType
}

var (
	// rangeAggregationOperations holds the mapping of range aggregation types to operations for an aggregator.
	rangeAggregationOperations = map[types.RangeAggregationType]aggregationOperation{
		types.RangeAggregationTypeSum:   aggregationOperationSum,
		types.RangeAggregationTypeCount: aggregationOperationCount,
		types.RangeAggregationTypeMax:   aggregationOperationMax,
		types.RangeAggregationTypeMin:   aggregationOperationMin,
	}
)

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

// timestampMatchingWindowsFunc resolves matching range interval windows for a specific timestamp.
// The list can be empty if the timestamp is out of bounds or does not match any of the range windows.
type timestampMatchingWindowsFunc func(time.Time) []window

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
	windowsForTimestamp timestampMatchingWindowsFunc // function to find matching time windows for a given timestamp
	evaluator           expressionEvaluator          // used to evaluate column expressions
	opts                rangeAggregationOptions
	region              *xcap.Region
}

func newRangeAggregationPipeline(inputs []Pipeline, evaluator expressionEvaluator, opts rangeAggregationOptions, region *xcap.Region) (*rangeAggregationPipeline, error) {
	r := &rangeAggregationPipeline{
		inputs:    inputs,
		evaluator: evaluator,
		opts:      opts,
		region:    region,
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
	r.windowsForTimestamp = f.createMatcher(windows)

	op, ok := rangeAggregationOperations[r.opts.operation]
	if !ok {
		panic(fmt.Sprintf("unknown range aggregation operation: %v", r.opts.operation))
	}

	r.aggregator = newAggregator(len(windows), op)
}

// Read reads the next value into its state.
// It returns an error if reading fails or when the pipeline is exhausted. In this case, the function returns EOF.
// The implementation must retain the returned error in its state and return it with subsequent Value() calls.
func (r *rangeAggregationPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if r.inputsExhausted {
		return nil, EOF
	}

	return r.read(ctx)
}

// TODOs:
// - Use columnar access pattern. Current approach is row-based which does not benefit from the storage format.
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
	)

	r.aggregator.Reset() // reset before reading new inputs
	inputsExhausted := false
	for !inputsExhausted {
		inputsExhausted = true

		for _, input := range r.inputs {
			record, err := input.Read(ctx)
			if err != nil {
				if errors.Is(err, EOF) {
					continue
				}
				return nil, err
			}

			inputsExhausted = false

			// extract all the columns that are used for grouping
			var arrays []*array.String
			var fields []arrow.Field
			if r.opts.grouping.Without {
				// Grouping without a lable set. Exclude lables from that set.
				schema := record.Schema()
				for i, field := range schema.Fields() {
					ident, err := semconv.ParseFQN(field.Name)
					if err != nil {
						return nil, err
					}

					if ident.ColumnType() == types.ColumnTypeLabel ||
						ident.ColumnType() == types.ColumnTypeMetadata ||
						ident.ColumnType() == types.ColumnTypeParsed {
						found := false
						for _, g := range r.opts.grouping.Columns {
							colExpr, ok := g.(*physical.ColumnExpr)
							if !ok {
								return nil, fmt.Errorf("unknown column expression %v", g)
							}

							// Match ambiguous columns only by name
							if colExpr.Ref.Type == types.ColumnTypeAmbiguous && colExpr.Ref.Column == ident.ShortName() {
								found = true
								break
							}

							// Match all other columns by name and type
							if colExpr.Ref.Column == ident.ShortName() && colExpr.Ref.Type == ident.ColumnType() {
								found = true
								break
							}
						}
						if !found {
							arrays = append(arrays, record.Column(i).(*array.String))
							fields = append(fields, field)
						}
					}
				}
			} else {
				// Gouping by a label set. Take only labels from that set.
				for _, columnExpr := range r.opts.grouping.Columns {
					vec, err := r.evaluator.eval(columnExpr, record)
					if err != nil {
						return nil, err
					}

					if vec.DataType().ID() != types.Arrow.String.ID() {
						return nil, fmt.Errorf("unsupported datatype for grouping %s", vec.DataType())
					}

					arr := vec.(*array.String)
					arrays = append(arrays, arr)

					colExpr, ok := columnExpr.(*physical.ColumnExpr)
					if !ok {
						return nil, fmt.Errorf("invalid column expression type %T", columnExpr)
					}
					ident := semconv.NewIdentifier(colExpr.Ref.Column, colExpr.Ref.Type, types.Loki.String)
					fields = append(fields, semconv.FieldFromIdent(ident, true))
				}
			}

			r.aggregator.AddLabels(fields)

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

			for row := range int(record.NumRows()) {
				var value float64
				if r.opts.operation != types.RangeAggregationTypeCount {
					if valArr.IsNull(row) {
						continue
					}

					value = valArr.Value(row)
				}

				windows := r.windowsForTimestamp(tsCol.Value(row).ToTime(arrow.Nanosecond))
				if len(windows) == 0 {
					continue // out of range, skip this row
				}

				labelValues := make([]string, 0, len(arrays))
				labels := make([]arrow.Field, 0, len(arrays))
				for i, arr := range arrays {
					val := arr.Value(row)
					if val != "" {
						labelValues = append(labelValues, val)
						labels = append(labels, fields[i])
					}
				}

				for _, w := range windows {
					r.aggregator.Add(w.end, value, labels, labelValues)
				}
			}
		}
	}

	r.inputsExhausted = true
	return r.aggregator.BuildRecord()
}

// Close closes the resources of the pipeline.
// The implementation must close all the of the pipeline's inputs.
func (r *rangeAggregationPipeline) Close() {
	if r.region != nil {
		r.region.End()
	}
	for _, input := range r.inputs {
		input.Close()
	}
}

// Region implements RegionProvider.
func (r *rangeAggregationPipeline) Region() *xcap.Region {
	return r.region
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
		return []window{windows[0]}
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
		return []window{windows[windowIndex]}
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
		matchingWindow := windows[windowIndex]

		// Verify the timestamp is within the window (not in a gap)
		if tNs > matchingWindow.start.UnixNano() {
			return []window{matchingWindow}
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
		if !f.bounds.Contains(t) {
			return nil // out of range
		}

		// Find the last window that could contain the timestamp.
		// We need to find the last window where t > window.startTs
		// so search for the first window where t <= window.startTs
		firstOOBIndex := sort.Search(len(windows), func(i int) bool {
			return t.Compare(windows[i].start) <= 0
		})

		windowIndex := firstOOBIndex - 1
		if windowIndex < 0 {
			return nil
		}

		// Iterate backwards from last matching window to find all matches
		var result []window
		for _, window := range slices.Backward(windows[:windowIndex+1]) {
			if t.Compare(window.start) > 0 && t.Compare(window.end) <= 0 {
				result = append(result, window)
			} else if t.Compare(window.end) > 0 {
				// we've gone past all possible matches
				break
			}
		}

		return result
	}
}
