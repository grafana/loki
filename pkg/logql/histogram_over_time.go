package logql

import (
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// histogramRangeVectorIterator builds, for each step, the histogram of the values of each series in
// the window of the step. Each sample of the vector it returns holds a histogram, in H, and no float.
//
// It is the iterator of histogram_over_time. BatchRangeVectorAggregator returns a float and is on the
// path of every other range aggregation, so the histograms are built here, as the sketches are.
type histogramRangeVectorIterator struct {
	*batchRangeVectorIterator

	// spec holds the buckets of the histograms. A nil spec selects the exponential buckets.
	spec *syntax.BucketSpec
}

// newHistogramIterator returns the range vector iterator of histogram_over_time. The input iterator
// must return samples in global timestamp order.
func newHistogramIterator(
	it iter.PeekingSampleIterator,
	spec *syntax.BucketSpec,
	selRange, step, start, end, offset int64,
	limits iteratorLimits,
) RangeVectorIterator {
	return &histogramRangeVectorIterator{
		batchRangeVectorIterator: newBatchRangeVectorIterator(it, selRange, step, start, end, offset, limits),
		spec:                     spec,
	}
}

// Next moves to the next step and builds its histograms. A histogram that goes over the bucket limit
// fails the iterator: Next returns false, and Error returns the error, so a step that failed is never
// returned as one without series.
func (r *histogramRangeVectorIterator) Next() bool {
	if !r.batchRangeVectorIterator.Next() {
		return false
	}

	// The vector is reused by the steps, as the one of any range aggregation is. The histograms in it are
	// new for each step, since they are handed on and a window does not own the ones of the step before.
	r.at = r.at[:0]
	// convert ts from nano to milli seconds as the iterator work with nanoseconds
	ts := (r.current + r.offset) / 1e+6
	for _, series := range r.window {
		b := newHistogramBuilder(r.spec)
		for _, p := range series.Floats {
			b.observe(p.F)
		}
		h, err := b.build()
		if err != nil {
			r.err = err
			return false
		}
		r.at = append(r.at, promql.Sample{T: ts, Metric: series.Metric, H: h})
	}
	return true
}

// At returns the histograms that Next built for the current step.
func (r *histogramRangeVectorIterator) At() (int64, StepResult) {
	return (r.current + r.offset) / 1e+6, SampleVector(r.at)
}
