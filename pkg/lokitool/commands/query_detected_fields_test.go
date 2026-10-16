package commands

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

const detectedFieldsResponse = `{
  "fields": [
    {"label": "status", "type": "int", "cardinality": 5, "parsers": ["logfmt"]},
    {"label": "level", "type": "string", "cardinality": 3, "parsers": ["logfmt"]}
  ]
}`

func TestQueryDetectedFields(t *testing.T) {
	t.Run("fields", func(t *testing.T) {
		var query string
		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			query = r.URL.RawQuery
			_, _ = w.Write([]byte(detectedFieldsResponse))
		})

		out, err := runApplication(t, "--address="+srv.URL, "query", "detected-fields",
			"--from=2023-11-14T22:00:00Z", "--to=2023-11-14T23:00:00Z", "--limit=10", "--line-limit=500", `{app="foo"}`)
		require.NoError(t, err)

		// The fields are sorted.
		require.Regexp(t, `(?s)label: level.*cardinality: 3.*label: status.*cardinality: 5`, out)
		require.Equal(t, "/loki/api/v1/detected_fields", srv.Requests()[0].Path)

		params, err := url.ParseQuery(query)
		require.NoError(t, err)
		require.Equal(t, "10", params.Get("limit"))
		require.Equal(t, "500", params.Get("line_limit"))
		require.Equal(t, "10s", params.Get("step"))
	})

	t.Run("values of a field", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"values": ["info", "error"], "limit": 100}`))
		})

		out, err := runApplication(t, "--address="+srv.URL, "query", "detected-fields", `{app="foo"}`, "level")
		require.NoError(t, err)
		require.Equal(t, "error\ninfo\n", out)
		require.Equal(t, "/loki/api/v1/detected_field/level/values", srv.Requests()[0].Path)
	})

	for _, format := range []string{"raw", "jsonl"} {
		t.Run("json with "+format, func(t *testing.T) {
			srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(detectedFieldsResponse))
			})

			out, err := runApplication(t, "--address="+srv.URL, "query", "--format="+format, "detected-fields", `{app="foo"}`)
			require.NoError(t, err)
			require.JSONEq(t, detectedFieldsResponse, out)
		})
	}

	t.Run("query is required", func(t *testing.T) {
		_, err := runApplication(t, "query", "detected-fields")
		require.ErrorContains(t, err, "required argument 'query' not provided")
	})

	t.Run("server error", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		})

		_, err := runApplication(t, "--address="+srv.URL, "query", "detected-fields", `{app="foo"}`)
		require.ErrorContains(t, err, "error doing request")
		require.ErrorContains(t, err, "500 Internal Server Error: boom")
	})
}
