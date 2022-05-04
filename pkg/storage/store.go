package storage

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/series"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway"
	"github.com/grafana/loki/pkg/storage/stores/tsdb"
	"github.com/grafana/loki/pkg/usagestats"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/deletion"
	util_log "github.com/grafana/loki/pkg/util/log"
)

var (
	indexTypeStats  = usagestats.NewString("store_index_type")
	objectTypeStats = usagestats.NewString("store_object_type")
	schemaStats     = usagestats.NewString("store_schema")
)

// Store is the Loki chunk store to retrieve and save chunks.
type Store interface {
	stores.Store
	SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error)
	SelectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error)
	Series(ctx context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error)
	GetSchemaConfigs() []config.PeriodConfig
	SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer)
}

type store struct {
	stores.Store
	composite *stores.CompositeStore

	cfg       Config
	storeCfg  config.ChunkStoreConfig
	schemaCfg config.SchemaConfig

	chunkMetrics       *ChunkMetrics
	chunkClientMetrics client.ChunkClientMetrics
	clientMetrics      ClientMetrics
	registerer         prometheus.Registerer

	indexReadCache   cache.Cache
	chunksCache      cache.Cache
	writeDedupeCache cache.Cache

	limits StoreLimits
	logger log.Logger

	chunkFilterer chunk.RequestChunkFilterer
}

