package indexgateway

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"slices"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/gate"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/distributor/clientpool"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/discovery"
	"github.com/grafana/loki/v3/pkg/util/jumphash"
)

// ClientConfig configures the Index Gateway client used to communicate with
// the Index Gateway server.
type ClientConfig struct {
	// Mode sets in which mode the client will operate. It is actually defined at the
	// index_gateway YAML section and reused here.
	Mode Mode `yaml:"-"`

	// PoolConfig defines the behavior of the gRPC connection pool used to communicate
	// with the Index Gateway.
	//
	// Only relevant for the ring mode.
	// It is defined at the distributors YAML section and reused here.
	PoolConfig clientpool.PoolConfig `yaml:"-"`

	// Ring is the Index Gateway ring used to find the appropriate Index Gateway instance
	// this client should talk to.
	//
	// Only relevant for the ring mode.
	Ring ring.ReadRing `yaml:"-"`

	// GRPCClientConfig configures the gRPC connection between the Index Gateway client and the server.
	//
	// Used by both, ring and simple mode.
	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config"`

	// Address of the Index Gateway instance responsible for retaining the index for all tenants.
	//
	// Only relevant for the simple mode.
	Address string `yaml:"server_address,omitempty"`

	// Forcefully disable the use of the index gateway client for the storage.
	// This is mainly useful for the index-gateway component which should always use the storage.
	Disabled bool `yaml:"-"`

	// LogGatewayRequests configures if requests sent to the gateway should be logged or not.
	// The log messages are of type debug and contain the address of the gateway and the relevant tenant.
	LogGatewayRequests bool `yaml:"log_gateway_requests"`

	GRPCUnaryClientInterceptors  []grpc.UnaryClientInterceptor  `yaml:"-"`
	GRCPStreamClientInterceptors []grpc.StreamClientInterceptor `yaml:"-"`

	TimeBasedShardingBuckets []string `yaml:"time_based_sharding_buckets" category:"Experimental"`

	// MinShuffleShardSize is the minimum number of index gateway instances included in the
	// shuffle shard, regardless of the max-capacity setting. Only applies to simple mode.
	MinShuffleShardSize int `yaml:"min_shuffle_shard_size"`

	// MaxInFlightRequests caps how many requests this client may have in flight at once.
	// Zero disables the cap.
	MaxInFlightRequests int `yaml:"max_in_flight_requests" category:"experimental"`

	// MaxRetries caps how many further index gateway instances a failed request is tried
	// against. -1 preserves the legacy retry limits; zero disables retries.
	MaxRetries int `yaml:"max_retries" category:"experimental"`

	// Sharding selects how requests are routed in ring mode: to the tenant's
	// shuffle shard (default), or split by index table to each table's owners
	// (per_index).
	Sharding string `yaml:"sharding" category:"experimental" doc:"hidden"`

	// PerIndexMaxConcurrency caps how many parts of one request are in flight
	// at once in per_index sharding.
	PerIndexMaxConcurrency int `yaml:"per_index_max_concurrency" category:"experimental" doc:"hidden"`

	// TableRange is the range of index tables of the schema period this client
	// serves. It is set by the store and used by per_index sharding.
	TableRange config.TableRange `yaml:"-"`
}

