package compactor

import (
	"context"
	"errors"
	"flag"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/storage"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	loki_storage "github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/deletion"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	"github.com/grafana/loki/pkg/storage/stores/util"
)

const delimiter = "/"

type Config struct {
	WorkingDirectory          string        `yaml:"working_directory"`
	SharedStoreType           string        `yaml:"shared_store"`
	SharedStoreKeyPrefix      string        `yaml:"shared_store_key_prefix"`
	CompactionInterval        time.Duration `yaml:"compaction_interval"`
	RetentionEnabled          bool          `yaml:"retention_enabled"`
	RetentionDeleteDelay      time.Duration `yaml:"retention_delete_delay"`
	RetentionDeleteWorkCount  int           `yaml:"retention_delete_worker_count"`
	DeleteRequestCancelPeriod time.Duration `yaml:"delete_request_cancel_period"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.WorkingDirectory, "boltdb.shipper.compactor.working-directory", "", "Directory where files can be downloaded for compaction.")
	f.StringVar(&cfg.SharedStoreType, "boltdb.shipper.compactor.shared-store", "", "Shared store used for storing boltdb files. Supported types: gcs, s3, azure, swift, filesystem")
	f.StringVar(&cfg.SharedStoreKeyPrefix, "boltdb.shipper.compactor.shared-store.key-prefix", "index/", "Prefix to add to Object Keys in Shared store. Path separator(if any) should always be a '/'. Prefix should never start with a separator but should always end with it.")
	f.DurationVar(&cfg.CompactionInterval, "boltdb.shipper.compactor.compaction-interval", 10*time.Minute, "Interval at which to re-run the compaction operation.")
	f.DurationVar(&cfg.RetentionDeleteDelay, "boltdb.shipper.compactor.retention-delete-delay", 2*time.Hour, "Delay after which chunks will be fully deleted during retention.")
	f.BoolVar(&cfg.RetentionEnabled, "boltdb.shipper.compactor.retention-enabled", false, "(Experimental) Activate custom (per-stream,per-tenant) retention.")
	f.IntVar(&cfg.RetentionDeleteWorkCount, "boltdb.shipper.compactor.retention-delete-worker-count", 150, "The total amount of worker to use to delete chunks.")
	f.DurationVar(&cfg.DeleteRequestCancelPeriod, "purger.delete-request-cancel-period", 24*time.Hour, "Allow cancellation of delete request until duration after they are created. Data would be deleted only after delete requests have been older than this duration. Ideally this should be set to at least 24h.")
}

func (cfg *Config) IsDefaults() bool {
	cpy := &Config{}
	cpy.RegisterFlags(flag.NewFlagSet("defaults", flag.ContinueOnError))
	return reflect.DeepEqual(cfg, cpy)
}

func (cfg *Config) Validate() error {
	return shipper_util.ValidateSharedStoreKeyPrefix(cfg.SharedStoreKeyPrefix)
}

type Compactor struct {
	services.Service

	cfg                   Config
	objectClient          chunk.ObjectClient
	tableMarker           retention.TableMarker
	sweeper               *retention.Sweeper
	DeleteRequestsHandler *deletion.DeleteRequestHandler
	metrics               *metrics
}

func NewCompactor(cfg Config, storageConfig storage.Config, schemaConfig loki_storage.SchemaConfig, limits retention.Limits, r prometheus.Registerer) (*Compactor, error) {
	if cfg.IsDefaults() {
		return nil, errors.New("Must specify compactor config")
	}

	objectClient, err := storage.NewObjectClient(cfg.SharedStoreType, storageConfig)
	if err != nil {
		return nil, err
	}

	err = chunk_util.EnsureDirectory(cfg.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	prefixedClient := util.NewPrefixedObjectClient(objectClient, cfg.SharedStoreKeyPrefix)

	retentionWorkDir := filepath.Join(cfg.WorkingDirectory, "retention")

	sweeper, err := retention.NewSweeper(retentionWorkDir, retention.NewDeleteClient(objectClient), cfg.RetentionDeleteWorkCount, cfg.RetentionDeleteDelay, r)
	if err != nil {
		return nil, err
	}

	deletionWorkDir := filepath.Join(cfg.WorkingDirectory, "deletion")

	deletesStore, err := deletion.NewDeleteStore(deletionWorkDir, prefixedClient)
	if err != nil {
		return nil, err
	}

	compactor := &Compactor{
		cfg:                   cfg,
		objectClient:          prefixedClient,
		metrics:               newMetrics(r),
		sweeper:               sweeper,
		DeleteRequestsHandler: deletion.NewDeleteRequestHandler(deletesStore, time.Hour, prometheus.DefaultRegisterer),
	}

	marker, err := retention.NewMarker(retentionWorkDir, schemaConfig, retention.NewExpirationChecker(limits), r)
	if err != nil {
		return nil, err
	}
	compactor.tableMarker = marker
	compactor.Service = services.NewBasicService(nil, compactor.loop, nil)
	return compactor, nil
}

func (c *Compactor) loop(ctx context.Context) error {
	runCompaction := func() {
		err := c.RunCompaction(ctx)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to run compaction", "err", err)
		}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runCompaction()

		ticker := time.NewTicker(c.cfg.CompactionInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				runCompaction()
			case <-ctx.Done():
				return
			}
		}
	}()
	if c.cfg.RetentionEnabled {
		wg.Add(1)
		go func() {
			// starts the chunk sweeper
			defer func() {
				c.sweeper.Stop()
				wg.Done()
			}()
			c.sweeper.Start()
			<-ctx.Done()
		}()
	}

	wg.Wait()
	return nil
}

func (c *Compactor) CompactTable(ctx context.Context, tableName string) error {
	table, err := newTable(ctx, filepath.Join(c.cfg.WorkingDirectory, tableName), c.objectClient, c.cfg.RetentionEnabled, c.tableMarker)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to initialize table for compaction", "table", tableName, "err", err)
		return err
	}

	err = table.compact()
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to compact files", "table", tableName, "err", err)
		return err
	}
	return nil
}

func (c *Compactor) RunCompaction(ctx context.Context) error {
	status := statusSuccess
	start := time.Now()

	defer func() {
		c.metrics.compactTablesOperationTotal.WithLabelValues(status).Inc()
		if status == statusSuccess {
			c.metrics.compactTablesOperationDurationSeconds.Set(time.Since(start).Seconds())
			c.metrics.compactTablesOperationLastSuccess.SetToCurrentTime()
		}
	}()

	_, dirs, err := c.objectClient.List(ctx, "", delimiter)
	if err != nil {
		status = statusFailure
		return err
	}

	tables := make([]string, len(dirs))
	for i, dir := range dirs {
		tables[i] = strings.TrimSuffix(string(dir), delimiter)
	}

	for _, tableName := range tables {
		if err := c.CompactTable(ctx, tableName); err != nil {
			status = statusFailure
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
