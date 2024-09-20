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
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing-contrib/go-stdlib/nethttp"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/jaeger-client-go"
	"github.com/uber/jaeger-client-go/config"
	"go.uber.org/atomic"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/lokifrontend/frontend/transport"
	"github.com/grafana/loki/v3/pkg/lokifrontend/frontend/v1/frontendv1pb"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	querier_worker "github.com/grafana/loki/v3/pkg/querier/worker"
	"github.com/grafana/loki/v3/pkg/queue"
	"github.com/grafana/loki/v3/pkg/scheduler/limits"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	query        = "/api/v1/query_range?end=1536716898&query=sum%28container_memory_rss%29+by+%28namespace%29&start=1536673680&step=120"
	responseBody = `{"status":"success","data":{"resultType":"Matrix","result":[{"metric":{"foo":"bar"},"values":[[1536673680,"137"],[1536673780,"137"]]}]}}`
	labelQuery   = `/api/v1/label/foo/values`
)

func TestFrontend(t *testing.T) {
	handler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return &queryrange.LokiLabelNamesResponse{Data: []string{"Hello", "world"}, Version: uint32(loghttp.VersionV1)}, nil
	})
	test := func(addr string, _ *Frontend) {
		req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/%s", addr, labelQuery), nil)
		require.NoError(t, err)
		err = user.InjectOrgIDIntoHTTPRequest(user.InjectOrgID(context.Background(), "1"), req)
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.JSONEq(t, `{"values":["Hello", "world"]}`, string(body))
	}

	testFrontend(t, defaultFrontendConfig(), handler, test, false, nil)
	testFrontend(t, defaultFrontendConfig(), handler, test, true, nil)
}

func TestFrontendPropagateTrace(t *testing.T) {
	closer, err := config.Configuration{}.InitGlobalTracer("test")
	require.NoError(t, err)
	defer closer.Close()

	observedTraceID := make(chan string, 2)

	handler := queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		sp := opentracing.SpanFromContext(ctx)
		defer sp.Finish()

		traceID := fmt.Sprintf("%v", sp.Context().(jaeger.SpanContext).TraceID())
		observedTraceID <- traceID

		return &queryrange.LokiLabelNamesResponse{Data: []string{"Hello", "world"}, Version: uint32(loghttp.VersionV1)}, nil
	})

	test := func(addr string, _ *Frontend) {
		sp, ctx := opentracing.StartSpanFromContext(context.Background(), "client")
		defer sp.Finish()
		traceID := fmt.Sprintf("%v", sp.Context().(jaeger.SpanContext).TraceID())

		req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/%s", addr, labelQuery), nil)
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
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.JSONEq(t, `{"values":["Hello", "world"]}`, string(body))

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
			qm := queue.NewMetrics(nil, constants.Loki, "query_frontend")
			f := &Frontend{
				log:          log.NewNopLogger(),
				requestQueue: queue.NewRequestQueue(5, 0, limits.NewQueueLimits(nil), qm),
			}
			for i := 0; i < tt.connectedClients; i++ {
				f.requestQueue.RegisterConsumerConnection("test")
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
	handler := queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		<-ctx.Done()
		tries.Inc()
		return nil, ctx.Err()
	})
	test := func(addr string, _ *Frontend) {
		req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/%s", addr, labelQuery), nil)
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
	handler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return &queryrange.LokiLabelNamesResponse{Data: []string{"Hello", "world"}, Version: uint32(loghttp.VersionV1)}, nil
	})

	for _, matchMaxConcurrency := range []bool{false, true} {
		reg := prometheus.NewPedanticRegistry()

		test := func(addr string, fr *Frontend) {
			req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/%s", addr, labelQuery), nil)
			require.NoError(t, err)
			err = user.InjectOrgIDIntoHTTPRequest(user.InjectOrgID(context.Background(), "1"), req)
			require.NoError(t, err)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode)
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			assert.JSONEq(t, `{"values":["Hello", "world"]}`, string(body))

			require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
				# HELP loki_query_frontend_queue_length Number of queries in the queue.
				# TYPE loki_query_frontend_queue_length gauge
				loki_query_frontend_queue_length{user="1"} 0
			`), "loki_query_frontend_queue_length"))

			fr.cleanupInactiveUserMetrics("1")

			require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
				# HELP loki_query_frontend_queue_length Number of queries in the queue.
				# TYPE loki_query_frontend_queue_length gauge
			`), "loki_query_frontend_queue_length"))
		}

		testFrontend(t, defaultFrontendConfig(), handler, test, matchMaxConcurrency, reg)
	}
}

func testFrontend(t *testing.T, config Config, handler queryrangebase.Handler, test func(addr string, frontend *Frontend), _ bool, reg prometheus.Registerer) {
	logger := log.NewNopLogger()

	var workerConfig querier_worker.Config
	flagext.DefaultValues(&workerConfig)
	workerConfig.MaxConcurrent = 1

	// localhost:0 prevents firewall warnings on Mac OS X.
	grpcListen, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	workerConfig.FrontendAddress = grpcListen.Addr().String()

	httpListen, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	v1, err := New(config, mockLimits{}, logger, reg, constants.Loki)
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

	rt := queryrange.NewSerializeHTTPHandler(transport.AdaptGrpcRoundTripperToHandler(v1, queryrange.DefaultCodec), queryrange.DefaultCodec)
	r := mux.NewRouter()
	r.PathPrefix("/").Handler(middleware.Merge(
		middleware.AuthenticateUser,
		middleware.Tracer{},
	).Wrap(rt))

	httpServer := http.Server{
		Handler: r,
	}
	defer httpServer.Shutdown(context.Background()) //nolint:errcheck

	go httpServer.Serve(httpListen) //nolint:errcheck
	go grpcServer.Serve(grpcListen) //nolint:errcheck

	var worker services.Service
	worker, err = querier_worker.NewQuerierWorker(workerConfig, nil, handler, logger, nil, queryrange.DefaultCodec)
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

type mockLimits struct {
	queriers      uint
	queryCapacity float64
}

func (l mockLimits) MaxQueriersPerUser(_ string) uint {
	return l.queriers
}

func (l mockLimits) MaxQueryCapacity(_ string) float64 {
	return l.queryCapacity
}
