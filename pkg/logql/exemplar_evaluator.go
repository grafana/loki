package logql

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/prometheus/prometheus/model/exemplar"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel"
)

// StepExemplarEvaluator evaluate a single step of a query.
type StepExemplarEvaluator interface {
	// while Next returns a promql.Value, the only acceptable types are Scalar and Vector.
	Next() (ok bool, ts int64, vec []exemplar.QueryResult)
	// Close all resources used.
	Close() error
	// Reports any error
	Error() error
}

type stepExemplarEvaluator struct {
	fn    func() (bool, int64, []exemplar.QueryResult)
	close func() error
	err   func() error
}

func newStepExemplarEvaluator(fn func() (bool, int64, []exemplar.QueryResult), close func() error, err func() error) (StepExemplarEvaluator, error) {
	if fn == nil {
		return nil, errors.New("nil step evaluator fn")
	}

	if close == nil {
		close = func() error { return nil }
	}

	if err == nil {
		err = func() error { return nil }
	}
	return &stepExemplarEvaluator{
		fn:    fn,
		close: close,
		err:   err,
	}, nil
}

func (e *stepExemplarEvaluator) Next() (bool, int64, []exemplar.QueryResult) {
	return e.fn()
}

func (e *stepExemplarEvaluator) Close() error {
	return e.close()
}

func (e *stepExemplarEvaluator) Error() error {
	return e.err()
}

type ExemplarEvaluator interface {
	// StepExemplarEvaluator returns a StepEvaluator for a given SampleExpr. It's explicitly passed another StepEvaluator// in order to enable arbitrary computation of embedded expressions. This allows more modular & extensible
	// StepEvaluator implementations which can be composed.
	StepExemplarEvaluator(ctx context.Context, nextEvaluator ExemplarEvaluator, expr syntax.SampleExpr, p Params) (StepExemplarEvaluator, error)
}

type ExemplarEvaluatorFunc func(ctx context.Context, nextEvaluator ExemplarEvaluator, expr syntax.SampleExpr, p Params) (StepExemplarEvaluator, error)

func (s ExemplarEvaluatorFunc) StepExemplarEvaluator(ctx context.Context, nextEvaluator ExemplarEvaluator, expr syntax.SampleExpr, p Params) (StepExemplarEvaluator, error) {
	return s(ctx, nextEvaluator, expr, p)
}

func (ev *DefaultEvaluator) StepExemplarEvaluator(
	ctx context.Context,
	nextEv ExemplarEvaluator,
	expr syntax.SampleExpr,
	q Params,
) (StepExemplarEvaluator, error) {
	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		if rangExpr, ok := e.Left.(*syntax.RangeAggregationExpr); ok && e.Operation == syntax.OpTypeSum {
			// if range expression is wrapped with a vector expression
			// we should send the vector expression for allowing reducing labels at the source.
			nextEv = ExemplarEvaluatorFunc(func(ctx context.Context, nextEvaluator ExemplarEvaluator, expr syntax.SampleExpr, p Params) (StepExemplarEvaluator, error) {
				it, err := ev.querier.SelectExemplars(ctx, SelectSampleParams{
					&logproto.SampleQueryRequest{
						Start:    q.Start().Add(-rangExpr.Left.Interval).Add(-rangExpr.Left.Offset),
						End:      q.End().Add(-rangExpr.Left.Offset),
						Selector: e.String(), // intentionally send the vector for reducing labels.
						Shards:   q.Shards(),
					},
				})
				if err != nil {
					return nil, err
				}
				return rangeAggExemplarEvaluator(iter.NewPeekingExemplarIterator(it), rangExpr, q, rangExpr.Left.Offset)
			})
		}
		return exemplarAggEvaluator(ctx, nextEv, e, q)
	case *syntax.RangeAggregationExpr:
		it, err := ev.querier.SelectExemplars(ctx, SelectSampleParams{
			&logproto.SampleQueryRequest{
				Start:    q.Start().Add(-e.Left.Interval).Add(-e.Left.Offset),
				End:      q.End().Add(-e.Left.Offset),
				Selector: expr.String(),
				Shards:   q.Shards(),
			},
		})
		if err != nil {
			return nil, err
		}
		return rangeAggExemplarEvaluator(iter.NewPeekingExemplarIterator(it), e, q, e.Left.Offset)
	case *syntax.BinOpExpr:
		return newBinOpExemplarIterator(q.Step().Nanoseconds(), q.Start().UnixNano(), q.End().UnixNano()), nil
	case *syntax.LabelReplaceExpr:
		return labelReplaceExemplarEvaluator(ctx, nextEv, e, q)
	case *syntax.VectorExpr:
		val, err := e.Value()
		if err != nil {
			return nil, err
		}
		return newVectorExemplarIterator(val, q.Step().Nanoseconds(), q.Start().UnixNano(), q.End().UnixNano()), nil
	default:
		return nil, EvaluatorUnsupportedType(e, ev)
	}
}

