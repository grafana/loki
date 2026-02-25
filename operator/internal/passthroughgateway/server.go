package passthroughgateway

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	config       *Config
	logger       logr.Logger
	metrics      *Metrics
	proxyServer  *http.Server
	adminServer  *http.Server
	shuttingDown atomic.Bool
}

func NewServer(cfg *Config, logger logr.Logger, reg prometheus.Registerer) (*Server, error) {
	metrics := NewMetrics(reg)

	router, err := NewLokiRouter(cfg, logger)
	if err != nil {
		return nil, err
	}

	proxyHandler := InstrumentedHandler(router, metrics)

	var proxyWriteTimeout time.Duration
	if cfg.Loki.Timeout > 0 {
		proxyWriteTimeout = cfg.Loki.Timeout + 10*time.Second
	}

	proxyServer := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      proxyHandler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: proxyWriteTimeout,
		IdleTimeout:  120 * time.Second,
	}

	adminServer := &http.Server{
		Addr:        cfg.AdminAddr,
		ReadTimeout: 10 * time.Second,
	}

	if cfg.TLSEnabled() {
		tlsConfig, err := BuildServerTLSConfigWithClientAuth(cfg.TLSOptions(), cfg.TLSClientCAFile)
		if err != nil {
			return nil, err
		}
		proxyServer.TLSConfig = tlsConfig

		adminTLSConfig, err := BuildServerTLSConfig(cfg.TLSOptions(), cfg.TLSClientCAFile)
		if err != nil {
			return nil, err
		}
		adminServer.TLSConfig = adminTLSConfig
	}

	adminMux := http.NewServeMux()
	adminMux.Handle("/metrics", promhttp.HandlerFor(
		reg.(prometheus.Gatherer),
		promhttp.HandlerOpts{Registry: reg},
	))
	adminMux.HandleFunc("/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	server := &Server{
		config:  cfg,
		logger:  logger,
		metrics: metrics,
	}

	adminMux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if server.shuttingDown.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("shutting down"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	adminServer.Handler = adminMux
	adminServer.WriteTimeout = 10 * time.Second

	server.proxyServer = proxyServer
	server.adminServer = adminServer

	return server, nil
}

func (s *Server) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		var err error
		if s.config.TLSEnabled() {
			s.logger.Info("starting HTTPS admin server", "addr", s.config.AdminAddr)
			err = s.adminServer.ListenAndServeTLS(s.config.TLSCertFile, s.config.TLSKeyFile)
		} else {
			s.logger.Info("starting HTTP admin server", "addr", s.config.AdminAddr)
			err = s.adminServer.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	go func() {
		defer wg.Done()
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

	var runErr error
	select {
	case <-ctx.Done():
		s.logger.Info("shutting down servers")
		runErr = s.Shutdown()
	case err := <-errChan:
		runErr = err
		_ = s.Shutdown()
	}

	wg.Wait()
	return runErr
}

func (s *Server) Shutdown() error {
	s.shuttingDown.Store(true)

	var (
		wg       sync.WaitGroup
		proxyErr error
		adminErr error
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.proxyServer.Shutdown(ctx); err != nil {
			s.logger.Error(err, "error shutting down proxy server")
			proxyErr = err
		}
	}()

	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.adminServer.Shutdown(ctx); err != nil {
			s.logger.Error(err, "error shutting down admin server")
			adminErr = err
		}
	}()

	wg.Wait()
	return errors.Join(proxyErr, adminErr)
}
