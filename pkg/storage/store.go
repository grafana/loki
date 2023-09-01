package storage

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/pkg/analytics"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/congestion"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores"
	chunkstore "github.com/grafana/loki/pkg/storage/stores/chunk"
	indexstore "github.com/grafana/loki/pkg/storage/stores/index"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/gatewayclient"
	"github.com/grafana/loki/pkg/storage/stores/series"
	series_index "github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway"
	"github.com/grafana/loki/pkg/storage/stores/tsdb"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/deletion"
)

var (
	indexTypeStats  = analytics.NewString("store_index_type")
	objectTypeStats = analytics.NewString("store_object_type")
	schemaStats     = analytics.NewString("store_schema")

	errWritingChunkUnsupported = errors.New("writing chunks is not supported while running store in read-only mode")
)

type Store interface {
	stores.Store
	chunkstore.ReadStore
	indexstore.Filterable
	GetSchemaConfigs() []config.PeriodConfig
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
	chunksCacheL2    cache.Cache
	writeDedupeCache cache.Cache

	limits StoreLimits
	logger log.Logger

	chunkFilterer               indexstore.RequestChunkFilterer
	congestionControllerFactory func(cfg congestion.Config, logger log.Logger, metrics *congestion.Metrics) congestion.Controller
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

	indexReadCache, err := cache.New(cfg.IndexQueriesCacheConfig, registerer, logger, stats.IndexCache)
	if err != nil {
		return nil, err
	}

	writeDedupeCache, err := cache.New(storeCfg.WriteDedupeCacheConfig, registerer, logger, stats.WriteDedupeCache)
	if err != nil {
		return nil, err
	}

	chunkCacheCfg := storeCfg.ChunkCacheConfig
	chunkCacheCfg.Prefix = "chunks"
	chunksCache, err := cache.New(chunkCacheCfg, registerer, logger, stats.ChunkCache)
	if err != nil {
		return nil, err
	}

	chunkCacheCfgL2 := storeCfg.ChunkCacheConfigL2
	chunkCacheCfgL2.Prefix = "chunksl2"
	// TODO(E.Welch) would we want to disambiguate this cache in the stats? I think not but we'd need to change stats.ChunkCache to do so.
	chunksCacheL2, err := cache.New(chunkCacheCfgL2, registerer, logger, stats.ChunkCache)
	if err != nil {
		return nil, err
	}

	// Cache is shared by multiple stores, which means they will try and Stop
	// it more than once.  Wrap in a StopOnce to prevent this.
	indexReadCache = cache.StopOnce(indexReadCache)
	chunksCache = cache.StopOnce(chunksCache)
	chunksCacheL2 = cache.StopOnce(chunksCacheL2)
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

		congestionControllerFactory: congestion.NewController,

		chunkClientMetrics: client.NewChunkClientMetrics(registerer),
		clientMetrics:      clientMetrics,
		chunkMetrics:       NewChunkMetrics(registerer, cfg.MaxChunkBatchSize),
		registerer:         registerer,

		indexReadCache:   indexReadCache,
		chunksCache:      chunksCache,
		chunksCacheL2:    chunksCacheL2,
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
	for i, p := range s.schemaCfg.Configs {
		p := p
		chunkClient, err := s.chunkClientForPeriod(p)
		if err != nil {
			return err
		}
		f, err := fetcher.New(s.chunksCache, s.chunksCacheL2, s.storeCfg.ChunkCacheStubs(), s.schemaCfg, chunkClient, s.storeCfg.ChunkCacheConfig.AsyncCacheWriteBackConcurrency, s.storeCfg.ChunkCacheConfig.AsyncCacheWriteBackBufferSize, s.storeCfg.L2ChunkCacheHandoff)
		if err != nil {
			return err
		}

		periodEndTime := config.DayTime{Time: math.MaxInt64}
		if i < len(s.schemaCfg.Configs)-1 {
			periodEndTime = config.DayTime{Time: s.schemaCfg.Configs[i+1].From.Time.Add(-time.Millisecond)}
		}
		w, idx, stop, err := s.storeForPeriod(p, p.GetIndexTableNumberRange(periodEndTime), chunkClient, f)
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

	var cc congestion.Controller
	ccCfg := s.cfg.CongestionControl

	if ccCfg.Enabled {
		cc = s.congestionControllerFactory(
			ccCfg,
			s.logger,
			congestion.NewMetrics(fmt.Sprintf("%s-%s", objectStoreType, p.From.String()), ccCfg),
		)
	}

	chunks, err := NewChunkClient(objectStoreType, s.cfg, s.schemaCfg, cc, chunkClientReg, s.clientMetrics)
	if err != nil {
		return nil, errors.Wrap(err, "error creating object client")
	}

	chunks = client.NewMetricsChunkClient(chunks, s.chunkClientMetrics)
	return chunks, nil
}

func shouldUseIndexGatewayClient(cfg indexshipper.Config) bool {
	if cfg.Mode != indexshipper.ModeReadOnly || cfg.IndexGatewayClientConfig.Disabled {
		return false
	}

	gatewayCfg := cfg.IndexGatewayClientConfig
	if gatewayCfg.Mode == indexgateway.SimpleMode && gatewayCfg.Address == "" {
		return false
	}

	return true
}

func (s *store) storeForPeriod(p config.PeriodConfig, tableRange config.TableRange, chunkClient client.Client, f *fetcher.Fetcher) (chunkstore.WriteStore, indexstore.ReadWriteStore, func(), error) {
	indexClientReg := prometheus.WrapRegistererWith(
		prometheus.Labels{
			"component": fmt.Sprintf(
				"index-store-%s-%s",
				p.IndexType,
				p.From.String(),
			),
		}, s.registerer)
	indexClientLogger := log.With(s.logger, "index-store", fmt.Sprintf("%s-%s", p.IndexType, p.From.String()))

	if p.IndexType == config.TSDBType {
		if shouldUseIndexGatewayClient(s.cfg.TSDBShipperConfig.Config) {
			// inject the index-gateway client into the index store
			gw, err := gatewayclient.NewGatewayClient(s.cfg.TSDBShipperConfig.IndexGatewayClientConfig, indexClientReg, s.limits, indexClientLogger)
			if err != nil {
				return nil, nil, nil, err
			}
			idx := series.NewIndexGatewayClientStore(gw, indexClientLogger)

			return failingChunkWriter{}, indexstore.NewMonitoredReaderWriter(idx, indexClientReg), func() {
				f.Stop()
				gw.Stop()
			}, nil
		}

		objectType := p.ObjectType
		if s.cfg.TSDBShipperConfig.SharedStoreType != "" {
			objectType = s.cfg.TSDBShipperConfig.SharedStoreType
		}

		objectClient, err := NewObjectClient(objectType, s.cfg, s.clientMetrics)
		if err != nil {
			return nil, nil, nil, err
		}

		var backupIndexWriter indexstore.WriteStore
		backupStoreStop := func() {}
		if s.cfg.TSDBShipperConfig.UseBoltDBShipperAsBackup {
			pCopy := p
			pCopy.IndexType = config.BoltDBShipperType
			pCopy.IndexTables.Prefix = fmt.Sprintf("%sbackup_", pCopy.IndexTables.Prefix)

			tableRange := tableRange
			tableRange.PeriodConfig = &pCopy

			_, backupIndexWriter, backupStoreStop, err = s.storeForPeriod(pCopy, tableRange, chunkClient, f)
			if err != nil {
				return nil, nil, nil, err
			}
		}

		indexReaderWriter, stopTSDBStoreFunc, err := tsdb.NewStore(fmt.Sprintf("%s_%s", p.ObjectType, p.From.String()), s.cfg.TSDBShipperConfig, s.schemaCfg, f, objectClient, s.limits,
			tableRange, backupIndexWriter, indexClientReg, indexClientLogger, s.indexReadCache)
		if err != nil {
			return nil, nil, nil, err
		}

		indexReaderWriter = indexstore.NewMonitoredReaderWriter(indexReaderWriter, indexClientReg)
		chunkWriter := stores.NewChunkWriter(f, s.schemaCfg, indexReaderWriter, s.storeCfg.DisableIndexDeduplication)

		return chunkWriter, indexReaderWriter,
			func() {
				f.Stop()
				chunkClient.Stop()
				stopTSDBStoreFunc()
				objectClient.Stop()
				backupStoreStop()
			}, nil
	}

	idx, err := NewIndexClient(p, tableRange, s.cfg, s.schemaCfg, s.limits, s.clientMetrics, nil, indexClientReg, indexClientLogger)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "error creating index client")
	}
	idx = series_index.NewCachingIndexClient(idx, s.indexReadCache, s.cfg.IndexCacheValidity, s.limits, indexClientLogger, s.cfg.DisableBroadIndexQueries)
	schema, err := series_index.CreateSchema(p)
	if err != nil {
		return nil, nil, nil, err
	}
	if s.storeCfg.CacheLookupsOlderThan != 0 {
		schema = series_index.NewSchemaCaching(schema, time.Duration(s.storeCfg.CacheLookupsOlderThan))
	}

	indexReaderWriter := series.NewIndexReaderWriter(s.schemaCfg, schema, idx, f, s.cfg.MaxChunkBatchSize, s.writeDedupeCache)
	indexReaderWriter = indexstore.NewMonitoredReaderWriter(indexReaderWriter, indexClientReg)
	chunkWriter := stores.NewChunkWriter(f, s.schemaCfg, indexReaderWriter, s.storeCfg.DisableIndexDeduplication)

	return chunkWriter,
		indexReaderWriter,
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

func (s *store) SetChunkFilterer(chunkFilterer indexstore.RequestChunkFilterer) {
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

	start := time.Now()
	chks, fetchers, err := s.GetChunkRefs(ctx, userID, from, through, matchers...)
	stats.AddChunkRefsFetchTime(time.Since(start))

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

	var chunkFilterer indexstore.Filterer
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

	var chunkFilterer indexstore.Filterer
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

type failingChunkWriter struct{}

func (f failingChunkWriter) Put(_ context.Context, _ []chunk.Chunk) error {
	return errWritingChunkUnsupported
}

func (f failingChunkWriter) PutOne(_ context.Context, _, _ model.Time, _ chunk.Chunk) error {
	return errWritingChunkUnsupported
}
