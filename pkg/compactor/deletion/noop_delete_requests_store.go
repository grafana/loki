package deletion

import (
	"context"
)

func NewNoOpDeleteRequestsStore() DeleteRequestsStore {
	return &noOpDeleteRequestsStore{}
}

type noOpDeleteRequestsStore struct{}

func (d *noOpDeleteRequestsStore) AddDeleteRequestGroup(_ context.Context, _ []DeleteRequest) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) GetDeleteRequestsByStatus(_ context.Context, _ DeleteRequestStatus) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) GetAllDeleteRequestsForUser(_ context.Context, _ string) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) UpdateStatus(_ context.Context, _ DeleteRequest, _ DeleteRequestStatus) error {
	return nil
}

func (d *noOpDeleteRequestsStore) GetDeleteRequestGroup(_ context.Context, _, _ string) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) RemoveDeleteRequests(_ context.Context, _ []DeleteRequest) error {
	return nil
}

func (d *noOpDeleteRequestsStore) GetCacheGenerationNumber(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (d *noOpDeleteRequestsStore) Stop() {}

func (d *noOpDeleteRequestsStore) Name() string {
	return ""
}
