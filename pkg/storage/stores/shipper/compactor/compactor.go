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
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	"github.com/grafana/loki/pkg/storage/stores/util"
	errUtil "github.com/grafana/loki/pkg/util"
)

const delimiter = "/"

type Config struct {
	WorkingDirectory         string        `yaml:"working_directory"`
	SharedStoreType          string        `yaml:"shared_store"`
	SharedStoreKeyPrefix     string        `yaml:"shared_store_key_prefix"`
	CompactionInterval       time.Duration `yaml:"compaction_interval"`
	RetentionEnabled         bool          `yaml:"retention_enabled"`
	RetentionInterval        time.Duration `yaml:"retention_interval"`
	RetentionDeleteDelay     time.Duration `yaml:"retention_delete_delay"`
	RetentionDeleteWorkCount int           `yaml:"retention_delete_worker_count"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.WorkingDirectory, "boltdb.shipper.compactor.working-directory", "", "Directory where files can be downloaded for compaction.")
	f.StringVar(&cfg.SharedStoreType, "boltdb.shipper.compactor.shared-store", "", "Shared store used for storing boltdb files. Supported types: gcs, s3, azure, swift, filesystem")
	f.StringVar(&cfg.SharedStoreKeyPrefix, "boltdb.shipper.compactor.shared-store.key-prefix", "index/", "Prefix to add to Object Keys in Shared store. Path separator(if any) should always be a '/'. Prefix should never start with a separator but should always end with it.")
	f.DurationVar(&cfg.CompactionInterval, "boltdb.shipper.compactor.compaction-interval", 2*time.Hour, "Interval at which to re-run the compaction operation.")
	f.DurationVar(&cfg.RetentionInterval, "boltdb.shipper.compactor.retention-interval", 10*time.Minute, "Interval at which to re-run the retention operation.")
	f.DurationVar(&cfg.RetentionDeleteDelay, "boltdb.shipper.compactor.retention-delete-delay", 2*time.Hour, "Delay after which chunks will be fully deleted during retention.")
	f.BoolVar(&cfg.RetentionEnabled, "boltdb.shipper.compactor.retention-enabled", false, "(Experimental) Activate custom (per-stream,per-tenant) retention.")
	f.IntVar(&cfg.RetentionDeleteWorkCount, "boltdb.shipper.compactor.retention-delete-worker-count", 150, "The total amount of worker to use to delete chunks.")
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

	cfg          Config
	objectClient chunk.ObjectClient
	tableMarker  *retention.Marker
	sweeper      *retention.Sweeper

	metrics *metrics
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
	marker, err := retention.NewMarker(retentionWorkDir, schemaConfig, prefixedClient, retention.NewExpirationChecker(limits), r)
	if err != nil {
		return nil, err
	}
	compactor := Compactor{
		cfg:          cfg,
		objectClient: prefixedClient,
		metrics:      newMetrics(r),
		tableMarker:  marker,
		sweeper:      sweeper,
	}

	compactor.Service = services.NewBasicService(nil, compactor.loop, nil)
	return &compactor, nil
}

func (c *Compactor) loop(ctx context.Context) error {
	runCompaction := func() {
		err := c.RunCompaction(ctx)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to run compaction", "err", err)
		}
	}
	runRetention := func() {
		err := c.RunRetention(ctx)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to run retention", "err", err)
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
		wg.Add(2)
		go func() {
			// starts the chunk sweeper
			defer func() {
				c.sweeper.Stop()
				wg.Done()
			}()
			c.sweeper.Start()
			<-ctx.Done()
		}()
		go func() {
			// start the index marker
			defer wg.Done()
			ticker := time.NewTicker(c.cfg.RetentionInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					runRetention()
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	wg.Wait()
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
		table, err := newTable(ctx, filepath.Join(c.cfg.WorkingDirectory, tableName), c.objectClient)
		if err != nil {
			status = statusFailure
			level.Error(util_log.Logger).Log("msg", "failed to initialize table for compaction", "table", tableName, "err", err)
			continue
		}

		err = table.compact()
		if err != nil {
			status = statusFailure
			level.Error(util_log.Logger).Log("msg", "failed to compact files", "table", tableName, "err", err)
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

func (c *Compactor) RunRetention(ctx context.Context) error {
	status := statusSuccess
	start := time.Now()

	defer func() {
		level.Debug(util_log.Logger).Log("msg", "finished to processing retention on all tables", "status", status, "duration", time.Since(start))
		c.metrics.retentionOperationTotal.WithLabelValues(status).Inc()
		if status == statusSuccess {
			c.metrics.retentionOperationDurationSeconds.Set(time.Since(start).Seconds())
			c.metrics.retentionOperationLastSuccess.SetToCurrentTime()
		}
	}()
	level.Debug(util_log.Logger).Log("msg", "starting to processing retention on all  all tables")

	_, dirs, err := c.objectClient.List(ctx, "", delimiter)
	if err != nil {
		status = statusFailure
		return err
	}

	tables := make([]string, len(dirs))
	for i, dir := range dirs {
		tables[i] = strings.TrimSuffix(string(dir), delimiter)
	}

	var errs errUtil.MultiError

	for _, tableName := range tables {
		if err := c.tableMarker.MarkForDelete(ctx, tableName); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to mark table for deletes", "table", tableName, "err", err)
			errs.Add(err)
			status = statusFailure
		}
	}
	return errs.Err()
}
