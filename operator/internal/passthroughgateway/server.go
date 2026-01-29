package passthroughgateway

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	config        *Config
	logger        logr.Logger
	metrics       *Metrics
	proxyServer   *http.Server
	metricsServer *http.Server
	shuttingDown  atomic.Bool
}

func NewServer(cfg *Config, logger logr.Logger, reg prometheus.Registerer) (*Server, error) {
	metrics := NewMetrics(reg)

	router, err := NewLokiRouter(cfg, logger)
	if err != nil {
		return nil, err
	}

	proxyHandler := InstrumentedHandler(router, metrics)
	proxyServer := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      proxyHandler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	metricsServer := &http.Server{
		Addr:        cfg.MetricsAddr,
		ReadTimeout: 10 * time.Second,
	}

	if cfg.TLSEnabled() {
		tlsConfig, err := BuildServerTLSConfigWithClientAuth(cfg.TLSOptions(), cfg.TLSClientCAFile)
		if err != nil {
			return nil, err
		}
		proxyServer.TLSConfig = tlsConfig

		metricsTLSConfig, err := BuildServerTLSConfig(cfg.TLSOptions(), cfg.TLSClientCAFile)
		if err != nil {
			return nil, err
		}
		metricsServer.TLSConfig = metricsTLSConfig
	}

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.HandlerFor(
		reg.(prometheus.Gatherer),
		promhttp.HandlerOpts{Registry: reg},
	))
	metricsMux.HandleFunc("/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	server := &Server{
		config:  cfg,
		logger:  logger,
		metrics: metrics,
	}

	metricsMux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if server.shuttingDown.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("shutting down"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	metricsServer.Handler = metricsMux
	metricsServer.WriteTimeout = 10 * time.Second

	server.proxyServer = proxyServer
	server.metricsServer = metricsServer

	return server, nil
}

func (s *Server) Run(ctx context.Context) error {
	errChan := make(chan error, 2)

	go func() {
		var err error
		if s.config.TLSEnabled() {
			s.logger.Info("starting HTTPS metrics server", "addr", s.config.MetricsAddr)
			err = s.metricsServer.ListenAndServeTLS(s.config.TLSCertFile, s.config.TLSKeyFile)
		} else {
			s.logger.Info("starting HTTP metrics server", "addr", s.config.MetricsAddr)
			err = s.metricsServer.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	go func() {
		var err error
		if s.config.TLSEnabled() {
			s.logger.Info("starting mTLS proxy server", "addr", s.config.ListenAddr)
			err = s.proxyServer.ListenAndServeTLS(s.config.TLSCertFile, s.config.TLSKeyFile)
		} else {
			s.logger.Info("starting HTTP proxy server", "addr", s.config.ListenAddr)
			err = s.proxyServer.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("shutting down servers")
		return s.Shutdown()
	case err := <-errChan:
		return err
	}
}

func (s *Server) Shutdown() error {
	s.shuttingDown.Store(true)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var lastErr error

	if err := s.proxyServer.Shutdown(ctx); err != nil {
		s.logger.Error(err, "error shutting down proxy server")
		lastErr = err
	}

	if err := s.metricsServer.Shutdown(ctx); err != nil {
		s.logger.Error(err, "error shutting down metrics server")
		lastErr = err
	}

	return lastErr
}
