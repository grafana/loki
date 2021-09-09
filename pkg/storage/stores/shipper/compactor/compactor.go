package compactor

import (
	"context"
	"errors"
	"flag"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	loki_storage "github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/chunk/objectclient"
	"github.com/grafana/loki/pkg/storage/chunk/storage"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/deletion"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
	shipper_storage "github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type Config struct {
	WorkingDirectory          string        `yaml:"working_directory"`
	SharedStoreType           string        `yaml:"shared_store"`
	SharedStoreKeyPrefix      string        `yaml:"shared_store_key_prefix"`
	CompactionInterval        time.Duration `yaml:"compaction_interval"`
	RetentionEnabled          bool          `yaml:"retention_enabled"`
	RetentionDeleteDelay      time.Duration `yaml:"retention_delete_delay"`
	RetentionDeleteWorkCount  int           `yaml:"retention_delete_worker_count"`
	DeleteRequestCancelPeriod time.Duration `yaml:"delete_request_cancel_period"`
	MaxCompactionParallelism  int           `yaml:"max_compaction_parallelism"`
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
	f.DurationVar(&cfg.DeleteRequestCancelPeriod, "boltdb.shipper.compactor.delete-request-cancel-period", 24*time.Hour, "Allow cancellation of delete request until duration after they are created. Data would be deleted only after delete requests have been older than this duration. Ideally this should be set to at least 24h.")
	f.IntVar(&cfg.MaxCompactionParallelism, "boltdb.shipper.compactor.max-compaction-parallelism", 1, "Maximum number of tables to compact in parallel. While increasing this value, please make sure compactor has enough disk space allocated to be able to store and compact as many tables.")
}

func (cfg *Config) IsDefaults() bool {
	cpy := &Config{}
	cpy.RegisterFlags(flag.NewFlagSet("defaults", flag.ContinueOnError))
	return reflect.DeepEqual(cfg, cpy)
}

func (cfg *Config) Validate() error {
	if cfg.MaxCompactionParallelism < 1 {
		return errors.New("max compaction parallelism must be >= 1")
	}
	return shipper_util.ValidateSharedStoreKeyPrefix(cfg.SharedStoreKeyPrefix)
}

type Compactor struct {
	services.Service

	cfg                   Config
	indexStorageClient    shipper_storage.Client
	tableMarker           retention.TableMarker
	sweeper               *retention.Sweeper
	deleteRequestsStore   deletion.DeleteRequestsStore
	DeleteRequestsHandler *deletion.DeleteRequestHandler
	deleteRequestsManager *deletion.DeleteRequestsManager
	expirationChecker     retention.ExpirationChecker
	metrics               *metrics
}

func NewCompactor(cfg Config, storageConfig storage.Config, schemaConfig loki_storage.SchemaConfig, limits retention.Limits, r prometheus.Registerer) (*Compactor, error) {
	if cfg.IsDefaults() {
		return nil, errors.New("Must specify compactor config")
	}

	compactor := &Compactor{
		cfg: cfg,
	}

	if err := compactor.init(storageConfig, schemaConfig, limits, r); err != nil {
		return nil, err
	}

	compactor.Service = services.NewBasicService(nil, compactor.loop, nil)
	return compactor, nil
}

func (c *Compactor) init(storageConfig storage.Config, schemaConfig loki_storage.SchemaConfig, limits retention.Limits, r prometheus.Registerer) error {
	objectClient, err := storage.NewObjectClient(c.cfg.SharedStoreType, storageConfig)
	if err != nil {
		return err
	}

	err = chunk_util.EnsureDirectory(c.cfg.WorkingDirectory)
	if err != nil {
		return err
	}
	c.indexStorageClient = shipper_storage.NewIndexStorageClient(objectClient, c.cfg.SharedStoreKeyPrefix)
	c.metrics = newMetrics(r)

	if c.cfg.RetentionEnabled {
		var encoder objectclient.KeyEncoder
		if _, ok := objectClient.(*local.FSObjectClient); ok {
			encoder = objectclient.Base64Encoder
		}

		chunkClient := objectclient.NewClient(objectClient, encoder)

		retentionWorkDir := filepath.Join(c.cfg.WorkingDirectory, "retention")
		c.sweeper, err = retention.NewSweeper(retentionWorkDir, chunkClient, c.cfg.RetentionDeleteWorkCount, c.cfg.RetentionDeleteDelay, r)
		if err != nil {
			return err
		}

		deletionWorkDir := filepath.Join(c.cfg.WorkingDirectory, "deletion")

		c.deleteRequestsStore, err = deletion.NewDeleteStore(deletionWorkDir, c.indexStorageClient)
		if err != nil {
			return err
		}

		c.DeleteRequestsHandler = deletion.NewDeleteRequestHandler(c.deleteRequestsStore, time.Hour, r)
		c.deleteRequestsManager = deletion.NewDeleteRequestsManager(c.deleteRequestsStore, c.cfg.DeleteRequestCancelPeriod, r)

		c.expirationChecker = newExpirationChecker(retention.NewExpirationChecker(limits), c.deleteRequestsManager)

		c.tableMarker, err = retention.NewMarker(retentionWorkDir, schemaConfig, c.expirationChecker, chunkClient, r)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Compactor) loop(ctx context.Context) error {
	if c.cfg.RetentionEnabled {
		defer c.deleteRequestsStore.Stop()
		defer c.deleteRequestsManager.Stop()
	}

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
	table, err := newTable(ctx, filepath.Join(c.cfg.WorkingDirectory, tableName), c.indexStorageClient, c.cfg.RetentionEnabled, c.tableMarker)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to initialize table for compaction", "table", tableName, "err", err)
		return err
	}

	interval := retention.ExtractIntervalFromTableName(tableName)
	intervalHasExpiredChunks := false
	if c.cfg.RetentionEnabled {
		intervalHasExpiredChunks = c.expirationChecker.IntervalHasExpiredChunks(interval)
	}

	err = table.compact(intervalHasExpiredChunks)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to compact files", "table", tableName, "err", err)
		return err
	}
	return nil
}

