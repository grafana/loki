package server

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof" // anonymous import to get the pprof handler registered
	"time"

	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/go-grpc-middleware"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/weaveworks/common/httpgrpc"
	httpgrpc_server "github.com/weaveworks/common/httpgrpc/server"
	"github.com/weaveworks/common/instrument"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/signals"
)

// Config for a Server
type Config struct {
	MetricsNamespace string
	HTTPListenPort   int
	GRPCListenPort   int

	RegisterInstrumentation bool
	ExcludeRequestInLog     bool

	ServerGracefulShutdownTimeout time.Duration
	HTTPServerReadTimeout         time.Duration
	HTTPServerWriteTimeout        time.Duration
	HTTPServerIdleTimeout         time.Duration

	GRPCOptions          []grpc.ServerOption
	GRPCMiddleware       []grpc.UnaryServerInterceptor
	GRPCStreamMiddleware []grpc.StreamServerInterceptor
	HTTPMiddleware       []middleware.Interface

	LogLevel logging.Level
	Log      logging.Interface
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&cfg.HTTPListenPort, "server.http-listen-port", 80, "HTTP server listen port.")
	f.IntVar(&cfg.GRPCListenPort, "server.grpc-listen-port", 9095, "gRPC server listen port.")
	f.BoolVar(&cfg.RegisterInstrumentation, "server.register-instrumentation", true, "Register the intrumentation handlers (/metrics etc).")
	f.DurationVar(&cfg.ServerGracefulShutdownTimeout, "server.graceful-shutdown-timeout", 30*time.Second, "Timeout for graceful shutdowns")
	f.DurationVar(&cfg.HTTPServerReadTimeout, "server.http-read-timeout", 30*time.Second, "Read timeout for HTTP server")
	f.DurationVar(&cfg.HTTPServerWriteTimeout, "server.http-write-timeout", 30*time.Second, "Write timeout for HTTP server")
	f.DurationVar(&cfg.HTTPServerIdleTimeout, "server.http-idle-timeout", 120*time.Second, "Idle timeout for HTTP server")
	cfg.LogLevel.RegisterFlags(f)
}

// Server wraps a HTTP and gRPC server, and some common initialization.
//
// Servers will be automatically instrumented for Prometheus metrics.
type Server struct {
	cfg          Config
	handler      *signals.Handler
	grpcListener net.Listener
	httpListener net.Listener

	HTTP       *mux.Router
	HTTPServer *http.Server
	GRPC       *grpc.Server
	Log        logging.Interface
}

// New makes a new Server
func New(cfg Config) (*Server, error) {
	// Setup listeners first, so we can fail early if the port is in use.
	httpListener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.HTTPListenPort))
	if err != nil {
		return nil, err
	}

	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCListenPort))
	if err != nil {
		return nil, err
	}

	// Prometheus histograms for requests.
	requestDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: cfg.MetricsNamespace,
		Name:      "request_duration_seconds",
		Help:      "Time (in seconds) spent serving HTTP requests.",
		Buckets:   instrument.DefBuckets,
	}, []string{"method", "route", "status_code", "ws"})
	prometheus.MustRegister(requestDuration)

	// If user doesn't supply a logging implementation, by default instantiate
	// logrus.
	log := cfg.Log
	if log == nil {
		log = logging.NewLogrus(cfg.LogLevel)
	}

	// Setup gRPC server
	serverLog := middleware.GRPCServerLog{
		WithRequest: !cfg.ExcludeRequestInLog,
		Log:         log,
	}
	grpcMiddleware := []grpc.UnaryServerInterceptor{
		serverLog.UnaryServerInterceptor,
		middleware.UnaryServerInstrumentInterceptor(requestDuration),
		otgrpc.OpenTracingServerInterceptor(opentracing.GlobalTracer()),
	}
	grpcMiddleware = append(grpcMiddleware, cfg.GRPCMiddleware...)

	grpcStreamMiddleware := []grpc.StreamServerInterceptor{
		serverLog.StreamServerInterceptor,
		middleware.StreamServerInstrumentInterceptor(requestDuration),
		otgrpc.OpenTracingStreamServerInterceptor(opentracing.GlobalTracer()),
	}
	grpcStreamMiddleware = append(grpcStreamMiddleware, cfg.GRPCStreamMiddleware...)

	grpcOptions := []grpc.ServerOption{
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
			grpcMiddleware...,
		)),
		grpc.StreamInterceptor(grpc_middleware.ChainStreamServer(
			grpcStreamMiddleware...,
		)),
	}
	grpcOptions = append(grpcOptions, cfg.GRPCOptions...)
	grpcServer := grpc.NewServer(grpcOptions...)

	// Setup HTTP server
	router := mux.NewRouter()
	if cfg.RegisterInstrumentation {
		RegisterInstrumentation(router)
	}
	httpMiddleware := []middleware.Interface{
		middleware.Tracer{
			RouteMatcher: router,
		},
		middleware.Log{
			Log: log,
		},
		middleware.Instrument{
			Duration:     requestDuration,
			RouteMatcher: router,
		},
	}

	httpMiddleware = append(httpMiddleware, cfg.HTTPMiddleware...)
	httpServer := &http.Server{
		ReadTimeout:  cfg.HTTPServerReadTimeout,
		WriteTimeout: cfg.HTTPServerWriteTimeout,
		IdleTimeout:  cfg.HTTPServerIdleTimeout,
		Handler:      middleware.Merge(httpMiddleware...).Wrap(router),
	}

	return &Server{
		cfg:          cfg,
		httpListener: httpListener,
		grpcListener: grpcListener,
		handler:      signals.NewHandler(log),

		HTTP:       router,
		HTTPServer: httpServer,
		GRPC:       grpcServer,
		Log:        log,
	}, nil
}

// RegisterInstrumentation on the given router.
func RegisterInstrumentation(router *mux.Router) {
	router.Handle("/metrics", prometheus.Handler())
	router.PathPrefix("/debug/pprof").Handler(http.DefaultServeMux)
}

// Run the server; blocks until SIGTERM or an error is received.
func (s *Server) Run() error {
	errChan := make(chan error, 1)

	// Wait for a signal
	go func() {
		s.handler.Loop()
		select {
		case errChan <- nil:
		default:
		}
	}()

	go func() {
		err := s.HTTPServer.Serve(s.httpListener)
		if err == http.ErrServerClosed {
			err = nil
		}

		select {
		case errChan <- err:
		default:
		}
	}()

	// Setup gRPC server
	// for HTTP over gRPC, ensure we don't double-count the middleware
	httpgrpc.RegisterHTTPServer(s.GRPC, httpgrpc_server.NewServer(s.HTTP))

	go func() {
		err := s.GRPC.Serve(s.grpcListener)
		if err == grpc.ErrServerStopped {
			err = nil
		}

		select {
		case errChan <- err:
		default:
		}
	}()

	return <-errChan
}

// Stop unblocks Run().
func (s *Server) Stop() {
	s.handler.Stop()
}

// Shutdown the server, gracefully.  Should be defered after New().
func (s *Server) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.ServerGracefulShutdownTimeout)
	defer cancel() // releases resources if httpServer.Shutdown completes before timeout elapses

	s.HTTPServer.Shutdown(ctx)
	s.GRPC.GracefulStop()
}
