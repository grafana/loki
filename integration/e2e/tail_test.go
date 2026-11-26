//go:build e2e

package e2e

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// testTail opens a tail connection, writes new lines to both test streams, and checks that the
// tail returns the dev line and, when a policy applies, never the prod line.
func testTail(t *testing.T, h *harness, policy map[string]string) {
	wsURL := "ws" + strings.TrimPrefix(h.readURL, "http") + "/loki/api/v1/tail?" +
		url.Values{"query": {`{job="smoke"}`}}.Encode()
	header := http.Header{"X-Scope-OrgID": {tenant}}
	for k, v := range policy {
		header.Set(k, v)
	}
	conn, resp, err := websocket.DefaultDialer.DialContext(t.Context(), wsURL, header)
	require.NoError(t, err)
	defer conn.Close()
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	// A tail returns entries that arrive after it opens, so keep writing until it returns one.
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case now := <-ticker.C:
				pushQuietly(h, now, devStream, prodStream)
			}
		}
	}()

	var seen strings.Builder
	deadline := time.Now().Add(60 * time.Second)
	require.NoError(t, conn.SetReadDeadline(deadline))
	for !strings.Contains(seen.String(), "dev line") {
		_, msg, err := conn.ReadMessage()
		require.NoError(t, err, "no tail response with the dev line")
		seen.Write(msg)
	}

	// Read on for a few more writes, which must not bring in the prod stream.
	if policy != nil {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(4*time.Second)))
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}
			seen.Write(msg)
		}
		require.NotContains(t, seen.String(), "prod line")
	}
}
