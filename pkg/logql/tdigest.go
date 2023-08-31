package logql

import (
	"math"

	"github.com/influxdata/tdigest"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logqlmodel"
)

type TDigestVector []tdigestSample
type TDigestMatrix []TDigestVector

func (TDigestMatrix) String() string {
	return "TDigestMatrix()"
}

func (TDigestMatrix) Type() promql_parser.ValueType { return "TDigestMatrix" }

type TDigestStepEvaluator struct {
	iter RangeVectorIterator[TDigestVector]

	err error
}

func (r *TDigestStepEvaluator) Next() (bool, int64, TDigestVector) {
	next := r.iter.Next()
	if !next {
		return false, 0, TDigestVector{}
	}
	ts, vec := r.iter.At()
	for _, s := range vec {
		// Errors are not allowed in metrics unless they've been specifically requested.
		if s.Metric.Has(logqlmodel.ErrorLabel) && s.Metric.Get(logqlmodel.PreserveErrorLabel) != "true" {
			r.err = logqlmodel.NewPipelineErr(s.Metric)
			return false, 0, TDigestVector{}
		}
	}
	return true, ts, vec
}

func (r *TDigestStepEvaluator) Close() error { return r.iter.Close() }

func (r *TDigestStepEvaluator) Error() error {
	if r.err != nil {
		return r.err
	}
	return r.iter.Error()
}

func (r *TDigestStepEvaluator) Explain(parent Node) {
	parent.Child("T-Digest")	
}

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

func (r *tdigestBatchRangeVectorIterator) At() (int64, TDigestVector) {
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

func JoinTDigest(stepEvaluator StepEvaluator[TDigestVector], params Params) (promql_parser.Value, error) {
	// TODO(karsten): check if ts should be used
	next, _, vec := stepEvaluator.Next()
	if stepEvaluator.Error() != nil {
		return nil, stepEvaluator.Error()
	}

	stepCount := int(math.Ceil(float64(params.End().Sub(params.Start()).Nanoseconds()) / float64(params.Step().Nanoseconds())))
	if stepCount <= 0 {
		stepCount = 1
	}

	result := make([]TDigestVector, 0)	

	for next {
		result = append(result, vec)

		next, _, vec = stepEvaluator.Next()
		if stepEvaluator.Error() != nil {
			return nil, stepEvaluator.Error()
		}
	}


	return TDigestMatrix(result), stepEvaluator.Error()
}
