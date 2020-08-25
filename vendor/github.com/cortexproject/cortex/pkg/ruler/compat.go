package ruler

import (
	"context"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/notifier"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/storage"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/ingester/client"
)

// Pusher is an ingester server that accepts pushes.
type Pusher interface {
	Push(context.Context, *client.WriteRequest) (*client.WriteResponse, error)
}

type pusherAppender struct {
	ctx     context.Context
	pusher  Pusher
	labels  []labels.Labels
	samples []client.Sample
	userID  string
}

func (a *pusherAppender) Add(l labels.Labels, t int64, v float64) (uint64, error) {
	a.labels = append(a.labels, l)
	a.samples = append(a.samples, client.Sample{
		TimestampMs: t,
		Value:       v,
	})
	return 0, nil
}

func (a *pusherAppender) AddFast(_ uint64, _ int64, _ float64) error {
	return storage.ErrNotFound
}

func (a *pusherAppender) Commit() error {
	// Since a.pusher is distributor, client.ReuseSlice will be called in a.pusher.Push.
	// We shouldn't call client.ReuseSlice here.
	_, err := a.pusher.Push(user.InjectOrgID(a.ctx, a.userID), client.ToWriteRequest(a.labels, a.samples, nil, client.RULE))
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
	pusher Pusher
	userID string
}

// Appender returns a storage.Appender
func (t *PusherAppendable) Appender(ctx context.Context) storage.Appender {
	return &pusherAppender{
		ctx:    ctx,
		pusher: t.pusher,
		userID: t.userID,
	}
}

// engineQueryFunc returns a new query function using the rules.EngineQueryFunc function
// and passing an altered timestamp.
func engineQueryFunc(engine *promql.Engine, q storage.Queryable, delay time.Duration) rules.QueryFunc {
	orig := rules.EngineQueryFunc(engine, q)
	return func(ctx context.Context, qs string, t time.Time) (promql.Vector, error) {
		return orig(ctx, qs, t.Add(-delay))
	}
}

type ManagerFactory = func(
	ctx context.Context,
	userID string,
	notifier *notifier.Manager,
	logger log.Logger,
	reg prometheus.Registerer,
) *rules.Manager

func DefaultTenantManagerFactory(
	cfg Config,
	p Pusher,
	q storage.Queryable,
	engine *promql.Engine,
) ManagerFactory {
	return func(
		ctx context.Context,
		userID string,
		notifier *notifier.Manager,
		logger log.Logger,
		reg prometheus.Registerer,
	) *rules.Manager {
		return rules.NewManager(&rules.ManagerOptions{
			Appendable:      &PusherAppendable{pusher: p, userID: userID},
			Queryable:       q,
			QueryFunc:       engineQueryFunc(engine, q, cfg.EvaluationDelay),
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
