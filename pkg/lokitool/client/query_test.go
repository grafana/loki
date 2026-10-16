package client

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/lokitool/volume"
)

const streamsResponse = `{
  "status": "success",
  "data": {
    "resultType": "streams",
    "result": [
      {"stream": {"app": "foo"}, "values": [["1700000000000000000", "line 1"], ["1700000001000000000", "line 2"]]}
    ],
    "stats": {}
  }
}`

func TestQuery(t *testing.T) {
	srv, requests := newServer(t, http.StatusOK, streamsResponse)
	c, err := New(Config{Address: srv.URL})
	require.NoError(t, err)

	ts := time.Unix(1700000000, 0)
	resp, err := c.Query(`{app="foo"}`, 10, ts, logproto.FORWARD)
	require.NoError(t, err)

	require.Equal(t, loghttp.QueryStatusSuccess, resp.Status)
	streams, ok := resp.Data.Result.(loghttp.Streams)
	require.True(t, ok)
	require.Len(t, streams, 1)
	require.Len(t, streams[0].Entries, 2)
	require.Equal(t, "line 1", streams[0].Entries[0].Line)

	got := requests()
	require.Len(t, got, 1)
	require.Equal(t, http.MethodGet, got[0].Method)
	require.Equal(t, "/loki/api/v1/query", got[0].Path)

	params, err := url.ParseQuery(got[0].Query)
	require.NoError(t, err)
	require.Equal(t, `{app="foo"}`, params.Get("query"))
	require.Equal(t, "10", params.Get("limit"))
	require.Equal(t, "1700000000000000000", params.Get("time"))
	require.Equal(t, "FORWARD", params.Get("direction"))
}

func TestQueryRange(t *testing.T) {
	start := time.Unix(1700000000, 0)
	end := start.Add(time.Hour)

	t.Run("all parameters", func(t *testing.T) {
		srv, requests := newServer(t, http.StatusOK, streamsResponse)
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		_, err = c.QueryRange(`{app="foo"}`, 100, start, end, logproto.BACKWARD, 30*time.Second, time.Minute)
		require.NoError(t, err)

		got := requests()
		require.Len(t, got, 1)
		require.Equal(t, "/loki/api/v1/query_range", got[0].Path)

		params, err := url.ParseQuery(got[0].Query)
		require.NoError(t, err)
		require.Equal(t, "100", params.Get("limit"))
		require.Equal(t, "1700000000000000000", params.Get("start"))
		require.Equal(t, "1700003600000000000", params.Get("end"))
		require.Equal(t, "BACKWARD", params.Get("direction"))
		require.Equal(t, "30", params.Get("step"))
		require.Equal(t, "60", params.Get("interval"))
	})

	t.Run("step and interval are optional", func(t *testing.T) {
		srv, requests := newServer(t, http.StatusOK, streamsResponse)
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		_, err = c.QueryRange(`{app="foo"}`, 100, start, end, logproto.BACKWARD, 0, 0)
		require.NoError(t, err)

		params, err := url.ParseQuery(requests()[0].Query)
		require.NoError(t, err)
		require.False(t, params.Has("step"))
		require.False(t, params.Has("interval"))
	})

	t.Run("error", func(t *testing.T) {
		srv, _ := newServer(t, http.StatusBadRequest, "parse error at line 1")
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		_, err = c.QueryRange(`{`, 100, start, end, logproto.BACKWARD, 0, 0)
		require.ErrorContains(t, err, "400 Bad Request: parse error at line 1")
	})
}

const labelsResponse = `{"status": "success", "data": ["app", "cluster"]}`