// RegisterFlagsWithPrefix register client-specific flags with the given prefix.
//
// Flags that are used by both, client and server, are defined in the indexgateway package.
func (i *ClientConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	i.GRPCClientConfig.RegisterFlagsWithPrefix(prefix+".grpc", f)
	f.StringVar(&i.Address, prefix+".server-address", "", "Hostname or IP of the Index Gateway gRPC server running in simple mode. Can also be prefixed with dns+, dnssrv+, or dnssrvnoa+ to resolve a DNS A record with multiple IP's, a DNS SRV record with a followup A record lookup, or a DNS SRV record without a followup A record lookup, respectively.")
	f.BoolVar(&i.LogGatewayRequests, prefix+".log-gateway-requests", false, "Whether requests sent to the gateway should be logged or not.")

	// Experimental: Time-based client side query sharding
	f.Var(
		(*flagext.StringSlice)(&i.TimeBasedShardingBuckets),
		prefix+".time-based-sharding-buckets",
		"Experimental: Defines buckets for time-based sharding. Time based sharding only takes affect when index gateways run in simple mode. To enable client side time-based sharding of queries across index gateway instances set at least one bucket in the format of a string representation of a time.Duration, e.g. ['168h', '336h', '504h']",
	)
	f.IntVar(&i.MinShuffleShardSize, prefix+".min-shuffle-shard-size", 3, "Minimum number of index gateway instances included in the shuffle shard, regardless of the max-capacity setting. A value of 0 disables the minimum. Only applies to simple mode.")
	f.IntVar(&i.MaxInFlightRequests, prefix+".max-in-flight-requests", 0, "Experimental: Maximum number of requests this index gateway client may have in flight at once. Requests arriving when the limit is reached are rejected immediately with an HTTP 503 status instead of waiting, which bounds the resources this process commits to an index gateway that is slow, saturated, or unreachable. The limit applies per client: one client is built per schema period config, so the process-wide number of in-flight requests can reach this value multiplied by the number of clients. 0 disables the limit.")
	f.IntVar(&i.MaxRetries, prefix+".max-retries", -1, "Experimental: Maximum number of other index gateway instances a failed request is retried against. Each instance is tried at most once, so a request makes at most this many retries plus one attempt in total. Bounding this stops a single request from walking every replica, which can otherwise block the calling goroutine for the sum of every replica's timeout. -1 preserves the existing behavior: up to 2 retries for GetShards and all candidate instances for other requests. 0 disables retries.")
	f.StringVar(&i.Sharding, prefix+".sharding", ShardingDefault, "Experimental: How requests are routed to index gateways in ring mode. 'default' sends each request to a gateway of the tenant's shuffle shard. 'per_index' splits each request by index table and sends each part to the gateways that own that table's index for the tenant, then merges the answers. 'per_index' requires -index-gateway.mode=ring and is meant to be used with -index-gateway.per-index-ownership.enabled.")
	f.IntVar(&i.PerIndexMaxConcurrency, prefix+".per-index-max-concurrency", 8, "Experimental: Maximum number of parts of one request sent at once when -"+prefix+".sharding=per_index. Each part counts towards -"+prefix+".max-in-flight-requests.")
}

func (i *ClientConfig) RegisterFlags(f *flag.FlagSet) {
	i.RegisterFlagsWithPrefix("index-gateway-client", f)
}

// Validate returns an error if the configuration is not usable.
func (i *ClientConfig) Validate() error {
	if i.MaxInFlightRequests < 0 {
		return errors.New("index gateway client max-in-flight-requests must be greater than or equal to 0")
	}
	if i.MaxRetries < -1 {
		return errors.New("index gateway client max-retries must be greater than or equal to -1")
	}
	switch i.Sharding {
	case ShardingDefault, ShardingPerIndex:
	default:
		return fmt.Errorf("index gateway client sharding %q not supported, supported values: %s, %s", i.Sharding, ShardingDefault, ShardingPerIndex)
	}
	if i.PerIndexMaxConcurrency < 1 {
		return errors.New("index gateway client per-index-max-concurrency must be greater than or equal to 1")
	}
	return nil
}

