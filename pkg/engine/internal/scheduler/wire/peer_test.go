package wire_test

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
)

// TestPeer_DiscardCancelsRunningHandler verifies a discard cancels a running
// handler and suppresses its ack.
func TestPeer_DiscardCancelsRunningHandler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()

		started, canceled := make(chan struct{}), make(chan struct{})
		handler := func(hctx context.Context, _ *wire.Peer, m wire.Message) error {
			if _, ok := m.(wire.TaskStatusMessage); ok { // the discarded message
				close(started)
				<-hctx.Done()
				close(canceled)
				return hctx.Err()
			}
			return nil // probe
		}

		client, serverConn := dialLocal(ctx, t)
		defer serve(ctx, newServer(serverConn, handler))()

		require.NoError(t, client.Send(ctx, wire.MessageFrame{ID: 1, Message: wire.TaskStatusMessage{}}))
		synctest.Wait()
		requireClosed(t, started, "handler did not start")

		require.NoError(t, client.Send(ctx, wire.DiscardFrame{ID: 1}))
		synctest.Wait()
		requireClosed(t, canceled, "handler was not canceled by the discard")

		// A probe's ack should be the only reply, proving message 1 wasn't acked.
		require.NoError(t, client.Send(ctx, wire.MessageFrame{ID: 2, Message: wire.WorkerReadyMessage{}}))
		frame, err := client.Recv(ctx)
		require.NoError(t, err)
		ack, ok := frame.(wire.AckFrame)
		require.True(t, ok, "expected an ack, got %#v", frame)
		require.Equal(t, uint64(2), ack.ID, "message 1 should not have been acked")
	})
}

// TestPeer_DiscardDropsQueuedMessage verifies a discard for a queued message
// skips its handler and ack.
func TestPeer_DiscardDropsQueuedMessage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()

		release, firstStarted := make(chan struct{}), make(chan struct{})
		var secondHandled atomic.Bool
		handler := func(hctx context.Context, _ *wire.Peer, m wire.Message) error {
			switch m.(type) {
			case wire.TaskStatusMessage: // blocks handleIncoming
				close(firstStarted)
				select {
				case <-release:
				case <-hctx.Done():
				}
			case wire.WorkerReadyMessage: // discarded; must not run
				secondHandled.Store(true)
			}
			return nil // probe
		}

		client, serverConn := dialLocal(ctx, t)
		defer serve(ctx, newServer(serverConn, handler))()

		require.NoError(t, client.Send(ctx, wire.MessageFrame{ID: 1, Message: wire.TaskStatusMessage{}}))
		synctest.Wait()
		requireClosed(t, firstStarted, "first handler did not start")

		// Queue message 2, then discard it before the handler is free.
		require.NoError(t, client.Send(ctx, wire.MessageFrame{ID: 2, Message: wire.WorkerReadyMessage{}}))
		synctest.Wait()
		require.NoError(t, client.Send(ctx, wire.DiscardFrame{ID: 2}))
		synctest.Wait()

		close(release)
		synctest.Wait()
		require.False(t, secondHandled.Load(), "discarded message must not be handled")

		frame, err := client.Recv(ctx)
		require.NoError(t, err)
		ack, ok := frame.(wire.AckFrame)
		require.True(t, ok, "expected an ack, got %#v", frame)
		require.Equal(t, uint64(1), ack.ID)

		// The next ack should be a probe's, proving message 2 wasn't acked.
		require.NoError(t, client.Send(ctx, wire.MessageFrame{ID: 3, Message: wire.WorkerHelloMessage{}}))
		frame, err = client.Recv(ctx)
		require.NoError(t, err)
		ack, ok = frame.(wire.AckFrame)
		require.True(t, ok, "expected an ack, got %#v", frame)
		require.Equal(t, uint64(3), ack.ID, "message 2 should not have been acked")
	})
}

// TestPeer_SendMessageCancelEmitsDiscard verifies canceling SendMessage cancels
// the remote handler.
func TestPeer_SendMessageCancelEmitsDiscard(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()

		started, canceled := make(chan struct{}), make(chan struct{})
		handler := func(hctx context.Context, _ *wire.Peer, _ wire.Message) error {
			close(started)
			<-hctx.Done()
			close(canceled)
			return hctx.Err()
		}

		clientConn, serverConn := dialLocal(ctx, t)
		defer serve(ctx, newServer(serverConn, handler))()
		client := &wire.Peer{Logger: log.NewNopLogger(), Metrics: wire.NewMetrics(), Conn: clientConn, Buffer: 4}
		defer serve(ctx, client)()

		sendCtx, sendCancel := context.WithCancel(ctx)
		sendErr := make(chan error, 1)
		go func() { sendErr <- client.SendMessage(sendCtx, wire.WorkerReadyMessage{}) }()

		synctest.Wait()
		requireClosed(t, started, "server handler did not start")

		sendCancel()
		synctest.Wait()
		requireClosed(t, canceled, "server handler was not canceled by the discard")
		require.ErrorIs(t, <-sendErr, context.Canceled)
	})
}

// dialLocal returns a connected Local Conn pair.
func dialLocal(ctx context.Context, t *testing.T) (client, server wire.Conn) {
	t.Helper()

	listener := &wire.Local{Address: wire.LocalScheduler}
	t.Cleanup(func() { _ = listener.Close(context.Background()) })

	type result struct {
		conn wire.Conn
		err  error
	}
	accepted := make(chan result, 1)
	go func() {
		c, err := listener.Accept(ctx)
		accepted <- result{c, err}
	}()

	client, err := listener.DialFrom(ctx, wire.LocalWorker)
	require.NoError(t, err)
	r := <-accepted
	require.NoError(t, r.err)
	return client, r.conn
}

// serve runs peer.Serve and returns a func to stop it and wait for exit.
func serve(ctx context.Context, peer *wire.Peer) (stop func()) {
	serveCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = peer.Serve(serveCtx) }()
	return func() { cancel(); wg.Wait() }
}

func requireClosed(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	default:
		t.Fatal(msg)
	}
}

func newServer(conn wire.Conn, handler wire.Handler) *wire.Peer {
	return &wire.Peer{Logger: log.NewNopLogger(), Metrics: wire.NewMetrics(), Conn: conn, Handler: handler, Buffer: 4}
}
