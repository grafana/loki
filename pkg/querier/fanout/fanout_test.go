package fanout

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/remote"
	"github.com/grafana/loki/pkg/util/marshal"
	serverutil "github.com/grafana/loki/pkg/util/server"
)

const (
	// Custom query timeout used in tests
	queryTimeout = 13 * time.Second
)

func TestQuerier_ReadBatch(t *testing.T) {
	server := &mockLokiHTTPServer{server: &http.Server{Addr: ":3100", Handler: nil}}
	from := time.Now().Add(time.Minute * -5)
	server.Run(t, from)
	time.Sleep(time.Second)
	defer server.Stop(t)
	remoteConf := remote.ReadConfig{
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

	querier, err := remote.NewQuerier("test", remoteConf)
	require.NoError(t, err)

	remoteConf2 := remote.ReadConfig{
		Name:          "remote-read-2",
		RemoteTimeout: queryTimeout,
		URL: &config_util.URL{
			URL: &url.URL{
				Scheme: "http",
				Host:   "localhost:3100",
			},
		},
		OrgID: "team2",
	}

	querier2, err := remote.NewQuerier("test", remoteConf2)
	require.NoError(t, err)

	fanoutQuerier := NewQuerier(querier, 5, 2, querier2)

	request := logproto.QueryRequest{
		Selector:  `{app="distributor"}`,
		Limit:     10,
		Start:     time.Now().Add(time.Minute * -5),
		End:       time.Now(),
		Direction: logproto.FORWARD,
	}

	iter, err := fanoutQuerier.SelectLogs(
		context.Background(),
		logql.SelectLogParams{QueryRequest: &request},
	)
	require.NoError(t, err)
	count := 0
	for iter.Next() {
		require.Equal(t, true, len(iter.Labels()) > 10)
		require.Equal(t, true, len(iter.Entry().Line) > 0)
		count++
	}
	require.Equal(t, 20, count)

	end := time.Now()
	mockLabelRequest := func(name string) *logproto.LabelRequest {
		return &logproto.LabelRequest{
			Name:  name,
			Start: &from,
			End:   &end,
		}
	}
	require.NoError(t, err)

	_, err = fanoutQuerier.Label(
		context.Background(),
		mockLabelRequest("app"),
	)
	require.NoError(t, err)

	req := &logproto.SeriesRequest{
		Start: time.Unix(0, 0),
		End:   time.Unix(10, 0),
	}
	_, err = fanoutQuerier.Series(
		context.Background(),
		req,
	)

	require.NoError(t, err)

}

func TestQuerier_Read(t *testing.T) {
	server := &mockLokiHTTPServer{server: &http.Server{Addr: ":3100", Handler: nil}}
	from := time.Now().Add(time.Minute * -5)
	server.Run(t, from)
	time.Sleep(time.Second)
	defer server.Stop(t)
	remoteConf := remote.ReadConfig{
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

	querier, err := remote.NewQuerier("test", remoteConf)
	require.NoError(t, err)

	remoteConf2 := remote.ReadConfig{
		Name:          "remote-read-2",
		RemoteTimeout: queryTimeout,
		URL: &config_util.URL{
			URL: &url.URL{
				Scheme: "http",
				Host:   "localhost:3100",
			},
		},
		OrgID: "team2",
	}

	querier2, err := remote.NewQuerier("test", remoteConf2)
	require.NoError(t, err)

	fanoutQuerier := NewQuerier(querier, 5, 0, querier2)

	request := logproto.QueryRequest{
		Selector:  `{app="distributor"}`,
		Limit:     10,
		Start:     time.Now().Add(time.Minute * -5),
		End:       time.Now(),
		Direction: logproto.FORWARD,
	}

	iter, err := fanoutQuerier.SelectLogs(
		context.Background(),
		logql.SelectLogParams{QueryRequest: &request},
	)
	require.NoError(t, err)
	count := 0
	for iter.Next() {
		require.Equal(t, true, len(iter.Labels()) > 10)
		require.Equal(t, true, len(iter.Entry().Line) > 0)
		count++
	}
	require.Equal(t, 20, count)

	end := time.Now()
	mockLabelRequest := func(name string) *logproto.LabelRequest {
		return &logproto.LabelRequest{
			Name:  name,
			Start: &from,
			End:   &end,
		}
	}
	require.NoError(t, err)

	_, err = fanoutQuerier.Label(
		context.Background(),
		mockLabelRequest("app"),
	)
	require.NoError(t, err)

	req := &logproto.SeriesRequest{
		Start: time.Unix(0, 0),
		End:   time.Unix(10, 0),
	}
	_, err = fanoutQuerier.Series(
		context.Background(),
		req,
	)

	require.NoError(t, err)

}

func TestQuerier_MetricsQuery(t *testing.T) {
	server := &mockLokiHTTPServer{server: &http.Server{Addr: ":3100", Handler: nil}}
	from := time.Now().Add(time.Minute * -5)
	server.Run(t, from)
	time.Sleep(time.Second)
	defer server.Stop(t)
	remoteConf := remote.ReadConfig{
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

	querier, err := remote.NewQuerier("test", remoteConf)
	require.NoError(t, err)

	remoteConf2 := remote.ReadConfig{
		Name:          "remote-read-2",
		RemoteTimeout: queryTimeout,
		URL: &config_util.URL{
			URL: &url.URL{
				Scheme: "http",
				Host:   "localhost:3100",
			},
		},
		OrgID: "team2",
	}

	querier2, err := remote.NewQuerier("test", remoteConf2)
	require.NoError(t, err)

	fanoutQuerier := NewQuerier(querier, 5, 0, querier2)

	request := logproto.SampleQueryRequest{
		Selector: `sum(count_over_time({app="distributor"}[1m]))`,
		Start:    time.Now().Add(time.Minute * -5),
		End:      time.Now(),
	}

	iter, err := fanoutQuerier.SelectSamples(
		context.Background(), logql.SelectSampleParams{SampleQueryRequest: &request},
	)
	require.NoError(t, err)
	count := 0
	for iter.Next() {
		require.Equal(t, true, len(iter.Labels()) > 10)
		count++
	}
	require.Equal(t, 6, count)

}

type mockLokiHTTPServer struct {
	server *http.Server
}

func (s *mockLokiHTTPServer) Run(t *testing.T, from time.Time) {
	var mux http.ServeMux
	mux.HandleFunc("/loki/api/v1/query_range", func(w http.ResponseWriter, r *http.Request) {

		err := r.ParseForm()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		request, err := loghttp.ParseRangeQuery(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		expr, err := syntax.ParseExpr(request.Query)
		if err != nil {
			serverutil.WriteError(err, w)
			return
		}
		var mockData logqlmodel.Result
		switch expr.(type) {
		case syntax.LogSelectorExpr:
			mockData = morkSelectData(request.Limit, from)
		case syntax.SampleExpr:
			mockData = morkMetricsQueryData()
		default:
			t.Fatal("error expr type")
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

	s.server.Handler = &mux
	go func() {
		_ = s.server.ListenAndServe()
	}()
}

func (s *mockLokiHTTPServer) Stop(t *testing.T) {
	err := s.server.Shutdown(context.Background())
	require.NoError(t, err)
}
func morkSelectData(limit uint32, from time.Time) logqlmodel.Result {
	data := make([]logproto.Entry, 0)
	for i := 0; i < int(limit); i++ {
		dur := time.Duration(i) * time.Millisecond
		newUUID, err := uuid.NewUUID()
		if err != nil {
			panic(err)
		}
		data = append(data, logproto.Entry{
			Timestamp: from.Add(dur),
			Line:      "log+" + strconv.Itoa(i) + newUUID.String(),
		})
	}

	mockData := logqlmodel.Result{
		Statistics: stats.Result{
			Summary: stats.Summary{QueueTime: 1, ExecTime: 2},
		},

		Data: logqlmodel.Streams{{
			Labels:  `{foo="bar"}`,
			Entries: data,
		}},
	}
	return mockData
}

func morkMetricsQueryData() logqlmodel.Result {
	mockData := logqlmodel.Result{
		Statistics: stats.Result{
			Summary: stats.Summary{QueueTime: 1, ExecTime: 2},
		},
		Data: promql.Matrix{
			{
				Floats: []promql.FPoint{
					{
						T: 1568404331324,
						F: 0.013333333333333334,
					},
				},
				Metric: []labels.Label{
					{
						Name:  "filename",
						Value: `/var/hostlog/apport.log`,
					},
					{
						Name:  "job",
						Value: "varlogs",
					},
				},
			},
			{
				Floats: []promql.FPoint{
					{
						T: 1568404331324,
						F: 3.45,
					},
					{
						T: 1568404331339,
						F: 4.45,
					},
				},
				Metric: []labels.Label{
					{
						Name:  "filename",
						Value: `/var/hostlog/syslog`,
					},
					{
						Name:  "job",
						Value: "varlogs",
					},
				},
			},
		},
	}
	return mockData
}