// validatePerIndex checks the settings that are only known once the client is
// built: the mode is copied from the index gateway config and the table range
// is set per schema period.
func (i *ClientConfig) validatePerIndex() error {
	if i.Sharding != ShardingPerIndex {
		return nil
	}
	if i.Mode != RingMode {
		return errors.New("index gateway client sharding=per_index requires index-gateway.mode=ring")
	}
	if i.PerIndexMaxConcurrency < 1 {
		return errors.New("index gateway client per-index-max-concurrency must be greater than or equal to 1")
	}
	if i.TableRange.PeriodConfig == nil {
		return errors.New("index gateway client sharding=per_index requires the table range of the schema period")
	}
	if p := i.TableRange.PeriodConfig.IndexTables.Period; p != tablePeriod {
		return fmt.Errorf("index gateway client sharding=per_index requires an index period of %s, got %s", tablePeriod, p)
	}
	return nil
}

type GatewayClient struct {
	logger                            log.Logger
	cfg                               ClientConfig
	storeGatewayClientRequestDuration *prometheus.HistogramVec
	retriesHistogram                  *prometheus.HistogramVec
	inFlight                          gate.Gate
	ownership                         *IndexOwnership
	fanoutMetrics                     *fanoutMetrics
	dnsProvider                       discovery.DNS
	pool                              *client.Pool
	ring                              ring.ReadRing
	limits                            Limits
	buckets                           []time.Duration
	done                              chan struct{}
}

// NewGatewayClient instantiates a new client used to communicate with an Index Gateway instance.
//
// If it is configured to be in ring mode, a pool of GRPC connections to all Index Gateway instances is created using a ring.
// Otherwise, it creates a GRPC connection pool to as many addresses as can be resolved from the given address.
func NewGatewayClient(cfg ClientConfig, r prometheus.Registerer, limits Limits, logger log.Logger, metricsNamespace string) (*GatewayClient, error) {
	if err := cfg.validatePerIndex(); err != nil {
		return nil, err
	}

	latency, err := registerOrExisting(r, prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Name:      "index_gateway_request_duration_seconds",
		Help:      "Time (in seconds) spent serving requests when using the index gateway",
		Buckets:   instrument.DefBuckets,
	}, []string{"operation", "status_code"}))
	if err != nil {
		return nil, err
	}

	retries, err := registerOrExisting(r, prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Name:      "index_gateway_request_retries",
		Help:      "Number of retries attempted before a successful or failed index gateway request",
		Buckets:   []float64{0, 1, 2, 3, 4, 5, 10, 15, 20, 25, 30, 40, 50, 100},
	}, []string{"status"}))
	if err != nil {
		return nil, err
	}

	fanout, err := newFanoutMetrics(r)
	if err != nil {
		return nil, err
	}

	buckets := make([]time.Duration, len(cfg.TimeBasedShardingBuckets))
	for i := range len(buckets) {
		b, err := time.ParseDuration(cfg.TimeBasedShardingBuckets[i])
		if err != nil {
			level.Warn(logger).Log("msg", "failed to parse time duration of bucket", "err", err.Error(), "value", cfg.TimeBasedShardingBuckets[i])
			continue
		}
		buckets[i] = b.Abs() * -1 // Buckets reference times in the past, so we need negative durations
	}
	// Sort descending, since we have negative duration values
	slices.SortFunc(buckets, func(a, b time.Duration) int { return cmp.Compare(b, a) })

	gateReg := prometheus.WrapRegistererWithPrefix("loki_index_gateway_client_", r)

	sgClient := &GatewayClient{
		logger:                            logger,
		cfg:                               cfg,
		storeGatewayClientRequestDuration: latency,
		retriesHistogram:                  retries,
		inFlight:                          newInFlightGate(cfg.MaxInFlightRequests, gateReg),
		fanoutMetrics:                     fanout,
		ring:                              cfg.Ring,
		limits:                            limits,
		buckets:                           buckets,
		done:                              make(chan struct{}),
	}
	if cfg.Sharding == ShardingPerIndex {
		sgClient.ownership = NewIndexOwnership(cfg.Ring)
	}

	unaryInterceptors, streamInterceptors := instrumentation(cfg, sgClient.storeGatewayClientRequestDuration)
	dialOpts, err := cfg.GRPCClientConfig.DialOption(unaryInterceptors, streamInterceptors, middleware.NoOpInvalidClusterValidationReporter)
	if err != nil {
		return nil, errors.Wrap(err, "index gateway grpc dial option")
	}
	dialOpts = append(dialOpts, grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	factory := func(addr string) (client.PoolClient, error) {
		igPool, err := NewClientPool(addr, dialOpts)
		if err != nil {
			return nil, errors.Wrap(err, "new index gateway grpc pool")
		}

		return igPool, nil
	}

	//FIXME(ewelch) we don't expose the pool configs nor set defaults, and register flags is kind of messed up with remote config being defined somewhere else
	//make a separate PR to make the pool config generic so it can be used with proper names in multiple places.
	sgClient.cfg.PoolConfig.RemoteTimeout = 2 * time.Second
	sgClient.cfg.PoolConfig.ClientCleanupPeriod = 5 * time.Second
	sgClient.cfg.PoolConfig.HealthCheckIngesters = true

	if sgClient.cfg.Mode == RingMode {
		sgClient.pool = clientpool.NewPool("index-gateway", sgClient.cfg.PoolConfig, sgClient.ring, client.PoolAddrFunc(factory), logger, metricsNamespace)
	} else {
		// Note we don't use clientpool.NewPool because we want to provide our own discovery function
		poolCfg := client.PoolConfig{
			CheckInterval:      sgClient.cfg.PoolConfig.ClientCleanupPeriod,
			HealthCheckEnabled: sgClient.cfg.PoolConfig.HealthCheckIngesters,
			HealthCheckTimeout: sgClient.cfg.PoolConfig.RemoteTimeout,
		}
		clients, err := registerOrExisting(r, prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "index_gateway_clients",
			Help:      "The current number of index gateway clients.",
		}))
		if err != nil {
			return nil, err
		}
		//TODO(ewelch) we can't use metrics in the provider because of duplicate registration errors
		dnsProvider := discovery.NewDNS(logger, sgClient.cfg.PoolConfig.ClientCleanupPeriod, sgClient.cfg.Address, nil)
		sgClient.dnsProvider = dnsProvider

		discovery := func() ([]string, error) {
			return dnsProvider.Addresses(), nil
		}
		sgClient.pool = client.NewPool("index gateway", poolCfg, discovery, client.PoolAddrFunc(factory), clients, logger)

	}

	// We have to start the pool service, it will handle removing stale clients in the background
	err = services.StartAndAwaitRunning(context.Background(), sgClient.pool)
	if err != nil {
		return nil, errors.Wrap(err, "failed to start index gateway connection pool")
	}

	return sgClient, nil
}

