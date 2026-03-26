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
	"slices"
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
	"github.com/grafana/loki/v3/pkg/querytee/goldfish"
)

var errMinBackends = errors.New("at least 1 backend is required")

type RoutingMode string

const (
	RoutingModeRace        RoutingMode = "race"
	RoutingModeV1Preferred RoutingMode = "v1-preferred"
	RoutingModeV2Preferred RoutingMode = "v2-preferred"
)

type RoutingConfig struct {
	Mode          RoutingMode
	V1Preferred   string
	V2Preferred   string
	RaceTolerance time.Duration

	// SplitStart is the start date of data available in v2 (dataobjs) storage.
	// Queries for data before this date will only go to the v1 backend.
	// If not set, assume v2 data is always available.
	//
	// When splitting queries will be split into up to three parts:
	// 1. Data before SplitStart -> split goes to the v1 backend only
	// 2. Data between SplitStart and (now - SplitLag) -> split goes to both v1 and v2 backends
	// 3. Data after (now - SplitLag) -> split goes to the v1 backend only
	SplitStart flagext.Time

	// SplitLag is the minimum age of data to route to v2.
	// Data newer than (now - SplitLag) will only go to the v1 backend.
	// When set to 0, query splitting is disabled.
	SplitLag time.Duration

	// SplitRetentionDays is the lifecycle of data objects in days.
	// If set, data in v2 storage is considered available only for retention days.
	// Queries for data before the retention period will go to the v1 backend.
	// When both SplitStart and SplitRetentionDays are set, the more restrictive of the two will
	// determine v2 data availability.
	SplitRetentionDays int64

	// AddRoutingDecisionsToWarnings controls whether routing decisions are added
	// as warnings to query responses. When enabled, responses will include
	// warnings indicating which backend handled the query and how it was routed.
	AddRoutingDecisionsToWarnings bool
}

func (cfg *RoutingConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar((*string)(&cfg.Mode), "routing.mode", string(RoutingModeV1Preferred), "Routing mode: race, v1-preferred, or v2-preferred")
	f.StringVar(&cfg.V1Preferred, "routing.v1-preferred", "", "The hostname of the preferred v1 (chunks) backend")
	f.StringVar(&cfg.V2Preferred, "routing.v2-preferred", "", "The hostname of the preferred v2 (dataobjs) backend")
	f.DurationVar(&cfg.RaceTolerance, "routing.race-tolerance", 100*time.Millisecond, "Race handicap for v2 in race mode")
	f.Var(&cfg.SplitStart, "routing.split-start", "Start date when v2 data became available. Format YYYY-MM-DD. Queries before this date go only to v1.")
	f.DurationVar(&cfg.SplitLag, "routing.split-lag", 0, "Minimum age of data to route to v2. Data newer than this goes only to v1. When 0 (default), splitting is disabled.")
	f.Int64Var(&cfg.SplitRetentionDays, "routing.split-retention-days", 0, "Lifecycle of data objects in days. If set, data outside of retention period will not be available in v2 storage. When both split-start and split-retention-days are set, the more restrictive of the two will apply.")
	f.BoolVar(&cfg.AddRoutingDecisionsToWarnings, "routing.add-routing-decisions-to-warnings", false, "Add routing decisions as warnings to query responses.")
}

func (cfg *RoutingConfig) Validate() error {
	switch cfg.Mode {
	case RoutingModeRace, RoutingModeV1Preferred, RoutingModeV2Preferred:
	default:
		return fmt.Errorf("invalid routing mode: %s", cfg.Mode)
	}

	if cfg.SplitLag < 0 {
		return fmt.Errorf("split lag must be >= 0")
	}

	return nil
}

type ProxyConfig struct {
	ServerServicePort              int
	BackendEndpoints               string
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
	SkipFanOutWhenNotSampling      bool
	Goldfish                       goldfish.Config

	Routing RoutingConfig
}

