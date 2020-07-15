package ruler

import (
	"context"
	"time"

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
type appendable struct {
	pusher  Pusher
	labels  []labels.Labels
	samples []client.Sample
	userID  string
}

func (a *appendable) Add(l labels.Labels, t int64, v float64) (uint64, error) {
	a.labels = append(a.labels, l)
	a.samples = append(a.samples, client.Sample{
		TimestampMs: t,
		Value:       v,
	})
	return 0, nil
}

func (a *appendable) AddFast(_ uint64, _ int64, _ float64) error {
	return storage.ErrNotFound
}

func (a *appendable) Commit() error {
	// Since a.pusher is distributor, client.ReuseSlice will be called in a.pusher.Push.
	// We shouldn't call client.ReuseSlice here.
	_, err := a.pusher.Push(user.InjectOrgID(context.Background(), a.userID), client.ToWriteRequest(a.labels, a.samples, nil, client.RULE))
	a.labels = nil
	a.samples = nil
	return err
}

func (a *appendable) Rollback() error {
	a.labels = nil
	a.samples = nil
	return nil
}

// appender fulfills the storage.Appendable interface for prometheus manager
type appender struct {
	pusher Pusher
	userID string
}

// Appender returns a storage.Appender
func (t *appender) Appender() storage.Appender {
	return &appendable{
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
