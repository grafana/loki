package logql

import (
	"github.com/influxdata/tdigest"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/iter"
)

type TDigestVector = []tdigestSample

func newTDigestIterator(
	it iter.PeekingSampleIterator,
	selRange, step, start, end, offset int64) (RangeVectorIterator[TDigestVector]) {
		inner := batchRangeVectorIterator{
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
	return &tdigestBatchRangeVectorIterator{
		batchRangeVectorIterator: inner,
	}
}

//batch

type tdigestSample struct {
	T int64
	F *tdigest.TDigest

	Metric labels.Labels
}

type tdigestBatchRangeVectorIterator struct {
	batchRangeVectorIterator
	at []tdigestSample
}

func (r *tdigestBatchRangeVectorIterator) At() (int64, []tdigestSample) {
	if r.at == nil {
		r.at = make([]tdigestSample, 0, len(r.window))
	}
	r.at = r.at[:0]
	// convert ts from nano to milli seconds as the iterator work with nanoseconds
	ts := r.current/1e+6 + r.offset/1e+6
	for _, series := range r.window {
		r.at = append(r.at, tdigestSample{
			F:      r.agg(series.Floats),
			T:      ts,
			Metric: series.Metric,
		})
	}
	return ts, r.at
}

func (r *tdigestBatchRangeVectorIterator) agg(samples []promql.FPoint) (*tdigest.TDigest) {
	t := tdigest.New()
	for _, v := range samples {
		t.Add(v.F, 1)
	}
	return t
}
