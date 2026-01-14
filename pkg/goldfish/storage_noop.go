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
func (n *NoopStorage) StoreQuerySample(_ context.Context, _ *QuerySample, _ *ComparisonResult) error {
	return nil
}

// StoreComparisonResult is a no-op
func (n *NoopStorage) StoreComparisonResult(_ context.Context, _ *ComparisonResult) error {
	return nil
}

// GetSampledQueries returns an error as goldfish is disabled
func (n *NoopStorage) GetSampledQueries(_ context.Context, _, _ int, _ QueryFilter) (*APIResponse, error) {
	return nil, errors.New("goldfish feature is disabled")
}

// GetQueryByCorrelationID returns an error as goldfish is disabled
func (n *NoopStorage) GetQueryByCorrelationID(_ context.Context, _ string) (*QuerySample, error) {
	return nil, errors.New("goldfish feature is disabled")
}

// GetStatistics returns an error as goldfish is disabled
func (n *NoopStorage) GetStatistics(_ context.Context, _ StatsFilter) (*Statistics, error) {
	return nil, errors.New("goldfish feature is disabled")
}

// Close is a no-op
func (n *NoopStorage) Close() error {
	return nil
}
