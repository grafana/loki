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

// Reader is an interface for a chunk store that reads data
type Reader interface {
	SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error)
	SelectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error)
	Series(ctx context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error)
}

// Writer is an interface for a chunk store that writes data
type Writer interface {
	Put(ctx context.Context, chunks []chunk.Chunk) error
	PutOne(ctx context.Context, from, through model.Time, chunk chunk.Chunk) error
}

// ReaderWriter is an interface for a chunk store that reads and writes data
type ReaderWriter interface {
	Reader
	Writer
}

// Fetcher is an interface for a chunk store that needs to fetch chunks
// from a local or remote location
type Fetcher interface {
	GetChunkFetcher(tm model.Time) *fetcher.Fetcher
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*fetcher.Fetcher, error)
}
