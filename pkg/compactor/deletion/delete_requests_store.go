package deletion

import (
	"context"
	"time"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	
	"github.com/prometheus/common/model"
)

type DeleteRequestsStoreDBType string

type DeleteRequestsStore interface {
	AddDeleteRequest(ctx context.Context, userID, query string, startTime, endTime model.Time, shardByInterval time.Duration) (string, error)
	addDeleteRequestWithID(ctx context.Context, requestID, userID, query string, startTime, endTime model.Time, shardByInterval time.Duration) error
	GetAllRequests(ctx context.Context) ([]DeleteRequest, error)
	GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error)
	RemoveDeleteRequest(ctx context.Context, userID string, requestID string) error
	GetDeleteRequest(ctx context.Context, userID, requestID string) (DeleteRequest, error)
	GetCacheGenerationNumber(ctx context.Context, userID string) (string, error)
	MergeShardedRequests(ctx context.Context) error

	// ToDo(Sandeep): To keep changeset smaller, below 2 methods treat a single shard as individual request. This can be refactored later in a separate PR.
	MarkShardAsProcessed(ctx context.Context, req DeleteRequest) error
	GetUnprocessedShards(ctx context.Context) ([]DeleteRequest, error)

	Stop()
}

func NewDeleteRequestsStore(workingDirectory string, indexStorageClient storage.Client) (DeleteRequestsStore, error) {
	return newDeleteRequestsStoreBoltDB(workingDirectory, indexStorageClient, model.Now)
}
