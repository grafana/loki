package deletion

import (
	"context"
	"github.com/prometheus/common/model"
	"time"
)

func NewNoOpDeleteRequestsStore() DeleteRequestsStore {
	return &noOpDeleteRequestsStore{}
}

type noOpDeleteRequestsStore struct{}

func (d *noOpDeleteRequestsStore) GetDeleteRequest(ctx context.Context, userID, requestID string) (DeleteRequest, error) {
	return DeleteRequest{}, nil
}

func (d *noOpDeleteRequestsStore) GetAllRequests(_ context.Context) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) GetAllShards(ctx context.Context) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) MergeShardedRequests(_ context.Context) error {
	return nil
}

func (d *noOpDeleteRequestsStore) AddDeleteRequest(ctx context.Context, userID, query string, startTime, endTime model.Time, shardByInterval time.Duration) (string, error) {
	return "", nil
}

func (d *noOpDeleteRequestsStore) GetUnprocessedShards(_ context.Context) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) GetAllDeleteRequestsForUser(_ context.Context, _ string) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) MarkShardAsProcessed(ctx context.Context, req DeleteRequest) error {
	return nil
}

func (d *noOpDeleteRequestsStore) GetDeleteRequestGroup(_ context.Context, _, _ string) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) RemoveDeleteRequest(ctx context.Context, userID string, requestID string) error {
	return nil
}

func (d *noOpDeleteRequestsStore) GetCacheGenerationNumber(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (d *noOpDeleteRequestsStore) Stop() {}

func (d *noOpDeleteRequestsStore) Name() string {
	return ""
}
