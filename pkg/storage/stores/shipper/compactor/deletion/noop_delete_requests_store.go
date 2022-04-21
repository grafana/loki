package deletion

import (
	"context"

	"github.com/prometheus/common/model"
)

func NewNoOpDeleteRequestsStore() DeleteRequestsStore {
	return &noOpDeleteRequestsStore{}
}

type noOpDeleteRequestsStore struct{}

func (d *noOpDeleteRequestsStore) AddDeleteRequest(ctx context.Context, userID string, startTime, endTime model.Time, query string) error {
	return nil
}

func (d *noOpDeleteRequestsStore) GetDeleteRequestsByStatus(ctx context.Context, status DeleteRequestStatus) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) UpdateStatus(ctx context.Context, userID, requestID string, newStatus DeleteRequestStatus) error {
	return nil
}

func (d *noOpDeleteRequestsStore) GetDeleteRequest(ctx context.Context, userID, requestID string) (*DeleteRequest, error) {
	return nil, nil
}

func (d *noOpDeleteRequestsStore) RemoveDeleteRequest(ctx context.Context, userID, requestID string, createdAt, startTime, endTime model.Time) error {
	return nil
}

func (d *noOpDeleteRequestsStore) GetCacheGenerationNumber(ctx context.Context, userID string) (string, error) {
	return "", nil
}

func (d *noOpDeleteRequestsStore) Stop() {}

func (d *noOpDeleteRequestsStore) Name() string {
	return ""
}