func (c *Compactor) RunCompaction(ctx context.Context) error {
	status := statusSuccess
	start := time.Now()

	if c.cfg.RetentionEnabled {
		c.expirationChecker.MarkPhaseStarted()
	}

	defer func() {
		c.metrics.compactTablesOperationTotal.WithLabelValues(status).Inc()
		if status == statusSuccess {
			c.metrics.compactTablesOperationDurationSeconds.Set(time.Since(start).Seconds())
			c.metrics.compactTablesOperationLastSuccess.SetToCurrentTime()
		}

		if c.cfg.RetentionEnabled {
			if status == statusSuccess {
				c.expirationChecker.MarkPhaseFinished()
			} else {
				c.expirationChecker.MarkPhaseFailed()
			}
		}
	}()

	tables, err := c.indexStorageClient.ListTables(ctx)
	if err != nil {
		status = statusFailure
		return err
	}

	compactTablesChan := make(chan string)
	errChan := make(chan error)

	for i := 0; i < c.cfg.MaxCompactionParallelism; i++ {
		go func() {
			var err error
			defer func() {
				errChan <- err
			}()

			for {
				select {
				case tableName, ok := <-compactTablesChan:
					if !ok {
						return
					}

					level.Info(util_log.Logger).Log("msg", "compacting table", "table-name", tableName)
					err = c.CompactTable(ctx, tableName)
					if err != nil {
						return
					}
					level.Info(util_log.Logger).Log("msg", "finished compacting table", "table-name", tableName)
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		for _, tableName := range tables {
			if tableName == deletion.DeleteRequestsTableName {
				// we do not want to compact or apply retention on delete requests table
				continue
			}

			select {
			case compactTablesChan <- tableName:
			case <-ctx.Done():
				return
			}
		}

		close(compactTablesChan)
	}()

	var firstErr error
	// read all the errors
	for i := 0; i < c.cfg.MaxCompactionParallelism; i++ {
		err := <-errChan
		if err != nil && firstErr == nil {
			status = statusFailure
			firstErr = err
		}
	}

	return firstErr
}

type expirationChecker struct {
	retentionExpiryChecker retention.ExpirationChecker
	deletionExpiryChecker  retention.ExpirationChecker
}

func newExpirationChecker(retentionExpiryChecker, deletionExpiryChecker retention.ExpirationChecker) retention.ExpirationChecker {
	return &expirationChecker{retentionExpiryChecker, deletionExpiryChecker}
}

func (e *expirationChecker) Expired(ref retention.ChunkEntry, now model.Time) (bool, []model.Interval) {
	if expired, nonDeletedIntervals := e.retentionExpiryChecker.Expired(ref, now); expired {
		return expired, nonDeletedIntervals
	}

	return e.deletionExpiryChecker.Expired(ref, now)
}

func (e *expirationChecker) MarkPhaseStarted() {
	e.retentionExpiryChecker.MarkPhaseStarted()
	e.deletionExpiryChecker.MarkPhaseStarted()
}

func (e *expirationChecker) MarkPhaseFailed() {
	e.retentionExpiryChecker.MarkPhaseFailed()
	e.deletionExpiryChecker.MarkPhaseFailed()
}

func (e *expirationChecker) MarkPhaseFinished() {
	e.retentionExpiryChecker.MarkPhaseFinished()
	e.deletionExpiryChecker.MarkPhaseFinished()
}

func (e *expirationChecker) IntervalHasExpiredChunks(interval model.Interval) bool {
	return e.retentionExpiryChecker.IntervalHasExpiredChunks(interval) || e.deletionExpiryChecker.IntervalHasExpiredChunks(interval)
}

func (e *expirationChecker) DropFromIndex(ref retention.ChunkEntry, tableEndTime model.Time, now model.Time) bool {
	return e.retentionExpiryChecker.DropFromIndex(ref, tableEndTime, now) || e.deletionExpiryChecker.DropFromIndex(ref, tableEndTime, now)
}
