package manager

import (
	"context"
	"time"

	"github.com/cortexproject/cortex/pkg/ruler"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
)

func LokiDelayedQueryFunc(engine *logql.Engine) ruler.DelayedQueryFunc {
	return func(delay time.Duration) rules.QueryFunc {
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

}

// func MemstoreTenantManager(
// 	cfg ruler.Config,
// 	queryFunc ruler.DelayedQueryFunc,
// ) ruler.TenantManagerFunc {
// 	metrics := NewMetrics()

// 	return ruler.TenantOptionsFunc(func(
// 		ctx context.Context,
// 		userID string,
// 		notifier *notifier.Manager,
// 		logger log.Logger,
// 		reg prometheus.Registerer,
// 	) *rules.Manager {
// 		return &rules.ManagerOptions{
// 			Appendable:      NoopAppender{},
// 			Queryable:       q,
// 			QueryFunc:       queryFunc(cfg.EvaluationDelay),
// 			Context:         user.InjectOrgID(ctx, userID),
// 			ExternalURL:     cfg.ExternalURL.URL,
// 			NotifyFunc:      sendAlerts(notifier, cfg.ExternalURL.URL.String()),
// 			Logger:          log.With(logger, "user", userID),
// 			Registerer:      reg,
// 			OutageTolerance: cfg.OutageTolerance,
// 			ForGracePeriod:  cfg.ForGracePeriod,
// 			ResendDelay:     cfg.ResendDelay,
// 		}
// 	})
// }
