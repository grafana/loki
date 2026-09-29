package distributor

import (
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
