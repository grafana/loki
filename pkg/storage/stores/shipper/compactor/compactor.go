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
	"github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/cortexproject/cortex/pkg/chunk/objectclient"
	"github.com/cortexproject/cortex/pkg/chunk/storage"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

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
	deleteRequestsStore   deletion.DeleteRequestsStore
	DeleteRequestsHandler *deletion.DeleteRequestHandler
	deleteRequestsManager *deletion.DeleteRequestsManager
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
	c.objectClient = util.NewPrefixedObjectClient(objectClient, c.cfg.SharedStoreKeyPrefix)
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

		c.deleteRequestsStore, err = deletion.NewDeleteStore(deletionWorkDir, c.objectClient)
		if err != nil {
			return err
		}

		c.DeleteRequestsHandler = deletion.NewDeleteRequestHandler(c.deleteRequestsStore, time.Hour, r)
		c.deleteRequestsManager = deletion.NewDeleteRequestsManager(c.deleteRequestsStore, c.cfg.DeleteRequestCancelPeriod, r)

		expirationChecker := newExpirationChecker(retention.NewExpirationChecker(limits), c.deleteRequestsManager)

		c.tableMarker, err = retention.NewMarker(retentionWorkDir, schemaConfig, expirationChecker, chunkClient, r)
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

	if c.cfg.RetentionEnabled {
		c.deleteRequestsManager.MarkPhaseStarted()
	}

	defer func() {
		c.metrics.compactTablesOperationTotal.WithLabelValues(status).Inc()
		dmCallback := c.deleteRequestsManager.MarkPhaseFailed
		if status == statusSuccess {
			dmCallback = c.deleteRequestsManager.MarkPhaseFinished
			c.metrics.compactTablesOperationDurationSeconds.Set(time.Since(start).Seconds())
			c.metrics.compactTablesOperationLastSuccess.SetToCurrentTime()
		}

		if c.cfg.RetentionEnabled {
			dmCallback()
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
		if tableName == deletion.DeleteRequestsTableName {
			// we do not want to compact or apply retention on delete requests table
			continue
		}
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
