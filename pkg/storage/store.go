package storage

import (
	"context"
	"flag"
	"sort"

	"github.com/cortexproject/cortex/pkg/chunk"
	cortex_local "github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/cortexproject/cortex/pkg/chunk/storage"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/stats"
	"github.com/grafana/loki/pkg/storage/stores/local"
	"github.com/grafana/loki/pkg/util"
)

// Config is the loki storage configuration
type Config struct {
	storage.Config      `yaml:",inline"`
	MaxChunkBatchSize   int                 `yaml:"max_chunk_batch_size"`
	BoltDBShipperConfig local.ShipperConfig `yaml:"boltdb_shipper"`
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Config.RegisterFlags(f)
	cfg.BoltDBShipperConfig.RegisterFlags(f)
	f.IntVar(&cfg.MaxChunkBatchSize, "max-chunk-batch-size", 50, "The maximum number of chunks to fetch per batch.")
}

// Store is the Loki chunk store to retrieve and save chunks.
type Store interface {
	chunk.Store
	LazyQuery(ctx context.Context, req logql.SelectParams) (iter.EntryIterator, error)
	GetSeries(ctx context.Context, req logql.SelectParams) ([]logproto.SeriesIdentifier, error)
}

type store struct {
	chunk.Store
	cfg Config
}

// NewStore creates a new Loki Store using configuration supplied.
func NewStore(cfg Config, storeCfg chunk.StoreConfig, schemaCfg chunk.SchemaConfig, limits storage.StoreLimits, registerer prometheus.Registerer) (Store, error) {
	s, err := storage.NewStore(cfg.Config, storeCfg, schemaCfg, limits, registerer, nil)
	if err != nil {
		return nil, err
	}
	return &store{
		Store: s,
		cfg:   cfg,
	}, nil
}

// decodeReq sanitizes an incoming request, rounds bounds, and appends the __name__ matcher
func decodeReq(req logql.SelectParams) ([]*labels.Matcher, logql.LineFilter, model.Time, model.Time, error) {
	expr, err := req.LogSelector()
	if err != nil {
		return nil, nil, 0, 0, err
	}

	filter, err := expr.Filter()
	if err != nil {
		return nil, nil, 0, 0, err
	}

	matchers := expr.Matchers()
	nameLabelMatcher, err := labels.NewMatcher(labels.MatchEqual, labels.MetricName, "logs")
	if err != nil {
		return nil, nil, 0, 0, err
	}
	matchers = append(matchers, nameLabelMatcher)

	from, through := util.RoundToMilliseconds(req.Start, req.End)
	return matchers, filter, from, through, nil
}

// lazyChunks is an internal function used to resolve a set of lazy chunks from the store without actually loading them. It's used internally by `LazyQuery` and `GetSeries`
func (s *store) lazyChunks(ctx context.Context, matchers []*labels.Matcher, from, through model.Time) ([]*chunkenc.LazyChunk, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	storeStats := stats.GetStoreData(ctx)

	chks, fetchers, err := s.GetChunkRefs(ctx, userID, from, through, matchers...)
	if err != nil {
		return nil, err
	}

	var totalChunks int
	for i := range chks {
		storeStats.TotalChunksRef += int64(len(chks[i]))
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
	return lazyChunks, nil
}

func (s *store) GetSeries(ctx context.Context, req logql.SelectParams) ([]logproto.SeriesIdentifier, error) {
	matchers, _, from, through, err := decodeReq(req)
	if err != nil {
		return nil, err
	}

	lazyChunks, err := s.lazyChunks(ctx, matchers, from, through)
	if err != nil {
		return nil, err
	}

	// group chunks by series
	chunksBySeries := partitionBySeriesChunks(lazyChunks)

	firstChunksPerSeries := make([]*chunkenc.LazyChunk, 0, len(chunksBySeries))

	// discard all but one chunk per series
	for _, chks := range chunksBySeries {
		firstChunksPerSeries = append(firstChunksPerSeries, chks[0][0])
	}

	results := make(logproto.SeriesIdentifiers, 0, len(firstChunksPerSeries))

	// bound concurrency
	groups := make([][]*chunkenc.LazyChunk, 0, len(firstChunksPerSeries)/s.cfg.MaxChunkBatchSize+1)

	split := s.cfg.MaxChunkBatchSize
	if len(firstChunksPerSeries) < split {
		split = len(firstChunksPerSeries)
	}

	for split > 0 {
		groups = append(groups, firstChunksPerSeries[:split])
		firstChunksPerSeries = firstChunksPerSeries[split:]
		if len(firstChunksPerSeries) < split {
			split = len(firstChunksPerSeries)
		}
	}

	for _, group := range groups {
		err = fetchLazyChunks(ctx, group)
		if err != nil {
			return nil, err
		}

	outer:
		for _, chk := range group {
			for _, matcher := range matchers {
				if !matcher.Matches(chk.Chunk.Metric.Get(matcher.Name)) {
					continue outer
				}
			}

			m := chk.Chunk.Metric.Map()
			delete(m, labels.MetricName)
			results = append(results, logproto.SeriesIdentifier{
				Labels: m,
			})
		}
	}
	sort.Sort(results)
	return results, nil

}

// LazyQuery returns an iterator that will query the store for more chunks while iterating instead of fetching all chunks upfront
// for that request.
func (s *store) LazyQuery(ctx context.Context, req logql.SelectParams) (iter.EntryIterator, error) {
	matchers, filter, from, through, err := decodeReq(req)
	if err != nil {
		return nil, err
	}

	lazyChunks, err := s.lazyChunks(ctx, matchers, from, through)
	if err != nil {
		return nil, err
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

func RegisterCustomIndexClients(cfg Config, registerer prometheus.Registerer) {
	// BoltDB Shipper is supposed to be run as a singleton.
	// This could also be done in NewBoltDBIndexClientWithShipper factory method but we are doing it here because that method is used
	// in tests for creating multiple instances of it at a time.
	var boltDBIndexClientWithShipper chunk.IndexClient

	storage.RegisterIndexStore(local.BoltDBShipperType, func() (chunk.IndexClient, error) {
		if boltDBIndexClientWithShipper != nil {
			return boltDBIndexClientWithShipper, nil
		}

		objectClient, err := storage.NewObjectClient(cfg.BoltDBShipperConfig.SharedStoreType, cfg.Config)
		if err != nil {
			return nil, err
		}

		boltDBIndexClientWithShipper, err = local.NewBoltDBIndexClientWithShipper(
			cortex_local.BoltDBConfig{Directory: cfg.BoltDBShipperConfig.ActiveIndexDirectory},
			objectClient, cfg.BoltDBShipperConfig, registerer)

		return boltDBIndexClientWithShipper, err
	}, func() (client chunk.TableClient, e error) {
		objectClient, err := storage.NewObjectClient(cfg.BoltDBShipperConfig.SharedStoreType, cfg.Config)
		if err != nil {
			return nil, err
		}

		return local.NewBoltDBShipperTableClient(objectClient), nil
	})
}
