package deletion

import (
	"context"
	"time"

	"github.com/prometheus/common/model"
)

func NewNoOpDeleteRequestsStore() DeleteRequestsStore {
	return &noOpDeleteRequestsStore{}
}

type noOpDeleteRequestsStore struct{}

func (d *noOpDeleteRequestsStore) GetDeleteRequest(_ context.Context, _, _ string) (DeleteRequest, error) {
	return DeleteRequest{}, nil
}

func (d *noOpDeleteRequestsStore) GetAllRequests(_ context.Context) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) GetAllShards(_ context.Context) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) MergeShardedRequests(_ context.Context) error {
	return nil
}

func (d *noOpDeleteRequestsStore) AddDeleteRequest(_ context.Context, _, _ string, _, _ model.Time, _ time.Duration) (string, error) {
	return "", nil
}

func (d *noOpDeleteRequestsStore) GetUnprocessedShards(_ context.Context) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) GetAllDeleteRequestsForUser(_ context.Context, _ string) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) MarkShardAsProcessed(_ context.Context, _ DeleteRequest) error {
	return nil
}

func (d *noOpDeleteRequestsStore) GetDeleteRequestGroup(_ context.Context, _, _ string) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) RemoveDeleteRequest(_ context.Context, _ string, _ string) error {
	return nil
}

func (d *noOpDeleteRequestsStore) GetCacheGenerationNumber(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (d *noOpDeleteRequestsStore) Stop() {}

func (d *noOpDeleteRequestsStore) Name() string {
	return ""
}
