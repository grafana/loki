package tsdb

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/downloads"
	"github.com/grafana/loki/pkg/storage/stores/series"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	util_log "github.com/grafana/loki/pkg/util/log"
)

// we do not need to build store for each schema config since we do not do any schema specific handling yet.
// If we do need to do schema specific handling, it would be a good idea to abstract away the handling since
// running multiple head managers would be complicated and wasteful.
var storeInstance *store

type store struct {
	indexWriter IndexWriter
	indexStore  series.IndexStore
}

// NewStore creates a new store if not initialized already.
// Each call to NewStore will always build a new stores.ChunkWriter even if the store was already initialized since
// fetcher.Fetcher instances could be different due to periodic configs having different types of object storage configured
// for storing chunks.
func NewStore(indexShipperCfg indexshipper.Config, p config.PeriodConfig, f *fetcher.Fetcher,
	objectClient client.ObjectClient, limits downloads.Limits, tableRanges config.TableRanges, reg prometheus.Registerer) (stores.ChunkWriter, series.IndexStore, error) {
	if storeInstance == nil {
		storeInstance = &store{}
		err := storeInstance.init(indexShipperCfg, p, objectClient, limits, tableRanges, reg)
		if err != nil {
			return nil, nil, err
		}
	}

	return NewChunkWriter(f, p, storeInstance.indexWriter), storeInstance.indexStore, nil
}

func (s *store) init(indexShipperCfg indexshipper.Config, p config.PeriodConfig,
	objectClient client.ObjectClient, limits downloads.Limits, tableRanges config.TableRanges, reg prometheus.Registerer) error {

	shpr, err := indexshipper.NewIndexShipper(
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

	if indexShipperCfg.Mode != indexshipper.ModeReadOnly {
		var (
			nodeName = indexShipperCfg.IngesterName
			dir      = indexShipperCfg.ActiveIndexDirectory
		)

		tsdbMetrics := NewMetrics(reg)
		tsdbManager := NewTSDBManager(
			nodeName,
			dir,
			shpr,
			p.IndexTables.Period,
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

	indices = append(indices, newIndexShipperQuerier(shpr))
	multiIndex, err := NewMultiIndex(indices...)
	if err != nil {
		return err
	}

	s.indexStore = NewIndexClient(multiIndex)

	return nil
}

type failingIndexWriter struct{}

func (f failingIndexWriter) Append(_ string, _ labels.Labels, _ index.ChunkMetas) error {
	return fmt.Errorf("index writer is not initialized due to tsdb store being initialized in read-only mode")
}
