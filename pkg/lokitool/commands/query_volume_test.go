package commands

import (
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueryVolume(t *testing.T) {
	t.Run("volume", func(t *testing.T) {
		var query string
		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			query = r.URL.RawQuery
			_, _ = w.Write([]byte(vectorResponse))
		})

		out, err := runApplication(t, "--address="+srv.URL, "query", "volume",
			"--from=2023-11-14T22:00:00Z", "--to=2023-11-14T23:00:00Z",
			"--limit=5", "--targetLabels=app", "--targetLabels=env", "--aggregateByLabels", `{app="foo"}`)
		require.NoError(t, err)

		require.Contains(t, out, `"app": "foo"`)
		require.Equal(t, "/loki/api/v1/index/volume", srv.Requests()[0].Path)

		params, err := url.ParseQuery(query)
		require.NoError(t, err)
		require.Equal(t, `{app="foo"}`, params.Get("query"))
		require.Equal(t, "1699999200000000000", params.Get("start"))
		require.Equal(t, "1700002800000000000", params.Get("end"))
		require.Equal(t, "5", params.Get("limit"))
		require.Equal(t, "app,env", params.Get("targetLabels"))
		require.Equal(t, "labels", params.Get("aggregateBy"))
	})

	t.Run("default limit", func(t *testing.T) {
		var query string
		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			query = r.URL.RawQuery
			_, _ = w.Write([]byte(vectorResponse))
		})

		_, err := runApplication(t, "--address="+srv.URL, "query", "volume", `{app="foo"}`)
		require.NoError(t, err)

		params, err := url.ParseQuery(query)
		require.NoError(t, err)
		require.Equal(t, "30", params.Get("limit"))
	})

	t.Run("not supported with stdin", func(t *testing.T) {
		r, w, err := os.Pipe()
		require.NoError(t, err)
		require.NoError(t, w.Close())

		stdin := os.Stdin
		os.Stdin = r
		t.Cleanup(func() { os.Stdin = stdin })

		_, err = runApplication(t, "query", "--stdin", "volume", `{app="foo"}`)
		require.ErrorContains(t, err, "not supported")
	})

	t.Run("query is required", func(t *testing.T) {
		_, err := runApplication(t, "query", "volume")
		require.ErrorContains(t, err, "required argument 'query' not provided")
	})

	t.Run("server error", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "volume endpoint requires tsdb", http.StatusBadRequest)
		})

		_, err := runApplication(t, "--address="+srv.URL, "query", "volume", `{app="foo"}`)
		require.ErrorContains(t, err, "volume endpoint requires tsdb")
	})
}
