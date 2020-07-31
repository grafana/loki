package manager

import (
	"context"
	"time"

	"github.com/cortexproject/cortex/pkg/ruler"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/notifier"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/rules"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
)

// engineQueryFunc returns a new query function using the rules.EngineQueryFunc function
// and passing an altered timestamp.
func engineQueryFunc(engine *logql.Engine, delay time.Duration) rules.QueryFunc {
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

func MemstoreTenantManager(
	cfg ruler.Config,
	engine *logql.Engine,
) ruler.ManagerFactory {
	var metrics *Metrics

	return func(
		ctx context.Context,
		userID string,
		notifier *notifier.Manager,
		logger log.Logger,
		reg prometheus.Registerer,
	) *rules.Manager {

		// We'll ignore the passed registere and use the default registerer to avoid prefix issues and other weirdness.
		// This closure prevents re-registering.
		if metrics == nil {
			metrics = NewMetrics(prometheus.DefaultRegisterer)
		}
		logger = log.With(logger, "user", userID)
		queryFunc := engineQueryFunc(engine, cfg.EvaluationDelay)
		memStore := NewMemStore(userID, queryFunc, metrics, 5*time.Minute, log.With(logger, "subcomponent", "MemStore"))

		mgr := rules.NewManager(&rules.ManagerOptions{
			Appendable:      NoopAppender{},
			Queryable:       memStore,
			QueryFunc:       queryFunc,
			Context:         user.InjectOrgID(ctx, userID),
			ExternalURL:     cfg.ExternalURL.URL,
			NotifyFunc:      ruler.SendAlerts(notifier, cfg.ExternalURL.URL.String()),
			Logger:          logger,
			Registerer:      reg,
			OutageTolerance: cfg.OutageTolerance,
			ForGracePeriod:  cfg.ForGracePeriod,
			ResendDelay:     cfg.ResendDelay,
			GroupLoader:     groupLoader{},
		})

		// initialize memStore, bound to the manager's alerting rules
		memStore.Start(mgr)

		return mgr
	}
}

type groupLoader struct {
	rules.FileLoader // embed the default and override the parse method for logql queries
}

func (groupLoader) Parse(query string) (parser.Expr, error) {
	expr, err := logql.ParseExpr(query)
	if err != nil {
		return nil, err
	}

	return exprAdapter{expr}, nil
}

// Allows logql expressions to be treated as promql expressions by the prometheus rules pkg.
type exprAdapter struct {
	logql.Expr
}

func (exprAdapter) PositionRange() parser.PositionRange { return parser.PositionRange{} }
func (exprAdapter) PromQLExpr()                         {}
func (exprAdapter) Type() parser.ValueType              { return parser.ValueType("unimplemented") }
