package wire

import (
	"context"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func TestHTTP2AcceptAfterHandlerFinishedReturnsErr(t *testing.T) {
	listener, handlerDone, shutdown := startHTTP2ListenerServer(t)
	defer shutdown()

	reqCtx, reqCancel := context.WithCancel(context.Background())
	defer reqCancel()

	dialErrCh := make(chan error, 1)
	go func() {
		_, err := NewHTTP2Dialer("/").Dial(reqCtx, listener.Addr(), listener.Addr())
		dialErrCh <- err
	}()

	require.Eventually(t, func() bool {
		return len(listener.connCh) > 0
	}, 2*time.Second, 10*time.Millisecond)

	reqCancel()

	select {
	case <-handlerDone:
	case <-time.After(50 * time.Second):
		t.Fatal("handler did not finish after request cancellation")
	}

	var acceptErr error
	require.NotPanics(t, func() {
		_, acceptErr = listener.Accept(context.Background())
	})
	require.ErrorIs(t, acceptErr, ErrConnClosed)

	select {
	case <-dialErrCh:
	case <-time.After(2 * time.Second):
		t.Fatal("dial did not return after cancellation")
	}
}

func startHTTP2ListenerServer(t *testing.T) (*HTTP2Listener, <-chan struct{}, func()) {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	listener := NewHTTP2Listener(
		l.Addr(),
		WithHTTP2ListenerMaxPendingConns(1),
		WithHTTP2ListenerLogger(log.NewNopLogger()),
	)

	handlerDone := make(chan struct{})
	var handlerOnce sync.Once
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		listener.ServeHTTP(w, r)
		handlerOnce.Do(func() {
			close(handlerDone)
		})
	})

	server := &http.Server{
		Handler: h2c.NewHandler(wrapped, &http2.Server{}),
	}

	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- server.Serve(l)
	}()

	return listener, handlerDone, func() {
		ctx := context.Background()
		require.NoError(t, listener.Close(ctx))
		require.NoError(t, server.Shutdown(ctx))
		require.Error(t, <-serveErrCh, http.ErrServerClosed)
	}
}
