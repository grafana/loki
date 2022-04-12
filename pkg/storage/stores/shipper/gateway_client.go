package shipper

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/go-kit/log/level"
	"github.com/gogo/status"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/grpcclient"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/instrument"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	util_math "github.com/grafana/loki/pkg/util/math"
)

const (
	maxQueriesPerGrpc      = 100
	maxConcurrentGrpcCalls = 10
)

type IndexGatewayClientConfig struct {
	Address          string            `yaml:"server_address,omitempty"`
	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config"`
}

// RegisterFlags registers flags.
func (cfg *IndexGatewayClientConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers flags with prefix.
func (cfg *IndexGatewayClientConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix(prefix, f)

	f.StringVar(&cfg.Address, prefix+".server-address", "", "Hostname or IP of the Index Gateway gRPC server.")
}

type GatewayClient struct {
	cfg IndexGatewayClientConfig

	storeGatewayClientRequestDuration *prometheus.HistogramVec
	conn                              *grpc.ClientConn
	indexgatewaypb.IndexGatewayClient
}

func NewGatewayClient(cfg IndexGatewayClientConfig, r prometheus.Registerer) (*GatewayClient, error) {
	latency := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki_boltdb_shipper",
		Name:      "store_gateway_request_duration_seconds",
		Help:      "Time (in seconds) spent serving requests when using boltdb shipper store gateway",
		Buckets:   instrument.DefBuckets,
	}, []string{"operation", "status_code"})
	err := r.Register(latency)
	if err != nil {
		alreadtErr, ok := err.(prometheus.AlreadyRegisteredError)
		if !ok {
			return nil, err
		}
		latency = alreadtErr.ExistingCollector.(*prometheus.HistogramVec)
	}
	sgClient := &GatewayClient{
		cfg:                               cfg,
		storeGatewayClientRequestDuration: latency,
	}

	dialOpts, err := cfg.GRPCClientConfig.DialOption(grpcclient.Instrument(sgClient.storeGatewayClientRequestDuration))
	if err != nil {
		return nil, err
	}

	sgClient.conn, err = grpc.Dial(cfg.Address, dialOpts...)
	if err != nil {
		return nil, err
	}

	sgClient.IndexGatewayClient = indexgatewaypb.NewIndexGatewayClient(sgClient.conn)
	return sgClient, nil
}

func HasGetRefsAPI(cfg IndexGatewayClientConfig) (bool, error) {
	dialOpts, err := cfg.GRPCClientConfig.DialOption(nil, nil)
	if err != nil {
		return false, err
	}
	conn, err := grpc.Dial(cfg.Address, dialOpts...)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	_, err = indexgatewaypb.NewIndexGatewayClient(conn).GetChunkRef(ctx, &indexgatewaypb.GetChunkRefRequest{})
	s, ok := status.FromError(err)
	if !ok {
		return false, err
	}
	return (s.Code() != codes.Unimplemented) && (s.Code() != codes.FailedPrecondition) && (s.Code() != codes.Unavailable), nil
}

func (s *GatewayClient) Stop() {
	s.conn.Close()
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

func (s *GatewayClient) doQueries(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
	queryKeyQueryMap := make(map[string]index.Query, len(queries))
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

	streamer, err := s.IndexGatewayClient.QueryIndex(ctx, &indexgatewaypb.QueryIndexRequest{Queries: gatewayQueries})
	if err != nil {
		return err
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

func (s *GatewayClient) NewWriteBatch() index.WriteBatch {
	panic("unsupported")
}

func (s *GatewayClient) BatchWrite(ctx context.Context, batch index.WriteBatch) error {
	panic("unsupported")
}

type readBatch struct {
	*indexgatewaypb.QueryIndexResponse
}

func (r *readBatch) Iterator() index.ReadBatchIterator {
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
