package goldfish

import (
	"context"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
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
func (s *NoOpStorage) StoreQuerySample(ctx context.Context, sample *QuerySample) error {
	level.Debug(s.logger).Log("msg", "discarding query sample", "correlation_id", sample.CorrelationID)
	return nil
}

// StoreComparisonResult discards the comparison result
func (s *NoOpStorage) StoreComparisonResult(ctx context.Context, result *ComparisonResult) error {
	level.Debug(s.logger).Log("msg", "discarding comparison result", "correlation_id", result.CorrelationID)
	return nil
}

// Close is a no-op
func (s *NoOpStorage) Close() error {
	return nil
}
