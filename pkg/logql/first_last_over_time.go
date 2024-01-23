package logql

import (
	"math"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/iter"
)

func newFirstWithTimestampIterator(
	it iter.PeekingSampleIterator,
	selRange, step, start, end, offset int64) RangeVectorIterator {
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
			T:      s.T / int64(time.Millisecond),
			Metric: series.Metric,
		})
	}
	return ts, SampleVector(r.at)
}

func (r *firstWithTimestampBatchRangeVectorIterator) agg(samples []promql.FPoint) promql.FPoint {
	if len(samples) == 0 {
		return promql.FPoint{F: math.NaN(), T: 0}
	}
	return samples[0]
}

func newLastWithTimestampIterator(
	it iter.PeekingSampleIterator,
	selRange, step, start, end, offset int64) RangeVectorIterator {
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

type lastWithTimestampBatchRangeVectorIterator struct {
	*batchRangeVectorIterator
	at []promql.Sample
}

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
			T:      s.T / int64(time.Millisecond),
			Metric: series.Metric,
		})
	}
	return ts, SampleVector(r.at)
}

func (r *lastWithTimestampBatchRangeVectorIterator) agg(samples []promql.FPoint) promql.FPoint {
	if len(samples) == 0 {
		return promql.FPoint{F: math.NaN(), T: 0}
	}
	return samples[len(samples)-1]
}

// Step 8
type firstOverTimeStepEvaluator struct {
	start, end, ts time.Time
	step           time.Duration
	matrices       []promql.Matrix
}

func NewMergeFirstOverTimeStepEvaluator(params Params, m []promql.Matrix) StepEvaluator {
	if len(m) == 0 {
		return EmptyEvaluator{}
	}

	var (
		start = params.Start()
		end   = params.End()
		step  = params.Step()
	)

	return &firstOverTimeStepEvaluator{
		start:    start,
		end:      end,
		ts:       start.Add(-step), // will be corrected on first Next() call
		step:     step,
		matrices: m,
	}
}

func (e *firstOverTimeStepEvaluator) Next() (bool, int64, StepResult) {

	var (
		vec promql.Vector
	)

	e.ts = e.ts.Add(e.step)
	if e.ts.After(e.end) {
		return false, 0, nil
	}
	ts := e.ts.UnixNano() / int64(time.Millisecond)

	// Merge other results
	for i, m := range e.matrices {
		// TODO: verify length and same labels/metric
		for j, series := range m {

			if len(series.Floats) == 0 || !e.inRange(series.Floats[0].T, ts) {
				continue
			}

			// Merge
			if len(vec) < len(m) {
				vec = append(vec, promql.Sample{
					Metric: series.Metric,
					T:      series.Floats[0].T,
					F:      series.Floats[0].F,
				})
			} else if vec[j].T > series.Floats[0].T {
				vec[j].F = series.Floats[0].F
				vec[j].T = series.Floats[0].T
			}

			// We've omitted the first matrix. That's why +1.
			e.pop(i, j)
		}
	}

	// Align vector timestamps with step
	for i := range vec {
		vec[i].T = ts
	}

	if len(vec) == 0 {
		return e.hasNext(), ts, SampleVector(vec)
	}

	return true, ts, SampleVector(vec)
}

func (e *firstOverTimeStepEvaluator) pop(r, s int) {
	if len(e.matrices[r][s].Floats) <= 1 {
		e.matrices[r][s].Floats = nil
		return
	}
	e.matrices[r][s].Floats = e.matrices[r][s].Floats[1:]
}

func (e *firstOverTimeStepEvaluator) inRange(t, ts int64) bool {
	previous := ts - e.step.Milliseconds()
	return previous <= t && t < ts
}

func (e *firstOverTimeStepEvaluator) hasNext() bool {
	for _, m := range e.matrices {
		for _, s := range m {
			if len(s.Floats) != 0 {
				return true
			}
		}
	}

	return false
}

func (*firstOverTimeStepEvaluator) Close() error { return nil }

func (*firstOverTimeStepEvaluator) Error() error { return nil }

type lastOverTimeStepEvaluator struct {
	start, end, ts time.Time
	step           time.Duration
	matrices       []promql.Matrix
}

func NewMergeLastOverTimeStepEvaluator(params Params, m []promql.Matrix) StepEvaluator {
	if len(m) == 0 {
		return EmptyEvaluator{}
	}

	var (
		start = params.Start()
		end   = params.End()
		step  = params.Step()
	)

	return &lastOverTimeStepEvaluator{
		start:    start,
		end:      end,
		ts:       start.Add(-step), // will be corrected on first Next() call
		step:     step,
		matrices: m,
	}
}

func (e *lastOverTimeStepEvaluator) Next() (bool, int64, StepResult) {

	var (
		vec promql.Vector
	)

	e.ts = e.ts.Add(e.step)
	if e.ts.After(e.end) {
		return false, 0, nil
	}
	ts := e.ts.UnixNano() / int64(time.Millisecond)

	// Merge other results
	for i, m := range e.matrices {
		// TODO: verify length and same labels/metric
		for j, series := range m {

			if len(series.Floats) == 0 || !e.inRange(series.Floats[0].T, ts) {
				continue
			}

			// Merge
			if len(vec) < len(m) {
				vec = append(vec, promql.Sample{
					Metric: series.Metric,
					T:      series.Floats[0].T,
					F:      series.Floats[0].F,
				})
			} else if vec[j].T < series.Floats[0].T {
				vec[j].F = series.Floats[0].F
				vec[j].T = series.Floats[0].T
			}

			// We've omitted the first matrix. That's why +1.
			e.pop(i, j)
		}
	}

	// Align vector timestamps with step
	for i := range vec {
		vec[i].T = ts
	}

	return true, ts, SampleVector(vec)
}

func (e *lastOverTimeStepEvaluator) pop(r, s int) {
	if len(e.matrices[r][s].Floats) <= 1 {
		e.matrices[r][s].Floats = nil
		return
	}
	e.matrices[r][s].Floats = e.matrices[r][s].Floats[1:]
}

func (e *lastOverTimeStepEvaluator) inRange(t, ts int64) bool {
	previous := ts - e.step.Milliseconds()
	return previous <= t && t < ts
}

func (e *lastOverTimeStepEvaluator) hasNext() bool {
	for _, m := range e.matrices {
		for _, s := range m {
			if len(s.Floats) != 0 {
				return true
			}
		}
	}

	return false
}

func (*lastOverTimeStepEvaluator) Close() error { return nil }

func (*lastOverTimeStepEvaluator) Error() error { return nil }