// NewStore creates a new Loki Store using configuration supplied.
func NewStore(cfg Config, storeCfg config.ChunkStoreConfig, schemaCfg config.SchemaConfig,
	limits StoreLimits, clientMetrics ClientMetrics, registerer prometheus.Registerer, logger log.Logger,
) (Store, error) {
	if len(schemaCfg.Configs) != 0 {
		if index := config.ActivePeriodConfig(schemaCfg.Configs); index != -1 && index < len(schemaCfg.Configs) {
			indexTypeStats.Set(schemaCfg.Configs[index].IndexType)
			objectTypeStats.Set(schemaCfg.Configs[index].ObjectType)
			schemaStats.Set(schemaCfg.Configs[index].Schema)
		}
	}

	indexReadCache, err := cache.New(cfg.IndexQueriesCacheConfig, registerer, logger)
	if err != nil {
		return nil, err
	}

	writeDedupeCache, err := cache.New(storeCfg.WriteDedupeCacheConfig, registerer, logger)
	if err != nil {
		return nil, err
	}

	chunkCacheCfg := storeCfg.ChunkCacheConfig
	chunkCacheCfg.Prefix = "chunks"
	chunksCache, err := cache.New(chunkCacheCfg, registerer, logger)
	if err != nil {
		return nil, err
	}

	// Cache is shared by multiple stores, which means they will try and Stop
	// it more than once.  Wrap in a StopOnce to prevent this.
	indexReadCache = cache.StopOnce(indexReadCache)
	chunksCache = cache.StopOnce(chunksCache)
	writeDedupeCache = cache.StopOnce(writeDedupeCache)

	// Lets wrap all caches except chunksCache with CacheGenMiddleware to facilitate cache invalidation using cache generation numbers.
	// chunksCache is not wrapped because chunks content can't be anyways modified without changing its ID so there is no use of
	// invalidating chunks cache. Also chunks can be fetched only by their ID found in index and we are anyways removing the index and invalidating index cache here.
	indexReadCache = cache.NewCacheGenNumMiddleware(indexReadCache)
	writeDedupeCache = cache.NewCacheGenNumMiddleware(writeDedupeCache)

	err = schemaCfg.Load()
	if err != nil {
		return nil, errors.Wrap(err, "error loading schema config")
	}
	stores := stores.NewCompositeStore(limits)

	s := &store{
		Store:     stores,
		composite: stores,
		cfg:       cfg,
		storeCfg:  storeCfg,
		schemaCfg: schemaCfg,

		chunkClientMetrics: client.NewChunkClientMetrics(registerer),
		clientMetrics:      clientMetrics,
		chunkMetrics:       NewChunkMetrics(registerer, cfg.MaxChunkBatchSize),
		registerer:         registerer,

		indexReadCache:   indexReadCache,
		chunksCache:      chunksCache,
		writeDedupeCache: writeDedupeCache,

		logger: logger,
		limits: limits,
	}
	if err := s.init(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *store) init() error {
	for _, p := range s.schemaCfg.Configs {
		chunkClient, err := s.chunkClientForPeriod(p)
		if err != nil {
			return err
		}
		f, err := fetcher.New(s.chunksCache, s.storeCfg.ChunkCacheStubs(), s.schemaCfg, chunkClient, s.storeCfg.ChunkCacheConfig.AsyncCacheWriteBackConcurrency, s.storeCfg.ChunkCacheConfig.AsyncCacheWriteBackBufferSize)
		if err != nil {
			return err
		}

		w, idx, stop, err := s.storeForPeriod(p, chunkClient, f)
		if err != nil {
			return err
		}
		s.composite.AddStore(p.From.Time, f, idx, w, stop)
	}

	if s.cfg.EnableAsyncStore {
		s.Store = NewAsyncStore(s.cfg.AsyncStoreConfig, s.Store, s.schemaCfg)
	}
	return nil
}

func (s *store) chunkClientForPeriod(p config.PeriodConfig) (client.Client, error) {
	objectStoreType := p.ObjectType
	if objectStoreType == "" {
		objectStoreType = p.IndexType
	}
	chunkClientReg := prometheus.WrapRegistererWith(
		prometheus.Labels{"component": "chunk-store-" + p.From.String()}, s.registerer)

	chunks, err := NewChunkClient(objectStoreType, s.cfg, s.schemaCfg, s.clientMetrics, chunkClientReg)
	if err != nil {
		return nil, errors.Wrap(err, "error creating object client")
	}

	chunks = client.NewMetricsChunkClient(chunks, s.chunkClientMetrics)
	return chunks, nil
}

func shouldUseBoltDBIndexGatewayClient(cfg Config) bool {
	if cfg.BoltDBShipperConfig.Mode != shipper.ModeReadOnly || cfg.BoltDBShipperConfig.IndexGatewayClientConfig.Disabled {
		return false
	}

	gatewayCfg := cfg.BoltDBShipperConfig.IndexGatewayClientConfig
	if gatewayCfg.Mode == indexgateway.SimpleMode && gatewayCfg.Address == "" {
		return false
	}

	return true
}

func (s *store) storeForPeriod(p config.PeriodConfig, chunkClient client.Client, f *fetcher.Fetcher) (stores.ChunkWriter, stores.Index, func(), error) {
	indexClientReg := prometheus.WrapRegistererWith(
		prometheus.Labels{"component": "index-store-" + p.From.String()}, s.registerer)

	if p.IndexType == config.TSDBType {
		var (
			nodeName = s.cfg.TSDBShipperConfig.IngesterName
			dir      = s.cfg.TSDBShipperConfig.ActiveIndexDirectory
		)
		tsdbMetrics := tsdb.NewMetrics(indexClientReg)
		objectClient, err := NewObjectClient(s.cfg.TSDBShipperConfig.SharedStoreType, s.cfg, s.clientMetrics)
		if err != nil {
			return nil, nil, nil, err
		}

		shpr, err := indexshipper.NewIndexShipper(
			s.cfg.TSDBShipperConfig,
			objectClient,
			s.limits,
			tsdb.OpenShippableTSDB,
		)
		if err != nil {
			return nil, nil, nil, err
		}
		tsdbManager := tsdb.NewTSDBManager(
			nodeName,
			dir,
			shpr,
			p.IndexTables.Period,
			util_log.Logger,
			tsdbMetrics,
		)
		// TODO(owen-d): Only need HeadManager
		// on the ingester. Otherwise, the TSDBManager is sufficient
		headManager := tsdb.NewHeadManager(
			util_log.Logger,
			dir,
			tsdbMetrics,
			tsdbManager,
		)
		if err := headManager.Start(); err != nil {
			return nil, nil, nil, err
		}
		idx := tsdb.NewIndexClient(headManager, p)
		writer := tsdb.NewChunkWriter(f, p, headManager)

		// TODO(owen-d): add TSDB index-gateway support

		return writer, idx,
			func() {
				chunkClient.Stop()
				f.Stop()
			}, nil
	}

	idx, err := NewIndexClient(p.IndexType, s.cfg, s.schemaCfg, s.limits, s.clientMetrics, indexClientReg)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "error creating index client")
	}
	idx = index.NewCachingIndexClient(idx, s.indexReadCache, s.cfg.IndexCacheValidity, s.limits, s.logger, s.cfg.DisableBroadIndexQueries)
	schema, err := index.CreateSchema(p)
	if err != nil {
		return nil, nil, nil, err
	}
	if s.storeCfg.CacheLookupsOlderThan != 0 {
		schema = index.NewSchemaCaching(schema, time.Duration(s.storeCfg.CacheLookupsOlderThan))
	}

	var (
		writer       stores.ChunkWriter = series.NewWriter(f, s.schemaCfg, idx, schema, s.writeDedupeCache, s.storeCfg.DisableIndexDeduplication)
		seriesdIndex *series.IndexStore = series.NewIndexStore(s.schemaCfg, schema, idx, f, s.cfg.MaxChunkBatchSize)
		index        stores.Index       = seriesdIndex
	)

	if shouldUseBoltDBIndexGatewayClient(s.cfg) {
		// inject the index-gateway client into the index store
		gw, err := shipper.NewGatewayClient(s.cfg.BoltDBShipperConfig.IndexGatewayClientConfig, indexClientReg, s.logger)
		if err != nil {
			return nil, nil, nil, err
		}
		index = series.NewIndexGatewayClientStore(gw, seriesdIndex)
	}

	return writer,
		index,
		func() {
			chunkClient.Stop()
			f.Stop()
			idx.Stop()
		},
		nil
}

