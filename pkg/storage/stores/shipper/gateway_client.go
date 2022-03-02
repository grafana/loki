package shipper

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"

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
	loki_util "github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	util_math "github.com/grafana/loki/pkg/util/math"
)

const (
	maxQueriesPerGrpc      = 100
	maxConcurrentGrpcCalls = 10
)

type IndexGatewayClientMode string

func (i *IndexGatewayClientMode) Set(v string) error {
	newMode := IndexGatewayClientMode(v)

	switch v {
	case "simple":
		i = &newMode
	case "ring":
		i = &newMode
	default:
		return fmt.Errorf("list of available modes: simple,ring. mode %s not supported.", v)
	}
	return nil
}

func (i *IndexGatewayClientMode) String() string {
	return string(*i)
}

const (
	SimpleMode IndexGatewayClientMode = "simple"
	RingMode   IndexGatewayClientMode = "ring"
)

type SimpleModeConfig struct {
	Address          string            `yaml:"server_address,omitempty"`
	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config"`
}

func (cfg *SimpleModeConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("", f)
}

type RingModeConfig struct {
	IndexGatewayRing loki_util.RingConfig  `yaml:"index_gateway_ring,omitempty"` // TODO: maybe just `yaml:"ring"`?
	PoolConfig       clientpool.PoolConfig `yaml:"pool_config"`
}

func (cfg *RingModeConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.IndexGatewayRing.RegisterFlagsWithPrefix("index-gateway.", "collectors/", f)
}

type IndexGatewayClientConfig struct {
	Mode             IndexGatewayClientMode `yaml:"mode"` // TODO: ring, simple
	RingModeConfig   `yaml:",inplace"`
	SimpleModeConfig `yaml:",inplace"`
}

// RegisterFlags registers flags.
func (cfg *IndexGatewayClientConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RingModeConfig.RegisterFlags(f)
	cfg.SimpleModeConfig.RegisterFlags(f)
}

// RegisterFlagsWithPrefix registers flags with prefix.
func (cfg *IndexGatewayClientConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix(prefix, f)

	f.StringVar(&cfg.Address, prefix+".server-address", "", "Hostname or IP of the Index Gateway gRPC server.")

	cfg.Mode = SimpleMode // default mode.
	f.Var(&cfg.Mode, prefix+".mode", "Mode to run the index gateway client (simple,ring)")
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
		return nil, fmt.Errorf("index gateway grpc dial option: %w", err)
	}

	if sgClient.mode == RingMode {
		factory := func(addr string) (ring_client.PoolClient, error) {

			igPool, err := NewIndexGatewayGRPCPool(addr, dialOpts)
			if err != nil {
				return nil, fmt.Errorf("new index gateway grpc pool: %w", err)
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
		return s.clientDoQueries(ctx, gatewayQueries, queryKeyQueryMap, callback)
	} else {
		return s.ringModeDoQueries(ctx, gatewayQueries, queryKeyQueryMap, callback)
	}
}

func (s *GatewayClient) clientDoQueries(ctx context.Context, gatewayQueries []*indexgatewaypb.IndexQuery, queryKeyQueryMap map[string]chunk.IndexQuery, callback chunk.QueryPagesCallback) error {
	streamer, err := s.grpcClient.QueryIndex(ctx, &indexgatewaypb.QueryIndexRequest{Queries: gatewayQueries})
	if err != nil {
		return fmt.Errorf("simple mode query index: %w", err)
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

func (s *GatewayClient) ringModeDoQueries(ctx context.Context, gatewayQueries []*indexgatewaypb.IndexQuery, queryKeyQueryMap map[string]chunk.IndexQuery, callback chunk.QueryPagesCallback) error {
	// TODO: should we keep iterating if an error occur?
	for _, q := range gatewayQueries {
		bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()

		tenantIDStr := q.HashValue // TODO: swap q.HashValue with the real tenantID
		tenantID, err := strconv.ParseUint(tenantIDStr, 10, 32)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to parse key hash value", "error", err, "key", tenantIDStr))
		}

		rs, err := s.ring.Get(uint32(tenantID), ring.WriteNoExtend, bufDescs, bufHosts, bufZones)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to get index key", "error", err))
			continue
		}

		_, err = rs.Do(ctx, 0 /* delay, TODO: what is it for? */, func(ctx context.Context, instanceDesc *ring.InstanceDesc) (interface{}, error) {
			genericClient, err := s.pool.GetClientFor(instanceDesc.Addr)
			if err != nil {
				return nil, fmt.Errorf("get client for address %s: %w", instanceDesc.Addr, err)
			}

			client := (genericClient.(indexgatewaypb.IndexGatewayClient))
			client.QueryIndex(ctx, &indexgatewaypb.QueryIndexRequest{Queries: gatewayQueries})

			if err := s.clientDoQueries(ctx, gatewayQueries, queryKeyQueryMap, callback); err != nil {
				return nil, err
			}

			return nil, nil
		})

		if err != nil {
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("index gateway ring client query failed", "error", err))
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
