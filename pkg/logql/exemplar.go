package logql

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/exemplar"
	"github.com/prometheus/prometheus/model/labels"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/httpreq"
	logutil "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/spanlogger"
	"github.com/grafana/loki/pkg/util/validation"
)

//
// DownstreamEvaluator + Engine + query .go
//

type ExemplarQueryable interface {
	// ExemplarQuerier returns a new ExemplarQuerier on the storage.
	ExemplarQuerier(param Params) ExemplarQuerier
}

// ExemplarQuerier provides reading access to time series data.
type ExemplarQuerier interface {
	// SelectExemplars all the exemplars that match the matchers.
	// Within a single slice of matchers, it is an intersection. Between the slices, it is a union.
	SelectExemplars(ctx context.Context) (logqlmodel.Result, error)
}

// ExemplarQuerier creates a new LogQL query.  Exemplar type is derived from the parameters.
func (ng *Engine) ExemplarQuerier(params Params) ExemplarQuerier {
	return &query{
		logger:    ng.logger,
		params:    params,
		evaluator: ng.evaluator,
		parse: func(_ context.Context, query string) (syntax.Expr, error) {
			return syntax.ParseExpr(query)
		},
		record: true,
		limits: ng.limits,
	}
}

func (q *query) SelectExemplars(ctx context.Context) (logqlmodel.Result, error) {
	log, ctx := spanlogger.New(ctx, "query.SelectExemplars")
	defer log.Finish()

	level.Info(logutil.WithContext(ctx, q.logger)).Log("msg", "executing query", "type", "exemplars", "query", q.params.Query())

	rangeType := GetRangeType(q.params)
	timer := prometheus.NewTimer(QueryTime.WithLabelValues(string(rangeType)))
	defer timer.ObserveDuration()

	// records query statistics
	start := time.Now()
	statsCtx, ctx := stats.NewContext(ctx)
	metadataCtx, ctx := metadata.NewContext(ctx)

	data, err := q.EvalExemplars(ctx)
	queueTime, _ := ctx.Value(httpreq.QueryQueueTimeHTTPHeader).(time.Duration)

	statResult := statsCtx.Result(time.Since(start), queueTime, q.resultLength(data))
	statResult.Log(level.Debug(log))

	return logqlmodel.Result{
		Data:       data,
		Statistics: statResult,
		Headers:    metadataCtx.Headers(),
	}, err
}

func (q *query) EvalExemplars(ctx context.Context) (promql_parser.Value, error) {
	tenants, _ := tenant.TenantIDs(ctx)
	queryTimeout := validation.SmallestPositiveNonZeroDurationPerTenant(tenants, q.limits.QueryTimeout)

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
		value, err := q.evalExemplars(ctx, e)
		return value, err
	case syntax.LogSelectorExpr:
		return nil, errors.New("Unexpected type: LogSelectorExpr")
	default:
		return nil, errors.New("Unexpected type (%T): cannot evaluate exemplars")
	}
}

// evalExemplars
func (q *query) evalExemplars(ctx context.Context, expr syntax.SampleExpr) (promql_parser.Value, error) {
	if _, ok := expr.(*syntax.LiteralExpr); ok {
		return logqlmodel.Exemplars{}, nil
	}
	if _, ok := expr.(*syntax.VectorExpr); ok {
		return logqlmodel.Exemplars{}, nil
	}

	expr, err := optimizeSampleExpr(expr)
	if err != nil {
		return nil, err
	}

	stepEvaluator, err := q.evaluator.StepExemplarEvaluator(ctx, q.evaluator, expr, q.params)
	if err != nil {
		return nil, err
	}
	defer util.LogErrorWithContext(ctx, "closing SampleExpr", stepEvaluator.Close)

	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}
	maxSeries := validation.SmallestPositiveIntPerTenant(tenantIDs, q.limits.MaxQuerySeries)
	seriesIndex := map[uint64]*exemplar.QueryResult{}

	next, ts, vec := stepEvaluator.Next()
	if stepEvaluator.Error() != nil {
		return nil, stepEvaluator.Error()
	}

	// fail fast for the first step or instant query
	if len(vec) > maxSeries {
		return nil, logqlmodel.NewSeriesLimitError(maxSeries)
	}
	if GetRangeType(q.params) == InstantType {
		sort.Slice(vec, func(i, j int) bool { return labels.Compare(vec[i].SeriesLabels, vec[j].SeriesLabels) < 0 })
		return logqlmodel.Exemplars(vec), nil
	}
	stepCount := int(math.Ceil(float64(q.params.End().Sub(q.params.Start()).Nanoseconds()) / float64(q.params.Step().Nanoseconds())))
	if stepCount <= 0 {
		stepCount = 1
	}

	for next {
		for _, p := range vec {
			var (
				series *exemplar.QueryResult
				hash   = p.SeriesLabels.Hash()
				ok     bool
			)

			series, ok = seriesIndex[hash]
			if !ok {
				series = &exemplar.QueryResult{
					SeriesLabels: p.SeriesLabels,
					Exemplars:    make([]exemplar.Exemplar, 0, stepCount),
				}
				seriesIndex[hash] = series
			}

			for _, ep := range p.Exemplars {
				series.Exemplars = append(series.Exemplars, exemplar.Exemplar{
					Ts:     ts,
					Value:  ep.Value,
					Labels: ep.Labels,
				})
			}

		}
		// as we slowly build the full query for each steps, make sure we don't go over the limit of unique series.
		if len(seriesIndex) > maxSeries {
			return nil, logqlmodel.NewSeriesLimitError(maxSeries)
		}
		next, ts, vec = stepEvaluator.Next()
		if stepEvaluator.Error() != nil {
			return nil, stepEvaluator.Error()
		}
	}

	series := make([]exemplar.QueryResult, 0, len(seriesIndex))
	for _, s := range seriesIndex {
		series = append(series, *s)
	}
	exemplars := logqlmodel.Exemplars(series)
	sort.Sort(exemplars)
	return exemplars, stepEvaluator.Error()
}

