package commands

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueryStats(t *testing.T) {
	t.Run("statistics", func(t *testing.T) {
		var query string
		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			query = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"streams": 2, "chunks": 10, "bytes": 2048, "entries": 100}`))
		})

		out, err := runApplication(t, "--address="+srv.URL, "query", "stats",
			"--from=2023-11-14T22:00:00Z", "--to=2023-11-14T23:00:00Z", `{app="foo"}`)
		require.NoError(t, err)

		require.True(t, strings.HasPrefix(out, "{\n") && strings.HasSuffix(out, "}\n"), out)
		for _, want := range []string{"streams", ": 2\n", "chunks", ": 10\n", "bytes", ": 2.0kB\n", "entries", ": 100\n"} {
			require.Contains(t, out, want)
		}
		require.Equal(t, "/loki/api/v1/index/stats", srv.Requests()[0].Path)

		params, err := url.ParseQuery(query)
		require.NoError(t, err)
		require.Equal(t, `{app="foo"}`, params.Get("query"))
		require.Equal(t, "1699999200000000000", params.Get("start"))
		require.Equal(t, "1700002800000000000", params.Get("end"))
	})

	t.Run("query is required", func(t *testing.T) {
		_, err := runApplication(t, "query", "stats")
		require.ErrorContains(t, err, "required argument 'query' not provided")
	})

	t.Run("server error", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		})

		_, err := runApplication(t, "--address="+srv.URL, "query", "stats", `{app="foo"}`)
		require.ErrorContains(t, err, "500 Internal Server Error: boom")
	})
}
