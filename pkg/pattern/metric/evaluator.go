package metric

import (
	"context"
	"fmt"
	"sort"
	"time"

	loki_iter "github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
)

// TODO(twhitney): duplication with code in NewStepEvaluator
func extractMetricType(expr syntax.SampleExpr) (MetricType, error) {
	var typ MetricType
	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		if rangeExpr, ok := e.Left.(*syntax.RangeAggregationExpr); ok && e.Operation == syntax.OpTypeSum {
			if rangeExpr.Operation == syntax.OpRangeTypeCount {
				typ = Count
			} else if rangeExpr.Operation == syntax.OpRangeTypeBytes {
				typ = Bytes
			} else {
				return Unsupported, fmt.Errorf("unsupported aggregation operation %s", e.Operation)
			}
		} else {
			return Unsupported, fmt.Errorf("unsupported aggregation operation %s", e.Operation)
		}
	case *syntax.RangeAggregationExpr:
		if e.Operation == syntax.OpRangeTypeCount {
			typ = Count
		} else if e.Operation == syntax.OpRangeTypeBytes {
			typ = Bytes
		} else {
			return Unsupported, fmt.Errorf("unsupported aggregation operation %s", e.Operation)
		}
	default:
		return Unsupported, fmt.Errorf("unexpected expression type %T", e)
	}
	return typ, nil
}

type SampleEvaluatorFactory interface {
	// NewStepEvaluator returns a NewStepEvaluator for a given SampleExpr.
	// It's explicitly passed another NewStepEvaluator
	// in order to enable arbitrary computation of embedded expressions. This allows more modular & extensible
	// NewStepEvaluator implementations which can be composed.
	NewStepEvaluator(
		ctx context.Context,
		nextEvaluatorFactory SampleEvaluatorFactory,
		expr syntax.SampleExpr,
		from, through, step model.Time,
	) (logql.StepEvaluator, error)
}

type SampleEvaluatorFunc func(
	ctx context.Context,
	nextEvaluatorFactory SampleEvaluatorFactory,
	expr syntax.SampleExpr,
	from, through, step model.Time,
) (logql.StepEvaluator, error)

func (s SampleEvaluatorFunc) NewStepEvaluator(
	ctx context.Context,
	nextEvaluatorFactory SampleEvaluatorFactory,
	expr syntax.SampleExpr,
	from, through, step model.Time,
) (logql.StepEvaluator, error) {
	return s(ctx, nextEvaluatorFactory, expr, from, through, step)
}

type DefaultEvaluatorFactory struct {
	chunks *Chunks
}

func NewDefaultEvaluatorFactory(chunks *Chunks) *DefaultEvaluatorFactory {
	return &DefaultEvaluatorFactory{
		chunks: chunks,
	}
}

func (ev *DefaultEvaluatorFactory) NewStepEvaluator(
	ctx context.Context,
	evFactory SampleEvaluatorFactory,
	expr syntax.SampleExpr,
	from, through, step model.Time,
) (logql.StepEvaluator, error) {
	metricType, err := extractMetricType(expr)
	if err != nil || metricType == Unsupported {
		return nil, err
	}

	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		if rangExpr, ok := e.Left.(*syntax.RangeAggregationExpr); ok && e.Operation == syntax.OpTypeSum {
			// if range expression is wrapped with a vector expression
			// we should send the vector expression for allowing reducing labels at the source.
			evFactory = SampleEvaluatorFunc(
				func(ctx context.Context,
					_ SampleEvaluatorFactory,
					_ syntax.SampleExpr,
					from, through, step model.Time,
				) (logql.StepEvaluator, error) {
					fromWithRangeAndOffset := from.Add(-rangExpr.Left.Interval).Add(-rangExpr.Left.Offset)
					throughWithOffset := through.Add(-rangExpr.Left.Offset)
					it, err := ev.chunks.Iterator(ctx, metricType, e.Grouping, fromWithRangeAndOffset, throughWithOffset, step)
					if err != nil {
						return nil, err
					}

					params := NewParams(
						e,
						from.Add(-rangExpr.Left.Interval).Add(-rangExpr.Left.Offset),
						through.Add(-rangExpr.Left.Offset),
						step,
					)
					return NewPatternSampleRangeAggEvaluator(iter.NewPeekingSampleIterator(it), rangExpr, params, rangExpr.Left.Offset)
				})
		}

		if e.Grouping == nil {
			return nil, errors.Errorf("aggregation operator '%q' without grouping", e.Operation)
		}
		nextEvaluator, err := evFactory.NewStepEvaluator(ctx, evFactory, e.Left, from, through, step)
		if err != nil {
			return nil, err
		}
		sort.Strings(e.Grouping.Groups)

		return logql.NewVectorAggEvaluator(
			nextEvaluator,
			e,
			make([]byte, 0, 1024),
			labels.NewBuilder(labels.Labels{}),
		), nil

	case *syntax.RangeAggregationExpr:
		fromWithRangeAndOffset := from.Add(-e.Left.Interval).Add(-e.Left.Offset)
		throughWithOffset := through.Add(-e.Left.Offset)
		it, err := ev.chunks.Iterator(ctx, metricType, e.Grouping, fromWithRangeAndOffset, throughWithOffset, step)
		if err != nil {
			return nil, err
		}

		params := NewParams(
			e,
			from.Add(-e.Left.Interval).Add(-e.Left.Offset),
			through.Add(-e.Left.Offset),
			step, // expecting nanoseconds
		)
		return NewPatternSampleRangeAggEvaluator(iter.NewPeekingSampleIterator(it), e, params, e.Left.Offset)
	default:
		return nil, errors.Errorf("unexpected expr type (%T)", e)
	}
}

