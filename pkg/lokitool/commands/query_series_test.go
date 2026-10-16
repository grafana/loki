package commands

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

const seriesResponse = `{
  "status": "success",
  "data": [
    {"app": "foo", "env": "dev"},
    {"app": "bar", "env": "dev"},
    {"app": "baz", "env": "prod"}
  ]
}`

func TestQuerySeries(t *testing.T) {
	t.Run("streams", func(t *testing.T) {
		var query string
		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			query = r.URL.RawQuery
			_, _ = w.Write([]byte(seriesResponse))
		})

		out, err := runApplication(t, "--address="+srv.URL, "query", "series",
			"--from=2023-11-14T22:00:00Z", "--to=2023-11-14T23:00:00Z", `{env=~".+"}`)
		require.NoError(t, err)

		require.Equal(t, `{app="foo", env="dev"}`+"\n"+`{app="bar", env="dev"}`+"\n"+`{app="baz", env="prod"}`+"\n", out)
		require.Equal(t, "/loki/api/v1/series", srv.Requests()[0].Path)

		params, err := url.ParseQuery(query)
		require.NoError(t, err)
		require.Equal(t, []string{`{env=~".+"}`}, params["match"])
		require.Equal(t, "1699999200000000000", params.Get("start"))
		require.Equal(t, "1700002800000000000", params.Get("end"))
	})

	t.Run("analyze labels", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(seriesResponse))
		})

		out, err := runApplication(t, "--address="+srv.URL, "query", "series", "--analyze-labels", `{}`)
		require.NoError(t, err)

		require.Contains(t, out, "Total Streams:  3")
		require.Contains(t, out, "Unique Labels:  2")
		require.Regexp(t, `app\s+3\s+3`, out)
		require.Regexp(t, `env\s+2\s+3`, out)
	})

	t.Run("matcher is required", func(t *testing.T) {
		_, err := runApplication(t, "query", "series")
		require.ErrorContains(t, err, "required argument 'matcher' not provided")
	})

	t.Run("server error", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "parse error", http.StatusBadRequest)
		})

		_, err := runApplication(t, "--address="+srv.URL, "query", "series", `{`)
		require.ErrorContains(t, err, "400 Bad Request: parse error")
	})
}
