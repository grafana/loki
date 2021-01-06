package compactor

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"math/rand"
	"path"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/tsdb"
	tsdb_errors "github.com/prometheus/prometheus/tsdb/errors"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/compact"
	"github.com/thanos-io/thanos/pkg/compact/downsample"
	"github.com/thanos-io/thanos/pkg/objstore"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/storage/bucket"
	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/storage/tsdb/bucketindex"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/services"
)

var (
	errInvalidBlockRanges = "compactor block range periods should be divisible by the previous one, but %s is not divisible by %s"
)

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
	CleanupConcurrency    int                      `yaml:"cleanup_concurrency"`
	DeletionDelay         time.Duration            `yaml:"deletion_delay"`

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
	f.IntVar(&cfg.CompactionRetries, "compactor.compaction-retries", 3, "How many times to retry a failed compaction during a single compaction interval")
	f.IntVar(&cfg.CompactionConcurrency, "compactor.compaction-concurrency", 1, "Max number of concurrent compactions running.")
	f.IntVar(&cfg.CleanupConcurrency, "compactor.cleanup-concurrency", 20, "Max number of tenants for which blocks should be cleaned up concurrently (deletion of blocks previously marked for deletion).")
	f.BoolVar(&cfg.ShardingEnabled, "compactor.sharding-enabled", false, "Shard tenants across multiple compactor instances. Sharding is required if you run multiple compactor instances, in order to coordinate compactions and avoid race conditions leading to the same tenant blocks simultaneously compacted by different instances.")
	f.DurationVar(&cfg.DeletionDelay, "compactor.deletion-delay", 12*time.Hour, "Time before a block marked for deletion is deleted from bucket. "+
		"If not 0, blocks will be marked for deletion and compactor component will delete blocks marked for deletion from the bucket. "+
		"If delete-delay is 0, blocks will be deleted straight away. Note that deleting blocks immediately can cause query failures, "+
		"if store gateway still has the block loaded, or compactor is ignoring the deletion because it's compacting the block at the same time.")

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

// Compactor is a multi-tenant TSDB blocks compactor based on Thanos.
type Compactor struct {
	services.Service

	compactorCfg Config
	storageCfg   cortex_tsdb.BlocksStorageConfig
	logger       log.Logger
	parentLogger log.Logger
	registerer   prometheus.Registerer

	// If empty, all users are enabled. If not empty, only users in the map are enabled (possibly owned by compactor, also subject to sharding configuration).
	enabledUsers map[string]struct{}

	// If empty, no users are disabled. If not empty, users in the map are disabled (not owned by this compactor).
	disabledUsers map[string]struct{}

	// Function that creates bucket client, TSDB planner and compactor using the context.
	// Useful for injecting mock objects from tests.
	createDependencies func(ctx context.Context) (objstore.Bucket, tsdb.Compactor, compact.Planner, error)

	// Users scanner, used to discover users from the bucket.
	usersScanner *cortex_tsdb.UsersScanner

	// Blocks cleaner is responsible to hard delete blocks marked for deletion.
	blocksCleaner *BlocksCleaner

	// Underlying compactor and planner used to compact TSDB blocks.
	tsdbCompactor tsdb.Compactor
	tsdbPlanner   compact.Planner

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
	blocksMarkedForDeletion        prometheus.Counter
	garbageCollectedBlocks         prometheus.Counter

	// TSDB syncer metrics
	syncerMetrics *syncerMetrics
}

// NewCompactor makes a new Compactor.
func NewCompactor(compactorCfg Config, storageCfg cortex_tsdb.BlocksStorageConfig, logger log.Logger, registerer prometheus.Registerer) (*Compactor, error) {
	createDependencies := func(ctx context.Context) (objstore.Bucket, tsdb.Compactor, compact.Planner, error) {
		bucketClient, err := bucket.NewClient(ctx, storageCfg.Bucket, "compactor", logger, registerer)
		if err != nil {
			return nil, nil, nil, errors.Wrap(err, "failed to create the bucket client")
		}

		compactor, err := tsdb.NewLeveledCompactor(ctx, registerer, logger, compactorCfg.BlockRanges.ToMilliseconds(), downsample.NewPool())
		if err != nil {
			return nil, nil, nil, err
		}

		planner := compact.NewTSDBBasedPlanner(logger, compactorCfg.BlockRanges.ToMilliseconds())
		return bucketClient, compactor, planner, nil
	}

	cortexCompactor, err := newCompactor(compactorCfg, storageCfg, logger, registerer, createDependencies)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create Cortex blocks compactor")
	}

	return cortexCompactor, nil
}

