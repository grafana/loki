package logql

import (
	"math"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/v3/pkg/iter"
)

// newFirstWithTimestampIterator returns an iterator the returns the first value
// of a windowed aggregation.
func newFirstWithTimestampIterator(
	it iter.PeekingSampleIterator,
	selRange, step, start, end, offset int64,
) RangeVectorIterator {
	// forces at least one step.
	if step == 0 {
		step = 1
	}
	if offset != 0 {
		start = start - offset
		end = end - offset
	}

	inner := &batchRangeVectorIterator{
		iter:     it,
		step:     step,
		end:      end,
		selRange: selRange,
		metrics:  map[string]labels.Labels{},
		window:   map[string]*promql.Series{},
		agg:      nil,
		current:  start - step, // first loop iteration will set it to start
		offset:   offset,
	}
	return &firstWithTimestampBatchRangeVectorIterator{
		batchRangeVectorIterator: inner,
	}
}

type firstWithTimestampBatchRangeVectorIterator struct {
	*batchRangeVectorIterator
	at []promql.Sample
}

// At aggregates the underlying window by picking the first sample with its
// timestamp.
func (r *firstWithTimestampBatchRangeVectorIterator) At() (int64, StepResult) {
	if r.at == nil {
		r.at = make([]promql.Sample, 0, len(r.window))
	}
	r.at = r.at[:0]
	// convert ts from nano to milli seconds as the iterator work with nanoseconds
	ts := r.current/1e+6 + r.offset/1e+6
	for _, series := range r.window {
		s := r.agg(series.Floats)
		r.at = append(r.at, promql.Sample{
			F:      s.F,
			T:      sampleTimestampToMillis(s.T),
			Metric: series.Metric,
		})
	}
	return ts, SampleVector(r.at)
}

// agg returns the first sample with its timestamp. The input is assumed to be
// in order.
func (r *firstWithTimestampBatchRangeVectorIterator) agg(samples []promql.FPoint) promql.FPoint {
	if len(samples) == 0 {
		return promql.FPoint{F: math.NaN(), T: 0}
	}
	return samples[0]
}

func newLastWithTimestampIterator(
	it iter.PeekingSampleIterator,
	selRange, step, start, end, offset int64,
) RangeVectorIterator {
	// forces at least one step.
	if step == 0 {
		step = 1
	}
	if offset != 0 {
		start = start - offset
		end = end - offset
	}

	inner := &batchRangeVectorIterator{
		iter:     it,
		step:     step,
		end:      end,
		selRange: selRange,
		metrics:  map[string]labels.Labels{},
		window:   map[string]*promql.Series{},
		agg:      nil,
		current:  start - step, // first loop iteration will set it to start
		offset:   offset,
	}
	return &lastWithTimestampBatchRangeVectorIterator{
		batchRangeVectorIterator: inner,
	}
}

// lastWithTimestampBatchRangeVectorIterator returns an iterator that returns the
// last point in a windowed aggregation.
type lastWithTimestampBatchRangeVectorIterator struct {
	*batchRangeVectorIterator
	at []promql.Sample
}

// At aggregates the underlying window by picking the last sample with its
// timestamp.
func (r *lastWithTimestampBatchRangeVectorIterator) At() (int64, StepResult) {
	if r.at == nil {
		r.at = make([]promql.Sample, 0, len(r.window))
	}
	r.at = r.at[:0]
	// convert ts from nano to milli seconds as the iterator work with nanoseconds
	ts := r.current/1e+6 + r.offset/1e+6
	for _, series := range r.window {
		s := r.agg(series.Floats)
		r.at = append(r.at, promql.Sample{
			F:      s.F,
			T:      sampleTimestampToMillis(s.T),
			Metric: series.Metric,
		})
	}
	return ts, SampleVector(r.at)
}

// agg returns the last sample with its timestamp. The input is assumed to be
// in order.
func (r *lastWithTimestampBatchRangeVectorIterator) agg(samples []promql.FPoint) promql.FPoint {
	if len(samples) == 0 {
		return promql.FPoint{F: math.NaN(), T: 0}
	}
	return samples[len(samples)-1]
}

type mergeOverTimeStepEvaluator struct {
	start, end, ts time.Time
	step           time.Duration
	matrices       []promql.Matrix
	merge          func(promql.Vector, int, int, promql.Series) promql.Vector
	offset         time.Duration
	rangeInterval  time.Duration
}

