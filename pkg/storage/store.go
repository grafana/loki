package storage

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/httpreq"

	lokilog "github.com/grafana/loki/v3/pkg/logql/log"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/indexgateway"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/congestion"
	"github.com/grafana/loki/v3/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores"
	"github.com/grafana/loki/v3/pkg/storage/stores/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/series"
	series_index "github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/deletion"
)

var (
	indexTypeStats  = analytics.NewString("store_index_type")
	objectTypeStats = analytics.NewString("store_object_type")
	schemaStats     = analytics.NewString("store_schema")

	errWritingChunkUnsupported = errors.New("writing chunks is not supported while running store in read-only mode")
)

type SelectStore interface {
	SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error)
	SelectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error)
	SelectSeries(ctx context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error)
}

type SchemaConfigProvider interface {
	GetSchemaConfigs() []config.PeriodConfig
}

type Instrumentable interface {
	SetExtractorWrapper(wrapper lokilog.SampleExtractorWrapper)

	SetPipelineWrapper(wrapper lokilog.PipelineWrapper)
}

type Store interface {
	stores.Store
	SelectStore
	SchemaConfigProvider
	Instrumentable
}

type LokiStore struct {
	stores.Store

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

	chunkFilterer               chunk.RequestChunkFilterer
	extractorWrapper            lokilog.SampleExtractorWrapper
	pipelineWrapper             lokilog.PipelineWrapper
	congestionControllerFactory func(cfg congestion.Config, logger log.Logger, metrics *congestion.Metrics) congestion.Controller

	metricsNamespace string
}

