package ruler

import (
	"context"
	"errors"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/notifier"
	"github.com/prometheus/prometheus/pkg/exemplar"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/value"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/storage"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/cortexpb"
)

// Pusher is an ingester server that accepts pushes.
type Pusher interface {
	Push(context.Context, *cortexpb.WriteRequest) (*cortexpb.WriteResponse, error)
}

type pusherAppender struct {
	ctx             context.Context
	pusher          Pusher
	labels          []labels.Labels
	samples         []cortexpb.Sample
	userID          string
	evaluationDelay time.Duration
}

func (a *pusherAppender) Append(_ uint64, l labels.Labels, t int64, v float64) (uint64, error) {
	a.labels = append(a.labels, l)

	// Adapt staleness markers for ruler evaluation delay. As the upstream code
	// is using the actual time, when there is a no longer available series.
	// This then causes 'out of order' append failures once the series is
	// becoming available again.
	// see https://github.com/prometheus/prometheus/blob/6c56a1faaaad07317ff585bda75b99bdba0517ad/rules/manager.go#L647-L660
	if a.evaluationDelay > 0 && value.IsStaleNaN(v) {
		t -= a.evaluationDelay.Milliseconds()
	}

	a.samples = append(a.samples, cortexpb.Sample{
		TimestampMs: t,
		Value:       v,
	})
	return 0, nil
}

func (a *pusherAppender) AppendExemplar(_ uint64, _ labels.Labels, _ exemplar.Exemplar) (uint64, error) {
	return 0, errors.New("exemplars are unsupported")
}

func (a *pusherAppender) Commit() error {
	// Since a.pusher is distributor, client.ReuseSlice will be called in a.pusher.Push.
	// We shouldn't call client.ReuseSlice here.
	_, err := a.pusher.Push(user.InjectOrgID(a.ctx, a.userID), cortexpb.ToWriteRequest(a.labels, a.samples, nil, cortexpb.RULE))
	a.labels = nil
	a.samples = nil
	return err
}

func (a *pusherAppender) Rollback() error {
	a.labels = nil
	a.samples = nil
	return nil
}

// PusherAppendable fulfills the storage.Appendable interface for prometheus manager
type PusherAppendable struct {
	pusher      Pusher
	userID      string
	rulesLimits RulesLimits
}

// Appender returns a storage.Appender
func (t *PusherAppendable) Appender(ctx context.Context) storage.Appender {
	return &pusherAppender{
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

// engineQueryFunc returns a new query function using the rules.EngineQueryFunc function
// and passing an altered timestamp.
func engineQueryFunc(engine *promql.Engine, q storage.Queryable, overrides RulesLimits, userID string) rules.QueryFunc {
	return func(ctx context.Context, qs string, t time.Time) (promql.Vector, error) {
		orig := rules.EngineQueryFunc(engine, q)
		// Delay the evaluation of all rules by a set interval to give a buffer
		// to metric that haven't been forwarded to cortex yet.
		evaluationDelay := overrides.EvaluationDelay(userID)
		return orig(ctx, qs, t.Add(-evaluationDelay))
	}
}

// This interface mimicks rules.Manager API. Interface is used to simplify tests.
type RulesManager interface {
	// Starts rules manager. Blocks until Stop is called.
	Run()

	// Stops rules manager. (Unblocks Run.)
	Stop()

	// Updates rules manager state.
	Update(interval time.Duration, files []string, externalLabels labels.Labels) error

	// Returns current rules groups.
	RuleGroups() []*rules.Group
}

// ManagerFactory is a function that creates new RulesManager for given user and notifier.Manager.
type ManagerFactory func(ctx context.Context, userID string, notifier *notifier.Manager, logger log.Logger, reg prometheus.Registerer) RulesManager

func DefaultTenantManagerFactory(cfg Config, p Pusher, q storage.Queryable, engine *promql.Engine, overrides RulesLimits) ManagerFactory {
	return func(ctx context.Context, userID string, notifier *notifier.Manager, logger log.Logger, reg prometheus.Registerer) RulesManager {
		return rules.NewManager(&rules.ManagerOptions{
			Appendable:      &PusherAppendable{pusher: p, userID: userID, rulesLimits: overrides},
			Queryable:       q,
			QueryFunc:       engineQueryFunc(engine, q, overrides, userID),
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