func newCompactor(
	compactorCfg Config,
	storageCfg cortex_tsdb.BlocksStorageConfig,
	logger log.Logger,
	registerer prometheus.Registerer,
	createDependencies func(ctx context.Context) (objstore.Bucket, tsdb.Compactor, compact.Planner, error),
) (*Compactor, error) {
	c := &Compactor{
		compactorCfg:       compactorCfg,
		storageCfg:         storageCfg,
		parentLogger:       logger,
		logger:             log.With(logger, "component", "compactor"),
		registerer:         registerer,
		syncerMetrics:      newSyncerMetrics(registerer),
		createDependencies: createDependencies,

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
		blocksMarkedForDeletion: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_blocks_marked_for_deletion_total",
			Help: "Total number of blocks marked for deletion in compactor.",
		}),
		garbageCollectedBlocks: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_garbage_collected_blocks_total",
			Help: "Total number of blocks marked for deletion by compactor.",
		}),
	}

	if len(compactorCfg.EnabledTenants) > 0 {
		c.enabledUsers = map[string]struct{}{}
		for _, u := range compactorCfg.EnabledTenants {
			c.enabledUsers[u] = struct{}{}
		}

		level.Info(c.logger).Log("msg", "using enabled users", "enabled", strings.Join(compactorCfg.EnabledTenants, ", "))
	}

	if len(compactorCfg.DisabledTenants) > 0 {
		c.disabledUsers = map[string]struct{}{}
		for _, u := range compactorCfg.DisabledTenants {
			c.disabledUsers[u] = struct{}{}
		}

		level.Info(c.logger).Log("msg", "using disabled users", "disabled", strings.Join(compactorCfg.DisabledTenants, ", "))
	}

	c.Service = services.NewBasicService(c.starting, c.running, c.stopping)

	return c, nil
}

// Start the compactor.
func (c *Compactor) starting(ctx context.Context) error {
	var err error

	// Create bucket client and compactor.
	c.bucketClient, c.tsdbCompactor, c.tsdbPlanner, err = c.createDependencies(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to initialize compactor objects")
	}

	// Wrap the bucket client to write block deletion marks in the global location too.
	c.bucketClient = bucketindex.BucketWithGlobalMarkers(c.bucketClient)

	// Create the users scanner.
	c.usersScanner = cortex_tsdb.NewUsersScanner(c.bucketClient, c.ownUser, c.parentLogger)

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
			if err := ring.WaitRingStability(ctx, c.ring, ring.Compactor, minWaiting, maxWaiting); err != nil {
				level.Warn(c.logger).Log("msg", "compactor is ring topology is not stable after the max waiting time, proceeding anyway")
			} else {
				level.Info(c.logger).Log("msg", "compactor is ring topology is stable")
			}
		}
	}

	// Create the blocks cleaner (service).
	c.blocksCleaner = NewBlocksCleaner(BlocksCleanerConfig{
		DataDir:             c.compactorCfg.DataDir,
		MetaSyncConcurrency: c.compactorCfg.MetaSyncConcurrency,
		DeletionDelay:       c.compactorCfg.DeletionDelay,
		CleanupInterval:     util.DurationWithJitter(c.compactorCfg.CompactionInterval, 0.1),
		CleanupConcurrency:  c.compactorCfg.CleanupConcurrency,
	}, c.bucketClient, c.usersScanner, c.parentLogger, c.registerer)

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
	c.compactUsersWithRetries(ctx)

	ticker := time.NewTicker(util.DurationWithJitter(c.compactorCfg.CompactionInterval, 0.05))
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.compactUsersWithRetries(ctx)
		case <-ctx.Done():
			return nil
		case err := <-c.ringSubservicesWatcher.Chan():
			return errors.Wrap(err, "compactor subservice failed")
		}
	}
}

func (c *Compactor) compactUsersWithRetries(ctx context.Context) {
	retries := util.NewBackoff(ctx, util.BackoffConfig{
		MinBackoff: c.compactorCfg.retryMinBackoff,
		MaxBackoff: c.compactorCfg.retryMaxBackoff,
		MaxRetries: c.compactorCfg.CompactionRetries,
	})

	c.compactionRunsStarted.Inc()

	for retries.Ongoing() {
		if err := c.compactUsers(ctx); err == nil {
			c.compactionRunsCompleted.Inc()
			c.compactionRunsLastSuccess.SetToCurrentTime()
			return
		} else if errors.Is(err, context.Canceled) {
			return
		}

		retries.Wait()
	}

	c.compactionRunsFailed.Inc()
}