// Stop stops the execution of this gateway client.
func (s *GatewayClient) Stop() {
	ctx, cancel := context.WithTimeoutCause(context.Background(), 10*time.Second, errors.New("service shutdown timeout expired"))
	defer cancel()
	err := services.StopAndAwaitTerminated(ctx, s.pool)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to stop index gateway connection pool", "err", err)
	}
	if s.cfg.Mode == SimpleMode {
		s.dnsProvider.Stop()
	}
}

func (s *GatewayClient) GetChunkRef(ctx context.Context, in *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error) {
	return callIndexGateway(ctx, s, "GetChunkRef", in, in.From, in.Through,
		func(r *logproto.GetChunkRefRequest, from, through model.Time) { r.From, r.Through = from, through },
		logproto.IndexGatewayClient.GetChunkRef, mergeChunkRefResponses)
}

func (s *GatewayClient) GetSeries(ctx context.Context, in *logproto.GetSeriesRequest) (*logproto.GetSeriesResponse, error) {
	return callIndexGateway(ctx, s, "GetSeries", in, in.From, in.Through,
		func(r *logproto.GetSeriesRequest, from, through model.Time) { r.From, r.Through = from, through },
		logproto.IndexGatewayClient.GetSeries, mergeSeriesResponses)
}

func (s *GatewayClient) LabelNamesForMetricName(ctx context.Context, in *logproto.LabelNamesForMetricNameRequest) (*logproto.LabelResponse, error) {
	return callIndexGateway(ctx, s, "LabelNamesForMetricName", in, in.From, in.Through,
		func(r *logproto.LabelNamesForMetricNameRequest, from, through model.Time) {
			r.From, r.Through = from, through
		},
		logproto.IndexGatewayClient.LabelNamesForMetricName, mergeLabelResponses)
}

