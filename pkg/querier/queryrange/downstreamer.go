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

	"github.com/grafana/loki/pkg/logql"
)

type DownstreamHandler struct {
	next queryrange.Handler
}

type QuerierFunc func(context.Context) (logql.Result, error)

func (fn QuerierFunc) Exec(ctx context.Context) (logql.Result, error) {
	return fn(ctx)
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

func (h DownstreamHandler) Downstream(expr logql.Expr, params logql.Params, shards logql.Shards) (logql.Query, error) {
	req := ParamsToLokiRequest(params).WithShards(shards).WithQuery(expr.String()).(*LokiRequest)

	return QuerierFunc(func(ctx context.Context) (logql.Result, error) {

		logger, ctx := spanlogger.New(ctx, "DownstreamHandler")
		defer logger.Finish()
		level.Debug(logger).Log("shards", req.Shards, "query", req.Query)

		res, err := h.next.Do(ctx, req)
		if err != nil {
			return logql.Result{}, err
		}
		return ResponseToResult(res)
	}), nil
}

// convert to matrix
func sampleStreamToMatrix(streams []queryrange.SampleStream) promql.Value {
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
