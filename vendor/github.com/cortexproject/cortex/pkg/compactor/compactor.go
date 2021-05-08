package compactor

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/compact"
	"github.com/thanos-io/thanos/pkg/compact/downsample"
	"github.com/thanos-io/thanos/pkg/objstore"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/storage/bucket"
	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/storage/tsdb/bucketindex"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/cortexproject/cortex/pkg/util/services"
)

const (
	blocksMarkedForDeletionName = "cortex_compactor_blocks_marked_for_deletion_total"
	blocksMarkedForDeletionHelp = "Total number of blocks marked for deletion in compactor."
)

var (
	errInvalidBlockRanges = "compactor block range periods should be divisible by the previous one, but %s is not divisible by %s"
	RingOp                = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, nil)

	DefaultBlocksGrouperFactory = func(ctx context.Context, cfg Config, bkt objstore.Bucket, logger log.Logger, reg prometheus.Registerer, blocksMarkedForDeletion prometheus.Counter, garbageCollectedBlocks prometheus.Counter) compact.Grouper {
		return compact.NewDefaultGrouper(
			logger,
			bkt,
			false, // Do not accept malformed indexes
			true,  // Enable vertical compaction
			reg,
			blocksMarkedForDeletion,
			garbageCollectedBlocks,
			metadata.NoneFunc)
	}

	DefaultBlocksCompactorFactory = func(ctx context.Context, cfg Config, logger log.Logger, reg prometheus.Registerer) (compact.Compactor, compact.Planner, error) {
		compactor, err := tsdb.NewLeveledCompactor(ctx, reg, logger, cfg.BlockRanges.ToMilliseconds(), downsample.NewPool())
		if err != nil {
			return nil, nil, err
		}

		planner := compact.NewTSDBBasedPlanner(logger, cfg.BlockRanges.ToMilliseconds())
		return compactor, planner, nil
	}
)

// BlocksGrouperFactory builds and returns the grouper to use to compact a tenant's blocks.
type BlocksGrouperFactory func(
	ctx context.Context,
	cfg Config,
	bkt objstore.Bucket,
	logger log.Logger,
	reg prometheus.Registerer,
	blocksMarkedForDeletion prometheus.Counter,
	garbageCollectedBlocks prometheus.Counter,
) compact.Grouper

// BlocksCompactorFactory builds and returns the compactor and planner to use to compact a tenant's blocks.
type BlocksCompactorFactory func(
	ctx context.Context,
	cfg Config,
	logger log.Logger,
	reg prometheus.Registerer,
) (compact.Compactor, compact.Planner, error)

// Config holds the Compactor config.
type Config struct {
	BlockRanges           cortex_tsdb.DurationList `yaml:"block_ranges"`
	BlockSyncConcurrency  int                      `yaml:"block_sync_concurrency"`
	MetaSyncConcurrency   int                      `yaml:"meta_sync_concurrency"`
	ConsistencyDelay      time.Duration            `yaml:"consistency_delay"`
	DataDir               string                   `yaml:"data_dir"`
	CompactionInterval    time.Duration            `yaml:"compaction_interval"`
	CompactionRetries     int                      `yaml:"compaction_retries"`
	CompactionConcurrency int                      `yaml:"compaction_concurrency"`
	CleanupInterval       time.Duration            `yaml:"cleanup_interval"`
	CleanupConcurrency    int                      `yaml:"cleanup_concurrency"`
	DeletionDelay         time.Duration            `yaml:"deletion_delay"`
	TenantCleanupDelay    time.Duration            `yaml:"tenant_cleanup_delay"`

	// Whether the migration of block deletion marks to the global markers location is enabled.
	BlockDeletionMarksMigrationEnabled bool `yaml:"block_deletion_marks_migration_enabled"`

	EnabledTenants  flagext.StringSliceCSV `yaml:"enabled_tenants"`
	DisabledTenants flagext.StringSliceCSV `yaml:"disabled_tenants"`

	// Compactors sharding.
	ShardingEnabled bool       `yaml:"sharding_enabled"`
	ShardingRing    RingConfig `yaml:"sharding_ring"`

	// No need to add options to customize the retry backoff,
	// given the defaults should be fine, but allow to override
	// it in tests.
	retryMinBackoff time.Duration `yaml:"-"`
	retryMaxBackoff time.Duration `yaml:"-"`

	// Allow downstream projects to customise the blocks compactor.
	BlocksGrouperFactory   BlocksGrouperFactory   `yaml:"-"`
	BlocksCompactorFactory BlocksCompactorFactory `yaml:"-"`
}

