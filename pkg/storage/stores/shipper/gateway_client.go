package shipper

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/cortexproject/cortex/pkg/util/grpcclient"
	util_math "github.com/cortexproject/cortex/pkg/util/math"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/instrument"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const maxQueriesPerGoroutine = 100

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
	grpcClient                        indexgatewaypb.IndexGatewayClient
}

func NewGatewayClient(cfg IndexGatewayClientConfig, r prometheus.Registerer) (*GatewayClient, error) {
	sgClient := &GatewayClient{
		cfg: cfg,
		storeGatewayClientRequestDuration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "store_gateway_request_duration_seconds",
			Help:      "Time (in seconds) spent serving requests when using boltdb shipper store gateway",
			Buckets:   instrument.DefBuckets,
		}, []string{"operation", "status_code"}),
	}

	dialOpts, err := cfg.GRPCClientConfig.DialOption(grpcclient.Instrument(sgClient.storeGatewayClientRequestDuration))
	if err != nil {
		return nil, err
	}

	sgClient.conn, err = grpc.Dial(cfg.Address, dialOpts...)
	if err != nil {
		return nil, err
	}

	sgClient.grpcClient = indexgatewaypb.NewIndexGatewayClient(sgClient.conn)
	return sgClient, nil
}

func (s *GatewayClient) Stop() {
	s.conn.Close()
}

func (s *GatewayClient) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	errs := make(chan error)

	for i := 0; i < len(queries); i += maxQueriesPerGoroutine {
		q := queries[i:util_math.Min(i+maxQueriesPerGoroutine, len(queries))]
		go func(queries []chunk.IndexQuery) {
			errs <- s.doQueries(ctx, queries, callback)
		}(q)
	}

	var lastErr error
	for i := 0; i < len(queries); i += maxQueriesPerGoroutine {
		err := <-errs
		if err != nil {
			lastErr = err
		}
	}

	return lastErr
}

func (s *GatewayClient) doQueries(ctx context.Context, queries []chunk.IndexQuery, callback util.Callback) error {
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

	streamer, err := s.grpcClient.QueryIndex(ctx, &indexgatewaypb.QueryIndexRequest{Queries: gatewayQueries})
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
