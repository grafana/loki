package loki

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/atomic"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/querier/worker"
)

func TestFlushShutdownQueryStats(t *testing.T) {
	t.Run("returns immediately when URL is empty", func(_ *testing.T) {
		flushShutdownQueryStats(worker.Config{}, prometheus.DefaultGatherer, log.NewNopLogger())
	})

	t.Run("pushes query-stats metric", func(t *testing.T) {
		var (
			requestCount  atomic.Int64
			requestPath   atomic.Value
			requestMethod atomic.Value
		)

		requestPath.Store("")
		requestMethod.Store("")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount.Add(1)
			requestPath.Store(r.URL.Path)
			requestMethod.Store(r.Method)
			require.NoError(t, r.Body.Close())
			w.WriteHeader(http.StatusAccepted)
		}))
		defer server.Close()

		reg := prometheus.NewRegistry()
		queryStats := prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: queryStatsBytesProcessedMetricName,
			Help: "Total number of bytes processed by LogQL queries, partitioned by tenant.",
		}, []string{"tenant"})
		reg.MustRegister(queryStats)
		queryStats.WithLabelValues("tenant-a").Add(123)

		cfg := worker.Config{
			QuerierID:                        "querier-1",
			ShutdownQueryStatsPushGatewayURL: server.URL,
			ShutdownQueryStatsPushJobName:    "loki-querier-shutdown",
			ShutdownQueryStatsPushTimeout:    time.Second,
		}

		flushShutdownQueryStats(cfg, reg, log.NewNopLogger())
		require.EqualValues(t, 1, requestCount.Load())
		require.Equal(t, http.MethodPut, requestMethod.Load().(string))
		require.True(t, strings.Contains(requestPath.Load().(string), "/metrics/job/loki-querier-shutdown"))
		require.True(t, strings.Contains(requestPath.Load().(string), "/component/querier"))
		require.True(t, strings.Contains(requestPath.Load().(string), "/instance/querier-1"))
	})

	t.Run("skips push if metric family does not exist", func(t *testing.T) {
		var requestCount atomic.Int64

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requestCount.Add(1)
			w.WriteHeader(http.StatusAccepted)
		}))
		defer server.Close()

		cfg := worker.Config{
			ShutdownQueryStatsPushGatewayURL: server.URL,
			ShutdownQueryStatsPushJobName:    "loki-querier-shutdown",
			ShutdownQueryStatsPushTimeout:    time.Second,
		}

		flushShutdownQueryStats(cfg, prometheus.NewRegistry(), log.NewNopLogger())
		require.EqualValues(t, 0, requestCount.Load())
	})
}