// RegisterFlags registers the Compactor flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.ShardingRing.RegisterFlags(f)

	cfg.BlockRanges = cortex_tsdb.DurationList{2 * time.Hour, 12 * time.Hour, 24 * time.Hour}
	cfg.retryMinBackoff = 10 * time.Second
	cfg.retryMaxBackoff = time.Minute

	f.Var(&cfg.BlockRanges, "compactor.block-ranges", "List of compaction time ranges.")
	f.DurationVar(&cfg.ConsistencyDelay, "compactor.consistency-delay", 0, fmt.Sprintf("Minimum age of fresh (non-compacted) blocks before they are being processed. Malformed blocks older than the maximum of consistency-delay and %s will be removed.", compact.PartialUploadThresholdAge))
	f.IntVar(&cfg.BlockSyncConcurrency, "compactor.block-sync-concurrency", 20, "Number of Go routines to use when syncing block index and chunks files from the long term storage.")
	f.IntVar(&cfg.MetaSyncConcurrency, "compactor.meta-sync-concurrency", 20, "Number of Go routines to use when syncing block meta files from the long term storage.")
	f.StringVar(&cfg.DataDir, "compactor.data-dir", "./data", "Data directory in which to cache blocks and process compactions")
	f.DurationVar(&cfg.CompactionInterval, "compactor.compaction-interval", time.Hour, "The frequency at which the compaction runs")
	f.IntVar(&cfg.CompactionRetries, "compactor.compaction-retries", 3, "How many times to retry a failed compaction within a single compaction run.")
	f.IntVar(&cfg.CompactionConcurrency, "compactor.compaction-concurrency", 1, "Max number of concurrent compactions running.")
	f.DurationVar(&cfg.CleanupInterval, "compactor.cleanup-interval", 15*time.Minute, "How frequently compactor should run blocks cleanup and maintenance, as well as update the bucket index.")
	f.IntVar(&cfg.CleanupConcurrency, "compactor.cleanup-concurrency", 20, "Max number of tenants for which blocks cleanup and maintenance should run concurrently.")
	f.BoolVar(&cfg.ShardingEnabled, "compactor.sharding-enabled", false, "Shard tenants across multiple compactor instances. Sharding is required if you run multiple compactor instances, in order to coordinate compactions and avoid race conditions leading to the same tenant blocks simultaneously compacted by different instances.")
	f.DurationVar(&cfg.DeletionDelay, "compactor.deletion-delay", 12*time.Hour, "Time before a block marked for deletion is deleted from bucket. "+
		"If not 0, blocks will be marked for deletion and compactor component will permanently delete blocks marked for deletion from the bucket. "+
		"If 0, blocks will be deleted straight away. Note that deleting blocks immediately can cause query failures.")
	f.DurationVar(&cfg.TenantCleanupDelay, "compactor.tenant-cleanup-delay", 6*time.Hour, "For tenants marked for deletion, this is time between deleting of last block, and doing final cleanup (marker files, debug files) of the tenant.")
	f.BoolVar(&cfg.BlockDeletionMarksMigrationEnabled, "compactor.block-deletion-marks-migration-enabled", true, "When enabled, at compactor startup the bucket will be scanned and all found deletion marks inside the block location will be copied to the markers global location too. This option can (and should) be safely disabled as soon as the compactor has successfully run at least once.")

	f.Var(&cfg.EnabledTenants, "compactor.enabled-tenants", "Comma separated list of tenants that can be compacted. If specified, only these tenants will be compacted by compactor, otherwise all tenants can be compacted. Subject to sharding.")
	f.Var(&cfg.DisabledTenants, "compactor.disabled-tenants", "Comma separated list of tenants that cannot be compacted by this compactor. If specified, and compactor would normally pick given tenant for compaction (via -compactor.enabled-tenants or sharding), it will be ignored instead.")
}

