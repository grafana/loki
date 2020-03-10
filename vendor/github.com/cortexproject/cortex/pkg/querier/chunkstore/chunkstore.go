package chunkstore

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/cortexproject/cortex/pkg/chunk"
)

// ChunkStore is the read-interface to the Chunk Store.  Made an interface here
// to reduce package coupling.
type ChunkStore interface {
	Get(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]chunk.Chunk, error)
}
