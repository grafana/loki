package distributor

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv/consul"
	"github.com/stretchr/testify/assert"

	"github.com/grafana/dskit/ring"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

func TestInstanceCountDelegateCounting(t *testing.T) {
	counter := atomic.NewUint32(0)

	var delegate ring.BasicLifecyclerDelegate
	delegate = ring.NewInstanceRegisterDelegate(ring.ACTIVE, 1 /* tokenCount */)
	delegate = newHealthyInstanceDelegate(counter, time.Second, delegate)

	now := time.Now().Unix()
	for _, tc := range []struct {
		name      string
		ingesters *ring.Desc
		want      int
	}{
		{
			name: "with all instances as healthy",
			ingesters: &ring.Desc{
				Ingesters: map[string]ring.InstanceDesc{
					"ingester-0": {State: ring.ACTIVE, Timestamp: now},
					"ingester-1": {State: ring.ACTIVE, Timestamp: now},
					"ingester-2": {State: ring.ACTIVE, Timestamp: now},
					"ingester-3": {State: ring.ACTIVE, Timestamp: now},
					"ingester-4": {State: ring.ACTIVE, Timestamp: now},
				},
			},
			want: 5,
		},
		{
			name: "mixed instances are healthy",
			ingesters: &ring.Desc{
				Ingesters: map[string]ring.InstanceDesc{
					"ingester-0": {State: ring.JOINING, Timestamp: now},
					"ingester-1": {State: ring.LEAVING, Timestamp: now},
					"ingester-2": {State: ring.ACTIVE, Timestamp: now},
					"ingester-3": {State: ring.PENDING, Timestamp: now},
					"ingester-4": {State: ring.ACTIVE, Timestamp: now},
					"ingester-5": {State: ring.LEFT, Timestamp: now},
				},
			},
			want: 2,
		},
	} {

		t.Run(tc.name, func(t *testing.T) {
			counter.Store(0)
			delegate.OnRingInstanceHeartbeat(nil, tc.ingesters, nil)
			require.Equal(t, uint32(tc.want), counter.Load())
		})
	}
}

// sentryDelegate is a simple LifecyclerDelegate that will observe for all calls without affecting the chain of delegates.
type sentryDelegate struct {
	ring.BasicLifecyclerDelegate

	calls map[string]int
}

func (s *sentryDelegate) OnRingInstanceHeartbeat(lifecycler *ring.BasicLifecycler, ringDesc *ring.Desc, instanceDesc *ring.InstanceDesc) {
	s.calls["Heartbeat"] = 1
	s.BasicLifecyclerDelegate.OnRingInstanceHeartbeat(lifecycler, ringDesc, instanceDesc)
}

func (s *sentryDelegate) OnRingInstanceRegister(lifecycler *ring.BasicLifecycler, ringDesc ring.Desc, instanceExists bool, instanceID string, instanceDesc ring.InstanceDesc) (ring.InstanceState, ring.Tokens) {
	s.calls["Register"] = 1
	return s.BasicLifecyclerDelegate.OnRingInstanceRegister(lifecycler, ringDesc, instanceExists, instanceID, instanceDesc)
}

func (s *sentryDelegate) OnRingInstanceTokens(lifecycler *ring.BasicLifecycler, tokens ring.Tokens) {
	s.calls["Tokens"] = 1
	s.BasicLifecyclerDelegate.OnRingInstanceTokens(lifecycler, tokens)
}

func (s *sentryDelegate) OnRingInstanceStopping(lifecycler *ring.BasicLifecycler) {
	s.calls["Stopping"] = 1
	s.BasicLifecyclerDelegate.OnRingInstanceStopping(lifecycler)
}

func TestInstanceCountDelegate_CorrectlyInvokesOtherDelegates(t *testing.T) {
	counter := atomic.NewUint32(0)

	sentry1 := map[string]int{}
	sentry2 := map[string]int{}
	store, closer := consul.NewInMemoryClient(ring.GetCodec(), log.NewNopLogger(), nil)
	t.Cleanup(func() { assert.NoError(t, closer.Close()) })

	var delegate ring.BasicLifecyclerDelegate
	delegate = ring.NewInstanceRegisterDelegate(ring.ACTIVE, 1 /* tokenCount */)
	delegate = &sentryDelegate{BasicLifecyclerDelegate: delegate, calls: sentry1} // sentry delegate BEFORE newHealthyInstancesDelegate
	delegate = newHealthyInstanceDelegate(counter, time.Second, delegate)
	delegate = &sentryDelegate{BasicLifecyclerDelegate: delegate, calls: sentry2} // sentry delegate AFTER newHealthyInstancesDelegate

	lifecycler, err := ring.NewBasicLifecycler(ring.BasicLifecyclerConfig{}, "test-ring", "test-ring-key", store, delegate, log.NewNopLogger(), nil)
	require.NoError(t, err)

	ingesters := ring.NewDesc()
	ingesters.AddIngester("ingester-0", "ingester-0:3100", "zone-a", []uint32{1}, ring.ACTIVE, time.Now())

	// initial state.
	require.Equal(t, 0, sentry1["Heartbeat"])
	require.Equal(t, 0, sentry2["Heartbeat"])
	require.Equal(t, 0, sentry1["Register"])
	require.Equal(t, 0, sentry2["Register"])
	require.Equal(t, 0, sentry1["Stopping"])
	require.Equal(t, 0, sentry2["Stopping"])
	require.Equal(t, 0, sentry1["Tokens"])
	require.Equal(t, 0, sentry2["Tokens"])

	delegate.OnRingInstanceHeartbeat(lifecycler, ingesters, nil)
	require.Equal(t, 1, sentry1["Heartbeat"])
	require.Equal(t, 1, sentry2["Heartbeat"])

	delegate.OnRingInstanceRegister(lifecycler, *ingesters, true, "ingester-0", ring.InstanceDesc{})
	require.Equal(t, 1, sentry1["Register"])
	require.Equal(t, 1, sentry2["Register"])

	delegate.OnRingInstanceStopping(lifecycler)
	require.Equal(t, 1, sentry1["Stopping"])
	require.Equal(t, 1, sentry2["Stopping"])

	delegate.OnRingInstanceTokens(lifecycler, ring.Tokens{})
	require.Equal(t, 1, sentry1["Stopping"])
	require.Equal(t, 1, sentry2["Stopping"])
}