func (cfg *Config) Validate() error {
	// Each block range period should be divisible by the previous one.
	for i := 1; i < len(cfg.BlockRanges); i++ {
		if cfg.BlockRanges[i]%cfg.BlockRanges[i-1] != 0 {
			return errors.Errorf(errInvalidBlockRanges, cfg.BlockRanges[i].String(), cfg.BlockRanges[i-1].String())
		}
	}

	return nil
}

// ConfigProvider defines the per-tenant config provider for the Compactor.
type ConfigProvider interface {
	bucket.TenantConfigProvider
	CompactorBlocksRetentionPeriod(user string) time.Duration
}

// Compactor is a multi-tenant TSDB blocks compactor based on Thanos.
type Compactor struct {
	services.Service

	compactorCfg   Config
	storageCfg     cortex_tsdb.BlocksStorageConfig
	cfgProvider    ConfigProvider
	logger         log.Logger
	parentLogger   log.Logger
	registerer     prometheus.Registerer
	allowedTenants *util.AllowedTenants

	// Functions that creates bucket client, grouper, planner and compactor using the context.
	// Useful for injecting mock objects from tests.
	bucketClientFactory    func(ctx context.Context) (objstore.Bucket, error)
	blocksGrouperFactory   BlocksGrouperFactory
	blocksCompactorFactory BlocksCompactorFactory

	// Users scanner, used to discover users from the bucket.
	usersScanner *cortex_tsdb.UsersScanner

	// Blocks cleaner is responsible to hard delete blocks marked for deletion.
	blocksCleaner *BlocksCleaner

	// Underlying compactor and planner used to compact TSDB blocks.
	blocksCompactor compact.Compactor
	blocksPlanner   compact.Planner

	// Client used to run operations on the bucket storing blocks.
	bucketClient objstore.Bucket

	// Ring used for sharding compactions.
	ringLifecycler         *ring.Lifecycler
	ring                   *ring.Ring
	ringSubservices        *services.Manager
	ringSubservicesWatcher *services.FailureWatcher

	// Metrics.
	compactionRunsStarted          prometheus.Counter
	compactionRunsCompleted        prometheus.Counter
	compactionRunsFailed           prometheus.Counter
	compactionRunsLastSuccess      prometheus.Gauge
	compactionRunDiscoveredTenants prometheus.Gauge
	compactionRunSkippedTenants    prometheus.Gauge
	compactionRunSucceededTenants  prometheus.Gauge
	compactionRunFailedTenants     prometheus.Gauge
	compactionRunInterval          prometheus.Gauge
	blocksMarkedForDeletion        prometheus.Counter
	garbageCollectedBlocks         prometheus.Counter

	// TSDB syncer metrics
	syncerMetrics *syncerMetrics
}

// NewCompactor makes a new Compactor.
func NewCompactor(compactorCfg Config, storageCfg cortex_tsdb.BlocksStorageConfig, cfgProvider ConfigProvider, logger log.Logger, registerer prometheus.Registerer) (*Compactor, error) {
	bucketClientFactory := func(ctx context.Context) (objstore.Bucket, error) {
		return bucket.NewClient(ctx, storageCfg.Bucket, "compactor", logger, registerer)
	}

	blocksGrouperFactory := compactorCfg.BlocksGrouperFactory
	if blocksGrouperFactory == nil {
		blocksGrouperFactory = DefaultBlocksGrouperFactory
	}

	blocksCompactorFactory := compactorCfg.BlocksCompactorFactory
	if blocksCompactorFactory == nil {
		blocksCompactorFactory = DefaultBlocksCompactorFactory
	}

	cortexCompactor, err := newCompactor(compactorCfg, storageCfg, cfgProvider, logger, registerer, bucketClientFactory, blocksGrouperFactory, blocksCompactorFactory)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create Cortex blocks compactor")
	}

	return cortexCompactor, nil
}

