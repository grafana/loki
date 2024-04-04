package pattern

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/iter"

	"github.com/grafana/loki/pkg/push"
)

func TestSweepInstance(t *testing.T) {
	ing, err := New(defaultIngesterTestConfig(t), "foo", prometheus.DefaultRegisterer, log.NewNopLogger())
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), ing) //nolint:errcheck
	err = services.StartAndAwaitRunning(context.Background(), ing)
	require.NoError(t, err)

	lbs := labels.New(labels.Label{Name: "test", Value: "test"})
	ctx := user.InjectOrgID(context.Background(), "foo")
	_, err = ing.Push(ctx, &push.PushRequest{
		Streams: []push.Stream{
			{
				Labels: lbs.String(),
				Entries: []push.Entry{
					{
						Timestamp: time.Unix(20, 0),
						Line:      "ts=1 msg=hello",
					},
				},
			},
			{
				Labels: `{test="test",foo="bar"}`,
				Entries: []push.Entry{
					{
						Timestamp: time.Now(),
						Line:      "ts=1 msg=foo",
					},
				},
			},
		},
	})
	require.NoError(t, err)

	inst, _ := ing.getInstanceByID("foo")

	it, err := inst.Iterator(ctx, &logproto.QueryPatternsRequest{
		Query: `{test="test"}`,
		Start: time.Unix(0, 0),
		End:   time.Unix(0, math.MaxInt64),
	})
	require.NoError(t, err)
	res, err := iter.ReadAll(it)
	require.NoError(t, err)
	require.Equal(t, 2, len(res.Series))
	ing.sweepUsers(true, true)
	it, err = inst.Iterator(ctx, &logproto.QueryPatternsRequest{
		Query: `{test="test"}`,
		Start: time.Unix(0, 0),
		End:   time.Unix(0, math.MaxInt64),
	})
	require.NoError(t, err)
	res, err = iter.ReadAll(it)
	require.NoError(t, err)
	require.Equal(t, 1, len(res.Series))
}

func defaultIngesterTestConfig(t testing.TB) Config {
	kvClient, err := kv.NewClient(kv.Config{Store: "inmemory"}, ring.GetCodec(), nil, log.NewNopLogger())
	require.NoError(t, err)

	cfg := Config{}
	flagext.DefaultValues(&cfg)
	cfg.FlushCheckPeriod = 99999 * time.Hour
	cfg.ConcurrentFlushes = 1
	cfg.LifecyclerConfig.RingConfig.KVStore.Mock = kvClient
	cfg.LifecyclerConfig.NumTokens = 1
	cfg.LifecyclerConfig.ListenPort = 0
	cfg.LifecyclerConfig.Addr = "localhost"
	cfg.LifecyclerConfig.ID = "localhost"
	cfg.LifecyclerConfig.FinalSleep = 0
	cfg.LifecyclerConfig.MinReadyDuration = 0

	return cfg
}
