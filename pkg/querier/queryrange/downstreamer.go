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

	"github.com/grafana/loki/pkg/logql"
)

const (
	DefaultDownstreamConcurrency = 32
)

type DownstreamHandler struct {
	next queryrange.Handler
}

func ParamsToLokiRequest(params logql.Params) *LokiRequest {
	return &LokiRequest{
		Query:     params.Query(),
		Limit:     params.Limit(),
		Step:      int64(params.Step() / time.Millisecond),
		StartTs:   params.Start(),
		EndTs:     params.End(),
		Direction: params.Direction(),
		Path:      "/loki/api/v1/query_range", // TODO(owen-d): make this derivable
	}
}

func (h DownstreamHandler) Downstreamer() logql.Downstreamer {
	return newInstance(DefaultDownstreamConcurrency, h.next)
}

func newInstance(parallelism int, handler queryrange.Handler) *instance {
	locks := make(chan struct{}, parallelism)
	for i := 0; i < parallelism; i++ {
		locks <- struct{}{}
	}
	return &instance{
		parallelism: parallelism,
		locks:       locks,
		handler:     handler,
	}
}

// instance is an intermediate struct for controlling concurrency across a single query
type instance struct {
	parallelism int
	locks       chan struct{}
	handler     queryrange.Handler
}

func (in instance) Downstream(ctx context.Context, queries []logql.DownstreamQuery) ([]logql.Result, error) {
	input := make([]interface{}, 0, len(queries))
	for _, q := range queries {
		input = append(input, interface{}(q))
	}

	res, err := in.For(input, func(x interface{}) (interface{}, error) {
		qry, ok := x.(logql.DownstreamQuery)
		if !ok {
			return nil, fmt.Errorf("unexpected (%T) when trying to process DownstreamQuery", x)
		}

		req := ParamsToLokiRequest(qry.Params).WithShards(qry.Shards...).WithQuery(qry.Expr.String()).(*LokiRequest)
		logger, ctx := spanlogger.New(ctx, "DownstreamHandler.instance")
		defer logger.Finish()
		level.Debug(logger).Log("shards", fmt.Sprintf("%+v", req.Shards), "query", req.Query)

		res, err := in.handler.Do(ctx, req)
		if err != nil {
			return logql.Result{}, err
		}
		return ResponseToResult(res)
	})

	if err != nil {
		return nil, err
	}

	results := make([]logql.Result, 0, len(res))
	for _, x := range res {
		results = append(results, x.(logql.Result))
	}
	return results, nil
}

// For runs a function against a list of queries, collecting the results or returning an error. The indices are preserved such that input[i] maps to output[i].
func (in instance) For(
	queries []interface{},
	fn func(interface{}) (interface{}, error),
) ([]interface{}, error) {
	type resp struct {
		i   int
		res interface{}
		err error
	}

	done := make(chan struct{})
	defer close(done)

	ch := make(chan resp)

	// Make one goroutine to dispatch the other goroutines, bounded by instance parallelism
	go func() {
		for i := 0; i < len(queries); i++ {
			select {
			case <-done:
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
					case <-done:
					case ch <- response:
					}
				}(i)
			}
		}
	}()

	results := make([]interface{}, len(queries))
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

func ResponseToResult(resp queryrange.Response) (logql.Result, error) {
	switch r := resp.(type) {
	case *LokiResponse:
		if r.Error != "" {
			return logql.Result{}, fmt.Errorf("%s: %s", r.ErrorType, r.Error)
		}

		streams := make(logql.Streams, 0, len(r.Data.Result))

		for _, stream := range r.Data.Result {
			streams = append(streams, stream)
		}

		return logql.Result{
			Statistics: r.Statistics,
			Data:       streams,
		}, nil

	case *LokiPromResponse:
		if r.Response.Error != "" {
			return logql.Result{}, fmt.Errorf("%s: %s", r.Response.ErrorType, r.Response.Error)
		}

		return logql.Result{
			Statistics: r.Statistics,
			Data:       sampleStreamToMatrix(r.Response.Data.Result),
		}, nil

	default:
		return logql.Result{}, fmt.Errorf("cannot decode (%T)", resp)
	}
}
