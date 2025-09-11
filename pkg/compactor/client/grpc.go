package client

import (
	"context"
	"flag"
	"strings"

	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"

	compactor_grpc "github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
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

	grpcClientRequestDuration *prometheus.HistogramVec
	conn                      *grpc.ClientConn
	grpcClient                compactor_grpc.CompactorClient
	jobQueueClient            compactor_grpc.JobQueueClient
}

// NewGRPCClient supports only methods which are used for internal communication of Loki like
// loading delete requests, cache gen numbers for query time filtering and interacting with job queue for horizontal scaling of compactor.
func NewGRPCClient(addr string, cfg GRPCConfig, r prometheus.Registerer) (CompactorClient, error) {
	client := &compactorGRPCClient{
		cfg: cfg,
		grpcClientRequestDuration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_compactor",
			Name:      "grpc_request_duration_seconds",
			Help:      "Time (in seconds) spent serving requests when using compactor GRPC client",
			Buckets:   instrument.DefBuckets,
		}, []string{"operation", "status_code"}),
	}

	dialOpts, err := cfg.GRPCClientConfig.DialOption(
		[]grpc.UnaryClientInterceptor{
			func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
				// JobQueue has no auth methods so do not try to set the auth header
				if !strings.HasPrefix(method, "/grpc.JobQueue/") {
					var err error
					ctx, err = user.InjectIntoGRPCRequest(ctx)
					if err != nil {
						return err
					}
				}
				return invoker(ctx, method, req, reply, cc, opts...)
			},
			middleware.UnaryClientInstrumentInterceptor(client.grpcClientRequestDuration),
		}, []grpc.StreamClientInterceptor{
			func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
				// JobQueue has no auth methods so do not try to set the auth header
				if !strings.HasPrefix(method, "/grpc.JobQueue/") {
					var err error
					ctx, err = user.InjectIntoGRPCRequest(ctx)
					if err != nil {
						return nil, err
					}
				}
				return streamer(ctx, desc, cc, method, opts...)
			},
			middleware.StreamClientInstrumentInterceptor(client.grpcClientRequestDuration),
		},
		middleware.NoOpInvalidClusterValidationReporter,
	)
	if err != nil {
		return nil, err
	}

	// nolint:staticcheck // grpc.Dial() has been deprecated; we'll address it before upgrading to gRPC 2.
	client.conn, err = grpc.Dial(addr, dialOpts...)
	if err != nil {
		return nil, err
	}

	client.grpcClient = compactor_grpc.NewCompactorClient(client.conn)
	client.jobQueueClient = compactor_grpc.NewJobQueueClient(client.conn)
	return client, nil
}

func (s *compactorGRPCClient) Stop() {
	s.conn.Close()
}

func (s *compactorGRPCClient) GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]deletionproto.DeleteRequest, error) {
	ctx = user.InjectOrgID(ctx, userID)
	grpcResp, err := s.grpcClient.GetDeleteRequests(ctx, &compactor_grpc.GetDeleteRequestsRequest{ForQuerytimeFiltering: true})
	if err != nil {
		return nil, err
	}

	deleteRequests := make([]deletionproto.DeleteRequest, len(grpcResp.DeleteRequests))
	for i, dr := range grpcResp.DeleteRequests {
		deleteRequests[i] = deletionproto.DeleteRequest{
			RequestID: dr.RequestID,
			StartTime: dr.StartTime,
			EndTime:   dr.EndTime,
			Query:     dr.Query,
			Status:    dr.Status,
			CreatedAt: dr.CreatedAt,
		}
	}

	return deleteRequests, nil
}

func (s *compactorGRPCClient) GetCacheGenerationNumber(ctx context.Context, userID string) (string, error) {
	ctx = user.InjectOrgID(ctx, userID)
	grpcResp, err := s.grpcClient.GetCacheGenNumbers(ctx, &compactor_grpc.GetCacheGenNumbersRequest{})
	if err != nil {
		return "", err
	}

	return grpcResp.ResultsCacheGen, nil
}

func (s *compactorGRPCClient) JobQueueClient() compactor_grpc.JobQueueClient {
	return compactor_grpc.NewJobQueueClient(s.conn)
}

func (s *compactorGRPCClient) Name() string {
	return "grpc_client"
}
