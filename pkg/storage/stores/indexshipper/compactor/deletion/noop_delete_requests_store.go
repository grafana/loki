package deletion

import (
	"context"
)

func NewNoOpDeleteRequestsStore() DeleteRequestsStore {
	return &noOpDeleteRequestsStore{}
}

type noOpDeleteRequestsStore struct{}

func (d *noOpDeleteRequestsStore) AddDeleteRequest(ctx context.Context, req DeleteRequest) (string, error) {
	return "", nil
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

func (d *noOpDeleteRequestsStore) GetDeleteRequest(ctx context.Context, userID, requestID string) (*DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) RemoveDeleteRequest(ctx context.Context, req DeleteRequest) error {
	return nil
}

func (d *noOpDeleteRequestsStore) GetCacheGenerationNumber(ctx context.Context, userID string) (string, error) {
	return "", nil
}

func (d *noOpDeleteRequestsStore) Stop() {}

func (d *noOpDeleteRequestsStore) Name() string {
	return ""
}
