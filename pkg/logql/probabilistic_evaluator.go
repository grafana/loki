package logql

import (
	"context"
	"fmt"
	"math"
	"sort"

	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/pkg/logql/sketch"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/validation"
)

type T int64

const (
	VecType T = iota
	TopKVecType
)

type StepResult interface {
	Type() T

	SampleVector() promql.Vector
	TopkVector() sketch.TopKVector
}

type SampleVector promql.Vector

func (SampleVector) Type() T {
	return VecType
}

func (s SampleVector) SampleVector() promql.Vector {
	return promql.Vector(s)
}

func (s SampleVector) TopkVector() sketch.TopKVector {
	return sketch.TopKVector{}
}

type TopKVector sketch.TopKVector

func (TopKVector) Type() T {
	return TopKVecType
}

func (v TopKVector) SampleVector() promql.Vector {
	return nil
}

func (v TopKVector) TopkVector() sketch.TopKVector {
	return sketch.TopKVector(v)
}

// ProbabilisticStepEvaluator evaluate a single step of a probabilistic query type.
type ProbabilisticStepEvaluator interface {
	// while Next returns a StepResult.
	Next() (ok bool, ts int64, r StepResult)
	// Close all resources used.
	Close() error
	// Reports any error
	Error() error
	// the type of a step result
	Type() T
}

type ProbabilisticEvaluator struct {
	Evaluator // this evaluator should be a DefaultEvaluator
	logger    log.Logger
}

type pTopkStepEvaluator struct {
	expr    *syntax.VectorAggregationExpr
	next    StepEvaluator
	k       int
	logger  log.Logger
	lastErr error
}

func (e *pTopkStepEvaluator) Type() T {
	return TopKVecType
}

func (e *pTopkStepEvaluator) Next() (bool, int64, StepResult) {
	next, ts, vec := e.next.Next()

	if !next {
		return false, 0, nil
	}
	if e.expr.Params < 1 {
		return next, ts, nil
	}

	// We only use one aggregation. The topk sketch compresses all
	// information and thus we don't need to take care of grouping
	// here.
	topkAggregation, err := sketch.NewCMSTopkForCardinality(e.logger, e.k, 100000)
	if err != nil {
		e.lastErr = err
		return false, ts, nil
	}

	buf := make([]byte, 0, 1024)
	sort.Strings(e.expr.Grouping.Groups)

	for _, s := range vec {
		metric := s.Metric

		var groupingKey uint64
		if e.expr.Grouping.Without {
			groupingKey, buf = metric.HashWithoutLabels(buf, e.expr.Grouping.Groups...)
		} else {
			groupingKey, buf = metric.HashForLabels(buf, e.expr.Grouping.Groups...)
		}

		// TODO(karsten): support floats.
		topkAggregation.ObserveForGroupingKey(s.Metric.String(), strconv.FormatUint(groupingKey, 10), uint32(s.F))
	}

	r := sketch.TopKVector{
		TS:   ts,
		Topk: topkAggregation,
	}

	return next, ts, TopKVector(r)
}

func (e *pTopkStepEvaluator) Close() error {
	return e.next.Close()
}

func (e *pTopkStepEvaluator) Error() error {
	if e.lastErr != nil {
		return e.lastErr
	}
	return e.next.Error()
}

// NewDefaultEvaluator constructs a DefaultEvaluator
func NewProbabilisticEvaluator(querier Querier, maxLookBackPeriod time.Duration) Evaluator {
	d := NewDefaultEvaluator(querier, maxLookBackPeriod)
	p := &ProbabilisticEvaluator{Evaluator: d}
	return p
}

type stepEvaluatorAdapter struct {
	d StepEvaluator
}

func (p stepEvaluatorAdapter) Next() (ok bool, ts int64, r StepResult) {
	a, s, d := p.d.Next()
	return a, s, SampleVector(d)
}

func (p stepEvaluatorAdapter) Close() error {
	return p.d.Close()
}

func (p stepEvaluatorAdapter) Error() error {
	return p.d.Error()
}

func (p stepEvaluatorAdapter) Type() T {
	return VecType
}

