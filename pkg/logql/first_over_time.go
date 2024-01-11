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

// Step 7
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
