package compactor

import (
	"context"
	"flag"
	"path/filepath"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/storage"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	pkg_util "github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/storage/stores/shipper"
	"github.com/grafana/loki/pkg/storage/stores/util"
)

type Config struct {
	WorkingDirectory string `yaml:"working_directory"`
	SharedStoreType  string `yaml:"shared_store"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.WorkingDirectory, "boltdb.shipper.compactor.working-directory", "", "Directory where files can be downloaded for compaction.")
	f.StringVar(&cfg.SharedStoreType, "boltdb.shipper.compactor.shared-store", "", "Shared store used for storing boltdb files. Supported types: gcs, s3, azure, swift, filesystem")
}

type Compactor struct {
	services.Service

	cfg          Config
	objectClient chunk.ObjectClient

	metrics *metrics
}

func NewCompactor(cfg Config, storageConfig storage.Config, r prometheus.Registerer) (*Compactor, error) {
	objectClient, err := storage.NewObjectClient(cfg.SharedStoreType, storageConfig)
	if err != nil {
		return nil, err
	}

	err = chunk_util.EnsureDirectory(cfg.WorkingDirectory)
	if err != nil {
		return nil, err
	}

	compactor := Compactor{
		cfg:          cfg,
		objectClient: util.NewPrefixedObjectClient(objectClient, shipper.StorageKeyPrefix),
		metrics:      newMetrics(r),
	}

	compactor.Service = services.NewTimerService(4*time.Hour, nil, compactor.Run, nil)
	return &compactor, nil
}

func (c *Compactor) Run(ctx context.Context) error {
	status := statusSuccess
	start := time.Now()

	defer func() {
		c.metrics.compactTablesOperationTotal.WithLabelValues(status).Inc()
		if status == statusSuccess {
			c.metrics.compactTablesOperationDurationSeconds.Set(time.Since(start).Seconds())
		}
	}()

	_, dirs, err := c.objectClient.List(ctx, "")
	if err != nil {
		status = statusFailure
		return err
	}

	tables := make([]string, len(dirs))
	for i, dir := range dirs {
		tables[i] = strings.TrimSuffix(string(dir), "/")
	}

	for _, tableName := range tables {
		table, err := newTable(ctx, filepath.Join(c.cfg.WorkingDirectory, tableName), c.objectClient)
		if err != nil {
			status = statusFailure
			level.Error(pkg_util.Logger).Log("msg", "failed to initialize table for compaction", "table", tableName, "err", err)
			continue
		}

		err = table.compact()
		if err != nil {
			status = statusFailure
			level.Error(pkg_util.Logger).Log("msg", "failed to compact files", "table", tableName, "err", err)
		}

		// check if context was cancelled before going for next table.
		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}

	return nil
}
