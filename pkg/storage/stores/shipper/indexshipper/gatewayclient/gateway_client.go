package gatewayclient

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/dns"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/distributor/clientpool"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/indexgateway"
	util_math "github.com/grafana/loki/pkg/util/math"
)

const (
	maxQueriesPerGrpc      = 100
	maxConcurrentGrpcCalls = 10
)

var (
	ErrIndexGatewayDNSLookupTimeout = errors.New("index gateway DNS lookup timeout")
)

// IndexGatewayClientConfig configures the Index Gateway client used to
// communicate with the Index Gateway server.
type IndexGatewayClientConfig struct {
	// Mode sets in which mode the client will operate. It is actually defined at the
	// index_gateway YAML section and reused here.
	Mode indexgateway.Mode `yaml:"-"`

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
}

// RegisterFlagsWithPrefix register client-specific flags with the given prefix.
//
// Flags that are used by both, client and server, are defined in the indexgateway package.
func (i *IndexGatewayClientConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	i.GRPCClientConfig.RegisterFlagsWithPrefix(prefix+".grpc", f)
	f.StringVar(&i.Address, prefix+".server-address", "", "Hostname or IP of the Index Gateway gRPC server running in simple mode. Can also be prefixed with dns+, dnssrv+, or dnssrvnoa+ to resolve a DNS A record with multiple IP's, a DNS SRV record with a followup A record lookup, or a DNS SRV record without a followup A record lookup, respectively.")
	f.BoolVar(&i.LogGatewayRequests, prefix+".log-gateway-requests", false, "Whether requests sent to the gateway should be logged or not.")
}

func (i *IndexGatewayClientConfig) RegisterFlags(f *flag.FlagSet) {
	i.RegisterFlagsWithPrefix("index-gateway-client", f)
}

type GatewayClient struct {
	logger log.Logger

	cfg IndexGatewayClientConfig

	storeGatewayClientRequestDuration *prometheus.HistogramVec

	dnsProvider *dns.Provider

	pool *ring_client.Pool

	ring ring.ReadRing

	limits indexgateway.Limits

	done chan struct{}
}

// NewGatewayClient instantiates a new client used to communicate with an Index Gateway instance.
//
// If it is configured to be in ring mode, a pool of GRPC connections to all Index Gateway instances is created using a ring.
// Otherwise, it creates a GRPC connection pool to as many addresses which can be resolved from the given address.
func NewGatewayClient(cfg IndexGatewayClientConfig, r prometheus.Registerer, limits indexgateway.Limits, logger log.Logger) (*GatewayClient, error) {
	latency := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "index_gateway_request_duration_seconds",
		Help:      "Time (in seconds) spent serving requests when using the index gateway",
		Buckets:   instrument.DefBuckets,
	}, []string{"operation", "status_code"})
	if r != nil {
		err := r.Register(latency)
		if err != nil {
			alreadyErr, ok := err.(prometheus.AlreadyRegisteredError)
			if !ok {
				return nil, err
			}
			latency = alreadyErr.ExistingCollector.(*prometheus.HistogramVec)
		}
	}

	sgClient := &GatewayClient{
		logger:                            logger,
		cfg:                               cfg,
		storeGatewayClientRequestDuration: latency,
		ring:                              cfg.Ring,
		limits:                            limits,
		done:                              make(chan struct{}),
	}

	dialOpts, err := cfg.GRPCClientConfig.DialOption(grpcclient.Instrument(sgClient.storeGatewayClientRequestDuration))
	if err != nil {
		return nil, errors.Wrap(err, "index gateway grpc dial option")
	}
	factory := func(addr string) (ring_client.PoolClient, error) {
		igPool, err := NewIndexGatewayGRPCPool(addr, dialOpts)
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

	if sgClient.cfg.Mode == indexgateway.RingMode {
		sgClient.pool = clientpool.NewPool(sgClient.cfg.PoolConfig, sgClient.ring, factory, logger)
	} else {
		// Note we don't use clientpool.NewPool because we want to provide our own discovery function
		poolCfg := ring_client.PoolConfig{
			CheckInterval:      sgClient.cfg.PoolConfig.ClientCleanupPeriod,
			HealthCheckEnabled: sgClient.cfg.PoolConfig.HealthCheckIngesters,
			HealthCheckTimeout: sgClient.cfg.PoolConfig.RemoteTimeout,
		}
		clients := prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "index_gateway_clients",
			Help:      "The current number of index gateway clients.",
		})
		if r != nil {
			err := r.Register(clients)
			if err != nil {
				alreadyErr, ok := err.(prometheus.AlreadyRegisteredError)
				if !ok {
					return nil, err
				}
				clients = alreadyErr.ExistingCollector.(prometheus.Gauge)
			}
		}
		//TODO(ewelch) we can't use metrics in the provider because of duplicate registration errors
		dnsProvider := dns.NewProvider(logger, nil, dns.GolangResolverType)
		sgClient.dnsProvider = dnsProvider

		// Make an attempt to do one DNS lookup so we can start with addresses
		sgClient.runDiscovery()
		go sgClient.discoveryLoop()

		discovery := func() ([]string, error) {
			return dnsProvider.Addresses(), err
		}
		sgClient.pool = ring_client.NewPool("index gateway", poolCfg, discovery, factory, clients, logger)

	}

	// We have to start the pool service, it will handle removing stale clients in the background
	err = services.StartAndAwaitRunning(context.Background(), sgClient.pool)
	if err != nil {
		return nil, errors.Wrap(err, "failed to start index gateway connection pool")
	}

	return sgClient, nil
}

