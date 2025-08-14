package goldfish

import (
	"context"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/v3/pkg/goldfish"
)

// NoOpStorage is a storage implementation that discards all data.
// Useful for testing or when you want sampling without persistence.
type NoOpStorage struct {
	logger log.Logger
}

// NewNoOpStorage creates a new no-op storage backend
func NewNoOpStorage(logger log.Logger) *NoOpStorage {
	return &NoOpStorage{
		logger: logger,
	}
}

// StoreQuerySample discards the query sample
func (s *NoOpStorage) StoreQuerySample(_ context.Context, sample *goldfish.QuerySample) error {
	level.Debug(s.logger).Log("msg", "discarding query sample", "correlation_id", sample.CorrelationID)
	return nil
}

// StoreComparisonResult discards the comparison result
func (s *NoOpStorage) StoreComparisonResult(_ context.Context, result *goldfish.ComparisonResult) error {
	level.Debug(s.logger).Log("msg", "discarding comparison result", "correlation_id", result.CorrelationID)
	return nil
}

// GetSampledQueries returns empty results
func (s *NoOpStorage) GetSampledQueries(_ context.Context, page, pageSize int, _ goldfish.QueryFilter) (*goldfish.APIResponse, error) {
	return &goldfish.APIResponse{
		Queries:  []goldfish.QuerySample{},
		Total:    0,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// Close is a no-op
func (s *NoOpStorage) Close() error {
	return nil
}
