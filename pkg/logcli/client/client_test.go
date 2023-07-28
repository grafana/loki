package client

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logcli/volume"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_buildURL(t *testing.T) {
	tests := []struct {
		name    string
		u, p, q string
		want    string
		wantErr bool
	}{
		{"err", "8://2", "/bar", "", "", true},
		{"strip /", "http://localhost//", "//bar", "a=b", "http://localhost/bar?a=b", false},
		{"sub path", "https://localhost/loki/", "/bar/foo", "c=d&e=f", "https://localhost/loki/bar/foo?c=d&e=f", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildURL(tt.u, tt.p, tt.q)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("buildURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getHTTPRequestHeader(t *testing.T) {
	tests := []struct {
		name    string
		client  DefaultClient
		want    http.Header
		wantErr bool
	}{
		{"empty", DefaultClient{}, http.Header{}, false},
		{"partial-headers", DefaultClient{
			OrgID:     "124",
			QueryTags: "source=abc",
		}, http.Header{
			"X-Scope-OrgID": []string{"124"},
			"X-Query-Tags":  []string{"source=abc"},
		}, false},
		{"basic-auth", DefaultClient{
			Username: "123",
			Password: "secure",
		}, http.Header{
			"Authorization": []string{"Basic " + base64.StdEncoding.EncodeToString([]byte("123:secure"))},
		}, false},
		{"bearer-token", DefaultClient{
			BearerToken: "secureToken",
		}, http.Header{
			"Authorization": []string{"Bearer " + "secureToken"},
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.client.getHTTPRequestHeader()
			if (err != nil) != tt.wantErr {
				t.Errorf("getHTTPRequestHeader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// User-Agent should be set all the time.
			assert.Equal(t, got["User-Agent"], []string{userAgent})

			for k := range tt.want {
				ck := http.CanonicalHeaderKey(k)
				assert.Equal(t, tt.want[k], got[ck])
			}
		})
	}
}

func Test_DefaultClient(t *testing.T) {
	now := time.Now()
	then := now.Add(-1 * time.Hour)

	server := NewMockLokiHTTPServer(t, now, then)
	client := &DefaultClient{
		Address: fmt.Sprintf("http://127.0.0.1%s", server.server.Addr),
	}

	server.Run(t)
	time.Sleep(time.Second)
	defer server.Stop(t)

	t.Run("GetVolume happy path", func(t *testing.T) {
		query := volume.Query{
			QueryString:       `{foo="bar"}`,
			Start:             then,
			End:               now,
			Step:              10 * time.Minute,
			Quiet:             false,
			Limit:             10,
			TargetLabels:      []string{},
			AggregateByLabels: false,
		}
		resp, err := client.GetVolume(&query)
		require.NoError(t, err)

		require.Equal(t, loghttp.QueryResponse{
			Status: loghttp.QueryStatusSuccess,
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeVector,
				Result: loghttp.Vector{
					{
						Metric: map[model.LabelName]model.LabelValue{
							"foo": "bar",
						},
						Value:     42,
						Timestamp: model.TimeFromUnixNano(now.UnixNano()),
					},
				},
			},
		}, *resp)
	})

	t.Run("GetVolumeRange happy path", func(t *testing.T) {
		query := volume.Query{
			QueryString:       `{foo="bar"}`,
			Start:             then,
			End:               now,
			Step:              10 * time.Minute,
			Quiet:             false,
			Limit:             10,
			TargetLabels:      []string{},
			AggregateByLabels: false,
		}
		resp, err := client.GetVolumeRange(&query)
		require.NoError(t, err)

		require.Equal(t, loghttp.QueryResponse{
			Status: loghttp.QueryStatusSuccess,
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeMatrix,
				Result: loghttp.Matrix{
					{
						Metric: map[model.LabelName]model.LabelValue{"foo": "bar"},
						Values: []model.SamplePair{
							{
								Timestamp: model.TimeFromUnixNano(then.UnixNano()),
								Value:     42,
							},
							{
								Timestamp: model.TimeFromUnixNano(now.UnixNano()),
								Value:     47,
							},
						},
					},
				},
			},
		}, *resp)

	})

}

type mockLokiHTTPServer struct {
	server   *http.Server
	tenantID string
	now      time.Time
	then      time.Time
}

func NewMockLokiHTTPServer(t *testing.T, now, then time.Time) *mockLokiHTTPServer {
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)

	port := listener.Addr().(*net.TCPAddr).Port
	err = listener.Close()
	require.NoError(t, err)

	return &mockLokiHTTPServer{
		server: &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: nil},
		now:    now,
		then:   then,
	}
}

func (s *mockLokiHTTPServer) getTenantIDUnsafe() string {
	return s.tenantID
}

func (s *mockLokiHTTPServer) Run(t *testing.T) {
	var mux http.ServeMux
	mux.HandleFunc("/loki/api/v1/index/volume", func(w http.ResponseWriter, request *http.Request) {
		labels := labels.Labels{
			{
				Name:  "foo",
				Value: "bar",
			},
		}

		volume := queryrange.LokiPromResponse{
			Response: &queryrangebase.PrometheusResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: queryrangebase.PrometheusData{
					ResultType: loghttp.ResultTypeVector,
					Result: []queryrangebase.SampleStream{
						{
							Labels: logproto.FromLabelsToLabelAdapters(labels),
							Samples: []logproto.LegacySample{
								{
									Value:       42,
									TimestampMs: s.now.UnixNano() / 1e6,
								},
							},
						},
					},
				},
			},
			Statistics: stats.Result{},
		}

		codec := queryrange.Codec{}
		resp, err := codec.EncodeResponse(request.Context(), request, &volume)
		require.NoError(t, err)
		bytes, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		w.WriteHeader(resp.StatusCode)
		w.Write(bytes)
	})

	mux.HandleFunc("/loki/api/v1/index/volume_range", func(w http.ResponseWriter, request *http.Request) {
		labels := labels.Labels{
			{
				Name:  "foo",
				Value: "bar",
			},
		}

		volume := queryrange.LokiPromResponse{
			Response: &queryrangebase.PrometheusResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: queryrangebase.PrometheusData{
					ResultType: loghttp.ResultTypeMatrix,
					Result: []queryrangebase.SampleStream{
						{
							Labels: logproto.FromLabelsToLabelAdapters(labels),
							Samples: []logproto.LegacySample{
								{
									Value:       42,
									TimestampMs: s.then.UnixNano() / 1e6,
								},
								{
									Value:       47,
									TimestampMs: s.now.UnixNano() / 1e6,
								},
							},
						},
					},
				},
			},
			Statistics: stats.Result{},
		}

		codec := queryrange.Codec{}
		resp, err := codec.EncodeResponse(request.Context(), request, &volume)
		require.NoError(t, err)
		bytes, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		w.WriteHeader(resp.StatusCode)
		w.Write(bytes)
	})

	s.server.Handler = &mux
	go func() {
		_ = s.server.ListenAndServe()
	}()
}

func (s *mockLokiHTTPServer) Stop(t *testing.T) {
	err := s.server.Shutdown(context.Background())
	require.NoError(t, err)
}
