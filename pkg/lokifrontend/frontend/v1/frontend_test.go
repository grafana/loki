package v1

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/services"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing-contrib/go-stdlib/nethttp"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/jaeger-client-go"
	"github.com/uber/jaeger-client-go/config"
	httpgrpc_server "github.com/weaveworks/common/httpgrpc/server"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/user"
	"go.uber.org/atomic"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/lokifrontend/frontend/transport"
	"github.com/grafana/loki/pkg/lokifrontend/frontend/v1/frontendv1pb"
	loki_nats "github.com/grafana/loki/pkg/nats"
	querier_worker "github.com/grafana/loki/pkg/querier/worker"
	"github.com/grafana/loki/pkg/scheduler/queue"
)

const (
	query        = "/api/v1/query_range?end=1536716898&query=sum%28container_memory_rss%29+by+%28namespace%29&start=1536673680&step=120"
	responseBody = `{"status":"success","data":{"resultType":"Matrix","result":[{"metric":{"foo":"bar"},"values":[[1536673680,"137"],[1536673780,"137"]]}]}}`
)

func TestFrontend(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte("Hello World"))
		require.NoError(t, err)
	})
	test := func(addr string, _ *Frontend) {
		req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/", addr), nil)
		require.NoError(t, err)
		err = user.InjectOrgIDIntoHTTPRequest(user.InjectOrgID(context.Background(), "1"), req)
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, "Hello World", string(body))
	}

	testFrontend(t, defaultFrontendConfig(), handler, test, false, nil)
	testFrontend(t, defaultFrontendConfig(), handler, test, true, nil)
}

func TestFrontendPropagateTrace(t *testing.T) {
	closer, err := config.Configuration{}.InitGlobalTracer("test")
	require.NoError(t, err)
	defer closer.Close()

	observedTraceID := make(chan string, 2)

	handler := middleware.Tracer{}.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sp := opentracing.SpanFromContext(r.Context())
		defer sp.Finish()

		traceID := fmt.Sprintf("%v", sp.Context().(jaeger.SpanContext).TraceID())
		observedTraceID <- traceID

		_, err = w.Write([]byte(responseBody))
		require.NoError(t, err)
	}))

	test := func(addr string, _ *Frontend) {
		sp, ctx := opentracing.StartSpanFromContext(context.Background(), "client")
		defer sp.Finish()
		traceID := fmt.Sprintf("%v", sp.Context().(jaeger.SpanContext).TraceID())

		req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/%s", addr, query), nil)
		require.NoError(t, err)
		req = req.WithContext(ctx)
		err = user.InjectOrgIDIntoHTTPRequest(user.InjectOrgID(ctx, "1"), req)
		require.NoError(t, err)

		req, tr := nethttp.TraceRequest(opentracing.GlobalTracer(), req)
		defer tr.Finish()

		client := http.Client{
			Transport: &nethttp.Transport{},
		}
		resp, err := client.Do(req)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		defer resp.Body.Close()
		_, err = io.ReadAll(resp.Body)
		require.NoError(t, err)

		// Query should do one call.
		assert.Equal(t, traceID, <-observedTraceID)
	}
	testFrontend(t, defaultFrontendConfig(), handler, test, false, nil)
	testFrontend(t, defaultFrontendConfig(), handler, test, true, nil)
}

func TestFrontendCheckReady(t *testing.T) {
	for _, tt := range []struct {
		name             string
		connectedClients int
		msg              string
		readyForRequests bool
	}{
		{"connected clients are ready", 3, "", true},
		{"no url, no clients is not ready", 0, "not ready: number of queriers connected to query-frontend is 0", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.NewNopLogger()
			qm := queue.NewMetrics("query_frontend", nil)
			q, _ := queue.NewRequestQueue(5, 0, loki_nats.Config{}, logger, qm)
			f := &Frontend{
				log:          log.NewNopLogger(),
				requestQueue: q,
			}
			for i := 0; i < tt.connectedClients; i++ {
				f.requestQueue.RegisterQuerierConnection("test")
			}
			err := f.CheckReady(context.Background())
			errMsg := ""

			if err != nil {
				errMsg = err.Error()
			}

			require.Equal(t, tt.msg, errMsg)
		})
	}
}

