package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/prompb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPushMetrics(t *testing.T) {
	var received prompb.WriteRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/x-protobuf", r.Header.Get("Content-Type"))
		assert.Equal(t, "snappy", r.Header.Get("Content-Encoding"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		decoded, err := snappy.Decode(nil, body)
		require.NoError(t, err)
		err = proto.Unmarshal(decoded, &received)
		require.NoError(t, err)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	series := []prompb.TimeSeries{{
		Labels:  []prompb.Label{{Name: "__name__", Value: "test_metric"}},
		Samples: []prompb.Sample{{Value: 1.0, Timestamp: 1000}},
	}}

	err := pushMetrics(server.URL, "", "", series)
	require.NoError(t, err)
	assert.Len(t, received.Timeseries, 1)
}

func TestPushMetrics_BasicAuth(t *testing.T) {
	var receivedUser, receivedPass string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUser, receivedPass, _ = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	series := []prompb.TimeSeries{{
		Labels:  []prompb.Label{{Name: "__name__", Value: "test_metric"}},
		Samples: []prompb.Sample{{Value: 1.0, Timestamp: 1000}},
	}}

	err := pushMetrics(server.URL, "myuser", "mypass", series)
	require.NoError(t, err)
	assert.Equal(t, "myuser", receivedUser)
	assert.Equal(t, "mypass", receivedPass)
}

func TestPushMetrics_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	series := []prompb.TimeSeries{{
		Labels:  []prompb.Label{{Name: "__name__", Value: "test_metric"}},
		Samples: []prompb.Sample{{Value: 1.0, Timestamp: 1000}},
	}}

	err := pushMetrics(server.URL, "", "", series)
	assert.Error(t, err)
}
