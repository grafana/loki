package tsdb

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/index"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/downloads"
	tsdb_index "github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type IndexWriter interface {
	Append(userID string, ls labels.Labels, fprint uint64, chks tsdb_index.ChunkMetas) error
}

type store struct {
	index.Reader
	indexShipper      indexshipper.IndexShipper
	indexWriter       IndexWriter
	backupIndexWriter index.Writer
	stopOnce          sync.Once
}

// NewStore creates a new TSDB store.
// This is meant to be a singleton and should be instantiated only once per storage.Store and reused for all schema configs.
// We do not need to build store for each schema config since we do not do any schema specific handling yet.
// If we do need to do schema specific handling, it would be a good idea to abstract away the handling since
// running multiple head managers would be complicated and wasteful.
// Note: The cmd/migrate tool needs this not to be a true global singleton
// as it will create multiple storage.Store instances in the same process.
func NewStore(indexShipperCfg indexshipper.Config,
	p config.PeriodConfig,
	f *fetcher.Fetcher,
	objectClient client.ObjectClient,
	limits downloads.Limits,
	tableRanges config.TableRanges,
	backupIndexWriter index.Writer,
	reg prometheus.Registerer) (index.ReaderWriter, func(), error) {
	if backupIndexWriter == nil {
		backupIndexWriter = noopBackupIndexWriter{}
	}
	storeInstance := &store{
		backupIndexWriter: backupIndexWriter,
	}
	err := storeInstance.init(indexShipperCfg, objectClient, limits, tableRanges, reg)
	if err != nil {
		return nil, nil, err
	}
	return storeInstance, storeInstance.Stop, nil
}

func (s *store) init(indexShipperCfg indexshipper.Config, objectClient client.ObjectClient,
	limits downloads.Limits, tableRanges config.TableRanges, reg prometheus.Registerer) error {

	var err error
	s.indexShipper, err = indexshipper.NewIndexShipper(
		indexShipperCfg,
		objectClient,
		limits,
		nil,
		OpenShippableTSDB,
		tableRanges,
		prometheus.WrapRegistererWithPrefix("loki_tsdb_shipper_", reg),
	)
	if err != nil {
		return err
	}

	var indices []Index
	opts := DefaultIndexClientOptions()

	if indexShipperCfg.Mode == indexshipper.ModeWriteOnly {
		// We disable bloom filters on write nodes
		// for the Stats() methods as it's of relatively little
		// benefit when compared to the memory cost. The bloom filters
		// help detect duplicates with some probability, but this
		// is only relevant across index bucket boundaries
		// & pre-compacted indices (replication, not valid on a single ingester).
		opts.UseBloomFilters = false
	}

	if indexShipperCfg.Mode != indexshipper.ModeReadOnly {
		dir := indexShipperCfg.ActiveIndexDirectory
		nodeName, err := indexShipperCfg.GetUniqueUploaderName()
		if err != nil {
			return err
		}

		tsdbMetrics := NewMetrics(reg)
		tsdbManager := NewTSDBManager(
			nodeName,
			dir,
			s.indexShipper,
			tableRanges,
			util_log.Logger,
			tsdbMetrics,
		)

		headManager := NewHeadManager(
			util_log.Logger,
			dir,
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

	indices = append(indices, newIndexShipperQuerier(s.indexShipper, tableRanges))
	multiIndex := NewMultiIndex(IndexSlice(indices))

	s.Reader = NewIndexClient(multiIndex, opts)

	return nil
}

func (s *store) Stop() {
	s.stopOnce.Do(func() {
		if hm, ok := s.indexWriter.(*HeadManager); ok {
			if err := hm.Stop(); err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to stop head manager", "err", err)
			}
		}
		s.indexShipper.Stop()
	})
}

func (s *store) IndexChunk(ctx context.Context, from model.Time, through model.Time, chk chunk.Chunk) error {
	// Always write the index to benefit durability via replication factor.
	approxKB := math.Round(float64(chk.Data.UncompressedSize()) / float64(1<<10))
	metas := tsdb_index.ChunkMetas{
		{
			Checksum: chk.ChunkRef.Checksum,
			MinTime:  int64(chk.ChunkRef.From),
			MaxTime:  int64(chk.ChunkRef.Through),
			KB:       uint32(approxKB),
			Entries:  uint32(chk.Data.Entries()),
		},
	}
	if err := s.indexWriter.Append(chk.UserID, chk.Metric, chk.ChunkRef.Fingerprint, metas); err != nil {
		return errors.Wrap(err, "writing index entry")
	}

	return s.backupIndexWriter.IndexChunk(ctx, from, through, chk)
}

type failingIndexWriter struct{}

func (f failingIndexWriter) Append(_ string, _ labels.Labels, _ uint64, _ tsdb_index.ChunkMetas) error {
	return fmt.Errorf("index writer is not initialized due to tsdb store being initialized in read-only mode")
}

type noopBackupIndexWriter struct{}

func (n noopBackupIndexWriter) IndexChunk(_ context.Context, _, _ model.Time, _ chunk.Chunk) error {
	return nil
}
