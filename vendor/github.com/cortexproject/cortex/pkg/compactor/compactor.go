package compactor

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
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
	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
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
	DeletionDelay         time.Duration            `yaml:"deletion_delay"`

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
	f.BoolVar(&cfg.ShardingEnabled, "compactor.sharding-enabled", false, "Shard tenants across multiple compactor instances. Sharding is required if you run multiple compactor instances, in order to coordinate compactions and avoid race conditions leading to the same tenant blocks simultaneously compacted by different instances.")
	f.DurationVar(&cfg.DeletionDelay, "compactor.deletion-delay", 12*time.Hour, "Time before a block marked for deletion is deleted from bucket. "+
		"If not 0, blocks will be marked for deletion and compactor component will delete blocks marked for deletion from the bucket. "+
		"If delete-delay is 0, blocks will be deleted straight away. Note that deleting blocks immediately can cause query failures, "+
		"if store gateway still has the block loaded, or compactor is ignoring the deletion because it's compacting the block at the same time.")
}

// Compactor is a multi-tenant TSDB blocks compactor based on Thanos.
type Compactor struct {
	services.Service

	compactorCfg Config
	storageCfg   cortex_tsdb.BlocksStorageConfig
	logger       log.Logger
	parentLogger log.Logger
	registerer   prometheus.Registerer

	// Function that creates bucket client and TSDB compactor using the context.
	// Useful for injecting mock objects from tests.
	createBucketClientAndTsdbCompactor func(ctx context.Context) (objstore.Bucket, tsdb.Compactor, error)

	// Users scanner, used to discover users from the bucket.
	usersScanner *UsersScanner

	// Blocks cleaner is responsible to hard delete blocks marked for deletion.
	blocksCleaner *BlocksCleaner

	// Underlying compactor used to compact TSDB blocks.
	tsdbCompactor tsdb.Compactor

	// Client used to run operations on the bucket storing blocks.
	bucketClient objstore.Bucket

	// Ring used for sharding compactions.
	ringLifecycler         *ring.Lifecycler
	ring                   *ring.Ring
	ringSubservices        *services.Manager
	ringSubservicesWatcher *services.FailureWatcher

	// Metrics.
	compactionRunsStarted     prometheus.Counter
	compactionRunsCompleted   prometheus.Counter
	compactionRunsFailed      prometheus.Counter
	compactionRunsLastSuccess prometheus.Gauge
	blocksMarkedForDeletion   prometheus.Counter
	garbageCollectedBlocks    prometheus.Counter

	// TSDB syncer metrics
	syncerMetrics *syncerMetrics
}

// NewCompactor makes a new Compactor.
func NewCompactor(compactorCfg Config, storageCfg cortex_tsdb.BlocksStorageConfig, logger log.Logger, registerer prometheus.Registerer) (*Compactor, error) {
	createBucketClientAndTsdbCompactor := func(ctx context.Context) (objstore.Bucket, tsdb.Compactor, error) {
		bucketClient, err := cortex_tsdb.NewBucketClient(ctx, storageCfg.Bucket, "compactor", logger, registerer)
		if err != nil {
			return nil, nil, errors.Wrap(err, "failed to create the bucket client")
		}

		compactor, err := tsdb.NewLeveledCompactor(ctx, registerer, logger, compactorCfg.BlockRanges.ToMilliseconds(), downsample.NewPool())
		return bucketClient, compactor, err
	}

	cortexCompactor, err := newCompactor(compactorCfg, storageCfg, logger, registerer, createBucketClientAndTsdbCompactor)
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
	createBucketClientAndTsdbCompactor func(ctx context.Context) (objstore.Bucket, tsdb.Compactor, error),
) (*Compactor, error) {
	c := &Compactor{
		compactorCfg:                       compactorCfg,
		storageCfg:                         storageCfg,
		parentLogger:                       logger,
		logger:                             log.With(logger, "component", "compactor"),
		registerer:                         registerer,
		syncerMetrics:                      newSyncerMetrics(registerer),
		createBucketClientAndTsdbCompactor: createBucketClientAndTsdbCompactor,

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
		blocksMarkedForDeletion: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_blocks_marked_for_deletion_total",
			Help: "Total number of blocks marked for deletion in compactor.",
		}),
		garbageCollectedBlocks: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_garbage_collected_blocks_total",
			Help: "Total number of blocks marked for deletion by compactor.",
		}),
	}

	c.Service = services.NewBasicService(c.starting, c.running, c.stopping)

	return c, nil
}

