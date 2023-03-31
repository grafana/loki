package shipper

import (
	"context"
	"flag"
	"fmt"
	"sync"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/instrument"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/downloads"
	series_index "github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/index/indexfile"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type Config struct {
	indexshipper.Config `yaml:",inline"`
	BuildPerTenantIndex bool `yaml:"build_per_tenant_index"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("boltdb.", f)

}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.Config.RegisterFlagsWithPrefix(prefix, f)
	f.BoolVar(&cfg.BuildPerTenantIndex, prefix+"shipper.build-per-tenant-index", false, "Build per tenant index files")
}

func (cfg *Config) Validate() error {
	return cfg.Config.Validate()
}

type writer interface {
	ForEach(ctx context.Context, tableName string, callback func(boltdb *bbolt.DB) error) error
	BatchWrite(ctx context.Context, batch series_index.WriteBatch) error
	Stop()
}

type indexClient struct {
	cfg          Config
	indexShipper indexshipper.IndexShipper
	writer       writer
	querier      index.Querier

	metrics  *metrics
	stopOnce sync.Once
}

// NewShipper creates a shipper for syncing local objects with a store
func NewShipper(cfg Config, storageClient client.ObjectClient, limits downloads.Limits,
	ownsTenantFn downloads.IndexGatewayOwnsTenant, tableRanges config.TableRanges, registerer prometheus.Registerer) (series_index.Client, error) {
	i := indexClient{
		cfg:     cfg,
		metrics: newMetrics(registerer),
	}

	err := i.init(storageClient, limits, ownsTenantFn, tableRanges, registerer)
	if err != nil {
		return nil, err
	}

	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("starting boltdb shipper in %s mode", cfg.Mode))

	return &i, nil
}

func (i *indexClient) init(storageClient client.ObjectClient, limits downloads.Limits,
	ownsTenantFn downloads.IndexGatewayOwnsTenant, tableRanges config.TableRanges, registerer prometheus.Registerer) error {
	var err error
	i.indexShipper, err = indexshipper.NewIndexShipper(i.cfg.Config, storageClient, limits, ownsTenantFn,
		indexfile.OpenIndexFile, tableRanges, prometheus.WrapRegistererWithPrefix("loki_boltdb_shipper_", registerer))
	if err != nil {
		return err
	}

	if i.cfg.Mode != indexshipper.ModeReadOnly {
		uploader, err := i.cfg.GetUniqueUploaderName()
		if err != nil {
			return err
		}

		cfg := index.Config{
			Uploader:             uploader,
			IndexDir:             i.cfg.ActiveIndexDirectory,
			DBRetainPeriod:       i.cfg.IngesterDBRetainPeriod,
			MakePerTenantBuckets: i.cfg.BuildPerTenantIndex,
		}
		i.writer, err = index.NewTableManager(cfg, i.indexShipper, registerer)
		if err != nil {
			return err
		}
	}

	i.querier = index.NewQuerier(i.writer, i.indexShipper)

	return nil
}

func (i *indexClient) Stop() {
	i.stopOnce.Do(i.stop)
}

func (i *indexClient) stop() {
	if i.writer != nil {
		i.writer.Stop()
	}
	i.indexShipper.Stop()
}

func (i *indexClient) NewWriteBatch() series_index.WriteBatch {
	return local.NewWriteBatch()
}

func (i *indexClient) BatchWrite(ctx context.Context, batch series_index.WriteBatch) error {
	return instrument.CollectedRequest(ctx, "WRITE", instrument.NewHistogramCollector(i.metrics.requestDurationSeconds), instrument.ErrorCode, func(ctx context.Context) error {
		return i.writer.BatchWrite(ctx, batch)
	})
}

func (i *indexClient) QueryPages(ctx context.Context, queries []series_index.Query, callback series_index.QueryPagesCallback) error {
	return instrument.CollectedRequest(ctx, "Shipper.Query", instrument.NewHistogramCollector(i.metrics.requestDurationSeconds), instrument.ErrorCode, func(ctx context.Context) error {
		return i.querier.QueryPages(ctx, queries, callback)
	})
}
