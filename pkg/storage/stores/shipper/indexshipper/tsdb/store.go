package tsdb

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/dskit/instrument"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/downloads"
	shipperindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/index"
	tsdbindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type storeMetrics struct {
	// duration in seconds spent in serving request on index managed by TSDB Shipper
	requestDurationSeconds *prometheus.HistogramVec
}

func newStoreMetrics(r prometheus.Registerer) *storeMetrics {
	return &storeMetrics{
		requestDurationSeconds: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_tsdb_shipper",
			Name:      "request_duration_seconds",
			Help:      "Time (in seconds) spent serving requests when using tsdb shipper",
			Buckets:   instrument.DefBuckets,
		}, []string{"operation", "status_code"}),
	}
}

type IndexWriter interface {
	Append(userID string, ls labels.Labels, fprint uint64, chks tsdbindex.ChunkMetas) error
}

type store struct {
	index.Reader
	indexShipper indexshipper.IndexShipper
	indexWriter  IndexWriter
	logger       log.Logger
	stopOnce     sync.Once
	metrics      *storeMetrics
}

// NewStore creates a new tsdb index ReaderWriter.
func NewStore(
	name string,
	indexShipperCfg IndexCfg,
	schemaCfg config.SchemaConfig,
	_ *fetcher.Fetcher,
	objectClient client.ObjectClient,
	limits downloads.Limits,
	tableRange config.TableRange,
	reg prometheus.Registerer,
	logger log.Logger,
	idxCache cache.Cache,
) (
	index.ReaderWriter,
	func(),
	error,
) {

	storeInstance := &store{
		logger: logger,
	}

	if err := storeInstance.init(name, indexShipperCfg, schemaCfg, objectClient, limits, tableRange, reg, idxCache); err != nil {
		return nil, nil, err
	}

	return storeInstance, storeInstance.Stop, nil
}

func (s *store) init(name string, indexCfg IndexCfg, schemaCfg config.SchemaConfig, objectClient client.ObjectClient,
	limits downloads.Limits, tableRange config.TableRange, reg prometheus.Registerer, idxCache cache.Cache) error {

	var sharedCache cache.Cache
	if indexCfg.CachePostings && indexCfg.Mode == indexshipper.ModeReadOnly && idxCache != nil {
		sharedCache = idxCache
	}

	openFn := func(p string) (shipperindex.Index, error) {
		return OpenShippableTSDB(p, IndexOpts{PostingsCache: sharedCache})
	}

	var err error
	s.indexShipper, err = indexshipper.NewIndexShipper(
		indexCfg.Config,
		objectClient,
		limits,
		nil,
		openFn,
		tableRange,
		prometheus.WrapRegistererWithPrefix("loki_tsdb_shipper_", reg),
		s.logger,
	)

	if err != nil {
		return err
	}

	s.metrics = newStoreMetrics(reg)

	var indices []Index
	opts := DefaultIndexClientOptions()

	if indexCfg.Mode == indexshipper.ModeWriteOnly {
		// We disable bloom filters on write nodes
		// for the Stats() methods as it's of relatively little
		// benefit when compared to the memory cost. The bloom filters
		// help detect duplicates with some probability, but this
		// is only relevant across index bucket boundaries
		// & pre-compacted indices (replication, not valid on a single ingester).
		opts.UseBloomFilters = false
	}

	if indexCfg.Mode != indexshipper.ModeReadOnly {
		nodeName, err := indexCfg.GetUniqueUploaderName()
		if err != nil {
			return err
		}

		tsdbMetrics := NewMetrics(reg)
		tsdbManager := NewTSDBManager(
			name,
			nodeName,
			indexCfg.ActiveIndexDirectory,
			s.indexShipper,
			tableRange,
			schemaCfg,
			s.logger,
			tsdbMetrics,
		)

		headManager := NewHeadManager(
			name,
			s.logger,
			indexCfg.ActiveIndexDirectory,
			tsdbMetrics,
			tsdbManager,
		)
		if err := headManager.Start(); err != nil {
			return err
		}

		s.indexWriter = headManager
		indices = append(indices, headManager)
	} else {
		s.indexWriter = failingIndexWriter{}
	}

	indices = append(indices, newIndexShipperQuerier(s.indexShipper, tableRange))
	multiIndex := NewMultiIndex(IndexSlice(indices))

	s.Reader = NewIndexClient(multiIndex, opts, limits, s.metrics)

	return nil
}

func (s *store) Stop() {
	s.stopOnce.Do(func() {
		if hm, ok := s.indexWriter.(*HeadManager); ok {
			if err := hm.Stop(); err != nil {
				level.Error(s.logger).Log("msg", "failed to stop head manager", "err", err)
			}
		}
		s.indexShipper.Stop()
	})
}

func (s *store) IndexChunk(ctx context.Context, _ model.Time, _ model.Time, chk chunk.Chunk) error {
	// Always write the index to benefit durability via replication factor.
	approxKB := math.Round(float64(chk.Data.UncompressedSize()) / float64(1<<10))
	metas := tsdbindex.ChunkMetas{
		{
			Checksum: chk.ChunkRef.Checksum,
			MinTime:  int64(chk.ChunkRef.From),
			MaxTime:  int64(chk.ChunkRef.Through),
			KB:       uint32(approxKB),
			Entries:  uint32(chk.Data.Entries()),
		},
	}
	if err := instrument.CollectedRequest(ctx, "Shipper.Query", instrument.NewHistogramCollector(s.metrics.requestDurationSeconds), instrument.ErrorCode, func(ctx context.Context) error {
		return s.indexWriter.Append(chk.UserID, chk.Metric, chk.ChunkRef.Fingerprint, metas)
	}); err != nil {
		return errors.Wrap(err, "writing index entry")
	}
	return nil
}

type failingIndexWriter struct{}

func (f failingIndexWriter) Append(_ string, _ labels.Labels, _ uint64, _ tsdbindex.ChunkMetas) error {
	return fmt.Errorf("index writer is not initialized due to tsdb store being initialized in read-only mode")
}
