package tsdb

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/downloads"
	"github.com/grafana/loki/pkg/storage/stores/series"
	util_log "github.com/grafana/loki/pkg/util/log"
)

func NewStore(indexShipperCfg indexshipper.Config, p config.PeriodConfig, f *fetcher.Fetcher,
	objectClient client.ObjectClient, limits downloads.Limits, reg prometheus.Registerer) (stores.ChunkWriter, series.IndexStore, error) {
	var (
		nodeName = indexShipperCfg.IngesterName
		dir      = indexShipperCfg.ActiveIndexDirectory
	)
	tsdbMetrics := NewMetrics(reg)

	shpr, err := indexshipper.NewIndexShipper(
		indexShipperCfg,
		objectClient,
		limits,
		OpenShippableTSDB,
	)
	if err != nil {
		return nil, nil, err
	}
	tsdbManager := NewTSDBManager(
		nodeName,
		dir,
		shpr,
		p.IndexTables.Period,
		util_log.Logger,
		tsdbMetrics,
	)
	// TODO(owen-d): Only need HeadManager
	// on the ingester. Otherwise, the TSDBManager is sufficient
	headManager := NewHeadManager(
		util_log.Logger,
		dir,
		tsdbMetrics,
		tsdbManager,
	)
	if err := headManager.Start(); err != nil {
		return nil, nil, err
	}
	idx := NewIndexClient(headManager, p)
	writer := NewChunkWriter(f, p, headManager)

	// TODO(owen-d): add TSDB index-gateway support

	return writer, idx, nil
}