// DownstreamEvaluator
func (ev DownstreamEvaluator) StepExemplarEvaluator(ctx context.Context,
	nextEv ExemplarEvaluator,
	expr syntax.SampleExpr,
	params Params,
) (StepExemplarEvaluator, error) {
	switch e := expr.(type) {

	case DownstreamSampleExpr:
		// downstream to a querier
		var shards []astmapper.ShardAnnotation
		if e.shard != nil {
			shards = append(shards, *e.shard)
		}
		results, err := ev.Downstream(ctx, []DownstreamQuery{{
			Expr:   e.SampleExpr,
			Params: params,
			Shards: shards,
		}})
		if err != nil {
			return nil, err
		}
		return ResultStepExemplarEvaluator(results[0], params)

	case *ConcatSampleExpr:
		cur := e
		var queries []DownstreamQuery
		for cur != nil {
			qry := DownstreamQuery{
				Expr:   cur.DownstreamSampleExpr.SampleExpr,
				Params: params,
			}
			if shard := cur.DownstreamSampleExpr.shard; shard != nil {
				qry.Shards = Shards{*shard}
			}
			queries = append(queries, qry)
			cur = cur.next
		}

		results, err := ev.Downstream(ctx, queries)
		if err != nil {
			return nil, err
		}

		xs := make([]StepExemplarEvaluator, 0, len(queries))
		for i, res := range results {
			stepper, err := ResultStepExemplarEvaluator(res, params)
			if err != nil {
				level.Warn(logutil.Logger).Log(
					"msg", "could not extract StepEvaluator",
					"err", err,
					"expr", queries[i].Expr.String(),
				)
				return nil, err
			}
			xs = append(xs, stepper)
		}

		return ConcatExemplarEvaluator(xs)
	default:
		return ev.defaultEvaluator.StepExemplarEvaluator(ctx, nextEv, e, params)
	}
}

// ResultStepExemplarEvaluator coerces a downstream exemplars into a StepExemplarEvaluator
func ResultStepExemplarEvaluator(res logqlmodel.Result, params Params) (StepExemplarEvaluator, error) {
	var (
		start = params.Start()
		end   = params.End()
		step  = params.Step()
	)

	switch data := res.Data.(type) {
	case logqlmodel.Exemplars:
		return NewExemplarStepper(start, end, step, data), nil
	default:
		return nil, fmt.Errorf("unexpected type (%s) uncoercible to StepEvaluator", data.Type())
	}
}

// ExemplarStepper
type ExemplarStepper struct {
	start, end, ts time.Time
	step           time.Duration
	m              logqlmodel.Exemplars
}

func NewExemplarStepper(start, end time.Time, step time.Duration, m logqlmodel.Exemplars) StepExemplarEvaluator {
	return &ExemplarStepper{
		start: start,
		end:   end,
		ts:    start.Add(-step), // will be corrected on first Next() call
		step:  step,
		m:     m,
	}
}

func (m *ExemplarStepper) Next() (bool, int64, []exemplar.QueryResult) {
	m.ts = m.ts.Add(m.step)
	if m.ts.After(m.end) {
		return false, 0, nil
	}

	ts := m.ts.UnixNano() / int64(time.Millisecond)
	vec := make([]exemplar.QueryResult, 0, len(m.m))

	for i, series := range m.m {
		ln := len(series.Exemplars)

		if ln == 0 || series.Exemplars[0].Ts != ts {
			continue
		}

		vec = append(vec, exemplar.QueryResult{
			Exemplars:    series.Exemplars,
			SeriesLabels: series.SeriesLabels,
		})
		m.m[i].Exemplars = m.m[i].Exemplars[1:]
	}

	return true, ts, vec
}

func (m *ExemplarStepper) Close() error { return nil }

func (m *ExemplarStepper) Error() error { return nil }

//

// ConcatExemplarEvaluator joins multiple StepExemplarEvaluator.
// Contract: They must be of identical start, end, and step values.
func ConcatExemplarEvaluator(evaluators []StepExemplarEvaluator) (StepExemplarEvaluator, error) {
	return newStepExemplarEvaluator(
		func() (ok bool, ts int64, vec []exemplar.QueryResult) {
			var cur []exemplar.QueryResult
			for _, eval := range evaluators {
				ok, ts, cur = eval.Next()
				vec = append(vec, cur...)
			}
			return ok, ts, vec
		},
		func() (lastErr error) {
			for _, eval := range evaluators {
				if err := eval.Close(); err != nil {
					lastErr = err
				}
			}
			return lastErr
		},
		func() error {
			var errs []error
			for _, eval := range evaluators {
				if err := eval.Error(); err != nil {
					errs = append(errs, err)
				}
			}
			switch len(errs) {
			case 0:
				return nil
			case 1:
				return errs[0]
			default:
				return util.MultiError(errs)
			}
		},
	)
}