func (s *GatewayClient) discoveryLoop() {
	ticker := time.NewTicker(s.cfg.PoolConfig.ClientCleanupPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.runDiscovery()
		case <-s.done:
			return
		}
	}
}

func (s *GatewayClient) runDiscovery() {
	ctx, cancel := context.WithTimeoutCause(context.Background(), 5*time.Second, ErrIndexGatewayDNSLookupTimeout)
	defer cancel()
	err := s.dnsProvider.Resolve(ctx, []string{s.cfg.Address})
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to resolve index gateway address", "err", err)
	}
}

// Stop stops the execution of this gateway client.
func (s *GatewayClient) Stop() {
	ctx, cancel := context.WithTimeoutCause(context.Background(), 10*time.Second, errors.New("service shutdown timeout expired"))
	defer cancel()
	err := services.StopAndAwaitTerminated(ctx, s.pool)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to stop index gateway connection pool", "err", err)
	}
	close(s.done)
}

func (s *GatewayClient) QueryPages(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
	if len(queries) <= maxQueriesPerGrpc {
		return s.doQueries(ctx, queries, callback)
	}

	jobsCount := len(queries) / maxQueriesPerGrpc
	if len(queries)%maxQueriesPerGrpc != 0 {
		jobsCount++
	}
	return concurrency.ForEachJob(ctx, jobsCount, maxConcurrentGrpcCalls, func(ctx context.Context, idx int) error {
		return s.doQueries(ctx, queries[idx*maxQueriesPerGrpc:util_math.Min((idx+1)*maxQueriesPerGrpc, len(queries))], callback)
	})
}

func (s *GatewayClient) QueryIndex(_ context.Context, _ *logproto.QueryIndexRequest, _ ...grpc.CallOption) (logproto.IndexGateway_QueryIndexClient, error) {
	panic("not implemented")
}

func (s *GatewayClient) GetChunkRef(ctx context.Context, in *logproto.GetChunkRefRequest, opts ...grpc.CallOption) (*logproto.GetChunkRefResponse, error) {
	var (
		resp *logproto.GetChunkRefResponse
		err  error
	)
	err = s.poolDo(ctx, func(client logproto.IndexGatewayClient) error {
		resp, err = client.GetChunkRef(ctx, in, opts...)
		return err
	})
	return resp, err
}

func (s *GatewayClient) GetSeries(ctx context.Context, in *logproto.GetSeriesRequest, opts ...grpc.CallOption) (*logproto.GetSeriesResponse, error) {
	var (
		resp *logproto.GetSeriesResponse
		err  error
	)
	err = s.poolDo(ctx, func(client logproto.IndexGatewayClient) error {
		resp, err = client.GetSeries(ctx, in, opts...)
		return err
	})
	return resp, err
}

func (s *GatewayClient) LabelNamesForMetricName(ctx context.Context, in *logproto.LabelNamesForMetricNameRequest, opts ...grpc.CallOption) (*logproto.LabelResponse, error) {
	var (
		resp *logproto.LabelResponse
		err  error
	)
	err = s.poolDo(ctx, func(client logproto.IndexGatewayClient) error {
		resp, err = client.LabelNamesForMetricName(ctx, in, opts...)
		return err
	})
	return resp, err
}

func (s *GatewayClient) LabelValuesForMetricName(ctx context.Context, in *logproto.LabelValuesForMetricNameRequest, opts ...grpc.CallOption) (*logproto.LabelResponse, error) {
	var (
		resp *logproto.LabelResponse
		err  error
	)
	err = s.poolDo(ctx, func(client logproto.IndexGatewayClient) error {
		resp, err = client.LabelValuesForMetricName(ctx, in, opts...)
		return err
	})
	return resp, err
}

func (s *GatewayClient) GetStats(ctx context.Context, in *logproto.IndexStatsRequest, opts ...grpc.CallOption) (*logproto.IndexStatsResponse, error) {
	var (
		resp *logproto.IndexStatsResponse
		err  error
	)
	err = s.poolDo(ctx, func(client logproto.IndexGatewayClient) error {
		resp, err = client.GetStats(ctx, in, opts...)
		return err
	})
	return resp, err
}

func (s *GatewayClient) GetVolume(ctx context.Context, in *logproto.VolumeRequest, opts ...grpc.CallOption) (*logproto.VolumeResponse, error) {
	var (
		resp *logproto.VolumeResponse
		err  error
	)
	err = s.poolDo(ctx, func(client logproto.IndexGatewayClient) error {
		resp, err = client.GetVolume(ctx, in, opts...)
		return err
	})
	return resp, err
}