func (p *ProbabilisticEvaluator) ProbabilisticStepEvaluator(
	ctx context.Context,
	nextEv SampleEvaluator,
	expr syntax.SampleExpr,
	q Params,
) (ProbabilisticStepEvaluator, error) {

	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		if e.Operation != syntax.OpTypeTopK {
			dEval, err := p.Evaluator.StepEvaluator(ctx, nextEv, expr, q)
			if err != nil {
				return nil, err
			}
			return stepEvaluatorAdapter{dEval}, nil
		}
		return p.newProbabilisticVectorAggEvaluator(ctx, nextEv, e, q)
	default:
		dEval, err := p.Evaluator.StepEvaluator(ctx, nextEv, expr, q)
		if err != nil {
			return nil, err
		}
		return stepEvaluatorAdapter{dEval}, nil
	}
}

type probabilisticQuery struct {
	evaluator ProbabilisticEvaluator
	query
}

// Exec Implements `Query`. It handles instrumentation & defers to Eval.
func (q *probabilisticQuery) Exec(ctx context.Context) (logqlmodel.Result, error) {
	return wrapEval(ctx, q.logger, q.logExecQuery, q.record, q.params, q.Eval)
}

func (q *probabilisticQuery) Eval(ctx context.Context) (promql_parser.Value, error) {
	tenants, _ := tenant.TenantIDs(ctx)
	timeoutCapture := func(id string) time.Duration { return q.limits.QueryTimeout(ctx, id) }
	queryTimeout := validation.SmallestPositiveNonZeroDurationPerTenant(tenants, timeoutCapture)
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	expr, err := q.parse(ctx, q.params.Query())
	if err != nil {
		return nil, err
	}

	if q.checkBlocked(ctx, tenants) {
		return nil, logqlmodel.ErrBlocked
	}

	switch e := expr.(type) {
	case syntax.SampleExpr:
		value, err := q.probabilisticEvalSample(ctx, e)
		return value, err

	default:
		return q.query.Eval(ctx)
	}
}

// evalSample evaluate a sampleExpr
func (q *probabilisticQuery) probabilisticEvalSample(ctx context.Context, expr syntax.SampleExpr) (promql_parser.Value, error) {
	if lit, ok := expr.(*syntax.LiteralExpr); ok {
		return q.evalLiteral(ctx, lit)
	}
	if vec, ok := expr.(*syntax.VectorExpr); ok {
		return q.evalVector(ctx, vec)
	}

	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}

	maxIntervalCapture := func(id string) time.Duration { return q.limits.MaxQueryRange(ctx, id) }
	maxQueryInterval := validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, maxIntervalCapture)
	if maxQueryInterval != 0 {
		err = q.checkIntervalLimit(expr, maxQueryInterval)
		if err != nil {
			return nil, err
		}
	}

	expr, err = optimizeSampleExpr(expr)
	if err != nil {
		return nil, err
	}

	stepEvaluator, err := q.evaluator.ProbabilisticStepEvaluator(ctx, q.evaluator.Evaluator, expr, q.params)
	if err != nil {
		return nil, err
	}
	defer util.LogErrorWithContext(ctx, "closing SampleExpr", stepEvaluator.Close)

	switch stepEvaluator.Type() {
	case VecType:
		return q.aggregateSampleVectors(ctx, stepEvaluator, tenantIDs)
	case TopKVecType:
		return q.aggregateTopKVectors(stepEvaluator)
	default:
		return nil, nil
	}
}

