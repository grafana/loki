package storage

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/storage/chunk"
	chunk_local "github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/chunk/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	"github.com/grafana/loki/pkg/tenant"
	"github.com/grafana/loki/pkg/usagestats"
	"github.com/grafana/loki/pkg/util"
)

var (
	indexTypeStats  = usagestats.NewString("store_index_type")
	objectTypeStats = usagestats.NewString("store_object_type")
	schemaStats     = usagestats.NewString("store_schema")
)

// SchemaConfig contains the config for our chunk index schemas
type SchemaConfig struct {
	chunk.SchemaConfig `yaml:",inline"`
}

// Validate the schema config and returns an error if the validation doesn't pass
func (cfg *SchemaConfig) Validate() error {
	if len(cfg.Configs) == 0 {
		return errZeroLengthConfig
	}
	activePCIndex := ActivePeriodConfig((*cfg).Configs)

	// if current index type is boltdb-shipper and there are no upcoming index types then it should be set to 24 hours.
	if cfg.Configs[activePCIndex].IndexType == shipper.BoltDBShipperType && cfg.Configs[activePCIndex].IndexTables.Period != 24*time.Hour && len(cfg.Configs)-1 == activePCIndex {
		return errCurrentBoltdbShipperNon24Hours
	}

	// if upcoming index type is boltdb-shipper, it should always be set to 24 hours.
	if len(cfg.Configs)-1 > activePCIndex && (cfg.Configs[activePCIndex+1].IndexType == shipper.BoltDBShipperType && cfg.Configs[activePCIndex+1].IndexTables.Period != 24*time.Hour) {
		return errUpcomingBoltdbShipperNon24Hours
	}

	return cfg.SchemaConfig.Validate()
}

// Store is the Loki chunk store to retrieve and save chunks.
type Store interface {
	chunk.Store
	SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error)
	SelectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error)
	GetSeries(ctx context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error)
	GetSchemaConfigs() []chunk.PeriodConfig
	SetChunkFilterer(chunkFilter RequestChunkFilterer)
}

// RequestChunkFilterer creates ChunkFilterer for a given request context.
type RequestChunkFilterer interface {
	ForRequest(ctx context.Context) ChunkFilterer
}

// ChunkFilterer filters chunks based on the metric.
type ChunkFilterer interface {
	ShouldFilter(metric labels.Labels) bool
}

type store struct {
	chunk.Store
	cfg          Config
	chunkMetrics *ChunkMetrics
	schemaCfg    SchemaConfig

	chunkFilterer RequestChunkFilterer
}

// NewStore creates a new Loki Store using configuration supplied.
func NewStore(cfg Config, schemaCfg SchemaConfig, chunkStore chunk.Store, registerer prometheus.Registerer) (Store, error) {
	if len(schemaCfg.Configs) != 0 {
		if index := ActivePeriodConfig(schemaCfg.Configs); index != -1 && index < len(schemaCfg.Configs) {
			indexTypeStats.Set(schemaCfg.Configs[index].IndexType)
			objectTypeStats.Set(schemaCfg.Configs[index].ObjectType)
			schemaStats.Set(schemaCfg.Configs[index].Schema)
		}
	}

	return &store{
		Store:        chunkStore,
		cfg:          cfg,
		chunkMetrics: NewChunkMetrics(registerer, cfg.MaxChunkBatchSize),
		schemaCfg:    schemaCfg,
	}, nil
}

// NewTableClient creates a TableClient for managing tables for index/chunk store.
// ToDo: Add support in Cortex for registering custom table client like index client.
func NewTableClient(name string, cfg Config) (chunk.TableClient, error) {
	if name == shipper.BoltDBShipperType {
		name = "boltdb"
		cfg.FSConfig = chunk_local.FSConfig{Directory: cfg.BoltDBShipperConfig.ActiveIndexDirectory}
	}
	return storage.NewTableClient(name, cfg.Config, prometheus.DefaultRegisterer)
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

func (s *store) SetChunkFilterer(chunkFilterer RequestChunkFilterer) {
	s.chunkFilterer = chunkFilterer
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

func (s *store) GetSeries(ctx context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
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
	return s.Store.GetSeries(ctx, userID, from, through, matchers...)
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

	expr, err := req.LogSelector()
	if err != nil {
		return nil, err
	}

	pipeline, err := expr.Pipeline()
	if err != nil {
		return nil, err
	}

	if len(lazyChunks) == 0 {
		return iter.NoopIterator, nil
	}
	var chunkFilterer ChunkFilterer
	if s.chunkFilterer != nil {
		chunkFilterer = s.chunkFilterer.ForRequest(ctx)
	}

	return newLogBatchIterator(ctx, s.schemaCfg.SchemaConfig, s.chunkMetrics, lazyChunks, s.cfg.MaxChunkBatchSize, matchers, pipeline, req.Direction, req.Start, req.End, chunkFilterer)
}

func (s *store) SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error) {
	matchers, from, through, err := decodeReq(req)
	if err != nil {
		return nil, err
	}

	expr, err := req.Expr()
	if err != nil {
		return nil, err
	}

	extractor, err := expr.Extractor()
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
	var chunkFilterer ChunkFilterer
	if s.chunkFilterer != nil {
		chunkFilterer = s.chunkFilterer.ForRequest(ctx)
	}

	return newSampleBatchIterator(ctx, s.schemaCfg.SchemaConfig, s.chunkMetrics, lazyChunks, s.cfg.MaxChunkBatchSize, matchers, extractor, req.Start, req.End, chunkFilterer)
}

func (s *store) GetSchemaConfigs() []chunk.PeriodConfig {
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

func RegisterCustomIndexClients(cfg *Config, cm storage.ClientMetrics, registerer prometheus.Registerer) {
	// BoltDB Shipper is supposed to be run as a singleton.
	// This could also be done in NewBoltDBIndexClientWithShipper factory method but we are doing it here because that method is used
	// in tests for creating multiple instances of it at a time.
	var boltDBIndexClientWithShipper chunk.IndexClient

	storage.RegisterIndexStore(shipper.BoltDBShipperType, func(limits storage.StoreLimits) (chunk.IndexClient, error) {
		if boltDBIndexClientWithShipper != nil {
			return boltDBIndexClientWithShipper, nil
		}

		if cfg.BoltDBShipperConfig.Mode == shipper.ModeReadOnly && cfg.BoltDBShipperConfig.IndexGatewayClientConfig.Address != "" {
			gateway, err := shipper.NewGatewayClient(cfg.BoltDBShipperConfig.IndexGatewayClientConfig, registerer)
			if err != nil {
				return nil, err
			}

			boltDBIndexClientWithShipper = gateway
			return gateway, nil
		}

		objectClient, err := storage.NewObjectClient(cfg.BoltDBShipperConfig.SharedStoreType, cfg.Config, cm)
		if err != nil {
			return nil, err
		}

		boltDBIndexClientWithShipper, err = shipper.NewShipper(cfg.BoltDBShipperConfig, objectClient, limits, registerer)

		return boltDBIndexClientWithShipper, err
	}, func() (client chunk.TableClient, e error) {
		objectClient, err := storage.NewObjectClient(cfg.BoltDBShipperConfig.SharedStoreType, cfg.Config, cm)
		if err != nil {
			return nil, err
		}

		return shipper.NewBoltDBShipperTableClient(objectClient, cfg.BoltDBShipperConfig.SharedStoreKeyPrefix), nil
	})
}
