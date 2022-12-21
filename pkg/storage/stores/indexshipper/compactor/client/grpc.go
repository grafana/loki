package client

import (
	"context"
	"flag"

	"github.com/grafana/dskit/grpcclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/instrument"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"

	deletion_grpc "github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/client/grpc"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/deletion"
)

type GRPCConfig struct {
	GRPCClientConfig grpcclient.Config `yaml:",inline"`
}

// RegisterFlags registers flags.
func (cfg *GRPCConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("", f)
}

type compactorGRPCClient struct {
	cfg GRPCConfig

	GRPCClientRequestDuration *prometheus.HistogramVec
	conn                      *grpc.ClientConn
	grpcClient                deletion_grpc.CompactorClient
}

// NewGRPCClient supports only methods which are used for internal communication of Loki like
// loading delete requests and cache gen numbers for query time filtering.
func NewGRPCClient(addr string, cfg GRPCConfig, r prometheus.Registerer) (deletion.CompactorClient, error) {
	client := &compactorGRPCClient{
		cfg: cfg,
		GRPCClientRequestDuration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_compactor",
			Name:      "grpc_request_duration_seconds",
			Help:      "Time (in seconds) spent serving requests when using compactor GRPC client",
			Buckets:   instrument.DefBuckets,
		}, []string{"operation", "status_code"}),
	}

	dialOpts, err := cfg.GRPCClientConfig.DialOption(grpcclient.Instrument(client.GRPCClientRequestDuration))
	if err != nil {
		return nil, err
	}

	client.conn, err = grpc.Dial(addr, dialOpts...)
	if err != nil {
		return nil, err
	}

	client.grpcClient = deletion_grpc.NewCompactorClient(client.conn)
	return client, nil
}

func (s *compactorGRPCClient) Stop() {
	s.conn.Close()
}

func (s *compactorGRPCClient) GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]deletion.DeleteRequest, error) {
	ctx = user.InjectOrgID(ctx, userID)
	grpcResp, err := s.grpcClient.GetDeleteRequests(ctx, &deletion_grpc.GetDeleteRequestsRequest{})
	if err != nil {
		return nil, err
	}

	deleteRequests := make([]deletion.DeleteRequest, len(grpcResp.DeleteRequests))
	for i, dr := range grpcResp.DeleteRequests {
		deleteRequests[i] = deletion.DeleteRequest{
			RequestID: dr.RequestID,
			StartTime: model.Time(dr.StartTime),
			EndTime:   model.Time(dr.EndTime),
			Query:     dr.Query,
			Status:    deletion.DeleteRequestStatus(dr.Status),
			CreatedAt: model.Time(dr.CreatedAt),
		}
	}

	return deleteRequests, nil
}

func (s *compactorGRPCClient) GetCacheGenerationNumber(ctx context.Context, userID string) (string, error) {
	ctx = user.InjectOrgID(ctx, userID)
	grpcResp, err := s.grpcClient.GetCacheGenNumbers(ctx, &deletion_grpc.GetCacheGenNumbersRequest{})
	if err != nil {
		return "", err
	}

	return grpcResp.ResultsCacheGen, nil
}

func (s *compactorGRPCClient) Name() string {
	return "grpc_client"
}
