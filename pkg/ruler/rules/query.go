package rules

import (
	"context"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
)

func LokiDelayedQueryFunc(engine *logql.Engine, delay time.Duration) rules.QueryFunc {
	return rules.QueryFunc(func(ctx context.Context, qs string, t time.Time) (promql.Vector, error) {
		adjusted := t.Add(-delay)
		params := logql.NewLiteralParams(
			qs,
			adjusted,
			adjusted,
			0,
			0,
			logproto.FORWARD,
			0,
			nil,
		)
		q := engine.Query(params)

		res, err := q.Exec(ctx)
		if err != nil {
			return nil, err
		}
		switch v := res.Data.(type) {
		case promql.Vector:
			return v, nil
		case promql.Scalar:
			return promql.Vector{promql.Sample{
				Point:  promql.Point(v),
				Metric: labels.Labels{},
			}}, nil
		default:
			return nil, errors.New("rule result is not a vector or scalar")
		}
	})
}