func newCompactor(
	compactorCfg Config,
	storageCfg cortex_tsdb.BlocksStorageConfig,
	cfgProvider ConfigProvider,
	logger log.Logger,
	registerer prometheus.Registerer,
	bucketClientFactory func(ctx context.Context) (objstore.Bucket, error),
	blocksGrouperFactory BlocksGrouperFactory,
	blocksCompactorFactory BlocksCompactorFactory,
) (*Compactor, error) {
	c := &Compactor{
		compactorCfg:           compactorCfg,
		storageCfg:             storageCfg,
		cfgProvider:            cfgProvider,
		parentLogger:           logger,
		logger:                 log.With(logger, "component", "compactor"),
		registerer:             registerer,
		syncerMetrics:          newSyncerMetrics(registerer),
		bucketClientFactory:    bucketClientFactory,
		blocksGrouperFactory:   blocksGrouperFactory,
		blocksCompactorFactory: blocksCompactorFactory,
		allowedTenants:         util.NewAllowedTenants(compactorCfg.EnabledTenants, compactorCfg.DisabledTenants),

		compactionRunsStarted: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_runs_started_total",
			Help: "Total number of compaction runs started.",
		}),
		compactionRunsCompleted: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_runs_completed_total",
			Help: "Total number of compaction runs successfully completed.",
		}),
		compactionRunsFailed: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_runs_failed_total",
			Help: "Total number of compaction runs failed.",
		}),
		compactionRunsLastSuccess: promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_compactor_last_successful_run_timestamp_seconds",
			Help: "Unix timestamp of the last successful compaction run.",
		}),
		compactionRunDiscoveredTenants: promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_compactor_tenants_discovered",
			Help: "Number of tenants discovered during the current compaction run. Reset to 0 when compactor is idle.",
		}),
		compactionRunSkippedTenants: promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_compactor_tenants_skipped",
			Help: "Number of tenants skipped during the current compaction run. Reset to 0 when compactor is idle.",
		}),
		compactionRunSucceededTenants: promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_compactor_tenants_processing_succeeded",
			Help: "Number of tenants successfully processed during the current compaction run. Reset to 0 when compactor is idle.",
		}),
		compactionRunFailedTenants: promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_compactor_tenants_processing_failed",
			Help: "Number of tenants failed processing during the current compaction run. Reset to 0 when compactor is idle.",
		}),
		compactionRunInterval: promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_compactor_compaction_interval_seconds",
			Help: "The configured interval on which compaction is run in seconds. Useful when compared to the last successful run metric to accurately detect multiple failed compaction runs.",
		}),
		blocksMarkedForDeletion: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Name:        blocksMarkedForDeletionName,
			Help:        blocksMarkedForDeletionHelp,
			ConstLabels: prometheus.Labels{"reason": "compaction"},
		}),
		garbageCollectedBlocks: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_garbage_collected_blocks_total",
			Help: "Total number of blocks marked for deletion by compactor.",
		}),
	}

	if len(compactorCfg.EnabledTenants) > 0 {
		level.Info(c.logger).Log("msg", "compactor using enabled users", "enabled", strings.Join(compactorCfg.EnabledTenants, ", "))
	}
	if len(compactorCfg.DisabledTenants) > 0 {
		level.Info(c.logger).Log("msg", "compactor using disabled users", "disabled", strings.Join(compactorCfg.DisabledTenants, ", "))
	}

	c.Service = services.NewBasicService(c.starting, c.running, c.stopping)

	// The last successful compaction run metric is exposed as seconds since epoch, so we need to use seconds for this metric.
	c.compactionRunInterval.Set(c.compactorCfg.CompactionInterval.Seconds())

	return c, nil
}

