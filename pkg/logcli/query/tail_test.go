package query

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logcli/client"
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
		errChan <- (&Query{}).tailQuery(0, &testTailClient{conn: conn}, nil, stopChan)
	}()

	stopChan <- os.Interrupt

	select {
	case err := <-errChan:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("TailQuery did not return after cancellation")
	}
}