// NewStore creates a new Loki Store using configuration supplied.
func NewStore(cfg Config, storeCfg config.ChunkStoreConfig, schemaCfg config.SchemaConfig,
	limits StoreLimits, clientMetrics ClientMetrics, registerer prometheus.Registerer, logger log.Logger,
	metricsNamespace string,
) (*LokiStore, error) {
	if len(schemaCfg.Configs) != 0 {
		if index := config.ActivePeriodConfig(schemaCfg.Configs); index != -1 && index < len(schemaCfg.Configs) {
			indexTypeStats.Set(schemaCfg.Configs[index].IndexType)
			objectTypeStats.Set(schemaCfg.Configs[index].ObjectType)
			schemaStats.Set(schemaCfg.Configs[index].Schema)
		}
	}

	indexReadCache, err := cache.New(cfg.IndexQueriesCacheConfig, registerer, logger, stats.IndexCache, metricsNamespace)
	if err != nil {
		return nil, err
	}

	if cache.IsCacheConfigured(storeCfg.WriteDedupeCacheConfig) {
		level.Warn(logger).Log("msg", "write dedupe cache is deprecated along with legacy index types. Consider using TSDB index which does not require a write dedupe cache.")
	}

	writeDedupeCache, err := cache.New(storeCfg.WriteDedupeCacheConfig, registerer, logger, stats.WriteDedupeCache, metricsNamespace)
	if err != nil {
		return nil, err
	}

	chunkCacheCfg := storeCfg.ChunkCacheConfig
	chunkCacheCfg.Prefix = "chunks"
	chunksCache, err := cache.New(chunkCacheCfg, registerer, logger, stats.ChunkCache, metricsNamespace)
	if err != nil {
		return nil, err
	}

	chunkCacheCfgL2 := storeCfg.ChunkCacheConfigL2
	chunkCacheCfgL2.Prefix = "chunksl2"
	// TODO(E.Welch) would we want to disambiguate this cache in the stats? I think not but we'd need to change stats.ChunkCache to do so.
	chunksCacheL2, err := cache.New(chunkCacheCfgL2, registerer, logger, stats.ChunkCache, metricsNamespace)
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

	s := &LokiStore{
		Store:     stores,
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

		metricsNamespace: metricsNamespace,
	}
	if err := s.init(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *LokiStore) init() error {
	for i, p := range s.schemaCfg.Configs {
		p := p
		chunkClient, err := s.chunkClientForPeriod(p)
		if err != nil {
			return err
		}
		f, err := fetcher.New(s.chunksCache, s.chunksCacheL2, s.storeCfg.ChunkCacheStubs(), s.schemaCfg, chunkClient, s.storeCfg.L2ChunkCacheHandoff)
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

		// s.Store is always assigned the CompositeStore implementation of the Store interface
		s.Store.(*stores.CompositeStore).AddStore(p.From.Time, f, idx, w, stop)
	}

	if s.cfg.EnableAsyncStore {
		s.Store = NewAsyncStore(s.cfg.AsyncStoreConfig, s.Store, s.schemaCfg)
	}

	return nil
}

func (s *LokiStore) chunkClientForPeriod(p config.PeriodConfig) (client.Client, error) {
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

	chunks, err := NewChunkClient(objectStoreType, s.cfg, s.schemaCfg, cc, chunkClientReg, s.clientMetrics, s.logger)
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

func (s *LokiStore) storeForPeriod(p config.PeriodConfig, tableRange config.TableRange, chunkClient client.Client, f *fetcher.Fetcher) (stores.ChunkWriter, index.ReaderWriter, func(), error) {
	indexClientReg := prometheus.WrapRegistererWith(
		prometheus.Labels{
			"component": fmt.Sprintf(
				"index-store-%s-%s",
				p.IndexType,
				p.From.String(),
			),
		}, s.registerer)
	indexClientLogger := log.With(s.logger, "index-store", fmt.Sprintf("%s-%s", p.IndexType, p.From.String()))

	if p.IndexType == types.TSDBType {
		if shouldUseIndexGatewayClient(s.cfg.TSDBShipperConfig) {
			// inject the index-gateway client into the index store
			gw, err := indexgateway.NewGatewayClient(s.cfg.TSDBShipperConfig.IndexGatewayClientConfig, indexClientReg, s.limits, indexClientLogger, s.metricsNamespace)
			if err != nil {
				return nil, nil, nil, err
			}
			idx := series.NewIndexGatewayClientStore(gw, indexClientLogger)

			return failingChunkWriter{}, index.NewMonitoredReaderWriter(idx, indexClientReg), func() {
				f.Stop()
				gw.Stop()
			}, nil
		}

		objectClient, err := NewObjectClient(p.ObjectType, s.cfg, s.clientMetrics)
		if err != nil {
			return nil, nil, nil, err
		}

		name := fmt.Sprintf("%s_%s", p.ObjectType, p.From.String())
		indexReaderWriter, stopTSDBStoreFunc, err := tsdb.NewStore(name, p.IndexTables.PathPrefix, s.cfg.TSDBShipperConfig, s.schemaCfg, f, objectClient, s.limits, tableRange, indexClientReg, indexClientLogger)
		if err != nil {
			return nil, nil, nil, err
		}

		indexReaderWriter = index.NewMonitoredReaderWriter(indexReaderWriter, indexClientReg)
		chunkWriter := stores.NewChunkWriter(f, s.schemaCfg, indexReaderWriter, s.storeCfg.DisableIndexDeduplication)

		return chunkWriter, indexReaderWriter,
			func() {
				f.Stop()
				chunkClient.Stop()
				stopTSDBStoreFunc()
				objectClient.Stop()
			}, nil
	}

	idx, err := NewIndexClient(p, tableRange, s.cfg, s.schemaCfg, s.limits, s.clientMetrics, nil, indexClientReg, indexClientLogger, s.metricsNamespace)
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
	monitoredReaderWriter := index.NewMonitoredReaderWriter(indexReaderWriter, indexClientReg)
	chunkWriter := stores.NewChunkWriter(f, s.schemaCfg, monitoredReaderWriter, s.storeCfg.DisableIndexDeduplication)

	return chunkWriter,
		monitoredReaderWriter,
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
	matchers, err = injectShardLabel(req.GetShards(), matchers)
	if err != nil {
		return nil, 0, 0, err
	}
	from, through := util.RoundToMilliseconds(req.GetStart(), req.GetEnd())
	return matchers, from, through, nil
}

// TODO(owen-d): refactor this. Injecting shard labels via matchers is a big hack and we shouldn't continue
// doing it, _but_ it requires adding `fingerprintfilter` support to much of our storage interfaces
// or a way to transform the base store into a more specialized variant.
func injectShardLabel(shards []string, matchers []*labels.Matcher) ([]*labels.Matcher, error) {
	if shards != nil {
		parsed, _, err := logql.ParseShards(shards)
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

func (s *LokiStore) SetChunkFilterer(chunkFilterer chunk.RequestChunkFilterer) {
	s.chunkFilterer = chunkFilterer
	s.Store.SetChunkFilterer(chunkFilterer)
}

func (s *LokiStore) SetExtractorWrapper(wrapper lokilog.SampleExtractorWrapper) {
	s.extractorWrapper = wrapper
}

func (s *LokiStore) SetPipelineWrapper(wrapper lokilog.PipelineWrapper) {
	s.pipelineWrapper = wrapper
}

// lazyChunks is an internal function used to resolve a set of lazy chunks from the store without actually loading them.
func (s *LokiStore) lazyChunks(
	ctx context.Context,
	from, through model.Time,
	predicate chunk.Predicate,
	storeChunksOverride *logproto.ChunkRefGroup,
) ([]*LazyChunk, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	stats := stats.FromContext(ctx)

	start := time.Now()
	chks, fetchers, err := s.GetChunks(ctx, userID, from, through, predicate, storeChunksOverride)
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

	if storeChunksOverride != nil {
		s.chunkMetrics.refsBypassed.Add(float64(len(storeChunksOverride.Refs)))
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

func (s *LokiStore) SelectSeries(ctx context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
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
		result[i] = logproto.SeriesIdentifierFromLabels(s)
	}
	return result, nil
}

// SelectLogs returns an iterator that will query the store for more chunks while iterating instead of fetching all chunks upfront
// for that request.
func (s *LokiStore) SelectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error) {
	matchers, from, through, err := decodeReq(req)
	if err != nil {
		return nil, err
	}

	lazyChunks, err := s.lazyChunks(ctx, from, through, chunk.NewPredicate(matchers, req.Plan), req.GetStoreChunks())
	if err != nil {
		return nil, err
	}

	if len(lazyChunks) == 0 {
		return iter.NoopEntryIterator, nil
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

	if s.pipelineWrapper != nil && httpreq.ExtractHeader(ctx, httpreq.LokiDisablePipelineWrappersHeader) != "true" {
		userID, err := tenant.TenantID(ctx)
		if err != nil {
			return nil, err
		}

		pipeline = s.pipelineWrapper.Wrap(ctx, pipeline, req.Plan.String(), userID)
	}

	var chunkFilterer chunk.Filterer
	if s.chunkFilterer != nil {
		chunkFilterer = s.chunkFilterer.ForRequest(ctx)
	}

	return newLogBatchIterator(ctx, s.schemaCfg, s.chunkMetrics, lazyChunks, s.cfg.MaxChunkBatchSize, matchers, pipeline, req.Direction, req.Start, req.End, chunkFilterer)
}

func (s *LokiStore) SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error) {
	matchers, from, through, err := decodeReq(req)
	if err != nil {
		return nil, err
	}

	lazyChunks, err := s.lazyChunks(ctx, from, through, chunk.NewPredicate(matchers, req.Plan), req.GetStoreChunks())
	if err != nil {
		return nil, err
	}

	if len(lazyChunks) == 0 {
		return iter.NoopSampleIterator, nil
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

	if s.extractorWrapper != nil && httpreq.ExtractHeader(ctx, httpreq.LokiDisablePipelineWrappersHeader) != "true" {
		userID, err := tenant.TenantID(ctx)
		if err != nil {
			return nil, err
		}

		extractor = s.extractorWrapper.Wrap(ctx, extractor, req.Plan.String(), userID)
	}

	var chunkFilterer chunk.Filterer
	if s.chunkFilterer != nil {
		chunkFilterer = s.chunkFilterer.ForRequest(ctx)
	}

	return newSampleBatchIterator(ctx, s.schemaCfg, s.chunkMetrics, lazyChunks, s.cfg.MaxChunkBatchSize, matchers, extractor, req.Start, req.End, chunkFilterer)
}

func (s *LokiStore) GetSchemaConfigs() []config.PeriodConfig {
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