func (s *GatewayClient) LabelValuesForMetricName(ctx context.Context, in *logproto.LabelValuesForMetricNameRequest) (*logproto.LabelResponse, error) {
	return callIndexGateway(ctx, s, "LabelValuesForMetricName", in, in.From, in.Through,
		func(r *logproto.LabelValuesForMetricNameRequest, from, through model.Time) {
			r.From, r.Through = from, through
		},
		logproto.IndexGatewayClient.LabelValuesForMetricName, mergeLabelResponses)
}

func (s *GatewayClient) GetStats(ctx context.Context, in *logproto.IndexStatsRequest) (*logproto.IndexStatsResponse, error) {
	return callIndexGateway(ctx, s, "GetStats", in, in.From, in.Through,
		func(r *logproto.IndexStatsRequest, from, through model.Time) { r.From, r.Through = from, through },
		logproto.IndexGatewayClient.GetStats, mergeStatsResponses)
}

func (s *GatewayClient) GetVolume(ctx context.Context, in *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
	return callIndexGateway(ctx, s, "GetVolume", in, in.From, in.Through,
		func(r *logproto.VolumeRequest, from, through model.Time) { r.From, r.Through = from, through },
		logproto.IndexGatewayClient.GetVolume,
		func(resps []*logproto.VolumeResponse) *logproto.VolumeResponse {
			return mergeVolumeResponses(resps, in.Limit)
		})
}

// callIndexGateway sends a unary request in covering [from, through]. With
// per_index sharding it is split by table, each part getting a copy of in with
// its range set by setRange, and the answers are combined with merge.
// Otherwise, or when the range reads no table of this client's period, in is
// sent to one gateway as it is.
func callIndexGateway[Req, Resp any](
	ctx context.Context,
	s *GatewayClient,
	op string,
	in *Req,
	from, through model.Time,
	setRange func(r *Req, from, through model.Time),
	call func(c logproto.IndexGatewayClient, ctx context.Context, in *Req, opts ...grpc.CallOption) (Resp, error),
	merge func([]Resp) Resp,
) (Resp, error) {
	var zero Resp
	if s.perIndex() {
		resps, ok, err := fanOut(ctx, s, op, from, through, func(ctx context.Context, client logproto.IndexGatewayClient, partFrom, partThrough model.Time) (Resp, error) {
			req := in
			if partFrom != from || partThrough != through {
				part := *in
				setRange(&part, partFrom, partThrough)
				req = &part
			}
			return call(client, ctx, req)
		})
		if ok {
			if err != nil {
				return zero, err
			}
			return merge(resps), nil
		}
	}

	resp := zero
	err := s.poolDo(ctx, func(client logproto.IndexGatewayClient) error {
		r, err := call(client, ctx, in)
		if err != nil {
			return err
		}
		resp = r
		return nil
	}, func(addrs []string) []string {
		return addressesForQueryEndTime(addrs, through.Time(), s.buckets, time.Now().UTC())
	})
	if err != nil {
		return zero, err
	}
	return resp, nil
}