// Start the compactor.
func (c *Compactor) starting(ctx context.Context) error {
	var err error

	// Create bucket client.
	c.bucketClient, err = c.bucketClientFactory(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to create bucket client")
	}

	// Create blocks compactor dependencies.
	c.blocksCompactor, c.blocksPlanner, err = c.blocksCompactorFactory(ctx, c.compactorCfg, c.logger, c.registerer)
	if err != nil {
		return errors.Wrap(err, "failed to initialize compactor dependencies")
	}

	// Wrap the bucket client to write block deletion marks in the global location too.
	c.bucketClient = bucketindex.BucketWithGlobalMarkers(c.bucketClient)

	// Create the users scanner.
	c.usersScanner = cortex_tsdb.NewUsersScanner(c.bucketClient, c.ownUser, c.parentLogger)

	// Create the blocks cleaner (service).
	c.blocksCleaner = NewBlocksCleaner(BlocksCleanerConfig{
		DeletionDelay:                      c.compactorCfg.DeletionDelay,
		CleanupInterval:                    util.DurationWithJitter(c.compactorCfg.CleanupInterval, 0.1),
		CleanupConcurrency:                 c.compactorCfg.CleanupConcurrency,
		BlockDeletionMarksMigrationEnabled: c.compactorCfg.BlockDeletionMarksMigrationEnabled,
		TenantCleanupDelay:                 c.compactorCfg.TenantCleanupDelay,
	}, c.bucketClient, c.usersScanner, c.cfgProvider, c.parentLogger, c.registerer)

	// Initialize the compactors ring if sharding is enabled.
	if c.compactorCfg.ShardingEnabled {
		lifecyclerCfg := c.compactorCfg.ShardingRing.ToLifecyclerConfig()
		c.ringLifecycler, err = ring.NewLifecycler(lifecyclerCfg, ring.NewNoopFlushTransferer(), "compactor", ring.CompactorRingKey, false, c.registerer)
		if err != nil {
			return errors.Wrap(err, "unable to initialize compactor ring lifecycler")
		}

		c.ring, err = ring.New(lifecyclerCfg.RingConfig, "compactor", ring.CompactorRingKey, c.registerer)
		if err != nil {
			return errors.Wrap(err, "unable to initialize compactor ring")
		}

		c.ringSubservices, err = services.NewManager(c.ringLifecycler, c.ring)
		if err == nil {
			c.ringSubservicesWatcher = services.NewFailureWatcher()
			c.ringSubservicesWatcher.WatchManager(c.ringSubservices)

			err = services.StartManagerAndAwaitHealthy(ctx, c.ringSubservices)
		}

		if err != nil {
			return errors.Wrap(err, "unable to start compactor ring dependencies")
		}

		// If sharding is enabled we should wait until this instance is
		// ACTIVE within the ring. This MUST be done before starting the
		// any other component depending on the users scanner, because the
		// users scanner depends on the ring (to check whether an user belongs
		// to this shard or not).
		level.Info(c.logger).Log("msg", "waiting until compactor is ACTIVE in the ring")
		if err := ring.WaitInstanceState(ctx, c.ring, c.ringLifecycler.ID, ring.ACTIVE); err != nil {
			return err
		}
		level.Info(c.logger).Log("msg", "compactor is ACTIVE in the ring")

		// In the event of a cluster cold start or scale up of 2+ compactor instances at the same
		// time, we may end up in a situation where each new compactor instance starts at a slightly
		// different time and thus each one starts with on a different state of the ring. It's better
		// to just wait the ring stability for a short time.
		if c.compactorCfg.ShardingRing.WaitStabilityMinDuration > 0 {
			minWaiting := c.compactorCfg.ShardingRing.WaitStabilityMinDuration
			maxWaiting := c.compactorCfg.ShardingRing.WaitStabilityMaxDuration

			level.Info(c.logger).Log("msg", "waiting until compactor ring topology is stable", "min_waiting", minWaiting.String(), "max_waiting", maxWaiting.String())
			if err := ring.WaitRingStability(ctx, c.ring, RingOp, minWaiting, maxWaiting); err != nil {
				level.Warn(c.logger).Log("msg", "compactor is ring topology is not stable after the max waiting time, proceeding anyway")
			} else {
				level.Info(c.logger).Log("msg", "compactor is ring topology is stable")
			}
		}
	}

	// Ensure an initial cleanup occurred before starting the compactor.
	if err := services.StartAndAwaitRunning(ctx, c.blocksCleaner); err != nil {
		c.ringSubservices.StopAsync()
		return errors.Wrap(err, "failed to start the blocks cleaner")
	}

	return nil
}

