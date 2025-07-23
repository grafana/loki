package compactor

import (
	"flag"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"github.com/grafana/dskit/backoff"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/compactor/jobqueue"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util/log"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	lokiring "github.com/grafana/loki/v3/pkg/util/ring"
)

type Config struct {
	WorkingDirectory               string                `yaml:"working_directory"`
	CompactionInterval             time.Duration         `yaml:"compaction_interval"`
	ApplyRetentionInterval         time.Duration         `yaml:"apply_retention_interval"`
	RetentionEnabled               bool                  `yaml:"retention_enabled"`
	RetentionDeleteDelay           time.Duration         `yaml:"retention_delete_delay"`
	RetentionDeleteWorkCount       int                   `yaml:"retention_delete_worker_count"`
	RetentionTableTimeout          time.Duration         `yaml:"retention_table_timeout"`
	RetentionBackoffConfig         backoff.Config        `yaml:"retention_backoff_config"`
	DeleteRequestStore             string                `yaml:"delete_request_store"`
	DeleteRequestStoreKeyPrefix    string                `yaml:"delete_request_store_key_prefix"`
	DeleteRequestStoreDBType       string                `yaml:"delete_request_store_db_type"`
	BackupDeleteRequestStoreDBType string                `yaml:"backup_delete_request_store_db_type"`
	DeleteBatchSize                int                   `yaml:"delete_batch_size"`
	DeleteRequestCancelPeriod      time.Duration         `yaml:"delete_request_cancel_period"`
	DeleteMaxInterval              time.Duration         `yaml:"delete_max_interval"`
	MaxCompactionParallelism       int                   `yaml:"max_compaction_parallelism"`
	UploadParallelism              int                   `yaml:"upload_parallelism"`
	CompactorRing                  lokiring.RingConfig   `yaml:"compactor_ring,omitempty" doc:"description=The hash ring configuration used by compactors to elect a single instance for running compactions. The CLI flags prefix for this block config is: compactor.ring"`
	RunOnce                        bool                  `yaml:"_" doc:"hidden"`
	TablesToCompact                int                   `yaml:"tables_to_compact"`
	SkipLatestNTables              int                   `yaml:"skip_latest_n_tables"`
	HorizontalScalingMode          string                `yaml:"horizontal_scaling_mode"`
	WorkerConfig                   jobqueue.WorkerConfig `yaml:"worker_config"`
	JobsConfig                     JobsConfig            `yaml:"jobs_config"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.WorkingDirectory, "compactor.working-directory", "/var/loki/compactor", "Directory where files can be downloaded for compaction.")
	f.DurationVar(&cfg.CompactionInterval, "compactor.compaction-interval", 10*time.Minute, "Interval at which to re-run the compaction operation.")
	f.DurationVar(&cfg.ApplyRetentionInterval, "compactor.apply-retention-interval", 0, "Interval at which to apply/enforce retention. 0 means run at same interval as compaction. If non-zero, it should always be a multiple of compaction interval.")
	f.DurationVar(&cfg.RetentionDeleteDelay, "compactor.retention-delete-delay", 2*time.Hour, "Delay after which chunks will be fully deleted during retention.")
	f.BoolVar(&cfg.RetentionEnabled, "compactor.retention-enabled", false, "Activate custom (per-stream,per-tenant) retention.")
	f.IntVar(&cfg.RetentionDeleteWorkCount, "compactor.retention-delete-worker-count", 150, "The total amount of worker to use to delete chunks.")
	f.StringVar(&cfg.DeleteRequestStore, "compactor.delete-request-store", "", "Store used for managing delete requests.")
	f.StringVar(&cfg.DeleteRequestStoreKeyPrefix, "compactor.delete-request-store.key-prefix", "index/", "Path prefix for storing delete requests.")
	f.StringVar(&cfg.DeleteRequestStoreDBType, "compactor.delete-request-store.db-type", string(deletion.DeleteRequestsStoreDBTypeBoltDB), fmt.Sprintf("Type of DB to use for storing delete requests. Supported types: %s", strings.Join(*(*[]string)(unsafe.Pointer(&deletion.SupportedDeleteRequestsStoreDBTypes)), ", ")))
	f.StringVar(&cfg.BackupDeleteRequestStoreDBType, "compactor.delete-request-store.backup-db-type", "", fmt.Sprintf("Type of DB to use as backup for storing delete requests. Backup DB should ideally be used while migrating from one DB type to another. Supported type(s): %s", deletion.DeleteRequestsStoreDBTypeBoltDB))
	f.IntVar(&cfg.DeleteBatchSize, "compactor.delete-batch-size", 70, "The max number of delete requests to run per compaction cycle.")
	f.DurationVar(&cfg.DeleteRequestCancelPeriod, "compactor.delete-request-cancel-period", 24*time.Hour, "Allow cancellation of delete request until duration after they are created. Data would be deleted only after delete requests have been older than this duration. Ideally this should be set to at least 24h.")
	f.DurationVar(&cfg.DeleteMaxInterval, "compactor.delete-max-interval", 24*time.Hour, "Constrain the size of any single delete request with line filters. When a delete request > delete_max_interval is input, the request is sharded into smaller requests of no more than delete_max_interval")
	f.DurationVar(&cfg.RetentionTableTimeout, "compactor.retention-table-timeout", 0, "The maximum amount of time to spend running retention and deletion on any given table in the index.")
	f.IntVar(&cfg.MaxCompactionParallelism, "compactor.max-compaction-parallelism", 1, "Maximum number of tables to compact in parallel. While increasing this value, please make sure compactor has enough disk space allocated to be able to store and compact as many tables.")
	f.IntVar(&cfg.UploadParallelism, "compactor.upload-parallelism", 10, "Number of upload/remove operations to execute in parallel when finalizing a compaction. NOTE: This setting is per compaction operation, which can be executed in parallel. The upper bound on the number of concurrent uploads is upload_parallelism * max_compaction_parallelism.")
	f.BoolVar(&cfg.RunOnce, "compactor.run-once", false, "Run the compactor one time to cleanup and compact index files only (no retention applied)")
	f.IntVar(&cfg.TablesToCompact, "compactor.tables-to-compact", 0, "Number of tables that compactor will try to compact. Newer tables are chosen when this is less than the number of tables available.")
	f.IntVar(&cfg.SkipLatestNTables, "compactor.skip-latest-n-tables", 0, "Do not compact N latest tables. Together with -compactor.run-once and -compactor.tables-to-compact, this is useful when clearing compactor backlogs.")

	cfg.RetentionBackoffConfig.RegisterFlagsWithPrefix("compactor.retention-backoff-config", f)
	// Ring
	skipFlags := []string{
		"compactor.ring.num-tokens",
		"compactor.ring.replication-factor",
	}
	cfg.CompactorRing.RegisterFlagsWithPrefix("compactor.", "collectors/", f, skipFlags...)
	f.IntVar(&cfg.CompactorRing.NumTokens, "compactor.ring.num-tokens", ringNumTokens, fmt.Sprintf("IGNORED: Num tokens is fixed to %d", ringNumTokens))
	f.IntVar(&cfg.CompactorRing.ReplicationFactor, "compactor.ring.replication-factor", ringReplicationFactor, fmt.Sprintf("IGNORED: Replication factor is fixed to %d", ringReplicationFactor))
	f.StringVar(&cfg.HorizontalScalingMode, "compactor.horizontal-scaling-mode", HorizontalScalingModeDisabled, fmt.Sprintf(
		`Experimental: Configuration to turn on and run horizontally scalable compactor. Supported modes -
	[%s]: Keeps the horizontal scaling mode disabled. Locally runs all the functions of the compactor.
	[%s]: Runs all functions of the compactor. Distributes work to workers where possible.
	[%s]: Runs the compactor in worker mode, only working on jobs built by the main compactor.`,
		HorizontalScalingModeDisabled, HorizontalScalingModeMain, HorizontalScalingModeWorker))
	cfg.WorkerConfig.RegisterFlagsWithPrefix("compactor.worker.", f)
	cfg.JobsConfig.RegisterFlagsWithPrefix("compactor.jobs.", f)
}

// Validate verifies the config does not contain inappropriate values
func (cfg *Config) Validate() error {
	if cfg.HorizontalScalingMode != HorizontalScalingModeDisabled {
		log.WarnExperimentalUse("Horizontally Scalable Compactor", util_log.Logger)
	}
	if cfg.MaxCompactionParallelism < 1 {
		return errors.New("max compaction parallelism must be >= 1")
	}

	if cfg.CompactorRing.NumTokens != ringNumTokens {
		return errors.New("Num tokens must not be changed as it will not take effect")
	}

	if cfg.CompactorRing.ReplicationFactor != ringReplicationFactor {
		return errors.New("Replication factor must not be changed as it will not take effect")
	}

	if cfg.RetentionEnabled {
		if cfg.DeleteRequestStore == "" {
			return fmt.Errorf("compactor.delete-request-store should be configured when retention is enabled")
		}

		if cfg.ApplyRetentionInterval == 0 {
			cfg.ApplyRetentionInterval = cfg.CompactionInterval
		}

		if cfg.ApplyRetentionInterval == cfg.CompactionInterval {
			// add some jitter to avoid running retention and compaction at same time
			cfg.ApplyRetentionInterval += min(10*time.Minute, cfg.ApplyRetentionInterval/2)
		}

		if err := config.ValidatePathPrefix(cfg.DeleteRequestStoreKeyPrefix); err != nil {
			return fmt.Errorf("validate delete store path prefix: %w", err)
		}
	}

	return cfg.WorkerConfig.Validate()
}

type JobsConfig struct {
	Deletion DeletionJobsConfig `yaml:"deletion"`
}

func (c *JobsConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	c.Deletion.RegisterFlagsWithPrefix(prefix+"deletion.", f)
}

func (c *JobsConfig) RegisterFlags(f *flag.FlagSet) {
	c.Deletion.RegisterFlagsWithPrefix("deletion.", f)
}

type DeletionJobsConfig struct {
	DeletionManifestStorePrefix string        `yaml:"deletion_manifest_store_prefix"`
	ChunkProcessingConcurrency  int           `yaml:"chunk_processing_concurrency"`
	Timeout                     time.Duration `yaml:"timeout"`
	MaxRetries                  int           `yaml:"max_retries"`
}

func (c *DeletionJobsConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&c.DeletionManifestStorePrefix, prefix+"deletion-manifest-store-prefix", "__deletion_manifest__/", "Object storage path prefix for storing deletion manifests.")
	f.IntVar(&c.ChunkProcessingConcurrency, prefix+"chunk-processing-concurrency", 3, "Maximum number of chunks to process concurrently in each worker.")
	f.DurationVar(&c.Timeout, prefix+"timeout", 15*time.Minute, "Maximum time to wait for a job before considering it failed and retrying.")
	f.IntVar(&c.MaxRetries, prefix+"max-retries", 3, "Maximum number of times to retry a failed or timed out job.")
}

func (c *DeletionJobsConfig) RegisterFlags(f *flag.FlagSet) {
	c.RegisterFlagsWithPrefix("", f)
}