func (s *GatewayClient) GetShards(ctx context.Context, in *logproto.ShardsRequest) (res *logproto.ShardsResponse, err error) {
	maxRetries := s.cfg.MaxRetries
	if maxRetries < 0 {
		// Keep the legacy GetShards ceiling when disabled
		maxRetries = 2
	}
	callback := func(client logproto.IndexGatewayClient) error {
		perReplicaResult := &logproto.ShardsResponse{}
		streamer, err := client.GetShards(ctx, in)
		if err != nil {
			return errors.Wrap(err, "get shards")
		}

		// TODO(owen-d): stream currently unused (buffered) because query planning doesn't expect a streamed response,
		// but can be improved easily in the future by using a stream here.
		for {
			resp, err := streamer.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return errors.WithStack(err)
			}
			perReplicaResult.Merge(resp)
		}

		// Since `poolDo` retries on error, we only want to set the response if we got a successful response.
		// This avoids cases where we add duplicates to the response on retries.
		res = perReplicaResult

		return nil
	}

	if s.perIndex() {
		if parts := splitByTable(in.From, in.Through, s.cfg.TableRange); len(parts) > 0 {
			tenantID, err := tenant.TenantID(ctx)
			if err != nil {
				return nil, errors.Wrap(err, "index gateway client get tenant ID")
			}
			// Shards are not merged across tables: a request spanning tables goes
			// whole to the owners of its first table. They answer it correctly, but
			// have to load the other tables as any non-owner would, so it is slower.
			// Multi-table shard requests are expected to be rare.
			tables := "single"
			if len(parts) > 1 {
				tables = "multi"
			}
			s.fanoutMetrics.getShardsRequests.WithLabelValues(tables).Inc()
			if err := s.doOnIndexOwners(ctx, tenantID, parts[0].Table, maxRetries, callback); err != nil {
				return nil, err
			}
			return res, nil
		}
	}

	if err := s.poolDoWithMaxRetries(
		ctx,
		maxRetries,
		callback,
		func(addrs []string) []string {
			return addressesForQueryEndTime(addrs, in.Through.Time(), s.buckets, time.Now().UTC())
		},
	); err != nil {
		return nil, err
	}
	return res, nil
}

// poolDo tries each gateway once, up to cfg.MaxRetries retries when configured.
func (s *GatewayClient) poolDo(
	ctx context.Context,
	callback func(client logproto.IndexGatewayClient) error,
	filterServerList func([]string) []string,
) error {
	return s.poolDoWithMaxRetries(ctx, s.cfg.MaxRetries, callback, filterServerList)
}

func (s *GatewayClient) poolDoWithMaxRetries(
	ctx context.Context,
	maxRetries int,
	callback func(client logproto.IndexGatewayClient) error,
	filterServerList func([]string) []string,
) error {
	if err := s.inFlight.Start(ctx); err != nil {
		return mapInFlightGateError(err)
	}
	defer s.inFlight.Done()

	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return errors.Wrap(err, "index gateway client get tenant ID")
	}
	addrs, err := s.getServerAddresses(userID)
	if err != nil {
		return err
	}

	if len(addrs) == 0 {
		level.Error(s.logger).Log("msg", fmt.Sprintf("no index gateway instances found for tenant %s", userID))
		return fmt.Errorf("no index gateway instances found for tenant %s", userID)
	}

	if s.cfg.Mode == SimpleMode {
		slices.Sort(addrs)
		addrs = filterServerList(addrs)
		addrs = s.jumpHashShuffleSharding(userID, addrs)
	}

	return s.tryAddrs(userID, addrs, maxRetries, callback)
}

