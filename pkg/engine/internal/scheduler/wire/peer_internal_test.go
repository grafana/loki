package wire

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

// TestPeer_SendMessageCancelDoesNotBlockOnFullQueue verifies canceling
// SendMessage returns promptly with a full outgoing queue.
func TestPeer_SendMessageCancelDoesNotBlockOnFullQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Buffer 1 and no Serve: the message frame fills the only outgoing slot.
		p := &Peer{Logger: log.NewNopLogger(), Metrics: NewMetrics(), Buffer: 1}
		p.lazyInit()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		sendErr := make(chan error, 1)
		go func() { sendErr <- p.SendMessage(ctx, WorkerReadyMessage{}) }()

		synctest.Wait() // The message frame has taken the only outgoing slot.
		require.Len(t, p.outgoing, 1)

		cancel()
		synctest.Wait()
		select {
		case err := <-sendErr:
			require.ErrorIs(t, err, context.Canceled)
		default:
			t.Fatal("SendMessage did not return after cancellation with a full outgoing queue")
		}
	})
}

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
