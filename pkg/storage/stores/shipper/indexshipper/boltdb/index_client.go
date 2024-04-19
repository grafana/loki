package boltdb

import (
	"context"
	"flag"
	"fmt"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/instrument"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	series_index "github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/downloads"
)

type indexClientMetrics struct {
	// duration in seconds spent in serving request on index managed by BoltDB Shipper
	requestDurationSeconds *prometheus.HistogramVec
}

func newIndexClientMetrics(r prometheus.Registerer) *indexClientMetrics {
	return &indexClientMetrics{
		requestDurationSeconds: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "request_duration_seconds",
			Help:      "Time (in seconds) spent serving requests when using boltdb shipper",
			Buckets:   instrument.DefBuckets,
		}, []string{"operation", "status_code"}),
	}
}

type IndexCfg struct {
	indexshipper.Config `yaml:",inline"`
	BuildPerTenantIndex bool `yaml:"build_per_tenant_index"`
}

// RegisterFlags registers flags.
func (cfg *IndexCfg) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("boltdb.", f)

}

func (cfg *IndexCfg) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.Config.RegisterFlagsWithPrefix(prefix, f)
	f.BoolVar(&cfg.BuildPerTenantIndex, prefix+"shipper.build-per-tenant-index", false, "Build per tenant index files")
}

func (cfg *IndexCfg) Validate() error {
	return cfg.Config.Validate()
}

type writer interface {
	ForEach(ctx context.Context, tableName string, callback func(b *bbolt.DB) error) error
	BatchWrite(ctx context.Context, batch series_index.WriteBatch) error
	Stop()
}

type IndexClient struct {
	cfg          IndexCfg
	indexShipper indexshipper.IndexShipper
	writer       writer
	querier      Querier

	metrics  *indexClientMetrics
	logger   log.Logger
	stopOnce sync.Once
}

// New creates a shipper for syncing local objects with a store
func NewIndexClient(prefix string, cfg IndexCfg, storageClient client.ObjectClient, limits downloads.Limits,
	tenantFilter downloads.TenantFilter, tableRange config.TableRange, registerer prometheus.Registerer, logger log.Logger) (*IndexClient, error) {
	i := IndexClient{
		cfg:     cfg,
		metrics: newIndexClientMetrics(registerer),
		logger:  logger,
	}

	err := i.init(prefix, storageClient, limits, tenantFilter, tableRange, registerer)
	if err != nil {
		return nil, err
	}

	level.Info(i.logger).Log("msg", fmt.Sprintf("starting boltdb shipper in %s mode", cfg.Mode))

	return &i, nil
}

func (i *IndexClient) init(prefix string, storageClient client.ObjectClient, limits downloads.Limits,
	tenantFilter downloads.TenantFilter, tableRange config.TableRange, registerer prometheus.Registerer) error {
	var err error
	i.indexShipper, err = indexshipper.NewIndexShipper(prefix, i.cfg.Config, storageClient, limits, tenantFilter,
		OpenIndexFile, tableRange, prometheus.WrapRegistererWithPrefix("loki_boltdb_shipper_", registerer), i.logger)
	if err != nil {
		return err
	}

	if i.cfg.Mode != indexshipper.ModeReadOnly {
		uploader, err := i.cfg.GetUniqueUploaderName()
		if err != nil {
			return err
		}

		cfg := Config{
			Uploader:             uploader,
			IndexDir:             i.cfg.ActiveIndexDirectory,
			DBRetainPeriod:       i.cfg.IngesterDBRetainPeriod,
			MakePerTenantBuckets: i.cfg.BuildPerTenantIndex,
		}
		i.writer, err = NewTableManager(cfg, i.indexShipper, tableRange, registerer, i.logger)
		if err != nil {
			return err
		}
	}

	i.querier = NewQuerier(i.writer, i.indexShipper)

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