func (c *Compactor) stopping(_ error) error {
	ctx := context.Background()

	services.StopAndAwaitTerminated(ctx, c.blocksCleaner) //nolint:errcheck
	if c.ringSubservices != nil {
		return services.StopManagerAndAwaitStopped(ctx, c.ringSubservices)
	}
	return nil
}

func (c *Compactor) running(ctx context.Context) error {
	// Run an initial compaction before starting the interval.
	c.compactUsers(ctx)

	ticker := time.NewTicker(util.DurationWithJitter(c.compactorCfg.CompactionInterval, 0.05))
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.compactUsers(ctx)
		case <-ctx.Done():
			return nil
		case err := <-c.ringSubservicesWatcher.Chan():
			return errors.Wrap(err, "compactor subservice failed")
		}
	}
}

func (c *Compactor) compactUsers(ctx context.Context) {
	succeeded := false
	compactionErrorCount := 0

	c.compactionRunsStarted.Inc()

	defer func() {
		if succeeded && compactionErrorCount == 0 {
			c.compactionRunsCompleted.Inc()
			c.compactionRunsLastSuccess.SetToCurrentTime()
		} else {
			c.compactionRunsFailed.Inc()
		}

		// Reset progress metrics once done.
		c.compactionRunDiscoveredTenants.Set(0)
		c.compactionRunSkippedTenants.Set(0)
		c.compactionRunSucceededTenants.Set(0)
		c.compactionRunFailedTenants.Set(0)
	}()

	level.Info(c.logger).Log("msg", "discovering users from bucket")
	users, err := c.discoverUsersWithRetries(ctx)
	if err != nil {
		level.Error(c.logger).Log("msg", "failed to discover users from bucket", "err", err)
		return
	}

	level.Info(c.logger).Log("msg", "discovered users from bucket", "users", len(users))
	c.compactionRunDiscoveredTenants.Set(float64(len(users)))

	// When starting multiple compactor replicas nearly at the same time, running in a cluster with
	// a large number of tenants, we may end up in a situation where the 1st user is compacted by
	// multiple replicas at the same time. Shuffling users helps reduce the likelihood this will happen.
	rand.Shuffle(len(users), func(i, j int) {
		users[i], users[j] = users[j], users[i]
	})

	// Keep track of users owned by this shard, so that we can delete the local files for all other users.
	ownedUsers := map[string]struct{}{}
	for _, userID := range users {
		// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
		if ctx.Err() != nil {
			level.Info(c.logger).Log("msg", "interrupting compaction of user blocks", "err", err)
			return
		}

		// Ensure the user ID belongs to our shard.
		if owned, err := c.ownUser(userID); err != nil {
			c.compactionRunSkippedTenants.Inc()
			level.Warn(c.logger).Log("msg", "unable to check if user is owned by this shard", "user", userID, "err", err)
			continue
		} else if !owned {
			c.compactionRunSkippedTenants.Inc()
			level.Debug(c.logger).Log("msg", "skipping user because it is not owned by this shard", "user", userID)
			continue
		}

		ownedUsers[userID] = struct{}{}

		if markedForDeletion, err := cortex_tsdb.TenantDeletionMarkExists(ctx, c.bucketClient, userID); err != nil {
			c.compactionRunSkippedTenants.Inc()
			level.Warn(c.logger).Log("msg", "unable to check if user is marked for deletion", "user", userID, "err", err)
			continue
		} else if markedForDeletion {
			c.compactionRunSkippedTenants.Inc()
			level.Debug(c.logger).Log("msg", "skipping user because it is marked for deletion", "user", userID)
			continue
		}

		level.Info(c.logger).Log("msg", "starting compaction of user blocks", "user", userID)

		if err = c.compactUserWithRetries(ctx, userID); err != nil {
			c.compactionRunFailedTenants.Inc()
			compactionErrorCount++
			level.Error(c.logger).Log("msg", "failed to compact user blocks", "user", userID, "err", err)
			continue
		}

		c.compactionRunSucceededTenants.Inc()
		level.Info(c.logger).Log("msg", "successfully compacted user blocks", "user", userID)
	}

	// Delete local files for unowned tenants, if there are any. This cleans up
	// leftover local files for tenants that belong to different compactors now,
	// or have been deleted completely.
	for userID := range c.listTenantsWithMetaSyncDirectories() {
		if _, owned := ownedUsers[userID]; owned {
			continue
		}

		dir := c.metaSyncDirForUser(userID)
		s, err := os.Stat(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				level.Warn(c.logger).Log("msg", "failed to stat local directory with user data", "dir", dir, "err", err)
			}
			continue
		}

		if s.IsDir() {
			err := os.RemoveAll(dir)
			if err == nil {
				level.Info(c.logger).Log("msg", "deleted directory for user not owned by this shard", "dir", dir)
			} else {
				level.Warn(c.logger).Log("msg", "failed to delete directory for user not owned by this shard", "dir", dir, "err", err)
			}
		}
	}

	succeeded = true
}

