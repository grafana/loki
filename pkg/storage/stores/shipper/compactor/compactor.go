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
	WorkingDirectory     string        `yaml:"working_directory"`
	SharedStoreType      string        `yaml:"shared_store"`
	SharedStoreKeyPrefix string        `yaml:"shared_store_key_prefix"`
	CompactionInterval   time.Duration `yaml:"compaction_interval"`
	RetentionInterval    time.Duration `yaml:"retention_interval"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.WorkingDirectory, "boltdb.shipper.compactor.working-directory", "", "Directory where files can be downloaded for compaction.")
	f.StringVar(&cfg.SharedStoreType, "boltdb.shipper.compactor.shared-store", "", "Shared store used for storing boltdb files. Supported types: gcs, s3, azure, swift, filesystem")
	f.StringVar(&cfg.SharedStoreKeyPrefix, "boltdb.shipper.compactor.shared-store.key-prefix", "index/", "Prefix to add to Object Keys in Shared store. Path separator(if any) should always be a '/'. Prefix should never start with a separator but should always end with it.")
	f.DurationVar(&cfg.CompactionInterval, "boltdb.shipper.compactor.compaction-interval", 2*time.Hour, "Interval at which to re-run the compaction operation.")
	f.DurationVar(&cfg.CompactionInterval, "boltdb.shipper.compactor.retention-interval", 2*time.Hour, "Interval at which to re-run the retention operation.")
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

	metrics *metrics
}

func NewCompactor(cfg Config, storageConfig storage.Config, schemaConfig loki_storage.SchemaConfig, r prometheus.Registerer) (*Compactor, error) {
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

	// todo configuration and expiration checker.
	compactor := Compactor{
		cfg:          cfg,
		objectClient: prefixedClient,
		metrics:      newMetrics(r),
		tableMarker:  retention.NewMarker(filepath.Join(cfg.WorkingDirectory, "retention"), schemaConfig, prefixedClient, nil),
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
	wg.Add(2)
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

	go func() {
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
		c.metrics.retentionOperationTotal.WithLabelValues(status).Inc()
		if status == statusSuccess {
			c.metrics.retentionOperationDurationSeconds.Set(time.Since(start).Seconds())
			c.metrics.retentionOperationLastSuccess.SetToCurrentTime()
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

	var errs errUtil.MultiError

	for _, tableName := range tables {
		if err := c.tableMarker.MarkTableForDelete(ctx, tableName); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to mark table for deletes", "table", tableName, "err", err)
			errs.Add(err)
			status = statusFailure
		}
	}
	return errs.Err()
}
