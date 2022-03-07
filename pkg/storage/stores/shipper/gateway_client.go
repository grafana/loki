package shipper

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/instrument"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/distributor/clientpool"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	"github.com/grafana/loki/pkg/util"
	loki_util "github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	util_math "github.com/grafana/loki/pkg/util/math"
)

const (
	maxQueriesPerGrpc      = 100
	maxConcurrentGrpcCalls = 10
)

// IndexGatewayClientMode represents in which mode an Index Gateway instance is running.
//
// Right now, two modes are supported: simple mode (default) and ring mode.
type IndexGatewayClientMode string

// Set implements a flag interface, and is necessary to use the IndexGatewayClientMode as a flag.
func (i IndexGatewayClientMode) Set(v string) error {
	switch v {
	case string(SimpleMode):
		i = SimpleMode
	case string(RingMode):
		i = RingMode
	default:
		return fmt.Errorf("mode %s not supported. list of supported modes: simple (default), ring", v)
	}
	return nil
}

// Stringg implements a flag interface, and is necessary to use the IndexGatewayClientMode as a flag.
func (i IndexGatewayClientMode) String() string {
	switch i {
	case RingMode:
		return string(RingMode)
	default:
		return string(SimpleMode)
	}
}

const (
	// SimpleMode is a mode where an Index Gateway instance solely handle all the work.
	SimpleMode IndexGatewayClientMode = "simple"

	// RingMode is a mode where different Index Gateway instances are assigned to handle different tenants.
	//
	// It is more horizontally scalable than the simple mode, but requires running a key-value store ring.
	RingMode IndexGatewayClientMode = "ring"
)

// SimpleModeConfig configures an Index Gateway in the simple mode, where a single instance is assigned all tenants.
//
// If the Index Gateway is running in ring mode this configuration shall be ignored.
type SimpleModeConfig struct {
	// Address of the Index Gateway instance responsible for retaining the index for all tenants.
	Address string `yaml:"server_address,omitempty"`
}

// RegisterFlagsWithPrefix register all SimpleModeConfig flags and all the flags of its subconfigs but with a prefix.
func (cfg *SimpleModeConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Address, prefix+".server-address", "", "Hostname or IP of the Index Gateway gRPC server running in simple mode.")
}

// RingModeConfig configures an Index Gateway in the ring mode, where an instance is responsible for a subset of tenants.
//
// If the Index Gateway is running in simple mode this configuration shall be ignored.
type RingModeConfig struct {
	// Ring configures the ring key-value store used to save and retrieve the different Index Gateway instances.
	//
	// In case it isn't explicitly set, it follows the same behavior of the other rings (ex: using the common configuration
	// section and the ingester configuration by default).
	Ring loki_util.RingConfig `yaml:"index_gateway_ring,omitempty"` // TODO: maybe just `yaml:"ring"`?

	// PoolConfig configures a pool of GRPC connections to the different Index Gateway instances.
	PoolConfig clientpool.PoolConfig `yaml:"pool_config"`

	// GRPCClientConfig configures GRPC parameters used in the connection between a component and the Index Gateway.
	//
	// It is used by both, simple and ring mode.
	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config"`
}

// RegisterFlagsWithPrefix register all RingModeConfig flags and all the flags of its subconfigs but with a prefix.
func (cfg *RingModeConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.Ring.RegisterFlagsWithPrefix(prefix+".", "collectors/", f)
}

// IndexGatewayClientConfig configures an Index Gateway client used by the different components.
//
// If the mode is set to simple (default), only the SimpleModeConfig is relevant. Otherwise, for the ring mode,
// only the RingModeConfig is relevant.
type IndexGatewayClientConfig struct {
	// Mode configures in which mode the client will be running when querying and communicating with an Index Gateway instance.
	Mode IndexGatewayClientMode `yaml:"mode"` // TODO: ring, simple

	// RingMode configures the client to communicate with Index Gateway instances in the ring mode.
	RingModeConfig `yaml:",inline"`

	// SimpleModeConfig configures the client to communicate with Index Gateway instances in the simple mode.
	SimpleModeConfig `yaml:",inline"`
}