func TestListLabelNames(t *testing.T) {
	srv, requests := newServer(t, http.StatusOK, labelsResponse)
	c, err := New(Config{Address: srv.URL})
	require.NoError(t, err)

	start := time.Unix(1700000000, 0)
	resp, err := c.ListLabelNames(start, start.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, []string{"app", "cluster"}, resp.Data)

	got := requests()
	require.Len(t, got, 1)
	require.Equal(t, "/loki/api/v1/labels", got[0].Path)

	params, err := url.ParseQuery(got[0].Query)
	require.NoError(t, err)
	require.Equal(t, "1700000000000000000", params.Get("start"))
	require.Equal(t, "1700003600000000000", params.Get("end"))
}

func TestListLabelValues(t *testing.T) {
	srv, requests := newServer(t, http.StatusOK, labelsResponse)
	c, err := New(Config{Address: srv.URL})
	require.NoError(t, err)

	start := time.Unix(1700000000, 0)
	resp, err := c.ListLabelValues("my/label", start, start.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, []string{"app", "cluster"}, resp.Data)

	got := requests()
	require.Len(t, got, 1)
	require.Equal(t, "/loki/api/v1/label/my%2Flabel/values", got[0].Path)
}

func TestSeries(t *testing.T) {
	srv, requests := newServer(t, http.StatusOK, `{"status": "success", "data": [{"app": "foo", "env": "dev"}, {"app": "bar"}]}`)
	c, err := New(Config{Address: srv.URL})
	require.NoError(t, err)

	start := time.Unix(1700000000, 0)
	resp, err := c.Series([]string{`{app="foo"}`, `{app="bar"}`}, start, start.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, []loghttp.LabelSet{{"app": "foo", "env": "dev"}, {"app": "bar"}}, resp.Data)

	got := requests()
	require.Len(t, got, 1)
	require.Equal(t, "/loki/api/v1/series", got[0].Path)

	params, err := url.ParseQuery(got[0].Query)
	require.NoError(t, err)
	require.Equal(t, []string{`{app="foo"}`, `{app="bar"}`}, params["match"])
	require.Equal(t, "1700000000000000000", params.Get("start"))
	require.Equal(t, "1700003600000000000", params.Get("end"))
}

func TestGetVolume(t *testing.T) {
	const response = `{"status": "success", "data": {"resultType": "vector", "result": [{"metric": {"app": "foo"}, "value": [1700000000, "1024"]}]}}`
	start := time.Unix(1700000000, 0)

	t.Run("all parameters", func(t *testing.T) {
		srv, requests := newServer(t, http.StatusOK, response)
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		resp, err := c.GetVolume(&volume.Query{
			QueryString:       `{app="foo"}`,
			Start:             start,
			End:               start.Add(time.Hour),
			Limit:             5,
			TargetLabels:      []string{"app", "env"},
			AggregateByLabels: true,
		})
		require.NoError(t, err)
		require.Equal(t, loghttp.ResultTypeVector, string(resp.Data.ResultType))

		got := requests()
		require.Len(t, got, 1)
		require.Equal(t, "/loki/api/v1/index/volume", got[0].Path)

		params, err := url.ParseQuery(got[0].Query)
		require.NoError(t, err)
		require.Equal(t, `{app="foo"}`, params.Get("query"))
		require.Equal(t, "1700000000000000000", params.Get("start"))
		require.Equal(t, "1700003600000000000", params.Get("end"))
		require.Equal(t, "5", params.Get("limit"))
		require.Equal(t, "app,env", params.Get("targetLabels"))
		require.Equal(t, "labels", params.Get("aggregateBy"))
	})

	t.Run("optional parameters", func(t *testing.T) {
		srv, requests := newServer(t, http.StatusOK, response)
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		_, err = c.GetVolume(&volume.Query{QueryString: `{app="foo"}`, Start: start, End: start.Add(time.Hour), Limit: 30})
		require.NoError(t, err)

		params, err := url.ParseQuery(requests()[0].Query)
		require.NoError(t, err)
		require.False(t, params.Has("targetLabels"))
		require.False(t, params.Has("aggregateBy"))
		require.False(t, params.Has("step"))
	})
}
