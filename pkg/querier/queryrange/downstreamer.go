package queryrange

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/tenant"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

const (
	DefaultDownstreamConcurrency = 128
)

type DownstreamHandler struct {
	limits Limits
	next   queryrangebase.Handler

	splitAlign bool
}

func ParamsToLokiRequest(params logql.Params) queryrangebase.Request {
	if logql.GetRangeType(params) == logql.InstantType {
		return &LokiInstantRequest{
			Query:     params.QueryString(),
			Limit:     params.Limit(),
			TimeTs:    params.Start(),
			Direction: params.Direction(),
			Path:      "/loki/api/v1/query", // TODO(owen-d): make this derivable
			Shards:    params.Shards(),
			Plan: &plan.QueryPlan{
				AST: params.GetExpression(),
			},
			StoreChunks:    params.GetStoreChunks(),
			CachingOptions: params.CachingOptions(),
		}
	}
	return &LokiRequest{
		Query:     params.QueryString(),
		Limit:     params.Limit(),
		Step:      params.Step().Milliseconds(),
		Interval:  params.Interval().Milliseconds(),
		StartTs:   params.Start(),
		EndTs:     params.End(),
		Direction: params.Direction(),
		Path:      "/loki/api/v1/query_range", // TODO(owen-d): make this derivable
		Shards:    params.Shards(),
		Plan: &plan.QueryPlan{
			AST: params.GetExpression(),
		},
		StoreChunks:    params.GetStoreChunks(),
		CachingOptions: params.CachingOptions(),
	}
}

// Note: After the introduction of the LimitedRoundTripper,
// bounding concurrency in the downstreamer is mostly redundant
// The reason we don't remove it is to prevent malicious queries
// from creating an unreasonably large number of goroutines, such as
// the case of a query like `a / a / a / a / a ..etc`, which could try
// to shard each leg, quickly dispatching an unreasonable number of goroutines.
// In the future, it's probably better to replace this with a channel based API
// so we don't have to do all this ugly edge case handling/accounting
func (h DownstreamHandler) Downstreamer(ctx context.Context) logql.Downstreamer {
	p := DefaultDownstreamConcurrency

	// We may increase parallelism above the default,
	// ensure we don't end up bottlenecking here.
	if user, err := tenant.TenantID(ctx); err == nil {
		if x := h.limits.MaxQueryParallelism(ctx, user); x > 0 {
			p = x
		}
	}

	locks := make(chan struct{}, p)
	for i := 0; i < p; i++ {
		locks <- struct{}{}
	}
	return &instance{
		parallelism: p,
		locks:       locks,
		handler:     h.next,
		splitAlign:  h.splitAlign,
	}
}

// instance is an intermediate struct for controlling concurrency across a single query
type instance struct {
	parallelism int
	locks       chan struct{}
	handler     queryrangebase.Handler

	splitAlign bool
}

// withoutOffset returns the given query string with offsets removed and timestamp adjusted accordingly. If no offset is present in original query, it will be returned as is.
func withoutOffset(query logql.DownstreamQuery) (string, time.Time, time.Time) {
	expr := query.Params.GetExpression()

	var (
		newStart = query.Params.Start()
		newEnd   = query.Params.End()
	)
	expr.Walk(func(e syntax.Expr) {
		switch rng := e.(type) {
		case *syntax.RangeAggregationExpr:
			off := rng.Left.Offset

			if off != 0 {
				rng.Left.Offset = 0 // remove offset

				// adjust start and end time
				newEnd = newEnd.Add(-off)
				newStart = newStart.Add(-off)

			}
		}
	})
	return expr.String(), newStart, newEnd
}

func (in instance) Downstream(ctx context.Context, queries []logql.DownstreamQuery, acc logql.Accumulator) ([]logqlmodel.Result, error) {
	return in.For(ctx, queries, acc, func(qry logql.DownstreamQuery) (logqlmodel.Result, error) {
		var req queryrangebase.Request
		if in.splitAlign {
			qs, newStart, newEnd := withoutOffset(qry)
			req = ParamsToLokiRequest(qry.Params).WithQuery(qs).WithStartEnd(newStart, newEnd)
		} else {
			req = ParamsToLokiRequest(qry.Params).WithQuery(qry.Params.GetExpression().String())
		}
		sp, ctx := opentracing.StartSpanFromContext(ctx, "DownstreamHandler.instance")
		defer sp.Finish()
		sp.LogKV("shards", fmt.Sprintf("%+v", qry.Params.Shards()), "query", req.GetQuery(), "start", req.GetStart(), "end", req.GetEnd(), "step", req.GetStep(), "handler", reflect.TypeOf(in.handler), "engine", "downstream")

		res, err := in.handler.Do(ctx, req)
		if err != nil {
			return logqlmodel.Result{}, err
		}
		return ResponseToResult(res)
	})
}

// For runs a function against a list of queries, collecting the results or returning an error. The indices are preserved such that input[i] maps to output[i].
func (in instance) For(
	ctx context.Context,
	queries []logql.DownstreamQuery,
	acc logql.Accumulator,
	fn func(logql.DownstreamQuery) (logqlmodel.Result, error),
) ([]logqlmodel.Result, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := make(chan logql.Resp)

	// ForEachJob blocks until all are done. However, we want to process the
	// results as they come in. That's why we start everything in another
	// gorouting.
	go func() {
		err := concurrency.ForEachJob(ctx, len(queries), in.parallelism, func(ctx context.Context, i int) error {
			res, err := fn(queries[i])
			if err != nil {
				return err
			}
			response := logql.Resp{
				I:   i,
				Res: res,
			}

			// Feed the result into the channel unless the work has completed.
			select {
			case <-ctx.Done():
			case ch <- response:
			}
			return nil
		})
		if err != nil {
			ch <- logql.Resp{
				I:   -1,
				Err: err,
			}
		}
		close(ch)
	}()

	var err error
	for resp := range ch {
		if err != nil {
			continue
		}
		if resp.Err != nil {
			err = resp.Err
			continue
		}
		err = acc.Accumulate(ctx, resp.Res, resp.I)
	}

	return acc.Result(), err
}

// convert to matrix
func sampleStreamToMatrix(streams []queryrangebase.SampleStream) parser.Value {
	xs := make(promql.Matrix, 0, len(streams))
	for _, stream := range streams {
		x := promql.Series{}
		x.Metric = make(labels.Labels, 0, len(stream.Labels))
		for _, l := range stream.Labels {
			x.Metric = append(x.Metric, labels.Label(l))
		}

		x.Floats = make([]promql.FPoint, 0, len(stream.Samples))
		for _, sample := range stream.Samples {
			x.Floats = append(x.Floats, promql.FPoint{
				T: sample.TimestampMs,
				F: sample.Value,
			})
		}

		xs = append(xs, x)
	}
	return xs
}

func sampleStreamToVector(streams []queryrangebase.SampleStream) parser.Value {
	xs := make(promql.Vector, 0, len(streams))
	for _, stream := range streams {
		x := promql.Sample{}
		x.Metric = make(labels.Labels, 0, len(stream.Labels))
		for _, l := range stream.Labels {
			x.Metric = append(x.Metric, labels.Label(l))
		}

		x.T = stream.Samples[0].TimestampMs
		x.F = stream.Samples[0].Value

		xs = append(xs, x)
	}
	return xs
}
