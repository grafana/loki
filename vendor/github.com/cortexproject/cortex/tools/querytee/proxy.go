package querytee

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	errMinBackends = errors.New("at least 1 backend is required")
)

type ProxyConfig struct {
	ServerServicePort              int
	BackendEndpoints               string
	PreferredBackend               string
	BackendReadTimeout             time.Duration
	CompareResponses               bool
	ValueComparisonTolerance       float64
	PassThroughNonRegisteredRoutes bool
}

func (cfg *ProxyConfig) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&cfg.ServerServicePort, "server.service-port", 80, "The port where the query-tee service listens to.")
	f.StringVar(&cfg.BackendEndpoints, "backend.endpoints", "", "Comma separated list of backend endpoints to query.")
	f.StringVar(&cfg.PreferredBackend, "backend.preferred", "", "The hostname of the preferred backend when selecting the response to send back to the client. If no preferred backend is configured then the query-tee will send back to the client the first successful response received without waiting for other backends.")
	f.DurationVar(&cfg.BackendReadTimeout, "backend.read-timeout", 90*time.Second, "The timeout when reading the response from a backend.")
	f.BoolVar(&cfg.CompareResponses, "proxy.compare-responses", false, "Compare responses between preferred and secondary endpoints for supported routes.")
	f.Float64Var(&cfg.ValueComparisonTolerance, "proxy.value-comparison-tolerance", 0.000001, "The tolerance to apply when comparing floating point values in the responses. 0 to disable tolerance and require exact match (not recommended).")
	f.BoolVar(&cfg.PassThroughNonRegisteredRoutes, "proxy.passthrough-non-registered-routes", false, "Passthrough requests for non-registered routes to preferred backend.")
}

type Route struct {
	Path               string
	RouteName          string
	Methods            []string
	ResponseComparator ResponsesComparator
}

type Proxy struct {
	cfg      ProxyConfig
	backends []*ProxyBackend
	logger   log.Logger
	metrics  *ProxyMetrics
	routes   []Route

	// The HTTP server used to run the proxy service.
	srv         *http.Server
	srvListener net.Listener

	// Wait group used to wait until the server has done.
	done sync.WaitGroup
}

func NewProxy(cfg ProxyConfig, logger log.Logger, routes []Route, registerer prometheus.Registerer) (*Proxy, error) {
	if cfg.CompareResponses && cfg.PreferredBackend == "" {
		return nil, fmt.Errorf("when enabling comparison of results -backend.preferred flag must be set to hostname of preferred backend")
	}

	if cfg.PassThroughNonRegisteredRoutes && cfg.PreferredBackend == "" {
		return nil, fmt.Errorf("when enabling passthrough for non-registered routes -backend.preferred flag must be set to hostname of backend where those requests needs to be passed")
	}

	p := &Proxy{
		cfg:     cfg,
		logger:  logger,
		metrics: NewProxyMetrics(registerer),
		routes:  routes,
	}

	// Parse the backend endpoints (comma separated).
	parts := strings.Split(cfg.BackendEndpoints, ",")

	for idx, part := range parts {
		// Skip empty ones.
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		u, err := url.Parse(part)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid backend endpoint %s", part)
		}

		// The backend name is hardcoded as the backend hostname.
		name := u.Hostname()
		preferred := name == cfg.PreferredBackend

		// In tests we have the same hostname for all backends, so we also
		// support a numeric preferred backend which is the index in the list
		// of backends.
		if preferredIdx, err := strconv.Atoi(cfg.PreferredBackend); err == nil {
			preferred = preferredIdx == idx
		}

		p.backends = append(p.backends, NewProxyBackend(name, u, cfg.BackendReadTimeout, preferred))
	}

	// At least 1 backend is required
	if len(p.backends) < 1 {
		return nil, errMinBackends
	}

	// If the preferred backend is configured, then it must exists among the actual backends.
	if cfg.PreferredBackend != "" {
		exists := false
		for _, b := range p.backends {
			if b.preferred {
				exists = true
				break
			}
		}

		if !exists {
			return nil, fmt.Errorf("the preferred backend (hostname) has not been found among the list of configured backends")
		}
	}

	if cfg.CompareResponses && len(p.backends) != 2 {
		return nil, fmt.Errorf("when enabling comparison of results number of backends should be 2 exactly")
	}

	// At least 2 backends are suggested
	if len(p.backends) < 2 {
		level.Warn(p.logger).Log("msg", "The proxy is running with only 1 backend. At least 2 backends are required to fulfil the purpose of the proxy and compare results.")
	}

	return p, nil
}

func (p *Proxy) Start() error {
	// Setup listener first, so we can fail early if the port is in use.
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", p.cfg.ServerServicePort))
	if err != nil {
		return err
	}

	router := mux.NewRouter()

	// Health check endpoint.
	router.Path("/").Methods("GET").Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// register routes
	for _, route := range p.routes {
		var comparator ResponsesComparator
		if p.cfg.CompareResponses {
			comparator = route.ResponseComparator
		}
		router.Path(route.Path).Methods(route.Methods...).Handler(NewProxyEndpoint(p.backends, route.RouteName, p.metrics, p.logger, comparator))
	}

	if p.cfg.PassThroughNonRegisteredRoutes {
		for _, backend := range p.backends {
			if backend.preferred {
				router.PathPrefix("/").Handler(httputil.NewSingleHostReverseProxy(backend.endpoint))
				break
			}
		}
	}

	p.srvListener = listener
	p.srv = &http.Server{
		ReadTimeout:  1 * time.Minute,
		WriteTimeout: 2 * time.Minute,
		Handler:      router,
	}

	// Run in a dedicated goroutine.
	p.done.Add(1)
	go func() {
		defer p.done.Done()

		if err := p.srv.Serve(p.srvListener); err != nil {
			level.Error(p.logger).Log("msg", "Proxy server failed", "err", err)
		}
	}()

	level.Info(p.logger).Log("msg", "The proxy is up and running.")
	return nil
}

func (p *Proxy) Stop() error {
	if p.srv == nil {
		return nil
	}

	return p.srv.Shutdown(context.Background())
}

func (p *Proxy) Await() {
	// Wait until terminated.
	p.done.Wait()
}

func (p *Proxy) Endpoint() string {
	if p.srvListener == nil {
		return ""
	}

	return p.srvListener.Addr().String()
}
