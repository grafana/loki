package commands

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueryLabels(t *testing.T) {
	t.Run("label names", func(t *testing.T) {
		var query string
		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			query = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"status": "success", "data": ["app", "cluster"]}`))
		})

		out, err := runApplication(t, "--address="+srv.URL, "query", "labels",
			"--from=2023-11-14T22:00:00Z", "--to=2023-11-14T23:00:00Z")
		require.NoError(t, err)
		require.Equal(t, "app\ncluster\n", out)

		require.Equal(t, "/loki/api/v1/labels", srv.Requests()[0].Path)
		params, err := url.ParseQuery(query)
		require.NoError(t, err)
		require.Equal(t, "1699999200000000000", params.Get("start"))
		require.Equal(t, "1700002800000000000", params.Get("end"))
	})

	t.Run("label values", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status": "success", "data": ["loki", "mimir"]}`))
		})

		out, err := runApplication(t, "--address="+srv.URL, "query", "labels", "--since=2h", "app")
		require.NoError(t, err)
		require.Equal(t, "loki\nmimir\n", out)
		require.Equal(t, "/loki/api/v1/label/app/values", srv.Requests()[0].Path)
	})

	t.Run("label name is escaped", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status": "success", "data": []}`))
		})

		_, err := runApplication(t, "--address="+srv.URL, "query", "labels", "a/b")
		require.NoError(t, err)
		require.Equal(t, "/loki/api/v1/label/a%2Fb/values", srv.Requests()[0].Path)
	})

	t.Run("invalid time", func(t *testing.T) {
		_, err := runApplication(t, "query", "labels", "--to=tomorrow")
		require.ErrorContains(t, err, "unable to parse time")
	})

	t.Run("server error", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		})

		_, err := runApplication(t, "--address="+srv.URL, "query", "labels")
		require.ErrorContains(t, err, "500 Internal Server Error: boom")
	})
}
