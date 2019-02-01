package server

import (
	"errors"
	"flag"
	"net/http"
	"strconv"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	google_protobuf "github.com/golang/protobuf/ptypes/empty"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/middleware"
	"golang.org/x/net/context"
)

type FakeServer struct{}

func (f FakeServer) FailWithError(ctx context.Context, req *google_protobuf.Empty) (*google_protobuf.Empty, error) {
	return nil, errors.New("test error")
}

func (f FakeServer) FailWithHTTPError(ctx context.Context, req *FailWithHTTPErrorRequest) (*google_protobuf.Empty, error) {
	return nil, httpgrpc.Errorf(int(req.Code), strconv.Itoa(int(req.Code)))
}

func (f FakeServer) Succeed(ctx context.Context, req *google_protobuf.Empty) (*google_protobuf.Empty, error) {
	return &google_protobuf.Empty{}, nil
}

func TestErrorInstrumentationMiddleware(t *testing.T) {
	var cfg Config
	cfg.RegisterFlags(flag.NewFlagSet("", flag.ExitOnError))
	cfg.GRPCListenPort = 1234
	server, err := New(cfg)
	require.NoError(t, err)

	fakeServer := FakeServer{}
	RegisterFakeServerServer(server.GRPC, fakeServer)

	go server.Run()
	defer server.Shutdown()

	conn, err := grpc.Dial("localhost:1234", grpc.WithInsecure())
	defer conn.Close()
	require.NoError(t, err)

	empty := google_protobuf.Empty{}
	client := NewFakeServerClient(conn)
	res, err := client.Succeed(context.Background(), &empty)
	require.NoError(t, err)
	require.EqualValues(t, &empty, res)

	res, err = client.FailWithError(context.Background(), &empty)
	require.Nil(t, res)
	require.Error(t, err)

	s, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, "test error", s.Message())

	res, err = client.FailWithHTTPError(context.Background(), &FailWithHTTPErrorRequest{Code: http.StatusPaymentRequired})
	require.Nil(t, res)
	errResp, ok := httpgrpc.HTTPResponseFromError(err)
	require.True(t, ok)
	require.Equal(t, int32(http.StatusPaymentRequired), errResp.Code)
	require.Equal(t, "402", string(errResp.Body))

	conn.Close()

	metrics, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	statuses := map[string]string{}
	for _, family := range metrics {
		if *family.Name == "request_duration_seconds" {
			for _, metric := range family.Metric {
				var route, statusCode string
				for _, label := range metric.GetLabel() {
					switch label.GetName() {
					case "status_code":
						statusCode = label.GetValue()
					case "route":
						route = label.GetValue()
					}
				}
				statuses[route] = statusCode
			}
		}
	}
	require.Equal(t, map[string]string{
		"/server.FakeServer/FailWithError":     "error",
		"/server.FakeServer/FailWithHTTPError": "402",
		"/server.FakeServer/Succeed":           "success",
	}, statuses)
}

func TestRunReturnsError(t *testing.T) {
	cfg := Config{
		HTTPListenPort: 9190,
		GRPCListenPort: 9191,
	}
	t.Run("http", func(t *testing.T) {
		cfg.MetricsNamespace = "testing_http"
		srv, err := New(cfg)
		require.NoError(t, err)

		errChan := make(chan error, 1)
		go func() {
			errChan <- srv.Run()
		}()

		require.NoError(t, srv.httpListener.Close())
		require.NotNil(t, <-errChan)

		// So that address is freed for further tests.
		srv.GRPC.Stop()
	})

	t.Run("grpc", func(t *testing.T) {
		cfg.MetricsNamespace = "testing_grpc"
		srv, err := New(cfg)
		require.NoError(t, err)

		errChan := make(chan error, 1)
		go func() {
			errChan <- srv.Run()
		}()

		require.NoError(t, srv.grpcListener.Close())
		require.NotNil(t, <-errChan)
	})
}

// Test to see what the logging of a 500 error looks like
func TestMiddlewareLogging(t *testing.T) {
	var level logging.Level
	level.Set("info")
	cfg := Config{
		HTTPListenPort:   9192,
		HTTPMiddleware:   []middleware.Interface{middleware.Logging},
		MetricsNamespace: "testing_logging",
		LogLevel:         level,
	}
	server, err := New(cfg)
	require.NoError(t, err)

	server.HTTP.HandleFunc("/error500", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})

	go server.Run()
	defer server.Shutdown()

	req, err := http.NewRequest("GET", "http://127.0.0.1:9192/error500", nil)
	require.NoError(t, err)
	http.DefaultClient.Do(req)
}
