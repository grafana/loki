package wire_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
)

// TestSendMessageNotify_ACK verifies that SendMessageNotify invokes the callback with nil on ACK.
func TestSendMessageNotify_ACK(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	listener := &wire.Local{Address: wire.LocalScheduler}

	// Accept must be running before DialFrom (Local blocks until Accept receives the connection).
	acceptDone := make(chan wire.Conn, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err == nil {
			acceptDone <- conn
		}
	}()

	clientConn, err := listener.DialFrom(ctx, wire.LocalWorker)
	require.NoError(t, err)
	defer clientConn.Close()

	serverConn := <-acceptDone
	require.NotNil(t, serverConn)
	defer serverConn.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	serverPeer := &wire.Peer{
		Logger:  log.NewNopLogger(),
		Metrics: wire.NewMetrics(),
		Conn:    serverConn,
		Handler: func(_ context.Context, _ *wire.Peer, _ wire.Message) error {
			return nil // ACK
		},
		Buffer: 10,
	}

	go func() {
		defer wg.Done()
		_ = serverPeer.Serve(ctx)
	}()

	clientPeer := &wire.Peer{
		Logger:  log.NewNopLogger(),
		Metrics: wire.NewMetrics(),
		Conn:    clientConn,
		Handler: func(_ context.Context, _ *wire.Peer, _ wire.Message) error {
			return nil
		},
		Buffer: 10,
	}

	go func() {
		_ = clientPeer.Serve(ctx)
	}()

	// Give peers time to start
	time.Sleep(10 * time.Millisecond)

	callbackInvoked := make(chan error, 1)
	err = clientPeer.SendMessageNotify(ctx, wire.WorkerReadyMessage{}, func(err error) {
		callbackInvoked <- err
	})
	require.NoError(t, err)

	select {
	case cbErr := <-callbackInvoked:
		require.NoError(t, cbErr)
	case <-ctx.Done():
		t.Fatal("callback was not invoked before timeout")
	}

	cancel()
	wg.Wait()
}

// TestSendMessageNotify_NACK verifies that SendMessageNotify invokes the callback with error on NACK.
func TestSendMessageNotify_NACK(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	listener := &wire.Local{Address: wire.LocalScheduler}

	acceptDone := make(chan wire.Conn, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err == nil {
			acceptDone <- conn
		}
	}()

	clientConn, err := listener.DialFrom(ctx, wire.LocalWorker)
	require.NoError(t, err)
	defer clientConn.Close()

	serverConn := <-acceptDone
	require.NotNil(t, serverConn)
	defer serverConn.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	nackErr := wire.Errorf(429, "too many requests")
	serverPeer := &wire.Peer{
		Logger:  log.NewNopLogger(),
		Metrics: wire.NewMetrics(),
		Conn:    serverConn,
		Handler: func(_ context.Context, _ *wire.Peer, _ wire.Message) error {
			return nackErr // NACK
		},
		Buffer: 10,
	}

	go func() {
		defer wg.Done()
		_ = serverPeer.Serve(ctx)
	}()

	clientPeer := &wire.Peer{
		Logger:  log.NewNopLogger(),
		Metrics: wire.NewMetrics(),
		Conn:    clientConn,
		Handler: func(_ context.Context, _ *wire.Peer, _ wire.Message) error {
			return nil
		},
		Buffer: 10,
	}

	go func() {
		_ = clientPeer.Serve(ctx)
	}()

	time.Sleep(10 * time.Millisecond)

	callbackInvoked := make(chan error, 1)
	err = clientPeer.SendMessageNotify(ctx, wire.WorkerReadyMessage{}, func(cbErr error) {
		callbackInvoked <- cbErr
	})
	require.NoError(t, err)

	select {
	case cbErr := <-callbackInvoked:
		require.Error(t, cbErr)
		var wireErr *wire.Error
		require.True(t, errors.As(cbErr, &wireErr) && wireErr.Code == 429, "expected 429 error, got %v", cbErr)
	case <-ctx.Done():
		t.Fatal("callback was not invoked before timeout")
	}

	cancel()
	wg.Wait()
}