// tryAddrs calls callback on the gateways at addrs in random order until one
// succeeds, giving up after maxRetries failures when maxRetries >= 0.
func (s *GatewayClient) tryAddrs(
	userID string,
	addrs []string,
	maxRetries int,
	callback func(client logproto.IndexGatewayClient) error,
) error {
	// shuffle addresses to make sure we don't always access the same Index Gateway instances in sequence for same tenant.
	rand.Shuffle(len(addrs), func(i, j int) {
		addrs[i], addrs[j] = addrs[j], addrs[i]
	})

	var (
		errCount int
		lastErr  error
		status   = "failure"
	)
	defer func() { s.retriesHistogram.WithLabelValues(status).Observe(float64(errCount)) }()

	for _, addr := range addrs {
		if s.cfg.LogGatewayRequests {
			level.Debug(s.logger).Log("msg", "sending request to gateway", "gateway", addr, "tenant", userID)
		}

		err := s.do(addr, callback)
		if err == nil {
			status = "success"
			return nil
		}

		lastErr = err
		errCount++

		if isServiceUnavailable(err) {
			level.Warn(s.logger).Log("msg", "index gateway request returned HTTP 503", "gateway", addr, "tenant", userID, "err", err)
		} else {
			level.Error(s.logger).Log("msg", "index gateway request failed, trying another instance", "gateway", addr, "tenant", userID, "err", err)
		}

		if maxRetries >= 0 && errCount > maxRetries {
			break
		}
	}

	return lastErr
}

func (s *GatewayClient) do(addr string, callback func(client logproto.IndexGatewayClient) error) error {
	genericClient, err := s.pool.GetClientFor(addr)
	if err != nil {
		return errors.Wrapf(err, "get client for index gateway %s", addr)
	}
	return callback(genericClient.(logproto.IndexGatewayClient))
}

// jumpHashShuffleSharding uses jump hash to consistently select a subset of index gateway instances for a tenant.
// It ensures that each tenant gets a deterministic set of gateways based on the IndexGatewayMaxCapacity limit,
// which is expressed as a fraction (0.0 to 1.0) of the total available gateways.
// The function hashes the tenant ID to distribute tenants across gateways,
// providing stable gateway assignments while allowing for controlled capacity allocation per tenant.
func (s *GatewayClient) jumpHashShuffleSharding(tenant string, addrs []string) []string {
	if len(addrs) <= 1 {
		return addrs
	}

	f := s.limits.IndexGatewayMaxCapacity(tenant)
	if f == 1.0 || f == 0.0 {
		return addrs
	}

	maxAvailableGateways := len(addrs)
	numUserGateways := int(math.Ceil(float64(maxAvailableGateways) * f))
	if numUserGateways < s.cfg.MinShuffleShardSize {
		numUserGateways = s.cfg.MinShuffleShardSize
	}
	if numUserGateways >= maxAvailableGateways {
		return addrs
	}

	cs := xxhash.Sum64String(tenant)
	idx := int(jumphash.Hash(cs, maxAvailableGateways))

	subset := make([]string, 0, numUserGateways)
	for i := range numUserGateways {
		subset = append(subset, addrs[(idx+i)%len(addrs)])
	}

	return subset
}

func (s *GatewayClient) getServerAddresses(tenantID string) ([]string, error) {
	var addrs []string
	// The GRPC pool we use only does discovery calls when cleaning up already existing connections,
	// so the list of addresses should always be provided from the external provider (ring or DNS)
	// and not from the RegisteredAddresses method as this list is only populated after a call to GetClientFor
	if s.cfg.Mode == RingMode {
		r := GetShuffleShardingSubring(s.ring, tenantID, s.limits)
		rs, err := r.GetReplicationSetForOperation(IndexesRead)
		if err != nil {
			return nil, errors.Wrap(err, "index gateway get ring")
		}
		addrs = rs.GetAddresses()
	} else {
		addrs = s.dnsProvider.Addresses()
	}

	// DNS and ring discovery can return duplicate addresses.
	return dedupe(addrs), nil
}

// dedupe removes duplicate addresses in place, preserving their first occurrence.
// Discovery returns a fresh slice, so its backing array can be reused.
func dedupe(addrs []string) []string {
	seen := make(map[string]struct{}, len(addrs))
	unique := addrs[:0]
	for _, addr := range addrs {
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		unique = append(unique, addr)
	}
	return unique
}

