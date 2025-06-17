package client

import (
	"context"
	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/compactor/deletion"
)

type CompactorClient interface {
	GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]deletion.DeleteRequest, error)
	GetCacheGenerationNumber(ctx context.Context, userID string) (string, error)

	JobQueueClient() grpc.JobQueueClient

	Name() string
	Stop()
}