func (c *Compactor) compactUserWithRetries(ctx context.Context, userID string) error {
	var lastErr error

	retries := util.NewBackoff(ctx, util.BackoffConfig{
		MinBackoff: c.compactorCfg.retryMinBackoff,
		MaxBackoff: c.compactorCfg.retryMaxBackoff,
		MaxRetries: c.compactorCfg.CompactionRetries,
	})

	for retries.Ongoing() {
		lastErr = c.compactUser(ctx, userID)
		if lastErr == nil {
			return nil
		}

		retries.Wait()
	}

	return lastErr
}

func (c *Compactor) compactUser(ctx context.Context, userID string) error {
	bucket := bucket.NewUserBucketClient(userID, c.bucketClient, c.cfgProvider)
	reg := prometheus.NewRegistry()
	defer c.syncerMetrics.gatherThanosSyncerMetrics(reg)

	ulogger := util_log.WithUserID(userID, c.logger)

	// Filters out duplicate blocks that can be formed from two or more overlapping
	// blocks that fully submatches the source blocks of the older blocks.
	deduplicateBlocksFilter := block.NewDeduplicateFilter()

	// While fetching blocks, we filter out blocks that were marked for deletion by using IgnoreDeletionMarkFilter.
	// The delay of deleteDelay/2 is added to ensure we fetch blocks that are meant to be deleted but do not have a replacement yet.
	ignoreDeletionMarkFilter := block.NewIgnoreDeletionMarkFilter(
		ulogger,
		bucket,
		time.Duration(c.compactorCfg.DeletionDelay.Seconds()/2)*time.Second,
		c.compactorCfg.MetaSyncConcurrency)

	fetcher, err := block.NewMetaFetcher(
		ulogger,
		c.compactorCfg.MetaSyncConcurrency,
		bucket,
		c.metaSyncDirForUser(userID),
		reg,
		// List of filters to apply (order matters).
		[]block.MetadataFilter{
			// Remove the ingester ID because we don't shard blocks anymore, while still
			// honoring the shard ID if sharding was done in the past.
			NewLabelRemoverFilter([]string{cortex_tsdb.IngesterIDExternalLabel}),
			block.NewConsistencyDelayMetaFilter(ulogger, c.compactorCfg.ConsistencyDelay, reg),
			ignoreDeletionMarkFilter,
			deduplicateBlocksFilter,
		},
		nil,
	)
	if err != nil {
		return err
	}

	syncer, err := compact.NewMetaSyncer(
		ulogger,
		reg,
		bucket,
		fetcher,
		deduplicateBlocksFilter,
		ignoreDeletionMarkFilter,
		c.blocksMarkedForDeletion,
		c.garbageCollectedBlocks,
		c.compactorCfg.BlockSyncConcurrency,
	)
	if err != nil {
		return errors.Wrap(err, "failed to create syncer")
	}

	compactor, err := compact.NewBucketCompactor(
		ulogger,
		syncer,
		c.blocksGrouperFactory(ctx, c.compactorCfg, bucket, ulogger, reg, c.blocksMarkedForDeletion, c.garbageCollectedBlocks),
		c.blocksPlanner,
		c.blocksCompactor,
		path.Join(c.compactorCfg.DataDir, "compact"),
		bucket,
		c.compactorCfg.CompactionConcurrency,
	)
	if err != nil {
		return errors.Wrap(err, "failed to create bucket compactor")
	}

	if err := compactor.Compact(ctx); err != nil {
		return errors.Wrap(err, "compaction")
	}

	return nil
}

