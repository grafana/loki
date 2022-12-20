package logql

import (
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/prometheus/model/exemplar"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql/syntax"
)

// RangeStreamingExemplarAgg streaming aggregates exemplar for each sample
type RangeStreamingExemplarAgg interface {
	agg(sample exemplar.Exemplar)
	at() exemplar.Exemplar
}

// RangeExemplarsIterator iterates through a range of samples.
type RangeExemplarsIterator interface {
	Next() bool
	At() (int64, []exemplar.QueryResult)
	Close() error
	Error() error
}

func newRangeExemplarIterator(
	it iter.PeekingExemplarIterator,
	expr *syntax.RangeAggregationExpr,
	selRange, step, start, end, offset int64) (RangeExemplarsIterator, error) {
	// forces at least one step.
	if step == 0 {
		step = 1
	}
	if offset != 0 {
		start = start - offset
		end = end - offset
	}

	return &streamRangeExemplarIterator{
		iter:     it,
		step:     step,
		end:      end,
		selRange: selRange,
		metrics:  map[string]labels.Labels{},
		r:        expr,
		current:  start - step, // first loop iteration will set it to start
		offset:   offset,
	}, nil

}

// streaming range agg
type streamRangeExemplarIterator struct {
	iter                                 iter.PeekingExemplarIterator
	selRange, step, end, current, offset int64
	windowRangeAgg                       map[string]RangeStreamingExemplarAgg
	r                                    *syntax.RangeAggregationExpr
	metrics                              map[string]labels.Labels
	at                                   []promql.Sample
	exemplars                            []exemplar.QueryResult
	agg                                  BatchRangeVectorAggregator
}

func (r *streamRangeExemplarIterator) Next() bool {
	// slides the range window to the next position
	r.current = r.current + r.step
	if r.current > r.end {
		return false
	}
	rangeEnd := r.current
	rangeStart := rangeEnd - r.selRange
	// load samples

	r.windowRangeAgg = make(map[string]RangeStreamingExemplarAgg, 0)
	r.metrics = map[string]labels.Labels{}
	r.load(rangeStart, rangeEnd)
	return true
}

func (r *streamRangeExemplarIterator) Close() error {
	return r.iter.Close()
}

func (r *streamRangeExemplarIterator) Error() error {
	return r.iter.Error()
}

// load the next sample range window.
func (r *streamRangeExemplarIterator) load(start, end int64) {
	for lbs, sample, hasNext := r.iter.Peek(); hasNext; lbs, sample, hasNext = r.iter.Peek() {
		if sample.TimestampMs > end {
			// not consuming the iterator as this belong to another range.
			return
		}
		// the lower bound of the range is not inclusive
		if sample.TimestampMs <= start {
			_ = r.iter.Next()
			continue
		}
		// adds the sample.
		var rangeAgg RangeStreamingExemplarAgg
		var ok bool
		rangeAgg, ok = r.windowRangeAgg[lbs]
		if !ok {
			var metric labels.Labels
			if _, ok = r.metrics[lbs]; !ok {
				var err error
				metric, err = promql_parser.ParseMetric(lbs)
				if err != nil {
					_ = r.iter.Next()
					continue
				}
				r.metrics[lbs] = metric
			}

			// never err here ,we have check error at evaluator.go rangeAggEvaluator() func
			rangeAgg = streamingExemplarregator()
			r.windowRangeAgg[lbs] = rangeAgg
		}
		p := exemplar.Exemplar{
			Ts:     sample.TimestampMs,
			Value:  sample.Value,
			Labels: logproto.FromLabelAdaptersToLabels(sample.Labels),
		}
		rangeAgg.agg(p)
		_ = r.iter.Next()
	}
}

func (r *streamRangeExemplarIterator) At() (int64, []exemplar.QueryResult) {
	if r.exemplars == nil {
		r.exemplars = make([]exemplar.QueryResult, 0, len(r.windowRangeAgg))
	}
	r.exemplars = r.exemplars[:0]
	ts := r.current/1e+6 + r.offset/1e+6
	for lbs, rangeAgg := range r.windowRangeAgg {
		exp := exemplar.Exemplar{
			Labels: rangeAgg.at().Labels,
			Ts:     ts,
		}

		eps := make([]exemplar.Exemplar, 0)
		eps = append(eps, exp)

		r.exemplars = append(r.exemplars, exemplar.QueryResult{
			SeriesLabels: r.metrics[lbs],
			Exemplars:    eps,
		})
	}
	return ts, r.exemplars
}

func streamingExemplarregator() RangeStreamingExemplarAgg {
	return &ExemplarAgg{}
}

type ExemplarAgg struct {
	exemplar exemplar.Exemplar
}

func (a *ExemplarAgg) agg(exemplar exemplar.Exemplar) {
	a.exemplar = exemplar
}

func (a *ExemplarAgg) at() exemplar.Exemplar {
	return a.exemplar
}