// RegisterFlags register all IndexGatewayClientConfig flags and all the flags of its subconfigs but with a prefix (ex: shipper).
func (cfg *IndexGatewayClientConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.Mode = SimpleMode // default mode.
	cfg.RingModeConfig.RegisterFlagsWithPrefix(prefix, f)
	cfg.SimpleModeConfig.RegisterFlagsWithPrefix(prefix, f)
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix(prefix+".grpc", f)

	f.Var(cfg.Mode, prefix+".mode", "Mode to run the index gateway client. Supported modes are simple (default) and ring.")
}

type GatewayClient struct {
	cfg IndexGatewayClientConfig

	storeGatewayClientRequestDuration *prometheus.HistogramVec

	conn       *grpc.ClientConn
	grpcClient indexgatewaypb.IndexGatewayClient

	mode IndexGatewayClientMode

	pool *ring_client.Pool

	ring ring.ReadRing
}

// NewGatewayClient instantiates a new client used to communicate with an Index Gateway instance.
//
// If it is configured to be in ring mode, a pool of GRPC connections to all Index Gateway instances is created.
// Otherwise, it creates a single GRPC connection to an Index Gateway instance running in simple mode.
func NewGatewayClient(cfg IndexGatewayClientConfig, indexGatewayRing ring.ReadRing, r prometheus.Registerer, logger log.Logger) (*GatewayClient, error) {
	sgClient := &GatewayClient{
		cfg: cfg,
		storeGatewayClientRequestDuration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "store_gateway_request_duration_seconds",
			Help:      "Time (in seconds) spent serving requests when using boltdb shipper store gateway",
			Buckets:   instrument.DefBuckets,
		}, []string{"operation", "status_code"}),
		mode: cfg.Mode,
		ring: indexGatewayRing,
	}

	dialOpts, err := cfg.GRPCClientConfig.DialOption(grpcclient.Instrument(sgClient.storeGatewayClientRequestDuration))
	if err != nil {
		return nil, errors.Wrap(err, "index gateway grpc dial option")
	}

	if sgClient.mode == RingMode {
		factory := func(addr string) (ring_client.PoolClient, error) {
			igPool, err := NewIndexGatewayGRPCPool(addr, dialOpts)
			if err != nil {
				return nil, errors.Wrap(err, "new index gateway grpc pool")
			}

			return igPool, nil
		}

		sgClient.pool = clientpool.NewPool(cfg.PoolConfig, indexGatewayRing, factory, logger)
	} else {
		sgClient.conn, err = grpc.Dial(cfg.Address, dialOpts...)

		sgClient.grpcClient = indexgatewaypb.NewIndexGatewayClient(sgClient.conn)
	}

	return sgClient, nil
}

// Stop stops the execution of this gateway client.
//
// If it is in simple mode, the sinlge GRPC connection is closed. Otherwise, nothing happens.
func (s *GatewayClient) Stop() {
	if s.mode == SimpleMode {
		s.conn.Close()
	}
}

