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
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/iter"

	"github.com/grafana/loki/pkg/push"
)

func TestSweepInstance(t *testing.T) {
	replicationSet := ring.ReplicationSet{
		Instances: []ring.InstanceDesc{
			{Id: "localhost", Addr: "ingester0"},
			{Id: "remotehost", Addr: "ingester1"},
			{Id: "otherhost", Addr: "ingester2"},
		},
	}

	fakeRing := &fakeRing{}
	fakeRing.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(replicationSet, nil)

	ringClient := &fakeRingClient{
		ring: fakeRing,
	}

	ing, err := New(defaultIngesterTestConfig(t), ringClient, "foo", nil, log.NewNopLogger())
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

type fakeRingClient struct {
	ring ring.ReadRing
}

func (f *fakeRingClient) Pool() *ring_client.Pool {
	panic("not implemented")
}

func (f *fakeRingClient) StartAsync(_ context.Context) error {
	panic("not implemented")
}

func (f *fakeRingClient) AwaitRunning(_ context.Context) error {
	panic("not implemented")
}

func (f *fakeRingClient) StopAsync() {
	panic("not implemented")
}

func (f *fakeRingClient) AwaitTerminated(_ context.Context) error {
	panic("not implemented")
}

func (f *fakeRingClient) FailureCase() error {
	panic("not implemented")
}

func (f *fakeRingClient) State() services.State {
	panic("not implemented")
}

func (f *fakeRingClient) AddListener(_ services.Listener) {
	panic("not implemented")
}

func (f *fakeRingClient) Ring() ring.ReadRing {
	return f.ring
}

type fakeRing struct {
	mock.Mock
}

// InstancesWithTokensCount returns the number of instances in the ring that have tokens.
func (f *fakeRing) InstancesWithTokensCount() int {
	args := f.Called()
	return args.Int(0)
}

// InstancesInZoneCount returns the number of instances in the ring that are registered in given zone.
func (f *fakeRing) InstancesInZoneCount(zone string) int {
	args := f.Called(zone)
	return args.Int(0)
}

// InstancesWithTokensInZoneCount returns the number of instances in the ring that are registered in given zone and have tokens.
func (f *fakeRing) InstancesWithTokensInZoneCount(zone string) int {
	args := f.Called(zone)
	return args.Int(0)
}

// ZonesCount returns the number of zones for which there's at least 1 instance registered in the ring.
func (f *fakeRing) ZonesCount() int {
	args := f.Called()
	return args.Int(0)
}

func (f *fakeRing) Get(
	key uint32,
	op ring.Operation,
	bufInstances []ring.InstanceDesc,
	bufStrings1, bufStrings2 []string,
) (ring.ReplicationSet, error) {
	args := f.Called(key, op, bufInstances, bufStrings1, bufStrings2)
	return args.Get(0).(ring.ReplicationSet), args.Error(1)
}

func (f *fakeRing) GetAllHealthy(op ring.Operation) (ring.ReplicationSet, error) {
	args := f.Called(op)
	return args.Get(0).(ring.ReplicationSet), args.Error(1)
}

func (f *fakeRing) GetReplicationSetForOperation(op ring.Operation) (ring.ReplicationSet, error) {
	args := f.Called(op)
	return args.Get(0).(ring.ReplicationSet), args.Error(1)
}

func (f *fakeRing) ReplicationFactor() int {
	args := f.Called()
	return args.Int(0)
}

func (f *fakeRing) InstancesCount() int {
	args := f.Called()
	return args.Int(0)
}

func (f *fakeRing) ShuffleShard(identifier string, size int) ring.ReadRing {
	args := f.Called(identifier, size)
	return args.Get(0).(ring.ReadRing)
}

func (f *fakeRing) GetInstanceState(instanceID string) (ring.InstanceState, error) {
	args := f.Called(instanceID)
	return args.Get(0).(ring.InstanceState), args.Error(1)
}

func (f *fakeRing) ShuffleShardWithLookback(
	identifier string,
	size int,
	lookbackPeriod time.Duration,
	now time.Time,
) ring.ReadRing {
	args := f.Called(identifier, size, lookbackPeriod, now)
	return args.Get(0).(ring.ReadRing)
}

func (f *fakeRing) HasInstance(instanceID string) bool {
	args := f.Called(instanceID)
	return args.Bool(0)
}

func (f *fakeRing) CleanupShuffleShardCache(identifier string) {
	f.Called(identifier)
}

func (f *fakeRing) GetTokenRangesForInstance(identifier string) (ring.TokenRanges, error) {
	args := f.Called(identifier)
	return args.Get(0).(ring.TokenRanges), args.Error(1)
}
