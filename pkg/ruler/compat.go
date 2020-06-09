package ruler

import (
	"context"
	"errors"
	"time"

	"github.com/cortexproject/cortex/pkg/ruler"
	"github.com/cortexproject/cortex/pkg/ruler/rules"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
)

func LokiDelayedQueryFunc(engine *logql.Engine) ruler.DelayedQueryFunc {
	return func(delay time.Duration) rules.QueryFunc {
		return func(ctx context.Context, qs string, t time.Time) (promql.Vector, error) {
			adjusted := t.Add(-delay)
			q := engine.Query(logql.NewLiteralParams(
				qs,
				adjusted,
				adjusted,
				0,
				0,
				logproto.BACKWARD,
				0,
				nil,
			))

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
		}
	}
}

func InMemoryAppendableHistory(userID string, opts *rules.ManagerOptions) (rules.Appendable, rules.AlertHistory) {
	hist := NewMemHistory(userID, opts)
	return hist, hist
}

type NoopAppender struct{}

func (a NoopAppender) Appender() (storage.Appender, error)                     { return a, nil }
func (a NoopAppender) Add(l labels.Labels, t int64, v float64) (uint64, error) { return 0, nil }
func (a NoopAppender) AddFast(ref uint64, t int64, v float64) error {
	return errors.New("unimplemented")
}
func (a NoopAppender) Commit() error   { return nil }
func (a NoopAppender) Rollback() error { return nil }