func (cfg *ProxyConfig) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&cfg.ServerServicePort, "server.service-port", 80, "The port where the query-tee service listens to.")
	f.StringVar(&cfg.BackendEndpoints, "backend.endpoints", "", "Comma separated list of backend endpoints to query.")

	cfg.Routing.RegisterFlags(f)

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
	f.BoolVar(&cfg.SkipFanOutWhenNotSampling, "proxy.skip-fanout-when-not-sampling", false, "When enabled, skip fanning out requests to secondary backends when goldfish sampling is disabled (default_rate=0 and no tenant rules). This reduces load on secondary backends when not doing comparisons.")

	cfg.Goldfish.RegisterFlags(f)
}

type Route struct {
	Path               string
	RouteName          string
	Methods            []string
	ResponseComparator comparator.ResponsesComparator
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

	// Goldfish manager for query sampling and comparison
	goldfishManager goldfish.Manager
}

func NewProxy(
	cfg ProxyConfig,
	logger log.Logger,
	readRoutes, writeRoutes []Route,
	registerer prometheus.Registerer,
) (*Proxy, error) {
	if err := cfg.Routing.Validate(); err != nil {
		return nil, err
	}

	if cfg.CompareResponses && cfg.Routing.V1Preferred == "" {
		return nil, fmt.Errorf("when enabling comparison of results -routing.v1-backend flag must be set")
	}

	if cfg.PassThroughNonRegisteredRoutes && cfg.Routing.V1Preferred == "" {
		return nil, fmt.Errorf("when enabling passthrough for non-registered routes -routing.v1-backend flag must be set")
	}

	if cfg.InstrumentCompares && !cfg.CompareResponses {
		return nil, fmt.Errorf("when enabling instrumentation of comparisons of results -proxy.compare-responses flag must be set")
	}

	// Validate Goldfish configuration
	if err := cfg.Goldfish.Validate(); err != nil {
		return nil, errors.Wrap(err, "invalid goldfish configuration")
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
		v1Preferred := name == cfg.Routing.V1Preferred
		v2Preferred := name == cfg.Routing.V2Preferred

		// In tests we have the same hostname for all backends, so we also
		// support a numeric preferred backend which is the index in the list
		// of backends.
		if preferredIdx, err := strconv.Atoi(cfg.Routing.V1Preferred); err == nil {
			v1Preferred = preferredIdx == idx
		}
		if preferredIdx, err := strconv.Atoi(cfg.Routing.V2Preferred); err == nil {
			v2Preferred = preferredIdx == idx
		}

		level.Debug(logger).Log("msg", "backend added", "name", name, "v1Preferred", v1Preferred, "v2Preferred", v2Preferred)
		backend, err := NewProxyBackend(name, u, cfg.BackendReadTimeout, v1Preferred, v2Preferred)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create backend %s", name)
		}
		p.backends = append(p.backends, backend)
	}

	// At least 1 backend is required
	if len(p.backends) < 1 {
		return nil, errMinBackends
	}

	// If the preferred backend is configured, then it must exists among the actual backends.
	if cfg.Routing.V1Preferred != "" {
		exists := false
		for _, b := range p.backends {
			if b.v1Preferred {
				exists = true
				break
			}
		}
		if !exists {
			return nil, fmt.Errorf("the v1 backend (hostname) has not been found among the list of configured backends")
		}
	}

	if cfg.Routing.V2Preferred != "" {
		exists := false
		for _, b := range p.backends {
			if b.v2Preferred {
				exists = true
				break
			}
		}
		if !exists {
			return nil, fmt.Errorf("the v2 backend (hostname) has not been found among the list of configured backends")
		}
	}

	if cfg.CompareResponses && len(p.backends) < 2 {
		return nil, fmt.Errorf("when enabling comparison of results number of backends should be at least 2")
	}

	// At least 2 backends are suggested
	if len(p.backends) < 2 {
		level.Warn(p.logger).Log("msg", "The proxy is running with only 1 backend. At least 2 backends are required to fulfill the purpose of the proxy and compare results.")
	}

	if cfg.DisableBackendReadProxy != "" {
		readDisabledBackendHosts := strings.Split(p.cfg.DisableBackendReadProxy, ",")
		if slices.Contains(readDisabledBackendHosts, cfg.Routing.V1Preferred) {
			return nil, fmt.Errorf("the v1 backend cannot be disabled for reading")
		}
	}

	// Pre-initialize raceWins metric for all backend/route/issuer combinations
	if cfg.Routing.Mode == RoutingModeRace {
		for _, backend := range p.backends {
			for _, route := range p.readRoutes {
				for _, issuer := range []string{unknownIssuer, canaryIssuer} {
					p.metrics.raceWins.WithLabelValues(backend.name, backend.Alias(), route.RouteName, issuer)
				}
			}
		}
	}

	// Initialize Goldfish if enabled
	if cfg.Goldfish.Enabled {
		// Create storage backend
		storage, err := goldfish.NewStorage(cfg.Goldfish.StorageConfig, logger)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create goldfish storage")
		}

		var resultStore goldfish.ResultStore
		if cfg.Goldfish.ResultsStorage.Enabled {
			resultStore, err = goldfish.NewResultStore(context.Background(), cfg.Goldfish.ResultsStorage, logger)
			if err != nil {
				storage.Close()
				return nil, errors.Wrap(err, "failed to create goldfish result store")
			}
		}

		// Create Goldfish manager
		goldfishManager, err := goldfish.NewManager(cfg.Goldfish, storage, resultStore, logger, registerer)
		if err != nil {
			if resultStore != nil {
				_ = resultStore.Close(context.Background())
			}
			storage.Close()
			return nil, errors.Wrap(err, "failed to create goldfish manager")
		}
		p.goldfishManager = goldfishManager

		level.Info(logger).Log("msg", "Goldfish enabled",
			"storage_type", cfg.Goldfish.StorageConfig.Type,
			"default_rate", cfg.Goldfish.SamplingConfig.DefaultRate,
			"results_mode", string(cfg.Goldfish.ResultsStorage.Mode),
			"results_backend", cfg.Goldfish.ResultsStorage.Backend)
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
		filteredBackends := filterReadDisabledBackends(p.backends, p.cfg.DisableBackendReadProxy)
		endpoint := NewProxyEndpoint(
			filteredBackends,
			route.RouteName,
			p.metrics,
			p.logger,
			comp,
			p.cfg.InstrumentCompares,
		)

		// Add Goldfish if configured
		if p.goldfishManager != nil {
			endpoint.WithGoldfish(p.goldfishManager)
			level.Info(p.logger).Log(
				"msg", "Goldfish attached to route",
				"path", route.Path,
				"methods", strings.Join(route.Methods, ","),
				"split_lag", p.cfg.Routing.SplitLag,
			)
		}

		// Create a route-specific handler factory with the filtered backends
		routeHandlerFactory := NewHandlerFactory(HandlerFactoryConfig{
			Backends:                      filteredBackends,
			Codec:                         queryrange.DefaultCodec,
			GoldfishManager:               p.goldfishManager,
			Logger:                        p.logger,
			Metrics:                       p.metrics,
			InstrumentCompares:            p.cfg.InstrumentCompares,
			RoutingMode:                   p.cfg.Routing.Mode,
			RaceTolerance:                 p.cfg.Routing.RaceTolerance,
			SkipFanOutWhenNotSampling:     p.cfg.SkipFanOutWhenNotSampling,
			SplitStart:                    p.cfg.Routing.SplitStart,
			SplitLag:                      p.cfg.Routing.SplitLag,
			SplitRetentionDays:            p.cfg.Routing.SplitRetentionDays,
			AddRoutingDecisionsToWarnings: p.cfg.Routing.AddRoutingDecisionsToWarnings,
		})
		queryHandler, err := routeHandlerFactory.CreateHandler(route.RouteName, comp)
		if err != nil {
			return err
		}
		endpoint.WithQueryHandler(queryHandler)
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
			p.cfg.InstrumentCompares,
		)

		router.Path(route.Path).Methods(route.Methods...).Handler(endpoint)
	}

	if p.cfg.PassThroughNonRegisteredRoutes {
		for _, backend := range p.backends {
			if backend.v1Preferred {
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

	// Close Goldfish manager if it exists
	if p.goldfishManager != nil {
		if err := p.goldfishManager.Close(); err != nil {
			level.Warn(p.logger).Log("msg", "Failed to close Goldfish manager", "err", err)
		}
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
		if !b.v1Preferred {
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
