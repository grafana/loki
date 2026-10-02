package logql

import (
	"context"
	"fmt"
	"slices"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

// streamFirstRangeVectorIterator is a RangeVectorIterator that accepts samples in any order.
//
// It is called stream-first because its input comes from a sample request in stream-first order.
// The stream-first merge iterators below it deduplicate the samples correctly for every source
// that returns them strictly in stream-first order. This iterator itself needs no order.
//
// It drains its input on the first call to Next, adding each sample to an accumulator of its
// series in every step whose window holds it. It then replays the step values one step at a time.
// Memory is proportional to series × steps, not to the number of samples.
type streamFirstRangeVectorIterator struct {
	it iter.PeekingSampleIterator

	newAccumulator                newStepAccumulatorFunc
	selRange, step, start, offset int64
	metadata                      *metadata.Context

	// lastStepIdx is the index of the last query step, so the query has lastStepIdx+1 steps.
	lastStepIdx int

	// maxSeries is the output series limit. Zero or less means no limit.
	maxSeries int

	// partialResultsAllowed is true for Logs Drilldown requests, which get the first maxSeries
	// output series and a warning instead of an error.
	partialResultsAllowed bool

	// grouping and sortedGroups are the grouping of the vector aggregation above the range
	// aggregation. The iterator groups its series by them to count the output series.
	grouping     *syntax.Grouping
	sortedGroups []string

	loaded bool
	series map[string]*accumulatedSeries
	// outputs holds the grouping key of every output series the iterator has a series for.
	outputs        map[uint64]struct{}
	buf            []byte
	currStepIdx    int
	currStepResult []promql.Sample
	err            error
}

// newStreamFirstRangeVectorIterator returns an iterator for expr over it, whose samples may come
// in any order.
//
// selRange, step, start, end and offset are in nanoseconds. start and end are the query bounds
// before the offset shift. A step of zero means an instant query.
//
// grouping is the grouping of the vector aggregation above expr. maxSeries limits the output
// series of that aggregation, which can be fewer than the iterator's series.
//
// A series that adds an output series beyond maxSeries fails the query. A Logs Drilldown request
// instead keeps the output series it has and gets a warning.
func newStreamFirstRangeVectorIterator(
	ctx context.Context,
	it iter.PeekingSampleIterator,
	expr *syntax.RangeAggregationExpr,
	selRange, step, start, end, offset int64,
	grouping *syntax.Grouping,
	maxSeries int,
) (RangeVectorIterator, error) {
	newAccumulator, ok := newStepAccumulatorFuncFor(expr)
	if !ok {
		return nil, fmt.Errorf(syntax.UnsupportedErr, expr.Operation)
	}

	// An instant query has step 0. Use 1, so lastStepIdx is 0 and the query has exactly one step.
	if step == 0 {
		step = 1
	}
	start -= offset
	end -= offset

	// The vector aggregation hashes the groups in sorted order, so do the same to get its keys.
	sortedGroups := slices.Clone(grouping.Groups)
	slices.Sort(sortedGroups)

	return &streamFirstRangeVectorIterator{
		it:                    it,
		newAccumulator:        newAccumulator,
		selRange:              selRange,
		step:                  step,
		start:                 start,
		offset:                offset,
		lastStepIdx:           int((end - start) / step),
		grouping:              grouping,
		sortedGroups:          sortedGroups,
		maxSeries:             maxSeries,
		partialResultsAllowed: httpreq.IsLogsDrilldownRequest(ctx),
		metadata:              metadata.FromContext(ctx),
		series:                map[string]*accumulatedSeries{},
		outputs:               map[uint64]struct{}{},
		currStepIdx:           -1,
	}, nil
}

func (r *streamFirstRangeVectorIterator) Next() bool {
	if !r.loaded {
		r.loaded = true
		r.load()
	}

	// load stops early on any error. The step values are then incomplete, so Next replays no step.
	if r.err != nil {
		return false
	}

	r.currStepIdx++
	return r.currStepIdx <= r.lastStepIdx
}

func (r *streamFirstRangeVectorIterator) At() (int64, StepResult) {
	if r.currStepResult == nil {
		r.currStepResult = make([]promql.Sample, 0, len(r.series))
	}
	r.currStepResult = r.currStepResult[:0]

	// Convert the step from nanoseconds to milliseconds, and undo the offset shift.
	stepTs := r.start + int64(r.currStepIdx)*r.step
	ts := stepTs/1e+6 + r.offset/1e+6

	for _, s := range r.series {
		value, ok := s.steps.value(r.currStepIdx)
		if !ok {
			continue
		}
		r.currStepResult = append(r.currStepResult, promql.Sample{F: value, T: ts, Metric: s.metric})
	}
	return ts, SampleVector(r.currStepResult)
}

func (r *streamFirstRangeVectorIterator) Close() error {
	return r.it.Close()
}

func (r *streamFirstRangeVectorIterator) Error() error {
	return r.err
}

// load drains the input and adds every sample to the steps whose window holds it.
func (r *streamFirstRangeVectorIterator) load() {
	var (
		lastLabels    string
		lastSeries    *accumulatedSeries
		hasLastSeries bool
	)

	for lbs, sample, ok := r.it.Peek(); ok; lbs, sample, ok = r.it.Peek() {
		// Next always returns true after Peek returned a sample. Read errors surface through Err,
		// which load checks after the loop.
		_ = r.it.Next()

		lo, hi, inRange := r.windowRange(sample.Timestamp)
		if !inRange {
			continue
		}

		// Consecutive samples usually belong to the same series, so reuse the last lookup. A nil
		// lastSeries means the series is dropped.
		if !hasLastSeries || lbs != lastLabels {
			series, err := r.accumulatedSeriesFor(lbs)
			if err != nil {
				r.err = err
				return
			}
			lastLabels, lastSeries, hasLastSeries = lbs, series, true
		}
		if lastSeries == nil {
			continue
		}

		lastSeries.steps.add(lo, hi, sample.Value)
	}

	// The input stops on a read error or a cancelled context, and reports it only through Err.
	if err := r.it.Err(); err != nil {
		r.err = err
		return
	}

	for _, s := range r.series {
		s.steps.finish()
	}
}

// windowRange returns the inclusive range [lo, hi] of the steps whose window holds ts. It returns
// false when no step holds ts.
func (r *streamFirstRangeVectorIterator) windowRange(ts int64) (lo, hi int, ok bool) {
	// Step k evaluates at the timestamp start + k*step. Its window holds the samples after that
	// timestamp minus selRange, up to and including that timestamp.
	//
	// So ts is in the window of step k when the step timestamp is at least ts and less than
	// ts + selRange. The first such step is lo, rounded up. The last is hi, rounded down, where the
	// -1 makes the upper bound exclusive. Both are clamped to the steps of the query.
	kLo := max(ceilDiv(ts-r.start, r.step), 0)
	kHi := min(floorDiv(ts+r.selRange-1-r.start, r.step), int64(r.lastStepIdx))
	if kLo > kHi {
		return 0, 0, false
	}
	return int(kLo), int(kHi), true
}

// accumulatedSeriesFor returns the accumulated series for the series labels lbs. It returns nil
// when the series limit drops the series.
func (r *streamFirstRangeVectorIterator) accumulatedSeriesFor(lbs string) (*accumulatedSeries, error) {
	if s, ok := r.series[lbs]; ok {
		return s, nil
	}

	// The labels come from the sample extractor or an ingester, which always render valid labels.
	// A parse failure means a bug or corrupted data, so it fails the query.
	metric, err := promql_parser.NewParser(promql_parser.Options{}).ParseMetric(lbs)
	if err != nil {
		return nil, fmt.Errorf("parsing series labels %q: %w", lbs, err)
	}

	output := r.groupingKey(metric)
	if _, ok := r.outputs[output]; !ok {
		if r.maxSeries > 0 && len(r.outputs) >= r.maxSeries {
			// Prefer the pipeline error to the limit error, because it tells the user what to fix.
			if metric.Has(logqlmodel.ErrorLabel) && metric.Get(logqlmodel.PreserveErrorLabel) != trueString {
				return nil, logqlmodel.NewPipelineErr(metric)
			}
			if !r.partialResultsAllowed {
				return nil, logqlmodel.NewSeriesLimitError(r.maxSeries)
			}
			r.metadata.AddWarning(seriesLimitPartialResultsWarning(r.maxSeries))
			return nil, nil
		}
		r.outputs[output] = struct{}{}
	}

	s := &accumulatedSeries{metric: metric, steps: r.newAccumulator(r.lastStepIdx + 1)}
	r.series[lbs] = s
	return s, nil
}

// groupingKey returns the grouping key of the output series that metric belongs to. It is the
// same key the vector aggregation computes.
func (r *streamFirstRangeVectorIterator) groupingKey(metric labels.Labels) uint64 {
	var key uint64
	if r.grouping.Without {
		key, r.buf = metric.HashWithoutLabels(r.buf, r.sortedGroups...)
	} else {
		key, r.buf = metric.HashForLabels(r.buf, r.sortedGroups...)
	}
	return key
}

// ceilDiv returns ceil(a/b) for b > 0.
//
// It does not use math.Ceil: a float64 cannot represent every nanosecond timestamp exactly, so
// the rounding could pick the wrong step at a window boundary.
func ceilDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && a > 0 {
		q++
	}
	return q
}

// floorDiv returns floor(a/b) for b > 0.
//
// It does not use math.Floor: a float64 cannot represent every nanosecond timestamp exactly, so
// the rounding could pick the wrong step at a window boundary.
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && a < 0 {
		q--
	}
	return q
}

// accumulatedSeries is one series of the range aggregation, with its accumulated step values.
type accumulatedSeries struct {
	metric labels.Labels
	steps  stepAccumulator
}
