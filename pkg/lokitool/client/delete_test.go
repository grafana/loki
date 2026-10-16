package client

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateDeleteRequest(t *testing.T) {
	t.Run("all parameters", func(t *testing.T) {
		srv, requests := newServer(t, http.StatusNoContent, "")
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		err = c.CreateDeleteRequest(DeleteRequestParams{Query: `{job="app"}`, Start: "1000", End: "2000", MaxInterval: "1h"})
		require.NoError(t, err)

		got := requests()
		require.Len(t, got, 1)
		require.Equal(t, http.MethodPost, got[0].Method)
		require.Equal(t, "/loki/api/v1/delete", got[0].Path)

		params, err := url.ParseQuery(got[0].Query)
		require.NoError(t, err)
		require.Equal(t, `{job="app"}`, params.Get("query"))
		require.Equal(t, "1000", params.Get("start"))
		require.Equal(t, "2000", params.Get("end"))
		require.Equal(t, "1h", params.Get("max_interval"))
	})

	t.Run("only the query", func(t *testing.T) {
		srv, requests := newServer(t, http.StatusNoContent, "")
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		require.NoError(t, c.CreateDeleteRequest(DeleteRequestParams{Query: `{job="app"}`}))

		params, err := url.ParseQuery(requests()[0].Query)
		require.NoError(t, err)
		require.False(t, params.Has("start"))
		require.False(t, params.Has("end"))
		require.False(t, params.Has("max_interval"))
	})

	t.Run("error", func(t *testing.T) {
		srv, _ := newServer(t, http.StatusBadRequest, "invalid delete request")
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		err = c.CreateDeleteRequest(DeleteRequestParams{Query: `{job="app"}`})
		require.ErrorContains(t, err, "400 Bad Request: invalid delete request")
	})
}

func TestListDeleteRequests(t *testing.T) {
	srv, requests := newServer(t, http.StatusOK, `[{"start_time": 1000, "end_time": 2000, "query": "{job=\"app\"}", "status": "received"}]`)
	c, err := New(Config{Address: srv.URL})
	require.NoError(t, err)

	got, err := c.ListDeleteRequests()
	require.NoError(t, err)
	require.Equal(t, []DeleteRequest{{StartTime: 1000, EndTime: 2000, Query: `{job="app"}`, Status: "received"}}, got)

	require.Equal(t, http.MethodGet, requests()[0].Method)
	require.Equal(t, "/loki/api/v1/delete", requests()[0].Path)
}

func TestCancelDeleteRequest(t *testing.T) {
	for _, force := range []bool{false, true} {
		srv, requests := newServer(t, http.StatusNoContent, "")
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		require.NoError(t, c.CancelDeleteRequest("abc123", force))

		got := requests()
		require.Len(t, got, 1)
		require.Equal(t, http.MethodDelete, got[0].Method)
		require.Equal(t, "/loki/api/v1/delete", got[0].Path)

		params, err := url.ParseQuery(got[0].Query)
		require.NoError(t, err)
		require.Equal(t, "abc123", params.Get("request_id"))
		require.Equal(t, force, params.Get("force") == "true")
	}
}
