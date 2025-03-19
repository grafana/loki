package client

import (
	"context"
	"flag"

	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"google.golang.org/grpc"

	deletion_grpc "github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/compactor/deletion"
)

type GRPCConfig struct {
	GRPCClientConfig grpcclient.Config `yaml:",inline"`
}

// RegisterFlags registers flags.
func (cfg *GRPCConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("compactor.grpc-client", f)
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

	unaryInterceptors, streamInterceptors := grpcclient.Instrument(client.GRPCClientRequestDuration)
	dialOpts, err := cfg.GRPCClientConfig.DialOption(unaryInterceptors, streamInterceptors, middleware.NoOpInvalidClusterValidationReporter)
	if err != nil {
		return nil, err
	}

	// nolint:staticcheck // grpc.Dial() has been deprecated; we'll address it before upgrading to gRPC 2.
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
	grpcResp, err := s.grpcClient.GetDeleteRequests(ctx, &deletion_grpc.GetDeleteRequestsRequest{ForQuerytimeFiltering: true})
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