// decodeReq sanitizes an incoming request, rounds bounds, appends the __name__ matcher,
// and adds the "__cortex_shard__" label if this is a sharded query.
// todo(cyriltovena) refactor this.
func decodeReq(req logql.QueryParams) ([]*labels.Matcher, model.Time, model.Time, error) {
	expr, err := req.LogSelector()
	if err != nil {
		return nil, 0, 0, err
	}

	matchers := expr.Matchers()
	nameLabelMatcher, err := labels.NewMatcher(labels.MatchEqual, labels.MetricName, "logs")
	if err != nil {
		return nil, 0, 0, err
	}
	matchers = append(matchers, nameLabelMatcher)
	if err != nil {
		return nil, 0, 0, err
	}
	matchers, err = injectShardLabel(req.GetShards(), matchers)
	if err != nil {
		return nil, 0, 0, err
	}
	from, through := util.RoundToMilliseconds(req.GetStart(), req.GetEnd())
	return matchers, from, through, nil
}

func injectShardLabel(shards []string, matchers []*labels.Matcher) ([]*labels.Matcher, error) {
	if shards != nil {
		parsed, err := logql.ParseShards(shards)
		if err != nil {
			return nil, err
		}
		for _, s := range parsed {
			shardMatcher, err := labels.NewMatcher(
				labels.MatchEqual,
				astmapper.ShardLabel,
				s.String(),
			)
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, shardMatcher)
			break // nolint:staticcheck
		}
	}
	return matchers, nil
}

func (s *store) SetChunkFilterer(chunkFilterer chunk.RequestChunkFilterer) {
	s.chunkFilterer = chunkFilterer
	s.Store.SetChunkFilterer(chunkFilterer)
}

