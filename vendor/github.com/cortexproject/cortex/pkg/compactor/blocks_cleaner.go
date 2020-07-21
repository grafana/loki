package compactor

import (
	"context"
	"path"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	tsdb_errors "github.com/prometheus/prometheus/tsdb/errors"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/compact"
	"github.com/thanos-io/thanos/pkg/objstore"

	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
)

type BlocksCleanerConfig struct {
	DataDir             string
	MetaSyncConcurrency int
	DeletionDelay       time.Duration
	CleanupInterval     time.Duration
}

type BlocksCleaner struct {
	services.Service

	cfg          BlocksCleanerConfig
	logger       log.Logger
	bucketClient objstore.Bucket
	usersScanner *UsersScanner

	// Metrics.
	runsStarted        prometheus.Counter
	runsCompleted      prometheus.Counter
	runsFailed         prometheus.Counter
	runsLastSuccess    prometheus.Gauge
	blocksCleanedTotal prometheus.Counter
	blocksFailedTotal  prometheus.Counter
}

func NewBlocksCleaner(cfg BlocksCleanerConfig, bucketClient objstore.Bucket, usersScanner *UsersScanner, logger log.Logger, reg prometheus.Registerer) *BlocksCleaner {
	c := &BlocksCleaner{
		cfg:          cfg,
		bucketClient: bucketClient,
		usersScanner: usersScanner,
		logger:       log.With(logger, "component", "cleaner"),
		runsStarted: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_block_cleanup_started_total",
			Help: "Total number of blocks cleanup runs started.",
		}),
		runsCompleted: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_block_cleanup_completed_total",
			Help: "Total number of blocks cleanup runs successfully completed.",
		}),
		runsFailed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_block_cleanup_failed_total",
			Help: "Total number of blocks cleanup runs failed.",
		}),
		runsLastSuccess: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_compactor_block_cleanup_last_successful_run_timestamp_seconds",
			Help: "Unix timestamp of the last successful blocks cleanup run.",
		}),
		blocksCleanedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_blocks_cleaned_total",
			Help: "Total number of blocks deleted.",
		}),
		blocksFailedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_block_cleanup_failures_total",
			Help: "Total number of blocks failed to be deleted.",
		}),
	}

	c.Service = services.NewTimerService(cfg.CleanupInterval, c.starting, c.ticker, nil)

	return c
}

func (c *BlocksCleaner) starting(ctx context.Context) error {
	// Run a cleanup so that any other service depending on this service
	// is guaranteed to start once the initial cleanup has been done.
	c.runCleanup(ctx)

	return nil
}

func (c *BlocksCleaner) ticker(ctx context.Context) error {
	c.runCleanup(ctx)

	return nil
}

func (c *BlocksCleaner) runCleanup(ctx context.Context) {
	level.Info(c.logger).Log("msg", "started hard deletion of blocks marked for deletion")
	c.runsStarted.Inc()

	if err := c.cleanUsers(ctx); err == nil {
		level.Info(c.logger).Log("msg", "successfully completed hard deletion of blocks marked for deletion")
		c.runsCompleted.Inc()
		c.runsLastSuccess.SetToCurrentTime()
	} else if errors.Is(err, context.Canceled) {
		level.Info(c.logger).Log("msg", "canceled hard deletion of blocks marked for deletion", "err", err)
		return
	} else {
		level.Error(c.logger).Log("msg", "failed to hard delete blocks marked for deletion", "err", err.Error())
		c.runsFailed.Inc()
	}
}

func (c *BlocksCleaner) cleanUsers(ctx context.Context) error {
	users, err := c.usersScanner.ScanUsers(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to discover users from bucket")
	}

	errs := tsdb_errors.MultiError{}
	for _, userID := range users {
		// Ensure the context has not been canceled (ie. shutdown has been triggered).
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err = c.cleanUser(ctx, userID); err != nil {
			errs.Add(errors.Wrapf(err, "failed to delete user blocks (user: %s)", userID))
			continue
		}
	}

	return errs.Err()
}

func (c *BlocksCleaner) cleanUser(ctx context.Context, userID string) error {
	userLogger := util.WithUserID(userID, c.logger)
	userBucket := cortex_tsdb.NewUserBucketClient(userID, c.bucketClient)

	ignoreDeletionMarkFilter := block.NewIgnoreDeletionMarkFilter(userLogger, userBucket, c.cfg.DeletionDelay)

	fetcher, err := block.NewMetaFetcher(
		userLogger,
		c.cfg.MetaSyncConcurrency,
		userBucket,
		// The fetcher stores cached metas in the "meta-syncer/" sub directory,
		// but we prefix it in order to guarantee no clashing with the compactor.
		path.Join(c.cfg.DataDir, "blocks-cleaner-meta-"+userID),
		// No metrics.
		nil,
		[]block.MetadataFilter{ignoreDeletionMarkFilter},
		nil,
	)
	if err != nil {
		return errors.Wrap(err, "error creating metadata fetcher")
	}

	// Runs a bucket scan to get a fresh list of all blocks and populate
	// the list of deleted blocks in filter.
	_, partials, err := fetcher.Fetch(ctx)
	if err != nil {
		return errors.Wrap(err, "error fetching metadata")
	}

	cleaner := compact.NewBlocksCleaner(
		userLogger,
		userBucket,
		ignoreDeletionMarkFilter,
		c.cfg.DeletionDelay,
		c.blocksCleanedTotal,
		c.blocksFailedTotal)

	if err := cleaner.DeleteMarkedBlocks(ctx); err != nil {
		return errors.Wrap(err, "error cleaning blocks")
	}

	// Partial blocks with a deletion mark can be cleaned up. This is a best effort, so we don't return
	// error if the cleanup of partial blocks fail.
	if len(partials) > 0 {
		level.Info(userLogger).Log("msg", "started cleaning of partial blocks marked for deletion")
		c.cleanUserPartialBlocks(ctx, partials, userBucket, userLogger)
		level.Info(userLogger).Log("msg", "cleaning of partial blocks marked for deletion done")
	}

	return nil
}

func (c *BlocksCleaner) cleanUserPartialBlocks(ctx context.Context, partials map[ulid.ULID]error, userBucket *cortex_tsdb.UserBucketClient, userLogger log.Logger) {
	for blockID, blockErr := range partials {
		// We can safely delete only blocks which are partial because the meta.json is missing.
		if blockErr != block.ErrorSyncMetaNotFound {
			continue
		}

		// We can safely delete only partial blocks with a deletion mark.
		_, err := metadata.ReadDeletionMark(ctx, userBucket, userLogger, blockID.String())
		if err == metadata.ErrorDeletionMarkNotFound {
			continue
		}
		if err != nil {
			level.Warn(userLogger).Log("msg", "error reading partial block deletion mark", "block", blockID, "err", err)
			continue
		}

		// Hard-delete partial blocks having a deletion mark, even if the deletion threshold has not
		// been reached yet.
		if err := block.Delete(ctx, userLogger, userBucket, blockID); err != nil {
			c.blocksFailedTotal.Inc()
			level.Warn(userLogger).Log("msg", "error deleting partial block marked for deletion", "block", blockID, "err", err)
			continue
		}

		c.blocksCleanedTotal.Inc()
		level.Info(userLogger).Log("msg", "deleted partial block marked for deletion", "block", blockID)
	}
}