// Next returns the first or last element within one step of each matrix.
func (e *mergeOverTimeStepEvaluator) Next() (bool, int64, StepResult) {
	vec := promql.Vector{}

	e.ts = e.ts.Add(e.step)
	if e.ts.After(e.end) {
		return false, 0, nil
	}
	ts := e.ts.UnixNano() / int64(time.Millisecond)

	// Merge other results
	for i, m := range e.matrices {
		for j, series := range m {
			if len(series.Floats) == 0 || !e.inRange(series.Floats[0].T, ts) {
				continue
			}

			vec = e.merge(vec, j, len(m), series)
			e.pop(i, j)
		}
	}

	// Align vector timestamps with step
	for i := range vec {
		vec[i].T = ts
	}

	return true, ts, SampleVector(vec)
}

// pop drops the float of the s'th series in the r'th matrix.
func (e *mergeOverTimeStepEvaluator) pop(r, s int) {
	if len(e.matrices[r][s].Floats) <= 1 {
		e.matrices[r][s].Floats = nil
		return
	}
	e.matrices[r][s].Floats = e.matrices[r][s].Floats[1:]
}

// inRange returns true if sampleTimestamp, the timestamp of the sample a shard picked as its
// first/last candidate, falls inside the aggregation window ending at stepTimestamp.
func (e *mergeOverTimeStepEvaluator) inRange(sampleTimestamp, stepTimestamp int64) bool {
	// The time stamp needs to be adjusted because the original datapoint at sampleTimestamp is
	// from a shifted query.
	stepTimestamp -= e.offset.Milliseconds()

	// An instant query has a single output step, so a candidate a shard returns has no other
	// step to be confused with. The shard already restricted it to the right window, so there
	// is nothing left to check here.
	if e.step.Milliseconds() == 0 {
		return true
	}

	// Compare against the range vector's own interval, not the step. The step would silently
	// drop series from grouped, sharded first/last_over_time range queries whenever the interval
	// is wider.
	//
	// The interval is half-open: the lower bound is exclusive, the upper bound is inclusive.
	return (stepTimestamp-e.rangeInterval.Milliseconds()) < sampleTimestamp && sampleTimestamp <= stepTimestamp
}

func (*mergeOverTimeStepEvaluator) Close() error { return nil }

func (*mergeOverTimeStepEvaluator) Error() error { return nil }

func NewMergeFirstOverTimeStepEvaluator(params Params, m []promql.Matrix, offset, rangeInterval time.Duration) StepEvaluator {
	if len(m) == 0 {
		return EmptyEvaluator[SampleVector]{}
	}

	var (
		start = params.Start()
		end   = params.End()
		step  = params.Step()
	)

	return &mergeOverTimeStepEvaluator{
		start:         start,
		end:           end,
		ts:            start.Add(-step), // will be corrected on first Next() call
		step:          step,
		matrices:      m,
		merge:         mergeFirstOverTime,
		offset:        offset,
		rangeInterval: rangeInterval,
	}
}

// mergeFirstOverTime selects the first sample by timestamp of each series.
func mergeFirstOverTime(vec promql.Vector, pos int, nSeries int, series promql.Series) promql.Vector {
	if len(vec) < nSeries {
		return append(vec, promql.Sample{
			Metric: series.Metric,
			T:      series.Floats[0].T,
			F:      series.Floats[0].F,
		})
	} else if vec[pos].T > series.Floats[0].T {
		vec[pos].F = series.Floats[0].F
		vec[pos].T = series.Floats[0].T
	}

	return vec
}

func NewMergeLastOverTimeStepEvaluator(params Params, m []promql.Matrix, offset, rangeInterval time.Duration) StepEvaluator {
	if len(m) == 0 {
		return EmptyEvaluator[SampleVector]{}
	}

	var (
		start = params.Start()
		end   = params.End()
		step  = params.Step()
	)

	return &mergeOverTimeStepEvaluator{
		start:         start,
		end:           end,
		ts:            start.Add(-step), // will be corrected on first Next() call
		step:          step,
		matrices:      m,
		merge:         mergeLastOverTime,
		offset:        offset,
		rangeInterval: rangeInterval,
	}
}

// mergeLastOverTime selects the last sample by timestamp of each series.
func mergeLastOverTime(vec promql.Vector, pos int, nSeries int, series promql.Series) promql.Vector {
	if len(vec) < nSeries {
		return append(vec, promql.Sample{
			Metric: series.Metric,
			T:      series.Floats[0].T,
			F:      series.Floats[0].F,
		})
	} else if vec[pos].T < series.Floats[0].T {
		vec[pos].F = series.Floats[0].F
		vec[pos].T = series.Floats[0].T
	}

	return vec
}

// sampleTimestampToMillis converts a sample's nanosecond timestamp to milliseconds, rounding up.
//
// Truncating down instead could shift the timestamp into the previous millisecond, making the
// sharded merge (mergeOverTimeStepEvaluator) attribute the sample to the wrong output step.
func sampleTimestampToMillis(sampleTimestamp int64) int64 {
	const millis = int64(time.Millisecond)
	return (sampleTimestamp + millis - 1) / millis
}