// rangeAggExemplarEvaluator
func rangeAggExemplarEvaluator(
	it iter.PeekingExemplarIterator,
	expr *syntax.RangeAggregationExpr,
	q Params,
	o time.Duration,
) (StepExemplarEvaluator, error) {
	iter, err := newRangeExemplarIterator(
		it, expr,
		expr.Left.Interval.Nanoseconds(),
		q.Step().Nanoseconds(),
		q.Start().UnixNano(), q.End().UnixNano(), o.Nanoseconds(),
	)
	if err != nil {
		return nil, err
	}
	return &rangeExemplarEvaluator{
		iter: iter,
	}, nil
}

type rangeExemplarEvaluator struct {
	iter RangeExemplarsIterator

	err error
}

func (r *rangeExemplarEvaluator) Next() (bool, int64, []exemplar.QueryResult) {
	next := r.iter.Next()
	if !next {
		return false, 0, make([]exemplar.QueryResult, 0)
	}
	ts, vec := r.iter.At()
	for _, s := range vec {
		// Errors are not allowed in metrics.
		if s.SeriesLabels.Has(logqlmodel.ErrorLabel) {
			r.err = logqlmodel.NewPipelineErr(s.SeriesLabels)
			return false, 0, make([]exemplar.QueryResult, 0)
		}
	}
	return true, ts, vec
}

func (r rangeExemplarEvaluator) Close() error { return r.iter.Close() }

func (r rangeExemplarEvaluator) Error() error {
	if r.err != nil {
		return r.err
	}
	return r.iter.Error()
}

func exemplarAggEvaluator(
	ctx context.Context,
	ev ExemplarEvaluator,
	expr *syntax.VectorAggregationExpr,
	q Params,
) (StepExemplarEvaluator, error) {
	if expr.Grouping == nil {
		return nil, fmt.Errorf("aggregation operator '%q' without grouping", expr.Operation)
	}
	nextEvaluator, err := ev.StepExemplarEvaluator(ctx, ev, expr.Left, q)
	if err != nil {
		return nil, err
	}
	lb := labels.NewBuilder(nil)
	buf := make([]byte, 0, 1024)
	sort.Strings(expr.Grouping.Groups)
	return newStepExemplarEvaluator(func() (bool, int64, []exemplar.QueryResult) {
		next, ts, vec := nextEvaluator.Next()

		if !next {
			return false, 0, make([]exemplar.QueryResult, 0)
		}
		result := map[uint64]*groupedAggregation{}
		if expr.Operation == syntax.OpTypeTopK || expr.Operation == syntax.OpTypeBottomK {
			if expr.Params < 1 {
				return next, ts, make([]exemplar.QueryResult, 0)
			}
		}
		for _, s := range vec {
			metric := s.SeriesLabels

			var groupingKey uint64
			if expr.Grouping.Without {
				groupingKey, buf = metric.HashWithoutLabels(buf, expr.Grouping.Groups...)
			} else {
				groupingKey, buf = metric.HashForLabels(buf, expr.Grouping.Groups...)
			}
			_, ok := result[groupingKey]
			// Add a new group if it doesn't exist.
			if ok {
				continue
			}
			var m labels.Labels

			if expr.Grouping.Without {
				lb.Reset(metric)
				lb.Del(expr.Grouping.Groups...)
				lb.Del(labels.MetricName)
				m = lb.Labels(nil)
			} else {
				m = make(labels.Labels, 0, len(expr.Grouping.Groups))
				for _, l := range metric {
					for _, n := range expr.Grouping.Groups {
						if l.Name == n {
							m = append(m, l)
							break
						}
					}
				}
				sort.Sort(m)
			}
			result[groupingKey] = &groupedAggregation{
				labels:     m,
				exemplars:  s.Exemplars,
				groupCount: 1,
			}
			///
		}
		vec = vec[:0]
		for _, aggr := range result {
			curntExemplar := exemplar.QueryResult{
				SeriesLabels: aggr.labels,
				Exemplars:    make([]exemplar.Exemplar, 0),
			}
			for _, e := range aggr.exemplars {
				curntExemplar.Exemplars = append(curntExemplar.Exemplars, exemplar.Exemplar{
					Ts:     ts,
					Labels: e.Labels,
				})
			}
			vec = append(vec, curntExemplar)
		}
		return next, ts, vec
	}, nextEvaluator.Close, nextEvaluator.Error)
}

