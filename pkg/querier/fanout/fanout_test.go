package fanout

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	serverutil "github.com/grafana/loki/pkg/util/server"
	config_util "github.com/prometheus/common/config"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/remote"
	"github.com/grafana/loki/pkg/util/marshal"
)

const (
	// Custom query timeout used in tests
	queryTimeout = 12 * time.Second
)

func TestQuerier_SelectLog(t *testing.T) {
	server := &mockLokiHTTPServer{server: &http.Server{Addr: ":3100", Handler: nil}}
	from := time.Now().Add(time.Minute * -5)
	server.Run(t, from)
	//wg.Wait()
	defer server.Stop(t)
	remoteConf := remote.RemoteReadConfig{
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

	remoteConf2 := remote.RemoteReadConfig{
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

	fanoutQuerier := NewFanoutQuerier(querier, querier2)

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
	require.Equal(t, 12, count)

	end := time.Now()
	mockLabelRequest := func(name string) *logproto.LabelRequest {
		return &logproto.LabelRequest{
			Name:   name,
			Values: name != "",
			Start:  &from,
			End:    &end,
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

type mockLokiHTTPServer struct {
	server *http.Server
}

func (s *mockLokiHTTPServer) Run(t *testing.T, from time.Time) {
	var mux http.ServeMux
	mux.HandleFunc("/loki/api/v1/query_range", func(w http.ResponseWriter, request *http.Request) {
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
	s.server.Handler = &mux
	go func() {
		err := s.server.ListenAndServe()
		require.NoError(t, err)

	}()
}

func (s *mockLokiHTTPServer) Stop(t *testing.T) {
	err := s.server.Shutdown(context.Background())
	require.NoError(t, err)
}