// TestFrontendCancel ensures that when client requests are cancelled,
// the underlying query is correctly cancelled _and not retried_.
func TestFrontendCancel(t *testing.T) {
	var tries atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		tries.Inc()
	})
	test := func(addr string, _ *Frontend) {
		req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/", addr), nil)
		require.NoError(t, err)
		err = user.InjectOrgIDIntoHTTPRequest(user.InjectOrgID(context.Background(), "1"), req)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		req = req.WithContext(ctx)

		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		_, err = http.DefaultClient.Do(req)
		require.Error(t, err)

		time.Sleep(100 * time.Millisecond)
		assert.Equal(t, int32(1), tries.Load())
	}
	testFrontend(t, defaultFrontendConfig(), handler, test, false, nil)
	tries.Store(0)
	testFrontend(t, defaultFrontendConfig(), handler, test, true, nil)
}

func TestFrontendMetricsCleanup(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte("Hello World"))
		require.NoError(t, err)
	})

	for _, matchMaxConcurrency := range []bool{false, true} {
		reg := prometheus.NewPedanticRegistry()

		test := func(addr string, fr *Frontend) {
			req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/", addr), nil)
			require.NoError(t, err)
			err = user.InjectOrgIDIntoHTTPRequest(user.InjectOrgID(context.Background(), "1"), req)
			require.NoError(t, err)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode)
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			assert.Equal(t, "Hello World", string(body))

			require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
				# HELP cortex_query_frontend_queue_length Number of queries in the queue.
				# TYPE cortex_query_frontend_queue_length gauge
				cortex_query_frontend_queue_length{user="1"} 0
			`), "cortex_query_frontend_queue_length"))

			fr.cleanupInactiveUserMetrics("1")

			require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
				# HELP cortex_query_frontend_queue_length Number of queries in the queue.
				# TYPE cortex_query_frontend_queue_length gauge
			`), "cortex_query_frontend_queue_length"))
		}

		testFrontend(t, defaultFrontendConfig(), handler, test, matchMaxConcurrency, reg)
	}
}

func testFrontend(t *testing.T, config Config, handler http.Handler, test func(addr string, frontend *Frontend), matchMaxConcurrency bool, reg prometheus.Registerer) {
	logger := log.NewNopLogger()

	var workerConfig querier_worker.Config
	flagext.DefaultValues(&workerConfig)
	workerConfig.Parallelism = 1
	workerConfig.MatchMaxConcurrency = matchMaxConcurrency
	workerConfig.MaxConcurrentRequests = 1

	// localhost:0 prevents firewall warnings on Mac OS X.
	grpcListen, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	workerConfig.FrontendAddress = grpcListen.Addr().String()

	httpListen, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	v1, err := New(config, loki_nats.Config{}, limits{}, logger, reg)
	require.NoError(t, err)
	require.NotNil(t, v1)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), v1))
	defer func() {
		require.NoError(t, services.StopAndAwaitTerminated(context.Background(), v1))
	}()

	grpcServer := grpc.NewServer(
		grpc.StreamInterceptor(otgrpc.OpenTracingStreamServerInterceptor(opentracing.GlobalTracer())),
	)
	defer grpcServer.GracefulStop()

	frontendv1pb.RegisterFrontendServer(grpcServer, v1)

	// Default HTTP handler config.
	handlerCfg := transport.HandlerConfig{}
	flagext.DefaultValues(&handlerCfg)

	rt := transport.AdaptGrpcRoundTripperToHTTPRoundTripper(v1)
	r := mux.NewRouter()
	r.PathPrefix("/").Handler(middleware.Merge(
		middleware.AuthenticateUser,
		middleware.Tracer{},
	).Wrap(transport.NewHandler(handlerCfg, rt, logger, nil)))

	httpServer := http.Server{
		Handler: r,
	}
	defer httpServer.Shutdown(context.Background()) //nolint:errcheck

	go httpServer.Serve(httpListen) //nolint:errcheck
	go grpcServer.Serve(grpcListen) //nolint:errcheck

	var worker services.Service
	worker, err = querier_worker.NewQuerierWorker(workerConfig, nil, httpgrpc_server.NewServer(handler), loki_nats.Config{}, logger, nil)
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), worker))

	test(httpListen.Addr().String(), v1)

	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), worker))
}

func defaultFrontendConfig() Config {
	config := Config{}
	flagext.DefaultValues(&config)
	return config
}

type limits struct {
	queriers int
}

func (l limits) MaxQueriersPerUser(_ string) int {
	return l.queriers
}
