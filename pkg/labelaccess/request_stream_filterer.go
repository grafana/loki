package labelaccess

import (
	"context"

	"github.com/grafana/loki/v3/pkg/engine"
)

// StreamFilterer is an alias for ChunkFilterer.
// Both implement the same ShouldFilter(labels.Labels) bool method for LBAC filtering.
// ChunkFilterer is used for the V1 engine (chunk-level filtering),
// StreamFilterer is used for the V2 engine (stream-level filtering on the data object).
type StreamFilterer = ChunkFilterer

var _ engine.StreamFilterer = (*StreamFilterer)(nil)

// RequestStreamFilterer implements engine.RequestStreamFilterer for LBAC enforcement
// in the V2 query engine. It creates a StreamFilterer for each request based on the
// LBAC policies in the request context.
type RequestStreamFilterer struct{}

var _ engine.RequestStreamFilterer = (*RequestStreamFilterer)(nil)

// ForRequest extracts LBAC policies from the context and returns a StreamFilterer
// that filters streams based on those policies.
// It reuses the same logic as RequestChunkFilterer.ForRequest via filtererForRequest.
func (r *RequestStreamFilterer) ForRequest(ctx context.Context) engine.StreamFilterer {
	f := filtererForRequest(ctx)
	if f == nil {
		// Return nil engine.StreamFilterer instead of (*ChunkFilterer)(nil)
		return nil
	}
	return f
}
