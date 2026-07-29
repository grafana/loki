package query

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/logcli/output"
	"github.com/grafana/loki/v3/pkg/loghttp"
)

type testTailClient struct {
	client.Client
	conn *websocket.Conn
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
	err = (&Query{}).tailQuery(
		0,
		&testTailClient{conn: conn},
		&testErrorOutput{err: expectedErr},
		conn,
		make(chan os.Signal),
	)
	require.ErrorIs(t, err, expectedErr)

	select {
	case <-connectionClosed:
	case <-time.After(5 * time.Second):
		t.Fatal("TailQuery did not close the websocket after returning an error")
	}
}
