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

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/middleware"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querytee/comparator"
)

var errMinBackends = errors.New("at least 1 backend is required")

// PrimaryBackendConfig configures the backend whose response the proxy returns to the client.
type PrimaryBackendConfig struct {
	Hostname string
}

// RegisterFlags registers the flags of the primary backend.
func (cfg *PrimaryBackendConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Hostname, "primary-backend.hostname", "", "The hostname of the primary backend. Required. The proxy always returns its response to the client.")
}

// SecondaryBackendConfig configures the backend whose responses the proxy compares with the
// primary backend's responses when -proxy.compare-responses is set. The proxy never returns its
// responses to the client.
type SecondaryBackendConfig struct {
	Hostname          string
	SelectionStrategy string
}

// RegisterFlags registers the flags of the secondary backend.
func (cfg *SecondaryBackendConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Hostname, "secondary-backend.hostname", "", "The hostname of the secondary backend. When -proxy.compare-responses is set, the proxy compares its responses with the primary's. The proxy never returns its responses to the client. Empty means no secondary backend.")
	f.StringVar(&cfg.SelectionStrategy, "secondary-backend.selection-strategy", selectionStrategyAll, "How the proxy picks the read requests that the secondary backend receives. Supported: "+selectionStrategyAll+" (every request) and "+selectionStrategyDataObj+" (the range and instant queries that the secondary backend reads from data objects).")
}

// Validate checks that the selection strategy is a known one, and that a strategy other than the
// default has a secondary backend.
func (cfg *SecondaryBackendConfig) Validate() error {
	switch cfg.SelectionStrategy {
	case selectionStrategyAll:
	case selectionStrategyDataObj:
		if cfg.Hostname == "" {
			return errors.New("-secondary-backend.selection-strategy needs -secondary-backend.hostname")
		}
	default:
		return fmt.Errorf("unknown selection strategy %q", cfg.SelectionStrategy)
	}
	return nil
}

// newSelectionStrategy builds the selection strategy that the config names.
func newSelectionStrategy(cfg SecondaryBackendConfig, lokiCfg *LokiParsedConfig) (SelectionStrategy, error) {
	switch cfg.SelectionStrategy {
	case selectionStrategyAll:
		return allStrategy{}, nil
	case selectionStrategyDataObj:
		return newDataObjStrategy(lokiCfg)
	default:
		return nil, fmt.Errorf("unknown selection strategy %q", cfg.SelectionStrategy)
	}
}

type ProxyConfig struct {
	ServerServicePort              int
	BackendEndpoints               string
	BackendReadTimeout             time.Duration
	CompareResponses               bool
	ValueComparisonTolerance       float64
	UseRelativeError               bool
	PassThroughNonRegisteredRoutes bool
	SkipRecentSamples              time.Duration
	SkipSamplesBefore              flagext.Time
	ReportMissingSeries            bool

	PrimaryBackend   PrimaryBackendConfig
	SecondaryBackend SecondaryBackendConfig
}

// backendRole returns the role of the backend at the given index of the backend list. It returns
// an error when the backend matches both hostnames.
func (cfg *ProxyConfig) backendRole(name string, idx int) (BackendRole, error) {
	matches := func(hostname string) bool {
		if hostname == "" {
			return false
		}

		// A numeric hostname is a 0-based index into the backend list, not a name. Only tests use
		// it, because all test backends share one hostname.
		if i, err := strconv.Atoi(hostname); err == nil {
			return i == idx
		}

		return name == hostname
	}

	primary, secondary := matches(cfg.PrimaryBackend.Hostname), matches(cfg.SecondaryBackend.Hostname)
	switch {
	case primary && secondary:
		return RoleOther, fmt.Errorf("the backend %s cannot be both the primary and the secondary backend", name)
	case primary:
		return RolePrimary, nil
	case secondary:
		return RoleSecondary, nil
	default:
		return RoleOther, nil
	}
}