func (s *GatewayClient) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback chunk.QueryPagesCallback) error {
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

// doQueries queries index pages from the Index Gateway.
func (s *GatewayClient) doQueries(ctx context.Context, queries []chunk.IndexQuery, callback chunk.QueryPagesCallback) error {
	queryKeyQueryMap := make(map[string]chunk.IndexQuery, len(queries))
	gatewayQueries := make([]*indexgatewaypb.IndexQuery, 0, len(queries))

	for _, query := range queries {
		queryKeyQueryMap[shipper_util.QueryKey(query)] = query
		gatewayQueries = append(gatewayQueries, &indexgatewaypb.IndexQuery{
			TableName:        query.TableName,
			HashValue:        query.HashValue,
			RangeValuePrefix: query.RangeValuePrefix,
			RangeValueStart:  query.RangeValueStart,
			ValueEqual:       query.ValueEqual,
		})
	}

	if s.mode == SimpleMode {
		return s.clientDoQueries(ctx, gatewayQueries, queryKeyQueryMap, callback, s.grpcClient)
	} else {
		return s.ringModeDoQueries(ctx, gatewayQueries, queryKeyQueryMap, callback)
	}
}

// clientDoQueries send a query request to an Index Gateway instance using the given gRPC client.
//
// It is used by both, simple and ring mode.
func (s *GatewayClient) clientDoQueries(ctx context.Context, gatewayQueries []*indexgatewaypb.IndexQuery,
	queryKeyQueryMap map[string]chunk.IndexQuery, callback chunk.QueryPagesCallback, client indexgatewaypb.IndexGatewayClient) error {
	streamer, err := client.QueryIndex(ctx, &indexgatewaypb.QueryIndexRequest{Queries: gatewayQueries})
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
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("unexpected %s QueryKey received, expected queries %s", resp.QueryKey, fmt.Sprint(queryKeyQueryMap)))
			return fmt.Errorf("unexpected %s QueryKey received", resp.QueryKey)
		}
		if !callback(query, &readBatch{resp}) {
			return nil
		}
	}

	return nil
}

// ringModeDoQueries prepares an index query to be sent to the Index Gateway, and then sends it
// using the clientDoQueries implementation.
//
// The preparation and sending phase includes:
// 1. Extracting the tenant name from the query.
// 2. Fetching different Index Gateway instances assigned to the extracted tenant.
// 3. Iterating in parallel over all fetched Index Gateway instances, getting their gRPC connections
//  from the pool and invoking clientDoQueries using their client.
func (s *GatewayClient) ringModeDoQueries(ctx context.Context, gatewayQueries []*indexgatewaypb.IndexQuery, queryKeyQueryMap map[string]chunk.IndexQuery, callback chunk.QueryPagesCallback) error {
	// TODO: should we keep iterating if an error occur?
	for _, q := range gatewayQueries {
		bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()

		splittedHashed := strings.Split(q.HashValue, ":")
		if len(splittedHashed) < 2 {
			level.Error(util_log.Logger).Log("msg", "query hash value in incorrect format", "hash_value", q.HashValue)
			continue
		}

		// tenant is the second parameter.
		tenantID := splittedHashed[1]
		key := util.TokenFor(tenantID, "" /* labels */)
		rs, err := s.ring.Get(key, ring.WriteNoExtend, bufDescs, bufHosts, bufZones)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to get index key", "error", err)
			continue
		}

		_, err = rs.Do(ctx, 0 /* delay, TODO: what is it for? */, func(ctx context.Context, instanceDesc *ring.InstanceDesc) (interface{}, error) {
			genericClient, err := s.pool.GetClientFor(instanceDesc.Addr)
			if err != nil {
				return nil, errors.Wrap(err, fmt.Sprintf("get client for instance %s", instanceDesc.Addr))
			}

			client := (genericClient.(indexgatewaypb.IndexGatewayClient))
			// TODO: does this works as a hedging request?
			if err := s.clientDoQueries(ctx, gatewayQueries, queryKeyQueryMap, callback, client); err != nil {
				return nil, err
			}

			return nil, nil
		})

		if err != nil {
			level.Error(util_log.Logger).Log("msg", "index gateway ring client query failed", "error", err)
		}
	}
	return nil
}

func (s *GatewayClient) NewWriteBatch() chunk.WriteBatch {
	panic("unsupported")
}

func (s *GatewayClient) BatchWrite(ctx context.Context, batch chunk.WriteBatch) error {
	panic("unsupported")
}

type readBatch struct {
	*indexgatewaypb.QueryIndexResponse
}

func (r *readBatch) Iterator() chunk.ReadBatchIterator {
	return &grpcIter{
		i:                  -1,
		QueryIndexResponse: r.QueryIndexResponse,
	}
}

type grpcIter struct {
	i int
	*indexgatewaypb.QueryIndexResponse
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