func (c *Compactor) compactUsers(ctx context.Context) error {
	// Reset progress metrics once done.
	defer func() {
		c.compactionRunDiscoveredTenants.Set(0)
		c.compactionRunSkippedTenants.Set(0)
		c.compactionRunSucceededTenants.Set(0)
		c.compactionRunFailedTenants.Set(0)
	}()

	level.Info(c.logger).Log("msg", "discovering users from bucket")
	users, err := c.discoverUsers(ctx)
	if err != nil {
		level.Error(c.logger).Log("msg", "failed to discover users from bucket", "err", err)
		return errors.Wrap(err, "failed to discover users from bucket")
	}

	level.Info(c.logger).Log("msg", "discovered users from bucket", "users", len(users))
	c.compactionRunDiscoveredTenants.Set(float64(len(users)))

	// When starting multiple compactor replicas nearly at the same time, running in a cluster with
	// a large number of tenants, we may end up in a situation where the 1st user is compacted by
	// multiple replicas at the same time. Shuffling users helps reduce the likelihood this will happen.
	rand.Shuffle(len(users), func(i, j int) {
		users[i], users[j] = users[j], users[i]
	})

	errs := tsdb_errors.NewMulti()

	for _, userID := range users {
		// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
		if ctx.Err() != nil {
			level.Info(c.logger).Log("msg", "interrupting compaction of user blocks", "err", err)
			return ctx.Err()
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

		if err = c.compactUser(ctx, userID); err != nil {
			c.compactionRunFailedTenants.Inc()
			level.Error(c.logger).Log("msg", "failed to compact user blocks", "user", userID, "err", err)
			errs.Add(errors.Wrapf(err, "failed to compact user blocks (user: %s)", userID))
			continue
		}

		c.compactionRunSucceededTenants.Inc()
		level.Info(c.logger).Log("msg", "successfully compacted user blocks", "user", userID)
	}

	return errs.Err()
}

func (c *Compactor) compactUser(ctx context.Context, userID string) error {
	bucket := bucket.NewUserBucketClient(userID, c.bucketClient)

	reg := prometheus.NewRegistry()
	defer c.syncerMetrics.gatherThanosSyncerMetrics(reg)

	ulogger := util.WithUserID(userID, c.logger)

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
		// The fetcher stores cached metas in the "meta-syncer/" sub directory,
		// but we prefix it with "compactor-meta-" in order to guarantee no clashing with
		// the directory used by the Thanos Syncer, whatever is the user ID.
		path.Join(c.compactorCfg.DataDir, "compactor-meta-"+userID),
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

	syncer, err := compact.NewSyncer(
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

	grouper := compact.NewDefaultGrouper(
		ulogger,
		bucket,
		false, // Do not accept malformed indexes
		true,  // Enable vertical compaction
		reg,
		c.blocksMarkedForDeletion,
		c.garbageCollectedBlocks,
	)

	compactor, err := compact.NewBucketCompactor(
		ulogger,
		syncer,
		grouper,
		c.tsdbPlanner,
		c.tsdbCompactor,
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

func (c *Compactor) discoverUsers(ctx context.Context) ([]string, error) {
	var users []string

	err := c.bucketClient.Iter(ctx, "", func(entry string) error {
		users = append(users, strings.TrimSuffix(entry, "/"))
		return nil
	})

	return users, err
}

func (c *Compactor) ownUser(userID string) (bool, error) {
	if !isAllowedUser(c.enabledUsers, c.disabledUsers, userID) {
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
	rs, err := c.ring.Get(userHash, ring.Compactor, []ring.IngesterDesc{})
	if err != nil {
		return false, err
	}

	if len(rs.Ingesters) != 1 {
		return false, fmt.Errorf("unexpected number of compactors in the shard (expected 1, got %d)", len(rs.Ingesters))
	}

	return rs.Ingesters[0].Addr == c.ringLifecycler.Addr, nil
}

func isAllowedUser(enabledUsers, disabledUsers map[string]struct{}, userID string) bool {
	if len(enabledUsers) > 0 {
		if _, ok := enabledUsers[userID]; !ok {
			return false
		}
	}

	if len(disabledUsers) > 0 {
		if _, ok := disabledUsers[userID]; ok {
			return false
		}
	}

	return true
}