func (cfg *ProxyConfig) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&cfg.ServerServicePort, "server.service-port", 80, "The port where the query-tee service listens to.")
	f.StringVar(&cfg.BackendEndpoints, "backend.endpoints", "", "Comma separated list of backend endpoints. The proxy sends every write to every backend. It sends every read to every backend, except that the secondary backend receives only the reads that its selection strategy picks.")

	cfg.PrimaryBackend.RegisterFlags(f)
	cfg.SecondaryBackend.RegisterFlags(f)

	f.DurationVar(&cfg.BackendReadTimeout, "backend.read-timeout", 90*time.Second, "The timeout when reading the response from a backend.")
	f.BoolVar(&cfg.CompareResponses, "proxy.compare-responses", false, "Compare the responses of the secondary backend with the responses of the primary backend on supported routes. Requires a secondary backend.")
	f.BoolVar(&cfg.ReportMissingSeries, "proxy.compare-report-missing-series", false, "Report, in the loki_querytee_missing_metrics_series metric, how many series of the primary backend's vector response the secondary backend's response lacks. Requires -proxy.compare-responses.")
	f.Float64Var(&cfg.ValueComparisonTolerance, "proxy.value-comparison-tolerance", 0.000001, "The tolerance to apply when comparing floating point values in the responses. 0 to disable tolerance and require exact match (not recommended).")
	f.BoolVar(&cfg.UseRelativeError, "proxy.compare-use-relative-error", false, "Use relative error tolerance when comparing floating point values.")
	f.DurationVar(&cfg.SkipRecentSamples, "proxy.compare-skip-recent-samples", 60*time.Second, "The window from now to skip comparing samples. 0 to disable.")
	f.Var(&cfg.SkipSamplesBefore, "proxy.compare-skip-samples-before", "Skip the samples before the given time for comparison. The time can be in RFC3339 format (or) RFC3339 without the timezone and seconds (or) date only.")
	f.BoolVar(&cfg.PassThroughNonRegisteredRoutes, "proxy.passthrough-non-registered-routes", false, "Passthrough requests for non-registered routes to the primary backend.")
}

type Route struct {
	Path               string
	RouteName          string
	Methods            []string
	ResponseComparator comparator.ResponsesComparator
}

type Proxy struct {
	cfg                      ProxyConfig
	backends                 ProxyBackends
	logger                   log.Logger
	metrics                  *ProxyMetrics
	readRoutes               []Route
	writeRoutes              []Route
	secondaryBackendStrategy SelectionStrategy

	// The HTTP server used to run the proxy service.
	srv         *http.Server
	srvListener net.Listener

	// Wait group used to wait until the server has done.
	done sync.WaitGroup
}

