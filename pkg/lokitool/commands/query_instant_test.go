package commands

import (
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

const vectorResponse = `{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [{"metric": {"app": "foo"}, "value": [1700000000, "3"]}],
    "stats": {}
  }
}`

func TestQueryInstant(t *testing.T) {
	t.Run("metric query", func(t *testing.T) {
		var query string
		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			query = r.URL.RawQuery
			_, _ = w.Write([]byte(vectorResponse))
		})

		out, err := runApplication(t, "--address="+srv.URL, "--id=tenant", "query", "instant",
			"--now=2023-11-14T22:13:20Z", "--limit=10", "--forward", `count_over_time({app="foo"}[1m])`)
		require.NoError(t, err)

		require.Contains(t, out, `"app": "foo"`)
		require.Equal(t, "/loki/api/v1/query", srv.Requests()[0].Path)
		require.Equal(t, "tenant", srv.Requests()[0].Header.Get("X-Scope-OrgID"))

		params, err := url.ParseQuery(query)
		require.NoError(t, err)
		require.Equal(t, `count_over_time({app="foo"}[1m])`, params.Get("query"))
		require.Equal(t, "10", params.Get("limit"))
		require.Equal(t, "1700000000000000000", params.Get("time"))
		require.Equal(t, "FORWARD", params.Get("direction"))
	})

	t.Run("log query", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(streamsResponse))
		})

		out, err := runApplication(t, "--address="+srv.URL, "query", "--format=raw", "instant", "--limit=2", `{app=~".+"}`)
		require.NoError(t, err)
		require.Equal(t, "line 2\nline 1\n", out)
	})

	t.Run("current time is the default", func(t *testing.T) {
		var query string
		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			query = r.URL.RawQuery
			_, _ = w.Write([]byte(vectorResponse))
		})

		_, err := runApplication(t, "--address="+srv.URL, "query", "instant", `count_over_time({app="foo"}[1m])`)
		require.NoError(t, err)

		params, err := url.ParseQuery(query)
		require.NoError(t, err)
		require.NotEmpty(t, params.Get("time"))
	})

	t.Run("invalid time", func(t *testing.T) {
		_, err := runApplication(t, "query", "instant", "--now=now", `{app="foo"}`)
		require.ErrorContains(t, err, "unable to parse time")
	})

	t.Run("server error", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "parse error", http.StatusBadRequest)
		})

		_, err := runApplication(t, "--address="+srv.URL, "query", "instant", `{`)
		require.ErrorContains(t, err, "400 Bad Request: parse error")
	})

	t.Run("stdin", func(t *testing.T) {
		r, w, err := os.Pipe()
		require.NoError(t, err)
		_, err = w.WriteString("level=info msg=started\nlevel=error msg=failed\n")
		require.NoError(t, err)
		require.NoError(t, w.Close())

		stdin := os.Stdin
		os.Stdin = r
		t.Cleanup(func() { os.Stdin = stdin })

		out, err := runApplication(t, "query", "--stdin", "--format=raw", "instant", `{source="logcli"} |= "error"`)
		require.NoError(t, err)
		require.Equal(t, "level=error msg=failed\n", out)
	})
}