// vectorIterator return simple vector like (1).
type vectorExemplarIterator struct {
	step, end, current int64
	val                float64
}

func newVectorExemplarIterator(val float64,
	step, start, end int64) StepExemplarEvaluator {
	if step == 0 {
		step = 1
	}
	return &vectorExemplarIterator{
		val:     val,
		step:    step,
		end:     end,
		current: start - step,
	}
}

func (r *vectorExemplarIterator) Next() (bool, int64, []exemplar.QueryResult) {
	r.current = r.current + r.step
	if r.current > r.end {
		return false, 0, nil
	}
	results := make([]exemplar.QueryResult, 0)
	curntExemplar := exemplar.QueryResult{
		Exemplars: make([]exemplar.Exemplar, 0),
	}
	curntExemplar.Exemplars = append(curntExemplar.Exemplars, exemplar.Exemplar{
		Ts: r.current,
		Labels: labels.Labels{
			{Name: "_source", Value: "vector"},
			{Name: "val", Value: strconv.FormatFloat(r.val, 'f', -1, 64)},
		},
	})
	results = append(results, curntExemplar)
	return true, r.current, results
}

func (r *vectorExemplarIterator) Close() error {
	return nil
}

func (r *vectorExemplarIterator) Error() error {
	return nil
}

//

// labelReplaceEvaluator
func labelReplaceExemplarEvaluator(
	ctx context.Context,
	ev ExemplarEvaluator,
	expr *syntax.LabelReplaceExpr,
	q Params,
) (StepExemplarEvaluator, error) {
	nextEvaluator, err := ev.StepExemplarEvaluator(ctx, ev, expr.Left, q)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 0, 1024)
	var labelCache map[uint64]labels.Labels
	return newStepExemplarEvaluator(func() (bool, int64, []exemplar.QueryResult) {
		next, ts, vec := nextEvaluator.Next()
		if !next {
			return false, 0, make([]exemplar.QueryResult, 0)
		}
		if labelCache == nil {
			labelCache = make(map[uint64]labels.Labels, len(vec))
		}
		var hash uint64
		for i, s := range vec {
			hash, buf = s.SeriesLabels.HashWithoutLabels(buf)
			if labels, ok := labelCache[hash]; ok {
				vec[i].SeriesLabels = labels
				continue
			}
			src := s.SeriesLabels.Get(expr.Src)
			indexes := expr.Re.FindStringSubmatchIndex(src)
			if indexes == nil {
				// If there is no match, no replacement should take place.
				labelCache[hash] = s.SeriesLabels
				continue
			}
			res := expr.Re.ExpandString([]byte{}, expr.Replacement, src, indexes)

			lb := labels.NewBuilder(s.SeriesLabels).Del(expr.Dst)
			if len(res) > 0 {
				lb.Set(expr.Dst, string(res))
			}
			outLbs := lb.Labels(nil)
			labelCache[hash] = outLbs
			vec[i].SeriesLabels = outLbs
		}
		return next, ts, vec
	}, nextEvaluator.Close, nextEvaluator.Error)
}

// vectorIterator return simple vector like (1).
type binOpExemplarIterator struct {
	step, end, current int64
}

func newBinOpExemplarIterator(
	step, start, end int64) StepExemplarEvaluator {
	if step == 0 {
		step = 1
	}
	return &binOpExemplarIterator{
		step:    step,
		end:     end,
		current: start - step,
	}
}

func (r *binOpExemplarIterator) Next() (bool, int64, []exemplar.QueryResult) {
	r.current = r.current + r.step
	if r.current > r.end {
		return false, 0, nil
	}
	results := make([]exemplar.QueryResult, 0)
	curntExemplar := exemplar.QueryResult{
		Exemplars: make([]exemplar.Exemplar, 0),
	}
	curntExemplar.Exemplars = append(curntExemplar.Exemplars, exemplar.Exemplar{
		Ts: r.current,
		Labels: labels.Labels{
			{Name: "_source", Value: "vector"},
			{Name: "val", Value: strconv.FormatFloat(0, 'f', -1, 64)},
		},
	})
	results = append(results, curntExemplar)
	return true, r.current, results
}

func (r *binOpExemplarIterator) Close() error {
	return nil
}

func (r *binOpExemplarIterator) Error() error {
	return nil
}
