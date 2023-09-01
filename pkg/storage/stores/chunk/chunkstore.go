package chunk

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
)

// ReadStore is an interface for a chunk store that reads data
type ReadStore interface {
	SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error)
	SelectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error)
	Series(ctx context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error)
}

// WriteStore is an interface for a chunk store that writes data
type WriteStore interface {
	Put(ctx context.Context, chunks []chunk.Chunk) error
	PutOne(ctx context.Context, from, through model.Time, chunk chunk.Chunk) error
}

// ReadWriteStore is an interface for a chunk store that reads and writes data
type ReadWriteStore interface {
	ReadStore
	WriteStore
}

// Fetcher is an interface for a chunk store that needs to fetch chunks
// from a local or remote location
type Fetcher interface {
	GetChunkFetcher(tm model.Time) *fetcher.Fetcher
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*fetcher.Fetcher, error)
}

type Store interface {
	ReadWriteStore
	Fetcher
}
