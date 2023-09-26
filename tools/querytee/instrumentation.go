package querytee

import (
	"fmt"
	"net"
	"net/http"

	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	util_log "github.com/grafana/loki/pkg/util/log"
)

type InstrumentationServer struct {
	port     int
	registry *prometheus.Registry
	srv      *http.Server
}

// NewInstrumentationServer returns a server exposing Prometheus metrics.
func NewInstrumentationServer(port int, registry *prometheus.Registry) *InstrumentationServer {
	return &InstrumentationServer{
		port:     port,
		registry: registry,
	}
}

// Start the instrumentation server.
func (s *InstrumentationServer) Start() error {
	// Setup listener first, so we can fail early if the port is in use.
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return err
	}

	router := mux.NewRouter()
	router.Handle("/metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}))

	s.srv = &http.Server{
		Handler: router,
	}

	go func() {
		if err := s.srv.Serve(listener); err != nil {
			level.Error(util_log.Logger).Log("msg", "metrics server terminated", "err", err)
		}
	}()

	return nil
}

// Stop closes the instrumentation server.
func (s *InstrumentationServer) Stop() {
	if s.srv != nil {
		s.srv.Close()
		s.srv = nil
	}
}
