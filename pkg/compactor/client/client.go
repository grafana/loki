package client

import (
	"context"

	"github.com/grafana/loki/v4/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v4/pkg/compactor/deletion"
	"github.com/grafana/loki/v4/pkg/compactor/deletion/deletionproto"
)

type CompactorClient interface {
	GetAllDeleteRequestsForUser(ctx context.Context, userID string, forQuerytimeFiltering bool, timeRange *deletion.TimeRange) ([]deletionproto.DeleteRequest, error)
	GetCacheGenerationNumber(ctx context.Context, userID string) (string, error)

	JobQueueClient() grpc.JobQueueClient

	Name() string
	Stop()
}
