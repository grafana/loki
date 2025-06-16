package client

import (
	"context"
	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/compactor/deletion"
)

type CompactorClient interface {
	GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]deletion.DeleteRequest, error)
	GetCacheGenerationNumber(ctx context.Context, userID string) (string, error)

	DequeueJob(ctx context.Context) (*grpc.Job, error)
	ReportJobResult(ctx context.Context, req *grpc.ReportJobResultRequest) error

	Name() string
	Stop()
}