func (q *probabilisticQuery) aggregateSampleVectors(
	ctx context.Context,
	stepEvaluator ProbabilisticStepEvaluator,
	tenantIDs []string,
) (promql_parser.Value, error) {

	// Assert stepEvaluator.Type() == VecType

	maxSeriesCapture := func(id string) int { return q.limits.MaxQuerySeries(ctx, id) }
	maxSeries := validation.SmallestPositiveIntPerTenant(tenantIDs, maxSeriesCapture)
	seriesIndex := map[uint64]*promql.Series{}

	next, ts, r := stepEvaluator.Next()
	if stepEvaluator.Error() != nil {
		return nil, stepEvaluator.Error()
	}

	if !next || r == nil {
		return promql.Vector{}, nil
	}

	// fail fast for the first step or instant query
	vec := r.SampleVector()
	if len(vec) > maxSeries {
		return nil, logqlmodel.NewSeriesLimitError(maxSeries)
	}

	if GetRangeType(q.params) == InstantType {
		sortByValue, err := Sortable(q.params)
		if err != nil {
			return nil, fmt.Errorf("fail to check Sortable, logql: %s ,err: %s", q.params.Query(), err)
		}
		if !sortByValue {
			sort.Slice(vec, func(i, j int) bool { return labels.Compare(vec[i].Metric, vec[j].Metric) < 0 })
		}
		return vec, nil
	}

	stepCount := int(math.Ceil(float64(q.params.End().Sub(q.params.Start()).Nanoseconds()) / float64(q.params.Step().Nanoseconds())))
	if stepCount <= 0 {
		stepCount = 1
	}

	for next {
		for _, p := range vec {
			var (
				series *promql.Series
				hash   = p.Metric.Hash()
				ok     bool
			)

			series, ok = seriesIndex[hash]
			if !ok {
				series = &promql.Series{
					Metric: p.Metric,
					Floats: make([]promql.FPoint, 0, stepCount),
				}
				seriesIndex[hash] = series
			}
			series.Floats = append(series.Floats, promql.FPoint{
				T: ts,
				F: p.F,
			})
		}
		// as we slowly build the full query for each steps, make sure we don't go over the limit of unique series.
		if len(seriesIndex) > maxSeries {
			return nil, logqlmodel.NewSeriesLimitError(maxSeries)
		}

		next, ts, r = stepEvaluator.Next()
		if stepEvaluator.Error() != nil {
			return nil, stepEvaluator.Error()
		}
		if r != nil {
			vec = r.SampleVector()
		}
	}

	series := make([]promql.Series, 0, len(seriesIndex))
	for _, s := range seriesIndex {
		series = append(series, *s)
	}
	result := promql.Matrix(series)
	sort.Sort(result)

	return result, stepEvaluator.Error()
}

func (q *probabilisticQuery) aggregateTopKVectors(stepEvaluator ProbabilisticStepEvaluator) (promql_parser.Value, error) {
	next, _, r := stepEvaluator.Next()

	if stepEvaluator.Error() != nil {
		return nil, stepEvaluator.Error()
	}

	topkMatrix := sketch.TopKMatrix{}

	for next {

		topkMatrix = append(topkMatrix, r.TopkVector())

		next, _, r = stepEvaluator.Next()
		if stepEvaluator.Error() != nil {
			return nil, stepEvaluator.Error()
		}
	}

	return topkMatrix, nil
}

func (p *ProbabilisticEvaluator) newProbabilisticVectorAggEvaluator(
	ctx context.Context,
	ev SampleEvaluator,
	expr *syntax.VectorAggregationExpr,
	q Params,
) (ProbabilisticStepEvaluator, error) {

	if expr.Operation != syntax.OpTypeTopK {
		return nil, errors.Errorf("unexpected operation: want 'topk', have '%q'", expr.Operation)
	}

	if expr.Grouping == nil {
		return nil, errors.Errorf("aggregation operator '%q' without grouping", expr.Operation)
	}
	nextEvaluator, err := ev.StepEvaluator(ctx, ev, expr.Left, q)
	if err != nil {
		return nil, err
	}
	sort.Strings(expr.Grouping.Groups)
	return &pTopkStepEvaluator{
		expr:   expr,
		next:   nextEvaluator,
		k:      expr.Params,
		logger: p.logger,
	}, nil
}

type TopkMatrixStepper struct {
	m sketch.TopKMatrix
	i int
}

func NewTopKMatrixStepper(m sketch.TopKMatrix) *TopkMatrixStepper {
	return &TopkMatrixStepper{
		m: m,
		i: 0,
	}
}

func (s *TopkMatrixStepper) Next() (ok bool, ts int64, r StepResult) {
	if s.i < len(s.m) {
		v := s.m[s.i]
		s.i++
		return true, v.TS, TopKVector(v)
	}

	return false, 0, nil
}

func (*TopkMatrixStepper) Close() error { return nil }

func (*TopkMatrixStepper) Error() error { return nil }

func (*TopkMatrixStepper) Type() T {
	return TopKVecType
}