func instrumentation(cfg ClientConfig, clientRequestDuration *prometheus.HistogramVec) ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	var unaryInterceptors []grpc.UnaryClientInterceptor
	unaryInterceptors = append(unaryInterceptors, cfg.GRPCUnaryClientInterceptors...)
	unaryInterceptors = append(unaryInterceptors, middleware.ClientUserHeaderInterceptor)
	unaryInterceptors = append(unaryInterceptors, middleware.UnaryClientInstrumentInterceptor(clientRequestDuration))

	var streamInterceptors []grpc.StreamClientInterceptor
	streamInterceptors = append(streamInterceptors, cfg.GRCPStreamClientInterceptors...)
	streamInterceptors = append(streamInterceptors, middleware.StreamClientUserHeaderInterceptor)
	streamInterceptors = append(streamInterceptors, middleware.StreamClientInstrumentInterceptor(clientRequestDuration))

	return unaryInterceptors, streamInterceptors
}

func addressesForQueryEndTime(addrs []string, t time.Time, buckets []time.Duration, now time.Time) []string {
	n := len(addrs)
	m := len(buckets)

	// If there are no buckets, return all addresses
	if m < 1 {
		return addrs
	}

	// The bucketing only really makes sense if there are equal or more than 2^len(buckets) index gateways.
	// Example with 3 buckets and 8 instances:
	// Bucket 0:  now       -> now - 7d   => addrs[0:4]
	// Bucket 1:  now - 7d  -> now - 14d  => addrs[4:6]
	// Bucket 2:  now - 14d -> now - 21d  => addrs[6:7]
	// Remainder: now - 21d -> now - Inf  => addrs[7:8]
	if n < (1 << m) {
		return addrs
	}

	today := now.Truncate(24 * time.Hour)
	start, end := 0, n>>1

	for i := range m {
		if t.After(today.Add(buckets[i])) {
			break
		}

		start = end
		end = end + (n >> (i + 2)) // n / 2^(i+2)

		if i == m-1 {
			end = n
		}
	}

	return addrs[start:end]
}

// registerOrExisting registers c with r, or returns the collector already
// registered under the same descriptor. One client is built per schema period,
// and they share their metrics.
func registerOrExisting[T prometheus.Collector](r prometheus.Registerer, c T) (T, error) {
	if r == nil {
		return c, nil
	}
	if err := r.Register(c); err != nil {
		alreadyErr, ok := err.(prometheus.AlreadyRegisteredError)
		if !ok {
			return c, err
		}
		return alreadyErr.ExistingCollector.(T), nil
	}
	return c, nil
}

// fanoutMetrics are the metrics of per_index sharding.
type fanoutMetrics struct {
	requestTables     *prometheus.HistogramVec
	boundaryParts     *prometheus.CounterVec
	getShardsRequests *prometheus.CounterVec
}

func newFanoutMetrics(r prometheus.Registerer) (*fanoutMetrics, error) {
	var (
		m   fanoutMetrics
		err error
	)
	m.requestTables, err = registerOrExisting(r, prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Name:      "index_gateway_client_request_tables",
		Help:      "Number of index tables a request is split into with per_index sharding. Each table is a separate index gateway request.",
		Buckets:   []float64{1, 2, 3, 4, 7, 14, 31},
	}, []string{"operation"}))
	if err != nil {
		return nil, err
	}
	m.boundaryParts, err = registerOrExisting(r, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "index_gateway_client_boundary_parts_total",
		Help:      "Index gateway requests sent with per_index sharding only because a request ends exactly at the start of an index table.",
	}, []string{"operation"}))
	if err != nil {
		return nil, err
	}
	m.getShardsRequests, err = registerOrExisting(r, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "index_gateway_client_get_shards_requests_total",
		Help:      "GetShards requests sent with per_index sharding, by whether they read a single index table or several. A request that reads several tables is sent whole to the owners of its first table.",
	}, []string{"tables"}))
	if err != nil {
		return nil, err
	}
	return &m, nil
}
