package server

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof" // anonymous import to get the pprof handler registered
	"time"

	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/mwitkow/go-grpc-middleware"
	"github.com/opentracing-contrib/go-stdlib/nethttp"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/weaveworks-experiments/loki/pkg/client"
	"github.com/weaveworks/common/httpgrpc"
	httpgrpc_server "github.com/weaveworks/common/httpgrpc/server"
	"github.com/weaveworks/common/instrument"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/signals"
)

func init() {
	tracer, err := loki.NewTracer()
	if err != nil {
		panic(fmt.Sprintf("Failed to create tracer: %v", err))
	} else {
		opentracing.InitGlobalTracer(tracer)
	}
}

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

	GRPCOptions    []grpc.ServerOption
	GRPCMiddleware []grpc.UnaryServerInterceptor
	HTTPMiddleware []middleware.Interface
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&cfg.HTTPListenPort, "server.http-listen-port", 80, "HTTP server listen port.")
	f.IntVar(&cfg.GRPCListenPort, "server.grpc-listen-port", 9095, "gRPC server listen port.")
	f.BoolVar(&cfg.RegisterInstrumentation, "server.register-instrumentation", true, "Register the intrumentation handlers (/metrics etc).")
	f.DurationVar(&cfg.ServerGracefulShutdownTimeout, "server.graceful-shutdown-timeout", 5*time.Second, "Timeout for graceful shutdowns")
	f.DurationVar(&cfg.HTTPServerReadTimeout, "server.http-read-timeout", 5*time.Second, "Read timeout for HTTP server")
	f.DurationVar(&cfg.HTTPServerWriteTimeout, "server.http-write-timeout", 5*time.Second, "Write timeout for HTTP server")
	f.DurationVar(&cfg.HTTPServerIdleTimeout, "server.http-idle-timeout", 120*time.Second, "Idle timeout for HTTP server")
}

// Server wraps a HTTP and gRPC server, and some common initialization.
//
// Servers will be automatically instrumented for Prometheus metrics
// and Loki tracing.  HTTP over gRPC
type Server struct {
	cfg          Config
	handler      *signals.Handler
	httpListener net.Listener
	grpcListener net.Listener
	httpServer   *http.Server

	HTTP *mux.Router
	GRPC *grpc.Server
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

	// Setup gRPC server
	serverLog := middleware.GRPCServerLog{WithRequest: !cfg.ExcludeRequestInLog}
	grpcMiddleware := []grpc.UnaryServerInterceptor{
		serverLog.UnaryServerInterceptor,
		middleware.ServerInstrumentInterceptor(requestDuration),
		otgrpc.OpenTracingServerInterceptor(opentracing.GlobalTracer()),
	}
	grpcMiddleware = append(grpcMiddleware, cfg.GRPCMiddleware...)
	grpcOptions := []grpc.ServerOption{
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
			grpcMiddleware...,
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
		middleware.Log{},
		middleware.Instrument{
			Duration:     requestDuration,
			RouteMatcher: router,
		},
		middleware.Func(func(handler http.Handler) http.Handler {
			return nethttp.Middleware(opentracing.GlobalTracer(), handler)
		}),
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
		httpServer:   httpServer,
		handler:      signals.NewHandler(log.StandardLogger()),

		HTTP: router,
		GRPC: grpcServer,
	}, nil
}

// RegisterInstrumentation on the given router.
func RegisterInstrumentation(router *mux.Router) {
	router.Handle("/metrics", prometheus.Handler())
	router.Handle("/traces", loki.Handler())
	router.PathPrefix("/debug/pprof").Handler(http.DefaultServeMux)
}

// Run the server; blocks until SIGTERM is received.
func (s *Server) Run() {
	go s.httpServer.Serve(s.httpListener)

	// Setup gRPC server
	// for HTTP over gRPC, ensure we don't double-count the middleware
	httpgrpc.RegisterHTTPServer(s.GRPC, httpgrpc_server.NewServer(s.HTTP))
	go s.GRPC.Serve(s.grpcListener)
	defer s.GRPC.GracefulStop()

	// Wait for a signal
	s.handler.Loop()
}

// Stop unblocks Run().
func (s *Server) Stop() {
	s.handler.Stop()
}

// Shutdown the server, gracefully.  Should be defered after New().
func (s *Server) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.ServerGracefulShutdownTimeout)
	defer cancel() // releases resources if httpServer.Shutdown completes before timeout elapses

	s.httpServer.Shutdown(ctx)
	s.GRPC.Stop()
}
