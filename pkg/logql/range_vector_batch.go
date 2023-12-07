package logql

import (
	"github.com/apache/arrow/go/v14/arrow/array"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"
)

func newRangeVectorBatchIterator(
	it iter.PeekingSampleBatchIterator,
	expr *syntax.RangeAggregationExpr,
	selRange, step, start, end, offset int64) (RangeVectorIterator, error) {
	// forces at least one step.
	if step == 0 {
		step = 1
	}
	if offset != 0 {
		start = start - offset
		end = end - offset
	}
	var overlap bool
	if selRange >= step && start != end {
		overlap = true
	}
	if !overlap {
		_, err := streamingAggregator(expr)
		if err != nil {
			return nil, err
		}
		return &streamRangeVectorBatchIterator{
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
	vectorAggregator, err := aggregator(expr)
	if err != nil {
		return nil, err
	}
	return &batchRangeVectorBatchIterator{
		iter:     it,
		step:     step,
		end:      end,
		selRange: selRange,
		metrics:  map[string]labels.Labels{},
		window:   map[string]*promql.Series{},
		agg:      vectorAggregator,
		current:  start - step, // first loop iteration will set it to start
		offset:   offset,
	}, nil
}

type batchRangeVectorBatchIterator struct {
	iter                                 iter.PeekingSampleBatchIterator
	selRange, step, end, current, offset int64
	window                               map[string]*promql.Series
	metrics                              map[string]labels.Labels
	at                                   []promql.Sample
	agg                                  BatchRangeVectorAggregator
}

func (r *batchRangeVectorBatchIterator) Next() bool {
	// slides the range window to the next position
	r.current = r.current + r.step
	if r.current > r.end {
		return false
	}
	rangeEnd := r.current
	rangeStart := rangeEnd - r.selRange
	// load samples
	r.popBack(rangeStart)
	r.load(rangeStart, rangeEnd)
	return true
}

func (r *batchRangeVectorBatchIterator) Close() error {
	return r.iter.Close()
}

func (r *batchRangeVectorBatchIterator) Error() error {
	return r.iter.Error()
}

// popBack removes all entries out of the current window from the back.
func (r *batchRangeVectorBatchIterator) popBack(newStart int64) {
	// possible improvement: if there is no overlap we can just remove all.
	for fp := range r.window {
		lastPoint := 0
		remove := false
		for i, p := range r.window[fp].Floats {
			if p.T <= newStart {
				lastPoint = i
				remove = true
				continue
			}
			break
		}
		if remove {
			r.window[fp].Floats = r.window[fp].Floats[lastPoint+1:]
		}
		if len(r.window[fp].Floats) == 0 {
			s := r.window[fp]
			delete(r.window, fp)
			putSeries(s)
		}
	}
}

// load the next sample range window.
func (r *batchRangeVectorBatchIterator) load(start, end int64) {
	for lbs, samples, hasNext := r.iter.Peek(); hasNext; lbs, samples, hasNext = r.iter.Peek() {

		timestamps := array.NewTimestampData(samples.Column(0).Data())

		var i int
		for i = 0; i < timestamps.Len() && int64(timestamps.Value(i)) < start; i++ {
			continue
		}

		// for ; i< timestamps.Len() && int64(timestamps.Value(i) > end; i++ {

		// }

		if sample.Timestamp > end {
			// not consuming the iterator as this belong to another range.
			return
		}
		// the lower bound of the range is not inclusive
		if sample.Timestamp <= start {
			_ = r.iter.Next()
			continue
		}
		// adds the sample.
		var series *promql.Series
		var ok bool
		series, ok = r.window[lbs]
		if !ok {
			var metric labels.Labels
			if metric, ok = r.metrics[lbs]; !ok {
				var err error
				metric, err = promql_parser.ParseMetric(lbs)
				if err != nil {
					_ = r.iter.Next()
					continue
				}
				r.metrics[lbs] = metric
			}

			series = getSeries()
			series.Metric = metric
			r.window[lbs] = series
		}
		p := promql.FPoint{
			T: sample.Timestamp,
			F: sample.Value,
		}
		series.Floats = append(series.Floats, p)
		_ = r.iter.Next()
	}
}
func (r *batchRangeVectorBatchIterator) At() (int64, StepResult) {
	if r.at == nil {
		r.at = make([]promql.Sample, 0, len(r.window))
	}
	r.at = r.at[:0]
	// convert ts from nano to milli seconds as the iterator work with nanoseconds
	ts := r.current/1e+6 + r.offset/1e+6
	for _, series := range r.window {
		r.at = append(r.at, promql.Sample{
			F:      r.agg(series.Floats),
			T:      ts,
			Metric: series.Metric,
		})
	}
	return ts, SampleVector(r.at)
}

// streaming range agg
type streamRangeVectorBatchIterator struct {
	iter                                 iter.PeekingSampleBatchIterator
	selRange, step, end, current, offset int64
	windowRangeAgg                       map[string]RangeStreamingAgg
	r                                    *syntax.RangeAggregationExpr
	metrics                              map[string]labels.Labels
	at                                   []promql.Sample
	agg                                  BatchRangeVectorAggregator
}

func (r *streamRangeVectorBatchIterator) Next() bool {
	// slides the range window to the next position
	r.current = r.current + r.step
	if r.current > r.end {
		return false
	}
	rangeEnd := r.current
	rangeStart := rangeEnd - r.selRange
	// load samples

	r.windowRangeAgg = make(map[string]RangeStreamingAgg, 0)
	r.metrics = map[string]labels.Labels{}
	r.load(rangeStart, rangeEnd)
	return true
}

func (r *streamRangeVectorBatchIterator) Close() error {
	return r.iter.Close()
}

func (r *streamRangeVectorBatchIterator) Error() error {
	return r.iter.Error()
}

// load the next sample range window.
func (r *streamRangeVectorBatchIterator) load(start, end int64) {
	for lbs, sample, hasNext := r.iter.Peek(); hasNext; lbs, sample, hasNext = r.iter.Peek() {
		if sample.Timestamp > end {
			// not consuming the iterator as this belong to another range.
			return
		}
		// the lower bound of the range is not inclusive
		if sample.Timestamp <= start {
			_ = r.iter.Next()
			continue
		}
		// adds the sample.
		var rangeAgg RangeStreamingAgg
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
			rangeAgg, _ = streamingAggregator(r.r)
			r.windowRangeAgg[lbs] = rangeAgg
		}
		p := promql.FPoint{
			T: sample.Timestamp,
			F: sample.Value,
		}
		rangeAgg.agg(p)
		_ = r.iter.Next()
	}
}

func (r *streamRangeVectorBatchIterator) At() (int64, StepResult) {
	if r.at == nil {
		r.at = make([]promql.Sample, 0, len(r.windowRangeAgg))
	}
	r.at = r.at[:0]
	// convert ts from nano to milli seconds as the iterator work with nanoseconds
	ts := r.current/1e+6 + r.offset/1e+6
	for lbs, rangeAgg := range r.windowRangeAgg {
		r.at = append(r.at, promql.Sample{
			F:      rangeAgg.at(),
			T:      ts,
			Metric: r.metrics[lbs],
		})
	}
	return ts, SampleVector(r.at)
}