// NewProxy returns a Proxy for the backends in cfg.BackendEndpoints.
//
// Exactly one backend must match the primary hostname. When the secondary hostname is set, exactly
// one other backend must match it. A backend that matches neither is an "other" backend. The proxy
// sends it every request and never compares its responses.
func NewProxy(
	cfg ProxyConfig,
	logger log.Logger,
	readRoutes, writeRoutes []Route,
	registerer prometheus.Registerer,
	lokiCfg *LokiParsedConfig,
) (*Proxy, error) {
	if err := cfg.SecondaryBackend.Validate(); err != nil {
		return nil, err
	}

	if cfg.ReportMissingSeries && !cfg.CompareResponses {
		return nil, fmt.Errorf("-proxy.compare-report-missing-series requires -proxy.compare-responses to be set")
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

		name := u.Hostname()
		role, err := cfg.backendRole(name, idx)
		if err != nil {
			return nil, err
		}

		level.Debug(logger).Log("msg", "backend added", "name", name, "role", role.String())
		backend, err := NewProxyBackend(name, u, cfg.BackendReadTimeout, role)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create backend %s", name)
		}
		p.backends = append(p.backends, backend)
	}

	// At least 1 backend is required
	if len(p.backends) < 1 {
		return nil, errMinBackends
	}

	// The primary backend is required. Exactly one configured backend must match its hostname.
	if cfg.PrimaryBackend.Hostname == "" {
		return nil, fmt.Errorf("-primary-backend.hostname flag must be set")
	}
	switch p.backends.CountFunc((*ProxyBackend).isPrimary) {
	case 0:
		return nil, fmt.Errorf("the primary backend (hostname) has not been found among the list of configured backends")
	case 1:
	default:
		return nil, fmt.Errorf("more than one configured backend has the hostname of the primary backend")
	}

	if cfg.SecondaryBackend.Hostname != "" {
		switch p.backends.CountFunc((*ProxyBackend).isSecondary) {
		case 0:
			return nil, fmt.Errorf("the secondary backend (hostname) has not been found among the list of configured backends")
		case 1:
		default:
			return nil, fmt.Errorf("more than one configured backend has the hostname of the secondary backend")
		}

		strategy, err := newSelectionStrategy(cfg.SecondaryBackend, lokiCfg)
		if err != nil {
			return nil, err
		}
		p.secondaryBackendStrategy = strategy
	}

	if cfg.CompareResponses && cfg.SecondaryBackend.Hostname == "" {
		return nil, fmt.Errorf("when enabling comparison of results -secondary-backend.hostname flag must be set")
	}

	// At least 2 backends are suggested
	if len(p.backends) < 2 {
		level.Warn(p.logger).Log("msg", "The proxy is running with only 1 backend. At least 2 backends are required to fulfill the purpose of the proxy and compare results.")
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
		var comp comparator.ResponsesComparator
		if p.cfg.CompareResponses {
			comp = route.ResponseComparator
		}
		endpoint := NewProxyEndpoint(
			p.backends,
			route.RouteName,
			p.metrics,
			p.logger,
			comp,
			p.cfg.ReportMissingSeries,
		)

		routeHandlerFactory := NewHandlerFactory(HandlerFactoryConfig{
			Backends:                 p.backends,
			Codec:                    queryrange.DefaultCodec,
			Logger:                   p.logger,
			Metrics:                  p.metrics,
			ReportMissingSeries:      p.cfg.ReportMissingSeries,
			SecondaryBackendStrategy: p.secondaryBackendStrategy,
		})
		endpoint.WithQueryHandler(routeHandlerFactory.CreateHandler(route.RouteName, comp))
		level.Info(p.logger).Log(
			"msg", "Query middleware handler attached to route",
			"path", route.Path,
			"methods", strings.Join(route.Methods, ","),
		)

		router.Path(route.Path).Methods(route.Methods...).Handler(endpoint)
	}

	// create a separate endpoint without a query handler for write requests
	for _, route := range p.writeRoutes {
		var comp comparator.ResponsesComparator
		if p.cfg.CompareResponses {
			comp = route.ResponseComparator
		}
		endpoint := NewProxyEndpoint(
			p.backends,
			route.RouteName,
			p.metrics,
			p.logger,
			comp,
			p.cfg.ReportMissingSeries,
		)

		router.Path(route.Path).Methods(route.Methods...).Handler(endpoint)
	}

	if p.cfg.PassThroughNonRegisteredRoutes {
		for _, backend := range p.backends {
			if backend.isPrimary() {
				router.PathPrefix("/").Handler(httputil.NewSingleHostReverseProxy(backend.endpoint))
				break
			}
		}
	}

	p.srvListener = listener
	// Wrap router with tracing middleware
	var handler http.Handler = router
	// Configure tracing middleware to extract trace headers
	// This ensures trace context is properly propagated from incoming requests
	tracer := middleware.NewTracer(nil, true, nil, nil) // true enables trace header extraction
	handler = tracer.Wrap(router)
	level.Info(p.logger).Log("msg", "HTTP tracing middleware enabled with header extraction")

	p.srv = &http.Server{
		ReadTimeout:  1 * time.Minute,
		WriteTimeout: 2 * time.Minute,
		Handler:      handler,
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
