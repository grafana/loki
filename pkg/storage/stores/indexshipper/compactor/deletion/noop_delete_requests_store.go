package deletion

import (
	"context"
)

func NewNoOpDeleteRequestsStore() DeleteRequestsStore {
	return &noOpDeleteRequestsStore{}
}

type noOpDeleteRequestsStore struct{}

func (d *noOpDeleteRequestsStore) AddDeleteRequestGroup(ctx context.Context, req []DeleteRequest) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) GetDeleteRequestsByStatus(ctx context.Context, status DeleteRequestStatus) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) UpdateStatus(ctx context.Context, req DeleteRequest, newStatus DeleteRequestStatus) error {
	return nil
}

func (d *noOpDeleteRequestsStore) GetDeleteRequestGroup(ctx context.Context, userID, requestID string) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) RemoveDeleteRequests(ctx context.Context, reqs []DeleteRequest) error {
	return nil
}

func (d *noOpDeleteRequestsStore) GetCacheGenerationNumber(ctx context.Context, userID string) (string, error) {
	return "", nil
}

func (d *noOpDeleteRequestsStore) Stop() {}

func (d *noOpDeleteRequestsStore) Name() string {
	return ""
}