func (c *Compactor) discoverUsersWithRetries(ctx context.Context) ([]string, error) {
	var lastErr error

	retries := util.NewBackoff(ctx, util.BackoffConfig{
		MinBackoff: c.compactorCfg.retryMinBackoff,
		MaxBackoff: c.compactorCfg.retryMaxBackoff,
		MaxRetries: c.compactorCfg.CompactionRetries,
	})

	for retries.Ongoing() {
		var users []string

		users, lastErr = c.discoverUsers(ctx)
		if lastErr == nil {
			return users, nil
		}

		retries.Wait()
	}

	return nil, lastErr
}

func (c *Compactor) discoverUsers(ctx context.Context) ([]string, error) {
	var users []string

	err := c.bucketClient.Iter(ctx, "", func(entry string) error {
		users = append(users, strings.TrimSuffix(entry, "/"))
		return nil
	})

	return users, err
}

func (c *Compactor) ownUser(userID string) (bool, error) {
	if !c.allowedTenants.IsAllowed(userID) {
		return false, nil
	}

	// Always owned if sharding is disabled.
	if !c.compactorCfg.ShardingEnabled {
		return true, nil
	}

	// Hash the user ID.
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(userID))
	userHash := hasher.Sum32()

	// Check whether this compactor instance owns the user.
	rs, err := c.ring.Get(userHash, RingOp, nil, nil, nil)
	if err != nil {
		return false, err
	}

	if len(rs.Instances) != 1 {
		return false, fmt.Errorf("unexpected number of compactors in the shard (expected 1, got %d)", len(rs.Instances))
	}

	return rs.Instances[0].Addr == c.ringLifecycler.Addr, nil
}

const compactorMetaPrefix = "compactor-meta-"

// metaSyncDirForUser returns directory to store cached meta files.
// The fetcher stores cached metas in the "meta-syncer/" sub directory,
// but we prefix it with "compactor-meta-" in order to guarantee no clashing with
// the directory used by the Thanos Syncer, whatever is the user ID.
func (c *Compactor) metaSyncDirForUser(userID string) string {
	return filepath.Join(c.compactorCfg.DataDir, compactorMetaPrefix+userID)
}

// This function returns tenants with meta sync directories found on local disk. On error, it returns nil map.
func (c *Compactor) listTenantsWithMetaSyncDirectories() map[string]struct{} {
	result := map[string]struct{}{}

	files, err := ioutil.ReadDir(c.compactorCfg.DataDir)
	if err != nil {
		return nil
	}

	for _, f := range files {
		if !f.IsDir() {
			continue
		}

		if !strings.HasPrefix(f.Name(), compactorMetaPrefix) {
			continue
		}

		result[f.Name()[len(compactorMetaPrefix):]] = struct{}{}
	}

	return result
}
