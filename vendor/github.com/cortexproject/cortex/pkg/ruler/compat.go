package ruler

import (
	"context"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ruler/rules"
)

// AppendableHistoryFunc allows creation of an Appendable and AlertHistory to be joined. The default implementation
// does not take advantage of this option.
type AppendableHistoryFunc = func(userID string, opts *rules.ManagerOptions) (rules.Appendable, rules.TenantAlertHistory)

// This is the default implementation which returns an unlinked Appendable/AlertHistory.
func DefaultAppendableHistoryFunc(p Pusher, q storage.Queryable) AppendableHistoryFunc {
	return func(userID string, opts *rules.ManagerOptions) (rules.Appendable, rules.TenantAlertHistory) {
		return &appender{
			pusher: p,
			userID: userID,
		}, rules.NewMetricsHistory(q, opts)
	}
}

// Pusher is an ingester server that accepts pushes.
type Pusher interface {
	Push(context.Context, *client.WriteRequest) (*client.WriteResponse, error)
}

type appender struct {
	pusher  Pusher
	labels  []labels.Labels
	samples []client.Sample
	userID  string
}

func (a *appender) Appender(_ rules.Rule) (storage.Appender, error) { return a, nil }

func (a *appender) Add(l labels.Labels, t int64, v float64) (uint64, error) {
	a.labels = append(a.labels, l)
	a.samples = append(a.samples, client.Sample{
		TimestampMs: t,
		Value:       v,
	})
	return 0, nil
}

func (a *appender) AddFast(_ uint64, _ int64, _ float64) error {
	return storage.ErrNotFound
}

func (a *appender) Commit() error {
	// Since a.pusher is distributor, client.ReuseSlice will be called in a.pusher.Push.
	// We shouldn't call client.ReuseSlice here.
	_, err := a.pusher.Push(user.InjectOrgID(context.Background(), a.userID), client.ToWriteRequest(a.labels, a.samples, nil, client.RULE))
	a.labels = nil
	a.samples = nil
	return err
}

func (a *appender) Rollback() error {
	a.labels = nil
	a.samples = nil
	return nil
}

// PromDelayedQueryFunc returns a DelayedQueryFunc bound to a promql engine.
func PromDelayedQueryFunc(engine *promql.Engine, q storage.Queryable) DelayedQueryFunc {
	return func(delay time.Duration) rules.QueryFunc {
		orig := rules.EngineQueryFunc(engine, q)
		return func(ctx context.Context, qs string, t time.Time) (promql.Vector, error) {
			return orig(ctx, qs, t.Add(-delay))
		}
	}
}

// DelayedQueryFunc consumes a queryable and a delay, returning a Queryfunc which
// takes this delay into account when executing against the queryable.
type DelayedQueryFunc = func(time.Duration) rules.QueryFunc
