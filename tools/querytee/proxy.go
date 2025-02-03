package querytee

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

var errMinBackends = errors.New("at least 1 backend is required")

type ProxyConfig struct {
	ServerServicePort              int
	BackendEndpoints               string
	PreferredBackend               string
	BackendReadTimeout             time.Duration
	CompareResponses               bool
	DisableBackendReadProxy        string
	ValueComparisonTolerance       float64
	UseRelativeError               bool
	PassThroughNonRegisteredRoutes bool
	SkipRecentSamples              time.Duration
	SkipSamplesBefore              flagext.Time
	RequestURLFilter               *regexp.Regexp
	InstrumentCompares             bool
}

func (cfg *ProxyConfig) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&cfg.ServerServicePort, "server.service-port", 80, "The port where the query-tee service listens to.")
	f.StringVar(&cfg.BackendEndpoints, "backend.endpoints", "", "Comma separated list of backend endpoints to query.")
	f.StringVar(&cfg.PreferredBackend, "backend.preferred", "", "The hostname of the preferred backend when selecting the response to send back to the client. If no preferred backend is configured then the query-tee will send back to the client the first successful response received without waiting for other backends.")
	f.DurationVar(&cfg.BackendReadTimeout, "backend.read-timeout", 90*time.Second, "The timeout when reading the response from a backend.")
	f.BoolVar(&cfg.CompareResponses, "proxy.compare-responses", false, "Compare responses between preferred and secondary endpoints for supported routes.")
	f.StringVar(&cfg.DisableBackendReadProxy, "proxy.disable-backend-read", "", "Comma separated list of non-primary backend hostnames to disable their read proxy. Typically used for temporarily not passing any read requests to specified backends.")
	f.Float64Var(&cfg.ValueComparisonTolerance, "proxy.value-comparison-tolerance", 0.000001, "The tolerance to apply when comparing floating point values in the responses. 0 to disable tolerance and require exact match (not recommended).")
	f.BoolVar(&cfg.UseRelativeError, "proxy.compare-use-relative-error", false, "Use relative error tolerance when comparing floating point values.")
	f.DurationVar(&cfg.SkipRecentSamples, "proxy.compare-skip-recent-samples", 60*time.Second, "The window from now to skip comparing samples. 0 to disable.")
	f.Var(&cfg.SkipSamplesBefore, "proxy.compare-skip-samples-before", "Skip the samples before the given time for comparison. The time can be in RFC3339 format (or) RFC3339 without the timezone and seconds (or) date only.")
	f.BoolVar(&cfg.PassThroughNonRegisteredRoutes, "proxy.passthrough-non-registered-routes", false, "Passthrough requests for non-registered routes to preferred backend.")
	f.Func("backend.filter", "A request filter as a regular expression. Only matches are proxied to non-preferred backends.", func(raw string) error {
		var err error
		cfg.RequestURLFilter, err = regexp.Compile(raw)
		return err
	})
	f.BoolVar(&cfg.InstrumentCompares, "proxy.compare-instrument", false, "Reports metrics on comparisons of responses between preferred and non-preferred endpoints for supported routes.")
}

type Route struct {
	Path               string
	RouteName          string
	Methods            []string
	ResponseComparator ResponsesComparator
}

type Proxy struct {
	cfg         ProxyConfig
	backends    []*ProxyBackend
	logger      log.Logger
	metrics     *ProxyMetrics
	readRoutes  []Route
	writeRoutes []Route

	// The HTTP server used to run the proxy service.
	srv         *http.Server
	srvListener net.Listener

	// Wait group used to wait until the server has done.
	done sync.WaitGroup
}

func NewProxy(cfg ProxyConfig, logger log.Logger, readRoutes, writeRoutes []Route, registerer prometheus.Registerer) (*Proxy, error) {
	if cfg.CompareResponses && cfg.PreferredBackend == "" {
		return nil, fmt.Errorf("when enabling comparison of results -backend.preferred flag must be set to hostname of preferred backend")
	}

	if cfg.PassThroughNonRegisteredRoutes && cfg.PreferredBackend == "" {
		return nil, fmt.Errorf("when enabling passthrough for non-registered routes -backend.preferred flag must be set to hostname of backend where those requests needs to be passed")
	}

	if cfg.InstrumentCompares && !cfg.CompareResponses {
		return nil, fmt.Errorf("when enabling instrumentation of comparisons of results -proxy.compare-responses flag must be set")
	}

	p := &Proxy{
		cfg:         cfg,
		logger:      logger,
		metrics:     NewProxyMetrics(registerer),
		readRoutes:  readRoutes,
		writeRoutes: writeRoutes,
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

	if cfg.CompareResponses && len(p.backends) < 2 {
		return nil, fmt.Errorf("when enabling comparison of results number of backends should be at least 2")
	}

	// At least 2 backends are suggested
	if len(p.backends) < 2 {
		level.Warn(p.logger).Log("msg", "The proxy is running with only 1 backend. At least 2 backends are required to fulfil the purpose of the proxy and compare results.")
	}

	if cfg.DisableBackendReadProxy != "" {
		readDisabledBackendHosts := strings.Split(p.cfg.DisableBackendReadProxy, ",")
		for _, host := range readDisabledBackendHosts {
			if host == cfg.PreferredBackend {
				return nil, fmt.Errorf("the preferred backend cannot be disabled for reading")
			}
		}
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

	// register read routes
	for _, route := range p.readRoutes {
		var comparator ResponsesComparator
		if p.cfg.CompareResponses {
			comparator = route.ResponseComparator
		}
		router.Path(route.Path).Methods(route.Methods...).Handler(NewProxyEndpoint(filterReadDisabledBackends(p.backends, p.cfg.DisableBackendReadProxy), route.RouteName, p.metrics, p.logger, comparator, p.cfg.InstrumentCompares))
	}

	for _, route := range p.writeRoutes {
		router.Path(route.Path).Methods(route.Methods...).Handler(NewProxyEndpoint(p.backends, route.RouteName, p.metrics, p.logger, nil, p.cfg.InstrumentCompares))
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

func filterReadDisabledBackends(backends []*ProxyBackend, disableReadProxyCfg string) []*ProxyBackend {
	readEnabledBackends := make([]*ProxyBackend, 0, len(backends))
	readDisabledBackendNames := strings.Split(disableReadProxyCfg, ",")
	for _, b := range backends {
		if !b.preferred {
			readDisabled := false
			for _, h := range readDisabledBackendNames {
				if strings.TrimSpace(h) == b.name {
					readDisabled = true
					break
				}
			}
			if readDisabled {
				continue
			}
		}
		readEnabledBackends = append(readEnabledBackends, b)
	}

	return readEnabledBackends
}