func (s *GatewayClient) doQueries(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
	queryKeyQueryMap := make(map[string]index.Query, len(queries))
	gatewayQueries := make([]*logproto.IndexQuery, 0, len(queries))

	for _, query := range queries {
		queryKeyQueryMap[index.QueryKey(query)] = query
		gatewayQueries = append(gatewayQueries, &logproto.IndexQuery{
			TableName:        query.TableName,
			HashValue:        query.HashValue,
			RangeValuePrefix: query.RangeValuePrefix,
			RangeValueStart:  query.RangeValueStart,
			ValueEqual:       query.ValueEqual,
		})
	}

	return s.poolDo(ctx, func(client logproto.IndexGatewayClient) error {
		return s.clientDoQueries(ctx, gatewayQueries, queryKeyQueryMap, callback, client)
	})

}

// clientDoQueries send a query request to an Index Gateway instance using the given gRPC client.
//
// It is used by both, simple and ring mode.
func (s *GatewayClient) clientDoQueries(ctx context.Context, gatewayQueries []*logproto.IndexQuery,
	queryKeyQueryMap map[string]index.Query, callback index.QueryPagesCallback, client logproto.IndexGatewayClient,
) error {
	streamer, err := client.QueryIndex(ctx, &logproto.QueryIndexRequest{Queries: gatewayQueries})
	if err != nil {
		return errors.Wrap(err, "query index")
	}

	for {
		resp, err := streamer.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.WithStack(err)
		}
		query, ok := queryKeyQueryMap[resp.QueryKey]
		if !ok {
			level.Error(s.logger).Log("msg", fmt.Sprintf("unexpected %s QueryKey received, expected queries %s", resp.QueryKey, fmt.Sprint(queryKeyQueryMap)))
			return fmt.Errorf("unexpected %s QueryKey received", resp.QueryKey)
		}
		if !callback(query, &readBatch{resp}) {
			return nil
		}
	}

	return nil
}

// poolDo executes the given function for each Index Gateway instance in the ring mapping to the correct tenant in the index.
// In case of callback failure, we'll try another member of the ring for that tenant ID.
func (s *GatewayClient) poolDo(ctx context.Context, callback func(client logproto.IndexGatewayClient) error) error {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return errors.Wrap(err, "index gateway client get tenant ID")
	}
	addrs, err := s.getServerAddresses(userID)
	if err != nil {
		return err
	}

	if len(addrs) == 0 {
		return fmt.Errorf("no index gateway instances found for tenant %s", userID)
	}

	var lastErr error
	for _, addr := range addrs {
		if s.cfg.LogGatewayRequests {
			level.Debug(s.logger).Log("msg", "sending request to gateway", "gateway", addr, "tenant", userID)
		}

		genericClient, err := s.pool.GetClientFor(addr)
		if err != nil {
			level.Error(s.logger).Log("msg", fmt.Sprintf("failed to get client for instance %s", addr), "err", err)
			continue
		}

		client := (genericClient.(logproto.IndexGatewayClient))
		if err := callback(client); err != nil {
			lastErr = err
			level.Error(s.logger).Log("msg", fmt.Sprintf("client do failed for instance %s", addr), "err", err)
			continue
		}

		return nil
	}

	return lastErr
}

func (s *GatewayClient) getServerAddresses(tenantID string) ([]string, error) {
	var addrs []string
	// The GRPC pool we use only does discovery calls when cleaning up already existing connections,
	// so the list of addresses should always be provided from the external provider (ring or DNS)
	// and not from the RegisteredAddresses method as this list is only populated after a call to GetClientFor
	if s.cfg.Mode == indexgateway.RingMode {
		r := indexgateway.GetShuffleShardingSubring(s.ring, tenantID, s.limits)
		rs, err := r.GetReplicationSetForOperation(indexgateway.IndexesRead)
		if err != nil {
			return nil, errors.Wrap(err, "index gateway get ring")
		}
		addrs = rs.GetAddresses()
	} else {
		addrs = s.dnsProvider.Addresses()
	}

	// shuffle addresses to make sure we don't always access the same Index Gateway instances in sequence for same tenant.
	rand.Shuffle(len(addrs), func(i, j int) {
		addrs[i], addrs[j] = addrs[j], addrs[i]
	})

	return addrs, nil
}

func (s *GatewayClient) NewWriteBatch() index.WriteBatch {
	panic("unsupported")
}

func (s *GatewayClient) BatchWrite(_ context.Context, _ index.WriteBatch) error {
	panic("unsupported")
}

type readBatch struct {
	*logproto.QueryIndexResponse
}

func (r *readBatch) Iterator() index.ReadBatchIterator {
	return &grpcIter{
		i:                  -1,
		QueryIndexResponse: r.QueryIndexResponse,
	}
}

type grpcIter struct {
	i int
	*logproto.QueryIndexResponse
}

func (b *grpcIter) Next() bool {
	b.i++
	return b.i < len(b.Rows)
}

func (b *grpcIter) RangeValue() []byte {
	return b.Rows[b.i].RangeValue
}

func (b *grpcIter) Value() []byte {
	return b.Rows[b.i].Value
}
