package storage

import (
	"context"
	"flag"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/storage"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/util"
)

// Config is the loki storage configuration
type Config struct {
	storage.Config    `yaml:",inline"`
	MaxChunkBatchSize int `yaml:"max_chunk_batch_size"`
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Config.RegisterFlags(f)
	f.IntVar(&cfg.MaxChunkBatchSize, "max-chunk-batch-size", 50, "The maximun of chunks to fetch per batch.")
}

// Store is the Loki chunk store to retrieve and save chunks.
type Store interface {
	chunk.Store
	LazyQuery(ctx context.Context, req logql.SelectParams) (iter.EntryIterator, error)
}

type store struct {
	chunk.Store
	cfg Config
}

// NewStore creates a new Loki Store using configuration supplied.
func NewStore(cfg Config, storeCfg chunk.StoreConfig, schemaCfg chunk.SchemaConfig, limits storage.StoreLimits) (Store, error) {
	s, err := storage.NewStore(cfg.Config, storeCfg, schemaCfg, limits)
	if err != nil {
		return nil, err
	}
	return &store{
		Store: s,
		cfg:   cfg,
	}, nil
}

// LazyQuery returns an iterator that will query the store for more chunks while iterating instead of fetching all chunks upfront
// for that request.
func (s *store) LazyQuery(ctx context.Context, req logql.SelectParams) (iter.EntryIterator, error) {
	expr, err := req.LogSelector()
	if err != nil {
		return nil, err
	}

	filter, err := expr.Filter()
	if err != nil {
		return nil, err
	}

	matchers := expr.Matchers()
	nameLabelMatcher, err := labels.NewMatcher(labels.MatchEqual, labels.MetricName, "logs")
	if err != nil {
		return nil, err
	}

	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	matchers = append(matchers, nameLabelMatcher)
	from, through := util.RoundToMilliseconds(req.Start, req.End)
	chks, fetchers, err := s.GetChunkRefs(ctx, userID, from, through, matchers...)
	if err != nil {
		return nil, err
	}

	var totalChunks int
	for i := range chks {
		chks[i] = filterChunksByTime(from, through, chks[i])
		totalChunks += len(chks[i])
	}
	// creates lazychunks with chunks ref.
	lazyChunks := make([]*chunkenc.LazyChunk, 0, totalChunks)
	for i := range chks {
		for _, c := range chks[i] {
			lazyChunks = append(lazyChunks, &chunkenc.LazyChunk{Chunk: c, Fetcher: fetchers[i]})
		}
	}
	return newBatchChunkIterator(ctx, lazyChunks, s.cfg.MaxChunkBatchSize, matchers, filter, req.QueryRequest), nil

}

func filterChunksByTime(from, through model.Time, chunks []chunk.Chunk) []chunk.Chunk {
	filtered := make([]chunk.Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.Through < from || through < chunk.From {
			continue
		}
		filtered = append(filtered, chunk)
	}
	return filtered
}
