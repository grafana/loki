package query

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/logcli/output"
	"github.com/grafana/loki/v3/pkg/loghttp"
)

type testTailClient struct {
	client.Client
	conn             *websocket.Conn
	reconnectStarted chan struct{}
	reconnectOnce    sync.Once
}

func (c *testTailClient) LiveTailQueryConn(
	_ string,
	_ time.Duration,
	_ int,
	_ time.Time,
	_ bool,
) (*websocket.Conn, error) {
	return c.conn, nil
}

func (c *testTailClient) LiveTailQueryConnContext(
	ctx context.Context,
	_ string,
	_ time.Duration,
	_ int,
	_ time.Time,
	_ bool,
) (*websocket.Conn, error) {
	if c.reconnectStarted == nil {
		return c.conn, nil
	}

	c.reconnectOnce.Do(func() {
		close(c.reconnectStarted)
	})
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestTailQueryReturnsNilWhenCanceled(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = conn.Close()
	})

	stopChan := make(chan os.Signal, 1)
	errChan := make(chan error, 1)
	go func() {
		errChan <- (&Query{}).tailQuery(0, &testTailClient{conn: conn}, nil, conn, stopChan)
	}()

	stopChan <- os.Interrupt

	select {
	case err := <-errChan:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("TailQuery did not return after cancellation")
	}
}

func TestTailQueryReturnsWhenCanceledDuringReconnect(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.UnderlyingConn().Close()
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = conn.Close()
	})

	reconnectStarted := make(chan struct{})
	stopChan := make(chan os.Signal, 1)
	errChan := make(chan error, 1)
	go func() {
		errChan <- (&Query{}).tailQuery(
			0,
			&testTailClient{
				conn:             conn,
				reconnectStarted: reconnectStarted,
			},
			nil,
			conn,
			stopChan,
		)
	}()

	select {
	case <-reconnectStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("TailQuery did not start reconnecting")
	}

	stopChan <- os.Interrupt

	select {
	case err := <-errChan:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("TailQuery did not return after cancellation during reconnect")
	}
}

type testErrorOutput struct {
	output.LogOutput
	err error
}

func (o *testErrorOutput) FormatAndPrintln(
	_ time.Time,
	_ loghttp.LabelSet,
	_ int,
	_ string,
) error {
	return o.err
}

func TestTailQueryClosesConnectionOnError(t *testing.T) {
	leaks := goleak.IgnoreCurrent()
	t.Cleanup(func() { goleak.VerifyNone(t, leaks) })
	connectionClosed := make(chan struct{})
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		err = conn.WriteMessage(
			websocket.TextMessage,
			[]byte(`{"streams":[{"stream":{"app":"foo"},"values":[["1","line"]]}]}`),
		)
		if err != nil {
			return
		}

		_, _, _ = conn.ReadMessage()
		close(connectionClosed)
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = conn.Close()
	})

	expectedErr := errors.New("output failed")
	err = (&Query{}).TailQuery(
		0,
		&testTailClient{conn: conn},
		&testErrorOutput{err: expectedErr},
	)
	require.ErrorIs(t, err, expectedErr)

	select {
	case <-connectionClosed:
	case <-time.After(5 * time.Second):
		t.Fatal("TailQuery did not close the websocket after returning an error")
	}
}

// Exercise a successful reconnect before shutdown, including cancellation while
// the dial is returning its new connection.
func TestTailQueryClosesReconnectedConnection(t *testing.T) {
	for _, cancelDuringDial := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancelDuringDial=%t", cancelDuringDial), func(t *testing.T) {
			stopChan := make(chan os.Signal, 1)
			connectionClosed := make(chan struct{})
			var requests atomic.Int32
			upgrader := websocket.Upgrader{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				if requests.Add(1) == 1 {
					return // Force the initial connection to reconnect.
				}
				if !cancelDuringDial {
					if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"streams":[{"stream":{"app":"foo"},"values":[["1","line"]]}]}`)); err != nil {
						return
					}
				}
				_, _, _ = conn.ReadMessage()
				close(connectionClosed)
			}))
			t.Cleanup(server.Close)

			c := &client.DefaultClient{Address: server.URL}
			conn, err := c.LiveTailQueryConn("", 0, 0, time.Time{}, true)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			reconnectingClient := &reconnectingTailClient{
				Client: c,
				dial: func(ctx context.Context) (*websocket.Conn, error) {
					next, err := c.LiveTailQueryConn("", 0, 0, time.Time{}, true)
					if err != nil {
						return nil, err
					}
					if cancelDuringDial {
						stopChan <- os.Interrupt
						<-ctx.Done()
					}
					return next, nil
				},
			}
			errChan := make(chan error, 1)
			go func() {
				errChan <- (&Query{Quiet: true}).tailQuery(0, reconnectingClient, &interruptOutput{stopChan: stopChan}, conn, stopChan)
			}()
			select {
			case err := <-errChan:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("tail did not stop after reconnect")
			}
			select {
			case <-connectionClosed:
			case <-time.After(5 * time.Second):
				t.Fatal("reconnected websocket was not closed")
			}
		})
	}
}

type reconnectingTailClient struct {
	client.Client
	dial func(context.Context) (*websocket.Conn, error)
}

func (c *reconnectingTailClient) LiveTailQueryConnContext(ctx context.Context, _ string, _ time.Duration, _ int, _ time.Time, _ bool) (*websocket.Conn, error) {
	return c.dial(ctx)
}

func TestLiveTailQueryConnClosesLateLegacyConnection(t *testing.T) {
	leaks := goleak.IgnoreCurrent()
	t.Cleanup(func() { goleak.VerifyNone(t, leaks) })
	serverConn, conn := newTailTestConnection(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	c := &legacyTailClient{dial: func() (*websocket.Conn, error) {
		close(started)
		<-release
		return conn, nil
	}}
	errChan := make(chan error, 1)
	go func() {
		_, err := liveTailQueryConn(ctx, c, "", 0, 0, time.Time{}, true)
		errChan <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("legacy dial did not start")
	}
	cancel()
	select {
	case err := <-errChan:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation waited for the legacy dial")
	}
	unblock()
	require.NoError(t, serverConn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, _, err := serverConn.ReadMessage()
	require.Error(t, err)
	var netErr net.Error
	require.False(t, errors.As(err, &netErr) && netErr.Timeout(), "late websocket was not closed")
}

type legacyTailClient struct {
	client.Client
	dial func() (*websocket.Conn, error)
}

func (c *legacyTailClient) LiveTailQueryConn(_ string, _ time.Duration, _ int, _ time.Time, _ bool) (*websocket.Conn, error) {
	return c.dial()
}

func newTailTestConnection(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			accepted <- conn
		}
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	serverConn := <-accepted
	t.Cleanup(func() { _ = serverConn.Close() })
	return serverConn, conn
}

// interruptOutput requests shutdown only after a log entry is read from the
// reconnected socket, proving the new connection has been installed.
type interruptOutput struct {
	output.LogOutput
	stopChan chan<- os.Signal
}

func (o *interruptOutput) FormatAndPrintln(_ time.Time, _ loghttp.LabelSet, _ int, _ string) error {
	o.stopChan <- os.Interrupt
	return nil
}
