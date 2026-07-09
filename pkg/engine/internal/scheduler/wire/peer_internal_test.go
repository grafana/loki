package wire

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPeer_processMessageHonorsDiscard covers nil-handler and unknown-ID discards.
func TestPeer_processMessageHonorsDiscard(t *testing.T) {
	ctx := context.Background()

	t.Run("discarded message is dropped and its entry released", func(t *testing.T) {
		p := &Peer{Metrics: NewMetrics(), Buffer: 4}
		p.lazyInit()

		hctx, cancel := context.WithCancelCause(ctx)
		p.inflight.Store(uint64(1), &inflightHandler{ctx: hctx, cancel: cancel})
		cancel(errDiscarded)
		p.handleMessage(ctx, 1, WorkerReadyMessage{})

		_, ok := p.inflight.Load(uint64(1))
		require.False(t, ok, "entry should be released")
		require.Empty(t, p.outgoing, "a discarded message must not be acked or nacked")
	})

	t.Run("discard for an unknown id is a no-op", func(t *testing.T) {
		p := &Peer{}
		require.NoError(t, p.routeFrame(ctx, DiscardFrame{ID: 999}))
	})
}