// Start the compactor.
func (c *Compactor) starting(ctx context.Context) error {
	var err error

	// Create bucket client and compactor.
	c.bucketClient, c.tsdbCompactor, err = c.createBucketClientAndTsdbCompactor(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to initialize compactor objects")
	}

	// Create the users scanner.
	c.usersScanner = NewUsersScanner(c.bucketClient, c.ownUser, c.parentLogger)

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
	}

	// Create the blocks cleaner (service).
	c.blocksCleaner = NewBlocksCleaner(BlocksCleanerConfig{
		DataDir:             c.compactorCfg.DataDir,
		MetaSyncConcurrency: c.compactorCfg.MetaSyncConcurrency,
		DeletionDelay:       c.compactorCfg.DeletionDelay,
		CleanupInterval:     util.DurationWithJitter(c.compactorCfg.CompactionInterval, 0.05),
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
	level.Info(c.logger).Log("msg", "discovering users from bucket")
	users, err := c.discoverUsers(ctx)
	if err != nil {
		level.Error(c.logger).Log("msg", "failed to discover users from bucket", "err", err)
		return errors.Wrap(err, "failed to discover users from bucket")
	}
	level.Info(c.logger).Log("msg", "discovered users from bucket", "users", len(users))

	errs := tsdb_errors.MultiError{}

	for _, userID := range users {
		// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
		if ctx.Err() != nil {
			level.Info(c.logger).Log("msg", "interrupting compaction of user blocks", "err", err)
			return ctx.Err()
		}

		// If sharding is enabled, ensure the user ID belongs to our shard.
		if c.compactorCfg.ShardingEnabled {
			if owned, err := c.ownUser(userID); err != nil {
				level.Warn(c.logger).Log("msg", "unable to check if user is owned by this shard", "user", userID, "err", err)
				continue
			} else if !owned {
				level.Debug(c.logger).Log("msg", "skipping user because not owned by this shard", "user", userID)
				continue
			}
		}

		level.Info(c.logger).Log("msg", "starting compaction of user blocks", "user", userID)

		if err = c.compactUser(ctx, userID); err != nil {
			level.Error(c.logger).Log("msg", "failed to compact user blocks", "user", userID, "err", err)
			errs.Add(errors.Wrapf(err, "failed to compact user blocks (user: %s)", userID))
			continue
		}

		level.Info(c.logger).Log("msg", "successfully compacted user blocks", "user", userID)
	}

	return errs.Err()
}

func (c *Compactor) compactUser(ctx context.Context, userID string) error {
	bucket := cortex_tsdb.NewUserBucketClient(userID, c.bucketClient)

	reg := prometheus.NewRegistry()
	defer c.syncerMetrics.gatherThanosSyncerMetrics(reg)

	ulogger := util.WithUserID(userID, c.logger)

	// Filters out duplicate blocks that can be formed from two or more overlapping
	// blocks that fully submatches the source blocks of the older blocks.
	deduplicateBlocksFilter := block.NewDeduplicateFilter()

	// While fetching blocks, we filter out blocks that were marked for deletion by using IgnoreDeletionMarkFilter.
	// The delay of deleteDelay/2 is added to ensure we fetch blocks that are meant to be deleted but do not have a replacement yet.
	ignoreDeletionMarkFilter := block.NewIgnoreDeletionMarkFilter(ulogger, bucket, time.Duration(c.compactorCfg.DeletionDelay.Seconds()/2)*time.Second)

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
	// Always owned if sharding is disabled.
	if !c.compactorCfg.ShardingEnabled {
		return true, nil
	}

	// Hash the user ID.
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(userID))
	userHash := hasher.Sum32()

	// Check whether this compactor instance owns the user.
	rs, err := c.ring.Get(userHash, ring.Read, []ring.IngesterDesc{})
	if err != nil {
		return false, err
	}

	if len(rs.Ingesters) != 1 {
		return false, fmt.Errorf("unexpected number of compactors in the shard (expected 1, got %d)", len(rs.Ingesters))
	}

	return rs.Ingesters[0].Addr == c.ringLifecycler.Addr, nil
}
