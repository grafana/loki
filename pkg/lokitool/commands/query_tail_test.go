package commands

import (
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

const tailMessage = `{"streams":[{"stream":{"app":"foo","env":"dev"},"values":[["1700000000000000000","line 1"],["1700000001000000000","line 2"]]}]}`

// tailServer starts a server that sends a tail message and closes the connection.
func tailServer(t *testing.T) (*server, func() url.Values) {
	t.Helper()

	var query url.Values
	upgrader := websocket.Upgrader{}
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		_ = conn.WriteMessage(websocket.TextMessage, []byte(tailMessage))
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	})
	return srv, func() url.Values { return query }
}

func TestQueryTail(t *testing.T) {
	t.Run("entries and request", func(t *testing.T) {
		srv, query := tailServer(t)

		// The server closes the connection after the message, which ends the tail with an error.
		out, err := runApplication(t, "--address="+srv.URL, "--id=tenant", "query", "--format=raw", "tail",
			"--from=2023-11-14T22:00:00Z", "--limit=5", "--delay-for=3", `{app="foo"}`)
		require.ErrorContains(t, err, "tail failed")
		require.Equal(t, "line 1\nline 2\n", out)

		got := srv.Requests()
		require.Len(t, got, 1)
		require.Equal(t, "/loki/api/v1/tail", got[0].Path)
		require.Equal(t, "tenant", got[0].Header.Get("X-Scope-OrgID"))

		require.Equal(t, `{app="foo"}`, query().Get("query"))
		require.Equal(t, "5", query().Get("limit"))
		require.Equal(t, "3", query().Get("delay_for"))
		require.Equal(t, "1699999200000000000", query().Get("start"))
	})

	t.Run("labels", func(t *testing.T) {
		srv, _ := tailServer(t)

		out, err := runApplication(t, "--address="+srv.URL, "query", "tail", `{app="foo"}`)
		require.Error(t, err)
		require.Contains(t, out, `{app="foo", env="dev"} line 1`)
	})

	t.Run("without labels", func(t *testing.T) {
		srv, _ := tailServer(t)

		out, err := runApplication(t, "--address="+srv.URL, "query", "tail", "--no-labels", `{app="foo"}`)
		require.Error(t, err)
		require.NotContains(t, out, "app=")
		require.Contains(t, out, "line 1")
	})

	t.Run("excluded labels", func(t *testing.T) {
		srv, _ := tailServer(t)

		out, err := runApplication(t, "--address="+srv.URL, "query", "tail", "--exclude-label=env", `{app="foo"}`)
		require.Error(t, err)
		require.Contains(t, out, `{app="foo"} line 1`)
		require.NotContains(t, out, "env=")
	})

	t.Run("included labels", func(t *testing.T) {
		srv, _ := tailServer(t)

		out, err := runApplication(t, "--address="+srv.URL, "query", "tail", "--include-label=env", `{app="foo"}`)
		require.Error(t, err)
		require.Contains(t, out, `{env="dev"} line 1`)
		require.NotContains(t, out, "app=")
	})

	t.Run("not supported with stdin", func(t *testing.T) {
		r, w, err := os.Pipe()
		require.NoError(t, err)
		require.NoError(t, w.Close())

		stdin := os.Stdin
		os.Stdin = r
		t.Cleanup(func() { os.Stdin = stdin })

		_, err = runApplication(t, "query", "--stdin", "tail", `{app="foo"}`)
		require.ErrorContains(t, err, "not supported")
	})

	t.Run("connection error", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "tail is not enabled", http.StatusNotFound)
		})

		_, err := runApplication(t, "--address="+srv.URL, "query", "tail", `{app="foo"}`)
		require.ErrorContains(t, err, "tail is not enabled")
	})

	t.Run("query is required", func(t *testing.T) {
		_, err := runApplication(t, "query", "tail")
		require.ErrorContains(t, err, "required argument 'query' not provided")
	})

	t.Run("range flags are not supported", func(t *testing.T) {
		_, err := runApplication(t, "query", "tail", "--forward", `{app="foo"}`)
		require.ErrorContains(t, err, "unknown long flag '--forward'")
	})
}
