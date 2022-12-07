package shipper

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/instrument"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/chunk/client/util"
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

// metrics singleton
var shipperMetrics *metrics

type indexClient struct {
	cfg          Config
	indexShipper indexshipper.IndexShipper
	writer       writer
	querier      index.Querier

	metrics  *metrics
	stopOnce sync.Once
}

// NewShipper creates a shipper for syncing local objects with a store
func NewShipper(name string, cfg Config, storageClient client.ObjectClient, limits downloads.Limits,
	ownsTenantFn downloads.IndexGatewayOwnsTenant, tableRange config.TableRange, registerer prometheus.Registerer) (series_index.Client, error) {
	if shipperMetrics == nil {
		shipperMetrics = newMetrics(registerer)
	}

	i := indexClient{
		cfg:     cfg,
		metrics: shipperMetrics,
	}

	err := i.init(name, storageClient, limits, ownsTenantFn, tableRange, registerer)
	if err != nil {
		return nil, err
	}

	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("starting boltdb shipper in %s mode", cfg.Mode))

	return &i, nil
}

func (i *indexClient) init(name string, storageClient client.ObjectClient, limits downloads.Limits,
	ownsTenantFn downloads.IndexGatewayOwnsTenant, tableRange config.TableRange, registerer prometheus.Registerer) error {
	var err error
	i.indexShipper, err = indexshipper.NewIndexShipper(name, i.cfg.Config, storageClient, limits, ownsTenantFn,
		indexfile.OpenIndexFile, tableRange, prometheus.WrapRegistererWithPrefix("loki_boltdb_shipper_", registerer))
	if err != nil {
		return err
	}

	if i.cfg.Mode != indexshipper.ModeReadOnly {
		uploader, err := i.getUploaderName()
		if err != nil {
			return err
		}

		cfg := index.Config{
			Uploader:             uploader,
			IndexDir:             path.Join(i.cfg.ActiveIndexDirectory, name),
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

func (i *indexClient) getUploaderName() (string, error) {
	uploader := fmt.Sprintf("%s-%d", i.cfg.IngesterName, time.Now().UnixNano())

	uploaderFilePath := path.Join(i.cfg.ActiveIndexDirectory, "uploader", "name")
	if err := util.EnsureDirectory(path.Dir(uploaderFilePath)); err != nil {
		return "", err
	}

	_, err := os.Stat(uploaderFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		if err := os.WriteFile(uploaderFilePath, []byte(uploader), 0o666); err != nil {
			return "", err
		}
	} else {
		ub, err := os.ReadFile(uploaderFilePath)
		if err != nil {
			return "", err
		}
		uploader = string(ub)
	}

	return uploader, nil
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
