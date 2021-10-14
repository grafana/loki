package ruler

import (
	"context"
	"errors"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/notifier"
	"github.com/prometheus/prometheus/pkg/exemplar"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/value"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/storage"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/cortexproject/cortex/pkg/querier"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
)

// Pusher is an ingester server that accepts pushes.
type Pusher interface {
	Push(context.Context, *cortexpb.WriteRequest) (*cortexpb.WriteResponse, error)
}

type PusherAppender struct {
	failedWrites prometheus.Counter
	totalWrites  prometheus.Counter

	ctx             context.Context
	pusher          Pusher
	labels          []labels.Labels
	samples         []cortexpb.Sample
	userID          string
	evaluationDelay time.Duration
}

func (a *PusherAppender) Append(_ uint64, l labels.Labels, t int64, v float64) (uint64, error) {
	a.labels = append(a.labels, l)

	// Adapt staleness markers for ruler evaluation delay. As the upstream code
	// is using the actual time, when there is a no longer available series.
	// This then causes 'out of order' append failures once the series is
	// becoming available again.
	// see https://github.com/prometheus/prometheus/blob/6c56a1faaaad07317ff585bda75b99bdba0517ad/rules/manager.go#L647-L660
	// Similar to staleness markers, the rule manager also appends actual time to the ALERTS and ALERTS_FOR_STATE series.
	// See: https://github.com/prometheus/prometheus/blob/ae086c73cb4d6db9e8b67d5038d3704fea6aec4a/rules/alerting.go#L414-L417
	metricName := l.Get(labels.MetricName)
	if a.evaluationDelay > 0 && (value.IsStaleNaN(v) || metricName == "ALERTS" || metricName == "ALERTS_FOR_STATE") {
		t -= a.evaluationDelay.Milliseconds()
	}

	a.samples = append(a.samples, cortexpb.Sample{
		TimestampMs: t,
		Value:       v,
	})
	return 0, nil
}

func (a *PusherAppender) AppendExemplar(_ uint64, _ labels.Labels, _ exemplar.Exemplar) (uint64, error) {
	return 0, errors.New("exemplars are unsupported")
}

func (a *PusherAppender) Commit() error {
	a.totalWrites.Inc()

	// Since a.pusher is distributor, client.ReuseSlice will be called in a.pusher.Push.
	// We shouldn't call client.ReuseSlice here.
	_, err := a.pusher.Push(user.InjectOrgID(a.ctx, a.userID), cortexpb.ToWriteRequest(a.labels, a.samples, nil, cortexpb.RULE))

	if err != nil {
		// Don't report errors that ended with 4xx HTTP status code (series limits, duplicate samples, out of order, etc.)
		if resp, ok := httpgrpc.HTTPResponseFromError(err); !ok || resp.Code/100 != 4 {
			a.failedWrites.Inc()
		}
	}

	a.labels = nil
	a.samples = nil
	return err
}

func (a *PusherAppender) Rollback() error {
	a.labels = nil
	a.samples = nil
	return nil
}

// PusherAppendable fulfills the storage.Appendable interface for prometheus manager
type PusherAppendable struct {
	pusher      Pusher
	userID      string
	rulesLimits RulesLimits

	totalWrites  prometheus.Counter
	failedWrites prometheus.Counter
}

func NewPusherAppendable(pusher Pusher, userID string, limits RulesLimits, totalWrites, failedWrites prometheus.Counter) *PusherAppendable {
	return &PusherAppendable{
		pusher:       pusher,
		userID:       userID,
		rulesLimits:  limits,
		totalWrites:  totalWrites,
		failedWrites: failedWrites,
	}
}

// Appender returns a storage.Appender
func (t *PusherAppendable) Appender(ctx context.Context) storage.Appender {
	return &PusherAppender{
		failedWrites: t.failedWrites,
		totalWrites:  t.totalWrites,

		ctx:             ctx,
		pusher:          t.pusher,
		userID:          t.userID,
		evaluationDelay: t.rulesLimits.EvaluationDelay(t.userID),
	}
}

// RulesLimits defines limits used by Ruler.
type RulesLimits interface {
	EvaluationDelay(userID string) time.Duration
	RulerTenantShardSize(userID string) int
	RulerMaxRuleGroupsPerTenant(userID string) int
	RulerMaxRulesPerRuleGroup(userID string) int
}

// EngineQueryFunc returns a new query function using the rules.EngineQueryFunc function
// and passing an altered timestamp.
func EngineQueryFunc(engine *promql.Engine, q storage.Queryable, overrides RulesLimits, userID string) rules.QueryFunc {
	return func(ctx context.Context, qs string, t time.Time) (promql.Vector, error) {
		orig := rules.EngineQueryFunc(engine, q)
		// Delay the evaluation of all rules by a set interval to give a buffer
		// to metric that haven't been forwarded to cortex yet.
		evaluationDelay := overrides.EvaluationDelay(userID)
		return orig(ctx, qs, t.Add(-evaluationDelay))
	}
}