// Need to create our own StepEvaluator since we only support bytes and count over time,
// and always sum to get those values. In order to accomplish this we need control over the
// aggregation operation..
func NewPatternSampleRangeAggEvaluator(
	it loki_iter.PeekingSampleIterator,
	expr *syntax.RangeAggregationExpr,
	q logql.Params,
	o time.Duration,
) (logql.StepEvaluator, error) {
	iter, err := newRangeVectorIterator(
		it, expr,
		expr.Left.Interval.Nanoseconds(),
		q.Step().Nanoseconds(),
		q.Start().UnixNano(), q.End().UnixNano(), o.Nanoseconds(),
	)
	if err != nil {
		return nil, err
	}

	return logql.NewRangeVectorEvaluator(iter), nil
}

func newRangeVectorIterator(
	it loki_iter.PeekingSampleIterator,
	expr *syntax.RangeAggregationExpr,
	selRange, step, start, end, offset int64,
) (logql.RangeVectorIterator, error) {
	// forces at least one step.
	if step == 0 {
		step = 1
	}
	if offset != 0 {
		start = start - offset
		end = end - offset
	}
	// TODO(twhitney): do I need a streaming aggregator?
	// if so the aggregator is going to make this
	// a bit of a bad time, as there's currently no
	// way to provide a custom one.
	//
	// var overlap bool
	// if selRange >= step && start != end {
	// 	overlap = true
	// }
	// if !overlap {
	// 	_, err := streamingAggregator(expr)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	return &streamRangeVectorIterator{
	// 		iter:     it,
	// 		step:     step,
	// 		end:      end,
	// 		selRange: selRange,
	// 		metrics:  map[string]labels.Labels{},
	// 		r:        expr,
	// 		current:  start - step, // first loop iteration will set it to start
	// 		offset:   offset,
	// 	}, nil
	// }

	// always sum
	aggregator := logql.BatchRangeVectorAggregator(func(samples []promql.FPoint) float64 {
		sum := 0.0
		for _, v := range samples {
			sum += v.F
		}
		return sum
	})

	return logql.NewBatchRangeVectorIterator(
		it,
		selRange,
		step,
		start,
		end,
		offset,
		aggregator,
	), nil
}

type SeriesToSampleIterator struct {
	floats []promql.FPoint
	curTs  int64
	cur    float64
	lbls   labels.Labels
}

// TODO: could this me a matrix iterator that returned multiple samples with
// different labels for the same timestamp?
func NewSeriesToSampleIterator(series *promql.Series) *SeriesToSampleIterator {
	return &SeriesToSampleIterator{
		floats: series.Floats,
		lbls:   series.Metric,
	}
}

func (s *SeriesToSampleIterator) Next() bool {
	if len(s.floats) == 0 {
		return false
	}

	current, rest := s.floats[0], s.floats[1:]

	s.curTs = current.T
	s.cur = current.F

	s.floats = rest
	return true
}

func (s *SeriesToSampleIterator) Pattern() string {
	return ""
}

func (s *SeriesToSampleIterator) Labels() labels.Labels {
	return s.lbls
}

func (s *SeriesToSampleIterator) At() logproto.PatternSample {
	return logproto.PatternSample{
		Timestamp: model.Time(s.curTs),
		Value:     int64(s.cur),
	}
}

func (s *SeriesToSampleIterator) Error() error {
	return nil
}

func (s *SeriesToSampleIterator) Close() error {
	return nil
}

type paramCompat struct {
	expr    syntax.SampleExpr
	from    model.Time
	through model.Time
	step    model.Time
}

func NewParams(
	expr syntax.SampleExpr,
	from, through, step model.Time,
) *paramCompat {
	return &paramCompat{
		expr:    expr,
		from:    from,
		through: through,
		step:    step,
	}
}

func (p *paramCompat) QueryString() string {
	return p.expr.String()
}

func (p *paramCompat) Start() time.Time {
	return p.from.Time()
}

func (p *paramCompat) End() time.Time {
	return p.through.Time()
}

func (p *paramCompat) Step() time.Duration {
	return time.Duration(p.step.UnixNano())
}

func (p *paramCompat) Interval() time.Duration {
	return time.Duration(0)
}

func (p *paramCompat) Limit() uint32 {
	return 0
}

func (p *paramCompat) Direction() logproto.Direction {
	return logproto.BACKWARD
}

func (p *paramCompat) Shards() []string {
	return []string{}
}

func (p *paramCompat) GetExpression() syntax.Expr {
	return p.expr
}

func (p *paramCompat) GetStoreChunks() *logproto.ChunkRefGroup {
	return nil
}

func (p *paramCompat) CachingOptions() (res resultscache.CachingOptions) {
	return
}
