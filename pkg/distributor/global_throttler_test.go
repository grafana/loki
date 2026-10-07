package distributor

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

// newTestGlobalThrottler builds a globalThrottler for unit-testing applyAddresses directly,
// without going through newGlobalThrottler's real DNS resolution. The addresses given to
// ShardedClient are never dialed eagerly (connections are opened lazily, on first use per
// shard -- see the client's own docs), so this never touches the network.
func newTestGlobalThrottler() *globalThrottler {
	return &globalThrottler{
		cfg: GlobalThrottlerConfig{
			Timeout:                 10 * time.Millisecond,
			FailOpen:                true,
			BreakerFailureThreshold: 5,
			BreakerOpenDuration:     time.Second,
		},
		logger: log.NewNopLogger(),
	}
}

func TestGlobalThrottler_ApplyAddresses(t *testing.T) {
	t.Run("zero addresses is an error and leaves state untouched", func(t *testing.T) {
		gt := newTestGlobalThrottler()

		err := gt.applyAddresses(nil)
		require.Error(t, err)
		require.Nil(t, gt.client.Load())
		require.Nil(t, gt.lastAddrs)
	})

	t.Run("blank addresses are an error, not a client dialing nothing", func(t *testing.T) {
		gt := newTestGlobalThrottler()

		require.Error(t, gt.applyAddresses([]string{""}))
		require.Error(t, gt.applyAddresses([]string{" ", ""}))
		require.Nil(t, gt.client.Load())
		require.Nil(t, gt.lastAddrs)
	})

	t.Run("blank entries are dropped from an otherwise valid set", func(t *testing.T) {
		gt := newTestGlobalThrottler()

		require.NoError(t, gt.applyAddresses([]string{"b:2", "", " a:1 "}))
		require.Equal(t, []string{"a:1", "b:2"}, gt.lastAddrs)
	})

	t.Run("first call builds a client", func(t *testing.T) {
		gt := newTestGlobalThrottler()

		require.NoError(t, gt.applyAddresses([]string{"b:2", "a:1"}))
		require.NotNil(t, gt.client.Load())
		require.Equal(t, []string{"a:1", "b:2"}, gt.lastAddrs) // sorted
	})

	t.Run("the same set in a different order is a no-op, not a rebuild", func(t *testing.T) {
		gt := newTestGlobalThrottler()
		require.NoError(t, gt.applyAddresses([]string{"a:1", "b:2"}))
		first := gt.client.Load()

		require.NoError(t, gt.applyAddresses([]string{"b:2", "a:1"}))
		require.Same(t, first, gt.client.Load(), "an unordered re-resolution of the same set must not rebuild the client")
	})

	t.Run("a genuinely different set swaps in a fresh client", func(t *testing.T) {
		gt := newTestGlobalThrottler()
		require.NoError(t, gt.applyAddresses([]string{"a:1", "b:2"}))
		first := gt.client.Load()

		require.NoError(t, gt.applyAddresses([]string{"a:1", "c:3"}))
		require.NotSame(t, first, gt.client.Load())
		require.Equal(t, []string{"a:1", "c:3"}, gt.lastAddrs)
	})
}

func TestThrottlerClientRef(t *testing.T) {
	t.Run("retire waits for an in-flight call and then rejects new ones", func(t *testing.T) {
		gt := newTestGlobalThrottler()
		require.NoError(t, gt.applyAddresses([]string{"a:1"}))
		ref := gt.client.Load()

		ref.mu.RLock() // simulate a call in flight
		retired := make(chan struct{})
		go func() {
			ref.retire()
			close(retired)
		}()

		select {
		case <-retired:
			t.Fatal("retire closed the client while a call was in flight")
		case <-time.After(50 * time.Millisecond):
		}

		ref.mu.RUnlock()
		select {
		case <-retired:
		case <-time.After(time.Second):
			t.Fatal("retire did not complete after the in-flight call finished")
		}

		_, err := ref.throttle(context.Background(), "tenant", nil)
		require.ErrorIs(t, err, errThrottlerClientRetired)
	})

	t.Run("Throttle after shutdown returns an error instead of spinning", func(t *testing.T) {
		gt := newTestGlobalThrottler()
		require.NoError(t, gt.applyAddresses([]string{"a:1"}))
		gt.client.Load().retire()

		_, err := gt.Throttle(context.Background(), "tenant", nil)
		require.ErrorIs(t, err, errThrottlerClientRetired)
	})
}

func TestNewGlobalThrottler_RequiresAddresses(t *testing.T) {
	for _, addrs := range []string{"", "   "} {
		gt, err := newGlobalThrottler(GlobalThrottlerConfig{Addresses: addrs, DiscoveryInterval: time.Hour}, log.NewNopLogger(), nil)
		require.Error(t, err, "addresses %q", addrs)
		require.Nil(t, gt)
	}
}
