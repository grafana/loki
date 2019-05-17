package server

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof" // anonymous import to get the pprof handler registered
	"time"

	"github.com/gorilla/mux"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
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
	MetricsNamespace string `yaml:"-"`
	HTTPListenHost   string `yaml:"http_listen_host"`
	HTTPListenPort   int    `yaml:"http_listen_port"`
	GRPCListenHost   string `yaml:"grpc_listen_host"`
	GRPCListenPort   int    `yaml:"grpc_listen_port"`

	RegisterInstrumentation bool `yaml:"-"`
	ExcludeRequestInLog     bool `yaml:"-"`

	ServerGracefulShutdownTimeout time.Duration `yaml:"graceful_shutdown_timeout"`
	HTTPServerReadTimeout         time.Duration `yaml:"http_server_read_timeout"`
	HTTPServerWriteTimeout        time.Duration `yaml:"http_server_write_timeout"`
	HTTPServerIdleTimeout         time.Duration `yaml:"http_server_idle_timeout"`

	GRPCOptions          []grpc.ServerOption            `yaml:"-"`
	GRPCMiddleware       []grpc.UnaryServerInterceptor  `yaml:"-"`
	GRPCStreamMiddleware []grpc.StreamServerInterceptor `yaml:"-"`
	HTTPMiddleware       []middleware.Interface         `yaml:"-"`

	GPRCServerMaxRecvMsgSize       int  `yaml:"grpc_server_max_recv_msg_size"`
	GRPCServerMaxSendMsgSize       int  `yaml:"grpc_server_max_send_msg_size"`
	GPRCServerMaxConcurrentStreams uint `yaml:"grpc_server_max_concurrent_streams"`

	LogLevel logging.Level     `yaml:"log_level"`
	Log      logging.Interface `yaml:"-"`

	PathPrefix string `yaml:"http_path_prefix"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.HTTPListenHost, "server.http-listen-host", "", "HTTP server listen host.")
	f.IntVar(&cfg.HTTPListenPort, "server.http-listen-port", 80, "HTTP server listen port.")
	f.StringVar(&cfg.GRPCListenHost, "server.grpc-listen-host", "", "gRPC server listen host.")
	f.IntVar(&cfg.GRPCListenPort, "server.grpc-listen-port", 9095, "gRPC server listen port.")
	f.BoolVar(&cfg.RegisterInstrumentation, "server.register-instrumentation", true, "Register the intrumentation handlers (/metrics etc).")
	f.DurationVar(&cfg.ServerGracefulShutdownTimeout, "server.graceful-shutdown-timeout", 30*time.Second, "Timeout for graceful shutdowns")
	f.DurationVar(&cfg.HTTPServerReadTimeout, "server.http-read-timeout", 30*time.Second, "Read timeout for HTTP server")
	f.DurationVar(&cfg.HTTPServerWriteTimeout, "server.http-write-timeout", 30*time.Second, "Write timeout for HTTP server")
	f.DurationVar(&cfg.HTTPServerIdleTimeout, "server.http-idle-timeout", 120*time.Second, "Idle timeout for HTTP server")
	f.IntVar(&cfg.GPRCServerMaxRecvMsgSize, "server.grpc-max-recv-msg-size-bytes", 4*1024*1024, "Limit on the size of a gRPC message this server can receive (bytes).")
	f.IntVar(&cfg.GRPCServerMaxSendMsgSize, "server.grpc-max-send-msg-size-bytes", 4*1024*1024, "Limit on the size of a gRPC message this server can send (bytes).")
	f.UintVar(&cfg.GPRCServerMaxConcurrentStreams, "server.grpc-max-concurrent-streams", 100, "Limit on the number of concurrent streams for gRPC calls (0 = unlimited)")
	f.StringVar(&cfg.PathPrefix, "server.path-prefix", "", "Base path to serve all API routes from (e.g. /v1/)")
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
	httpListener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.HTTPListenHost, cfg.HTTPListenPort))
	if err != nil {
		return nil, err
	}

	grpcListener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.GRPCListenHost, cfg.GRPCListenPort))
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

	log.WithField("http", httpListener.Addr()).WithField("grpc", grpcListener.Addr()).Infof("server listening on addresses")

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
		grpc.MaxRecvMsgSize(cfg.GPRCServerMaxRecvMsgSize),
		grpc.MaxSendMsgSize(cfg.GRPCServerMaxSendMsgSize),
		grpc.MaxConcurrentStreams(uint32(cfg.GPRCServerMaxConcurrentStreams)),
	}
	grpcOptions = append(grpcOptions, cfg.GRPCOptions...)
	grpcServer := grpc.NewServer(grpcOptions...)

	// Setup HTTP server
	router := mux.NewRouter()
	if cfg.PathPrefix != "" {
		// Expect metrics and pprof handlers to be prefixed with server's path prefix.
		// e.g. /loki/metrics or /loki/debug/pprof
		router = router.PathPrefix(cfg.PathPrefix).Subrouter()
	}
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
