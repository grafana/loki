package remote

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	config_util "github.com/prometheus/common/config"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/util/marshal"
	serverutil "github.com/grafana/loki/pkg/util/server"
)

const (
	// Custom query timeout used in tests
	queryTimeout = 12 * time.Second
)

func TestQuerier_Read(t *testing.T) {
	server := &mockLokiHTTPServer{server: &http.Server{Addr: ":3100", Handler: nil}}
	from := time.Now().Add(time.Minute * -5)
	server.Run(t, from)
	time.Sleep(time.Second)
	defer server.Stop(t)
	remoteConf := ReadConfig{
		Name:          "remote-read-1",
		RemoteTimeout: queryTimeout,
		URL: &config_util.URL{
			URL: &url.URL{
				Scheme: "http",
				Host:   "localhost:3100",
			},
		},
		OrgID: "team1",
	}

	querier, err := NewQuerier("test", remoteConf)
	require.NoError(t, err)

	request := logproto.QueryRequest{
		Selector:  `{app="distributor"}`,
		Limit:     6,
		Start:     from,
		End:       time.Now(),
		Direction: logproto.FORWARD,
	}

	runtimeTenantID := "runtime1"
	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, runtimeTenantID)
	iter, err := querier.SelectLogs(
		ctx,
		logql.SelectLogParams{QueryRequest: &request},
	)
	require.Equal(t, runtimeTenantID, server.getTenantIDUnsafe())
	require.NoError(t, err)
	count := 0
	for iter.Next() {
		require.Equal(t, true, len(iter.Labels()) > 10)
		require.Equal(t, true, len(iter.Entry().Line) > 0)
		count++
	}
	require.Equal(t, 6, count)

	end := time.Now()
	mockLabelRequest := func(name string) *logproto.LabelRequest {
		return &logproto.LabelRequest{
			Name:  name,
			Start: &from,
			End:   &end,
		}
	}

	_, err = querier.Label(
		context.Background(),
		mockLabelRequest("app"),
	)
	require.NoError(t, err)

	req := &logproto.SeriesRequest{
		Start: time.Unix(0, 0),
		End:   time.Unix(10, 0),
	}
	_, err = querier.Series(
		context.Background(),
		req,
	)

	require.NoError(t, err)

	volumeReq := &logproto.VolumeRequest{
		From:     0,
		Through:  1000,
		Matchers: `{foo="bar"}`,
		Limit:    10,
	}
	_, err = querier.Volume(
		context.Background(),
		volumeReq,
	)
	require.NoError(t, err)

}

type mockLokiHTTPServer struct {
	server   *http.Server
	tenantID string
}

func (s *mockLokiHTTPServer) getTenantIDUnsafe() string {
	return s.tenantID
}

func (s *mockLokiHTTPServer) Run(t *testing.T, from time.Time) {
	var mux http.ServeMux
	mux.HandleFunc("/loki/api/v1/query_range", func(w http.ResponseWriter, request *http.Request) {
		tenantID, _, err := user.ExtractOrgIDFromHTTPRequest(request)
		if err != nil {
			return
		}
		s.tenantID = tenantID
		mockData := logqlmodel.Result{
			Statistics: stats.Result{
				Summary: stats.Summary{QueueTime: 1, ExecTime: 2},
			},
			Data: logqlmodel.Streams{{
				Labels: `{foo="bar"}`,
				Entries: []logproto.Entry{
					{
						Timestamp: from,
						Line:      "1",
					},
					{
						Timestamp: from.Add(time.Millisecond),
						Line:      "2",
					},
					{
						Timestamp: from.Add(2 * time.Millisecond),
						Line:      "3",
					},
					{
						Timestamp: from.Add(3 * time.Millisecond),
						Line:      "4",
					},
					{
						Timestamp: from.Add(4 * time.Millisecond),
						Line:      "5",
					},
					{
						Timestamp: from.Add(5 * time.Millisecond),
						Line:      "6",
					},
				},
			}},
		}
		if err := marshal.WriteQueryResponseJSON(mockData, w); err != nil {
			serverutil.WriteError(err, w)
			return
		}
	})

	mux.HandleFunc("/loki/api/v1/labels", func(w http.ResponseWriter, request *http.Request) {
		lvs := logproto.LabelResponse{Values: []string{"test2"}}
		if err := marshal.WriteLabelResponseJSON(lvs, w); err != nil {
			serverutil.WriteError(err, w)
			return
		}
	})

	mux.HandleFunc("/loki/api/v1/series", func(w http.ResponseWriter, request *http.Request) {
		series := logproto.SeriesResponse{Series: []logproto.SeriesIdentifier{{Labels: map[string]string{"test": "test"}}}}
		if err := marshal.WriteSeriesResponseJSON(series, w); err != nil {
			serverutil.WriteError(err, w)
			return
		}
	})

	mux.HandleFunc("/loki/api/v1/index/series_volume", func(w http.ResponseWriter, request *http.Request) {
		seriesVolume := logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{Name: `{foo="bar"}`, Volume: 1024},
				{Name: `{bar="baz"}`, Volume: 3350},
			},
			Limit: 5,
		}
		if err := marshal.WriteVolumeResponseJSON(&seriesVolume, w); err != nil {
			serverutil.WriteError(err, w)
			return
		}
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
