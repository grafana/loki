package commands

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

const deleteRequestsResponse = `[
  {"start_time": 1700000000, "end_time": 1700003600, "query": "{job=\"app\"}", "status": "received"},
  {"start_time": 1700007200, "end_time": 1700010800, "query": "{job=\"web\"}", "status": "processed"}
]`

func TestDeleteCreate(t *testing.T) {
	var query string
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	})

	out, err := runApplication(t, "--address="+srv.URL, "--id=tenant", "delete", "create",
		"--from=2023-11-14T22:00:00Z", "--to=2023-11-14T23:00:00Z", "--max-interval=1h", `{job="app"}`)
	require.NoError(t, err)
	require.Equal(t, "Delete request created successfully\n", out)

	got := srv.Requests()
	require.Len(t, got, 1)
	require.Equal(t, http.MethodPost, got[0].Method)
	require.Equal(t, "/loki/api/v1/delete", got[0].Path)
	require.Equal(t, "tenant", got[0].Header.Get("X-Scope-OrgID"))

	params, err := url.ParseQuery(query)
	require.NoError(t, err)
	require.Equal(t, `{job="app"}`, params.Get("query"))
	require.Equal(t, "1699999200", params.Get("start"))
	require.Equal(t, "1700002800", params.Get("end"))
	require.Equal(t, "1h", params.Get("max_interval"))

	t.Run("query is required", func(t *testing.T) {
		_, err := runApplication(t, "delete", "create")
		require.ErrorContains(t, err, "required argument 'query' not provided")
	})

	t.Run("invalid time", func(t *testing.T) {
		_, err := runApplication(t, "--address="+srv.URL, "delete", "create", "--from=yesterday", `{job="app"}`)
		require.ErrorContains(t, err, "unable to parse time")
	})

	t.Run("server error", func(t *testing.T) {
		failing := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "deletion is not enabled", http.StatusForbidden)
		})

		_, err := runApplication(t, "--address="+failing.URL, "delete", "create", `{job="app"}`)
		require.ErrorContains(t, err, "403 Forbidden: deletion is not enabled")
	})
}

func TestDeleteList(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(deleteRequestsResponse))
	})

	t.Run("default format", func(t *testing.T) {
		out, err := runApplication(t, "--address="+srv.URL, "delete", "list")
		require.NoError(t, err)

		require.Contains(t, out, "Found 2 delete requests:\n")
		require.Contains(t, out, "Query: {job=\"app\"}\n")
		require.Contains(t, out, "Status: received\n")
		require.Contains(t, out, "Query: {job=\"web\"}\n")
		require.Contains(t, out, "Status: processed\n")
	})

	for _, args := range [][]string{{"--format=jsonl"}, {"--output=jsonl"}, {"-o", "jsonl"}} {
		t.Run(args[0], func(t *testing.T) {
			out, err := runApplication(t, append([]string{"--address=" + srv.URL, "delete"}, append(args, "list")...)...)
			require.NoError(t, err)
			require.JSONEq(t, deleteRequestsResponse, out)
		})
	}

	t.Run("invalid format", func(t *testing.T) {
		_, err := runApplication(t, "--address="+srv.URL, "delete", "--format=raw", "list")
		require.ErrorContains(t, err, "enum value must be one of default,jsonl")
	})
}

func TestDeleteCancel(t *testing.T) {
	var query string
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	})

	out, err := runApplication(t, "--address="+srv.URL, "delete", "cancel", "--request-id=abc123", "--force")
	require.NoError(t, err)
	require.Equal(t, "Delete request abc123 cancelled successfully\n", out)

	got := srv.Requests()
	require.Len(t, got, 1)
	require.Equal(t, http.MethodDelete, got[0].Method)

	params, err := url.ParseQuery(query)
	require.NoError(t, err)
	require.Equal(t, "abc123", params.Get("request_id"))
	require.Equal(t, "true", params.Get("force"))

	t.Run("request id is required", func(t *testing.T) {
		_, err := runApplication(t, "delete", "cancel")
		require.ErrorContains(t, err, "required flag(s) '--request-id' not provided")
	})
}