func MetricsQueryFunc(qf rules.QueryFunc, queries, failedQueries prometheus.Counter) rules.QueryFunc {
	return func(ctx context.Context, qs string, t time.Time) (promql.Vector, error) {
		queries.Inc()
		result, err := qf(ctx, qs, t)

		// We only care about errors returned by underlying Queryable. Errors returned by PromQL engine are "user-errors",
		// and not interesting here.
		qerr := QueryableError{}
		if err != nil && errors.As(err, &qerr) {
			origErr := qerr.Unwrap()

			// Not all errors returned by Queryable are interesting, only those that would result in 500 status code.
			//
			// We rely on TranslateToPromqlApiError to do its job here... it returns nil, if err is nil.
			// It returns promql.ErrStorage, if error should be reported back as 500.
			// Other errors it returns are either for canceled or timed-out queriers (we're not reporting those as failures),
			// or various user-errors (limits, duplicate samples, etc. ... also not failures).
			//
			// All errors will still be counted towards "evaluation failures" metrics and logged by Prometheus Ruler,
			// but we only want internal errors here.
			if _, ok := querier.TranslateToPromqlAPIError(origErr).(promql.ErrStorage); ok {
				failedQueries.Inc()
			}

			// Return unwrapped error.
			return result, origErr
		}

		return result, err
	}
}

func RecordAndReportRuleQueryMetrics(qf rules.QueryFunc, queryTime prometheus.Counter, logger log.Logger) rules.QueryFunc {
	if queryTime == nil {
		return qf
	}

	return func(ctx context.Context, qs string, t time.Time) (promql.Vector, error) {
		// If we've been passed a counter we want to record the wall time spent executing this request.
		timer := prometheus.NewTimer(nil)
		defer func() {
			querySeconds := timer.ObserveDuration().Seconds()
			queryTime.Add(querySeconds)

			// Log ruler query stats.
			logMessage := []interface{}{
				"msg", "query stats",
				"component", "ruler",
				"cortex_ruler_query_seconds_total", querySeconds,
				"query", qs,
			}
			level.Info(util_log.WithContext(ctx, logger)).Log(logMessage...)
		}()

		result, err := qf(ctx, qs, t)
		return result, err
	}
}

// This interface mimicks rules.Manager API. Interface is used to simplify tests.
type RulesManager interface {
	// Starts rules manager. Blocks until Stop is called.
	Run()

	// Stops rules manager. (Unblocks Run.)
	Stop()

	// Updates rules manager state.
	Update(interval time.Duration, files []string, externalLabels labels.Labels, externalURL string) error

	// Returns current rules groups.
	RuleGroups() []*rules.Group
}

// ManagerFactory is a function that creates new RulesManager for given user and notifier.Manager.
type ManagerFactory func(ctx context.Context, userID string, notifier *notifier.Manager, logger log.Logger, reg prometheus.Registerer) RulesManager

func DefaultTenantManagerFactory(cfg Config, p Pusher, q storage.Queryable, engine *promql.Engine, overrides RulesLimits, reg prometheus.Registerer) ManagerFactory {
	totalWrites := promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Name: "cortex_ruler_write_requests_total",
		Help: "Number of write requests to ingesters.",
	})
	failedWrites := promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Name: "cortex_ruler_write_requests_failed_total",
		Help: "Number of failed write requests to ingesters.",
	})

	totalQueries := promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Name: "cortex_ruler_queries_total",
		Help: "Number of queries executed by ruler.",
	})
	failedQueries := promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Name: "cortex_ruler_queries_failed_total",
		Help: "Number of failed queries by ruler.",
	})
	var rulerQuerySeconds *prometheus.CounterVec
	if cfg.EnableQueryStats {
		rulerQuerySeconds = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "cortex_ruler_query_seconds_total",
			Help: "Total amount of wall clock time spent processing queries by the ruler.",
		}, []string{"user"})
	}

	// Wrap errors returned by Queryable to our wrapper, so that we can distinguish between those errors
	// and errors returned by PromQL engine. Errors from Queryable can be either caused by user (limits) or internal errors.
	// Errors from PromQL are always "user" errors.
	q = querier.NewErrorTranslateQueryableWithFn(q, WrapQueryableErrors)

	return func(ctx context.Context, userID string, notifier *notifier.Manager, logger log.Logger, reg prometheus.Registerer) RulesManager {
		var queryTime prometheus.Counter = nil
		if rulerQuerySeconds != nil {
			queryTime = rulerQuerySeconds.WithLabelValues(userID)
		}

		return rules.NewManager(&rules.ManagerOptions{
			Appendable:      NewPusherAppendable(p, userID, overrides, totalWrites, failedWrites),
			Queryable:       q,
			QueryFunc:       RecordAndReportRuleQueryMetrics(MetricsQueryFunc(EngineQueryFunc(engine, q, overrides, userID), totalQueries, failedQueries), queryTime, logger),
			Context:         user.InjectOrgID(ctx, userID),
			ExternalURL:     cfg.ExternalURL.URL,
			NotifyFunc:      SendAlerts(notifier, cfg.ExternalURL.URL.String()),
			Logger:          log.With(logger, "user", userID),
			Registerer:      reg,
			OutageTolerance: cfg.OutageTolerance,
			ForGracePeriod:  cfg.ForGracePeriod,
			ResendDelay:     cfg.ResendDelay,
		})
	}
}

type QueryableError struct {
	err error
}

func (q QueryableError) Unwrap() error {
	return q.err
}

func (q QueryableError) Error() string {
	return q.err.Error()
}

func WrapQueryableErrors(err error) error {
	if err == nil {
		return err
	}

	return QueryableError{err: err}
}
