package goldfish

import (
	"context"
	"errors"
)

// NoopStorage is a no-op implementation of the Storage interface
type NoopStorage struct{}

// NewNoopStorage creates a new no-op storage backend
func NewNoopStorage() *NoopStorage {
	return &NoopStorage{}
}

// StoreQuerySample is a no-op
func (n *NoopStorage) StoreQuerySample(_ context.Context, _ *QuerySample) error {
	return nil
}

// StoreComparisonResult is a no-op
func (n *NoopStorage) StoreComparisonResult(_ context.Context, _ *ComparisonResult) error {
	return nil
}

// GetSampledQueries returns an error as goldfish is disabled
func (n *NoopStorage) GetSampledQueries(_ context.Context, _, _ int, _ string) (*APIResponse, error) {
	return nil, errors.New("goldfish feature is disabled")
}

// Close is a no-op
func (n *NoopStorage) Close() error {
	return nil
}
