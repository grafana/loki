package shipper

import (
	"context"
	"flag"
	"fmt"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/instrument"
	"github.com/prometheus/client_golang/prometheus"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/downloads"
	series_index "github.com/grafana/loki/pkg/storage/stores/series/index"
	indexfile "github.com/grafana/loki/pkg/storage/stores/shipper/boltdb"
	"github.com/grafana/loki/pkg/storage/stores/shipper/index"
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

type IndexClient struct {
	cfg          Config
	indexShipper indexshipper.IndexShipper
	writer       writer
	querier      index.Querier

	metrics  *metrics
	logger   log.Logger
	stopOnce sync.Once
}

// NewIndexClient creates a shipper for syncing local objects with a store
func NewIndexClient(cfg Config, storageClient client.ObjectClient, limits downloads.Limits,
	tenantFilter downloads.TenantFilter, tableRange config.TableRange, registerer prometheus.Registerer, logger log.Logger) (*IndexClient, error) {
	i := IndexClient{
		cfg:     cfg,
		metrics: newMetrics(registerer),
		logger:  logger,
	}

	err := i.init(storageClient, limits, tenantFilter, tableRange, registerer)
	if err != nil {
		return nil, err
	}

	level.Info(i.logger).Log("msg", fmt.Sprintf("starting boltdb shipper in %s mode", cfg.Mode))

	return &i, nil
}

func (i *IndexClient) init(storageClient client.ObjectClient, limits downloads.Limits,
	tenantFilter downloads.TenantFilter, tableRange config.TableRange, registerer prometheus.Registerer) error {
	var err error
	i.indexShipper, err = indexshipper.NewIndexShipper(i.cfg.Config, storageClient, limits, tenantFilter,
		indexfile.OpenIndexFile, tableRange, prometheus.WrapRegistererWithPrefix("loki_boltdb_shipper_", registerer), i.logger)
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
		i.writer, err = index.NewTableManager(cfg, i.indexShipper, tableRange, registerer, i.logger)
		if err != nil {
			return err
		}
	}

	i.querier = index.NewQuerier(i.writer, i.indexShipper)

	return nil
}

func (i *IndexClient) Stop() {
	i.stopOnce.Do(i.stop)
}

func (i *IndexClient) stop() {
	if i.writer != nil {
		i.writer.Stop()
	}
	i.indexShipper.Stop()
}

func (i *IndexClient) NewWriteBatch() series_index.WriteBatch {
	return local.NewWriteBatch()
}

func (i *IndexClient) BatchWrite(ctx context.Context, batch series_index.WriteBatch) error {
	return instrument.CollectedRequest(ctx, "WRITE", instrument.NewHistogramCollector(i.metrics.requestDurationSeconds), instrument.ErrorCode, func(ctx context.Context) error {
		return i.writer.BatchWrite(ctx, batch)
	})
}

func (i *IndexClient) QueryPages(ctx context.Context, queries []series_index.Query, callback series_index.QueryPagesCallback) error {
	return instrument.CollectedRequest(ctx, "Shipper.Query", instrument.NewHistogramCollector(i.metrics.requestDurationSeconds), instrument.ErrorCode, func(ctx context.Context) error {
		return i.querier.QueryPages(ctx, queries, callback)
	})
}
