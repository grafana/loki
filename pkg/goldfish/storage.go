package goldfish

import (
	"context"
)

// Storage defines the interface for storing and retrieving query samples and comparison results
type Storage interface {
	// Write operations (used by querytee)
	StoreQuerySample(ctx context.Context, sample *QuerySample) error
	StoreComparisonResult(ctx context.Context, result *ComparisonResult) error

	// Read operations (used by UI)
	GetSampledQueries(ctx context.Context, page, pageSize int, outcome string) (*APIResponse, error)

	// Lifecycle
	Close() error
}

// APIResponse represents the paginated API response for UI
type APIResponse struct {
	Queries  []QuerySample `json:"queries"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
}