// lazyChunks is an internal function used to resolve a set of lazy chunks from the store without actually loading them. It's used internally by `LazyQuery` and `GetSeries`
func (s *store) lazyChunks(ctx context.Context, matchers []*labels.Matcher, from, through model.Time) ([]*LazyChunk, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	stats := stats.FromContext(ctx)

	chks, fetchers, err := s.GetChunkRefs(ctx, userID, from, through, matchers...)
	if err != nil {
		return nil, err
	}

	var prefiltered int
	var filtered int
	for i := range chks {
		prefiltered += len(chks[i])
		stats.AddChunksRef(int64(len(chks[i])))
		chks[i] = filterChunksByTime(from, through, chks[i])
		filtered += len(chks[i])
	}

	s.chunkMetrics.refs.WithLabelValues(statusDiscarded).Add(float64(prefiltered - filtered))
	s.chunkMetrics.refs.WithLabelValues(statusMatched).Add(float64(filtered))

	// creates lazychunks with chunks ref.
	lazyChunks := make([]*LazyChunk, 0, filtered)
	for i := range chks {
		for _, c := range chks[i] {
			lazyChunks = append(lazyChunks, &LazyChunk{Chunk: c, Fetcher: fetchers[i]})
		}
	}
	return lazyChunks, nil
}

func (s *store) Series(ctx context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	var from, through model.Time
	var matchers []*labels.Matcher

	// The Loki parser doesn't allow for an empty label matcher but for the Series API
	// we allow this to select all series in the time range.
	if req.Selector == "" {
		from, through = util.RoundToMilliseconds(req.Start, req.End)
		nameLabelMatcher, err := labels.NewMatcher(labels.MatchEqual, labels.MetricName, "logs")
		if err != nil {
			return nil, err
		}
		matchers = []*labels.Matcher{nameLabelMatcher}
		matchers, err = injectShardLabel(req.GetShards(), matchers)
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		matchers, from, through, err = decodeReq(req)
		if err != nil {
			return nil, err
		}
	}
	series, err := s.Store.GetSeries(ctx, userID, from, through, matchers...)
	if err != nil {
		return nil, err
	}
	result := make([]logproto.SeriesIdentifier, len(series))
	for i, s := range series {
		result[i] = logproto.SeriesIdentifier{
			Labels: s.Map(),
		}
	}
	return result, nil
}

// SelectLogs returns an iterator that will query the store for more chunks while iterating instead of fetching all chunks upfront
// for that request.
func (s *store) SelectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error) {
	matchers, from, through, err := decodeReq(req)
	if err != nil {
		return nil, err
	}

	lazyChunks, err := s.lazyChunks(ctx, matchers, from, through)
	if err != nil {
		return nil, err
	}

	if len(lazyChunks) == 0 {
		return iter.NoopIterator, nil
	}

	expr, err := req.LogSelector()
	if err != nil {
		return nil, err
	}

	pipeline, err := expr.Pipeline()
	if err != nil {
		return nil, err
	}

	pipeline, err = deletion.SetupPipeline(req, pipeline)
	if err != nil {
		return nil, err
	}

	var chunkFilterer chunk.Filterer
	if s.chunkFilterer != nil {
		chunkFilterer = s.chunkFilterer.ForRequest(ctx)
	}

	return newLogBatchIterator(ctx, s.schemaCfg, s.chunkMetrics, lazyChunks, s.cfg.MaxChunkBatchSize, matchers, pipeline, req.Direction, req.Start, req.End, chunkFilterer)
}

func (s *store) SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error) {
	matchers, from, through, err := decodeReq(req)
	if err != nil {
		return nil, err
	}

	lazyChunks, err := s.lazyChunks(ctx, matchers, from, through)
	if err != nil {
		return nil, err
	}

	if len(lazyChunks) == 0 {
		return iter.NoopIterator, nil
	}

	expr, err := req.Expr()
	if err != nil {
		return nil, err
	}

	extractor, err := expr.Extractor()
	if err != nil {
		return nil, err
	}

	extractor, err = deletion.SetupExtractor(req, extractor)
	if err != nil {
		return nil, err
	}

	var chunkFilterer chunk.Filterer
	if s.chunkFilterer != nil {
		chunkFilterer = s.chunkFilterer.ForRequest(ctx)
	}

	return newSampleBatchIterator(ctx, s.schemaCfg, s.chunkMetrics, lazyChunks, s.cfg.MaxChunkBatchSize, matchers, extractor, req.Start, req.End, chunkFilterer)
}

func (s *store) GetSchemaConfigs() []config.PeriodConfig {
	return s.schemaCfg.Configs
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
