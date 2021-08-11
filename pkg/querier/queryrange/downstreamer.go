package queryrange

import (
	"context"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel"
)

const (
	DefaultDownstreamConcurrency = 32
)

type DownstreamHandler struct {
	next queryrange.Handler
}

func ParamsToLokiRequest(params logql.Params, shards logql.Shards) queryrange.Request {
	if params.Start() == params.End() {
		return &LokiInstantRequest{
			Query:     params.Query(),
			Limit:     params.Limit(),
			TimeTs:    params.Start(),
			Direction: params.Direction(),
			Path:      "/loki/api/v1/query", // TODO(owen-d): make this derivable
			Shards:    shards.Encode(),
		}
	}
	return &LokiRequest{
		Query:     params.Query(),
		Limit:     params.Limit(),
		Step:      int64(params.Step() / time.Millisecond),
		StartTs:   params.Start(),
		EndTs:     params.End(),
		Direction: params.Direction(),
		Path:      "/loki/api/v1/query_range", // TODO(owen-d): make this derivable
		Shards:    shards.Encode(),
	}
}

func (h DownstreamHandler) Downstreamer() logql.Downstreamer {
	p := DefaultDownstreamConcurrency
	locks := make(chan struct{}, p)
	for i := 0; i < p; i++ {
		locks <- struct{}{}
	}
	return &instance{
		parallelism: p,
		locks:       locks,
		handler:     h.next,
	}
}

// instance is an intermediate struct for controlling concurrency across a single query
type instance struct {
	parallelism int
	locks       chan struct{}
	handler     queryrange.Handler
}

func (in instance) Downstream(ctx context.Context, queries []logql.DownstreamQuery) ([]logqlmodel.Result, error) {
	return in.For(ctx, queries, func(qry logql.DownstreamQuery) (logqlmodel.Result, error) {
		req := ParamsToLokiRequest(qry.Params, qry.Shards).WithQuery(qry.Expr.String())
		logger, ctx := spanlogger.New(ctx, "DownstreamHandler.instance")
		defer logger.Finish()
		level.Debug(logger).Log("shards", fmt.Sprintf("%+v", qry.Shards), "query", req.GetQuery(), "step", req.GetStep())

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
	fn func(logql.DownstreamQuery) (logqlmodel.Result, error),
) ([]logqlmodel.Result, error) {
	type resp struct {
		i   int
		res logqlmodel.Result
		err error
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch := make(chan resp)

	// Make one goroutine to dispatch the other goroutines, bounded by instance parallelism
	go func() {
		for i := 0; i < len(queries); i++ {
			select {
			case <-ctx.Done():
				break
			case <-in.locks:
				go func(i int) {
					// release lock back into pool
					defer func() {
						in.locks <- struct{}{}
					}()

					res, err := fn(queries[i])
					response := resp{
						i:   i,
						res: res,
						err: err,
					}

					// Feed the result into the channel unless the work has completed.
					select {
					case <-ctx.Done():
					case ch <- response:
					}
				}(i)
			}
		}
	}()

	results := make([]logqlmodel.Result, len(queries))
	for i := 0; i < len(queries); i++ {
		resp := <-ch
		if resp.err != nil {
			return nil, resp.err
		}
		results[resp.i] = resp.res
	}
	return results, nil
}

// convert to matrix
func sampleStreamToMatrix(streams []queryrange.SampleStream) parser.Value {
	xs := make(promql.Matrix, 0, len(streams))
	for _, stream := range streams {
		x := promql.Series{}
		x.Metric = make(labels.Labels, 0, len(stream.Labels))
		for _, l := range stream.Labels {
			x.Metric = append(x.Metric, labels.Label(l))
		}

		x.Points = make([]promql.Point, 0, len(stream.Samples))
		for _, sample := range stream.Samples {
			x.Points = append(x.Points, promql.Point{
				T: sample.TimestampMs,
				V: sample.Value,
			})
		}

		xs = append(xs, x)
	}
	return xs
}

func sampleStreamToVector(streams []queryrange.SampleStream) parser.Value {
	xs := make(promql.Vector, 0, len(streams))
	for _, stream := range streams {
		x := promql.Sample{}
		x.Metric = make(labels.Labels, 0, len(stream.Labels))
		for _, l := range stream.Labels {
			x.Metric = append(x.Metric, labels.Label(l))
		}

		x.Point = promql.Point{
			T: stream.Samples[0].TimestampMs,
			V: stream.Samples[0].Value,
		}

		xs = append(xs, x)
	}
	return xs
}

func ResponseToResult(resp queryrange.Response) (logqlmodel.Result, error) {
	switch r := resp.(type) {
	case *LokiResponse:
		if r.Error != "" {
			return logqlmodel.Result{}, fmt.Errorf("%s: %s", r.ErrorType, r.Error)
		}

		streams := make(logqlmodel.Streams, 0, len(r.Data.Result))

		for _, stream := range r.Data.Result {
			streams = append(streams, stream)
		}

		return logqlmodel.Result{
			Statistics: r.Statistics,
			Data:       streams,
		}, nil

	case *LokiPromResponse:
		if r.Response.Error != "" {
			return logqlmodel.Result{}, fmt.Errorf("%s: %s", r.Response.ErrorType, r.Response.Error)
		}
		if r.Response.Data.ResultType == loghttp.ResultTypeVector {
			return logqlmodel.Result{
				Statistics: r.Statistics,
				Data:       sampleStreamToVector(r.Response.Data.Result),
			}, nil
		}
		return logqlmodel.Result{
			Statistics: r.Statistics,
			Data:       sampleStreamToMatrix(r.Response.Data.Result),
		}, nil

	default:
		return logqlmodel.Result{}, fmt.Errorf("cannot decode (%T)", resp)
	}
}
