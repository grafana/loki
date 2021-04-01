package querier

import (
	"context"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
	"github.com/thanos-io/thanos/pkg/objstore"

	"github.com/cortexproject/cortex/pkg/storage/bucket"
	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/storage/tsdb/bucketindex"
	"github.com/cortexproject/cortex/pkg/storegateway"
	"github.com/cortexproject/cortex/pkg/util"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/cortexproject/cortex/pkg/util/services"
)

var (
	errBucketScanBlocksFinderNotRunning = errors.New("bucket scan blocks finder is not running")
	errInvalidBlocksRange               = errors.New("invalid blocks time range")
)

type BucketScanBlocksFinderConfig struct {
	ScanInterval             time.Duration
	TenantsConcurrency       int
	MetasConcurrency         int
	CacheDir                 string
	ConsistencyDelay         time.Duration
	IgnoreDeletionMarksDelay time.Duration
}

// BucketScanBlocksFinder is a BlocksFinder implementation periodically scanning the bucket to discover blocks.
type BucketScanBlocksFinder struct {
	services.Service

	cfg             BucketScanBlocksFinderConfig
	cfgProvider     bucket.TenantConfigProvider
	logger          log.Logger
	bucketClient    objstore.Bucket
	fetchersMetrics *storegateway.MetadataFetcherMetrics
	usersScanner    *cortex_tsdb.UsersScanner

	// We reuse the metadata fetcher instance for a given tenant both because of performance
	// reasons (the fetcher keeps a in-memory cache) and being able to collect and group metrics.
	fetchersMx sync.Mutex
	fetchers   map[string]userFetcher

	// Keep the per-tenant/user metas found during the last run.
	userMx            sync.RWMutex
	userMetas         map[string]bucketindex.Blocks
	userMetasLookup   map[string]map[ulid.ULID]*bucketindex.Block
	userDeletionMarks map[string]map[ulid.ULID]*bucketindex.BlockDeletionMark

	scanDuration    prometheus.Histogram
	scanLastSuccess prometheus.Gauge
}

func NewBucketScanBlocksFinder(cfg BucketScanBlocksFinderConfig, bucketClient objstore.Bucket, cfgProvider bucket.TenantConfigProvider, logger log.Logger, reg prometheus.Registerer) *BucketScanBlocksFinder {
	d := &BucketScanBlocksFinder{
		cfg:               cfg,
		cfgProvider:       cfgProvider,
		logger:            logger,
		bucketClient:      bucketClient,
		fetchers:          make(map[string]userFetcher),
		usersScanner:      cortex_tsdb.NewUsersScanner(bucketClient, cortex_tsdb.AllUsers, logger),
		userMetas:         make(map[string]bucketindex.Blocks),
		userMetasLookup:   make(map[string]map[ulid.ULID]*bucketindex.Block),
		userDeletionMarks: map[string]map[ulid.ULID]*bucketindex.BlockDeletionMark{},
		fetchersMetrics:   storegateway.NewMetadataFetcherMetrics(),
		scanDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "cortex_querier_blocks_scan_duration_seconds",
			Help:    "The total time it takes to run a full blocks scan across the storage.",
			Buckets: []float64{1, 10, 20, 30, 60, 120, 180, 240, 300, 600},
		}),
		scanLastSuccess: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_querier_blocks_last_successful_scan_timestamp_seconds",
			Help: "Unix timestamp of the last successful blocks scan.",
		}),
	}

	if reg != nil {
		prometheus.WrapRegistererWith(prometheus.Labels{"component": "querier"}, reg).MustRegister(d.fetchersMetrics)
	}

	// Apply a jitter to the sync frequency in order to increase the probability
	// of hitting the shared cache (if any).
	scanInterval := util.DurationWithJitter(cfg.ScanInterval, 0.2)
	d.Service = services.NewTimerService(scanInterval, d.starting, d.scan, nil)

	return d
}

// GetBlocks returns known blocks for userID containing samples within the range minT
// and maxT (milliseconds, both included). Returned blocks are sorted by MaxTime descending.
func (d *BucketScanBlocksFinder) GetBlocks(_ context.Context, userID string, minT, maxT int64) (bucketindex.Blocks, map[ulid.ULID]*bucketindex.BlockDeletionMark, error) {
	// We need to ensure the initial full bucket scan succeeded.
	if d.State() != services.Running {
		return nil, nil, errBucketScanBlocksFinderNotRunning
	}
	if maxT < minT {
		return nil, nil, errInvalidBlocksRange
	}

	d.userMx.RLock()
	defer d.userMx.RUnlock()

	userMetas, ok := d.userMetas[userID]
	if !ok {
		return nil, nil, nil
	}

	// Given we do expect the large majority of queries to have a time range close
	// to "now", we're going to find matching blocks iterating the list in reverse order.
	var matchingMetas bucketindex.Blocks
	for i := len(userMetas) - 1; i >= 0; i-- {
		if userMetas[i].Within(minT, maxT) {
			matchingMetas = append(matchingMetas, userMetas[i])
		}

		// We can safely break the loop because metas are sorted by MaxTime.
		if userMetas[i].MaxTime <= minT {
			break
		}
	}

	// Filter deletion marks by matching blocks only.
	matchingDeletionMarks := map[ulid.ULID]*bucketindex.BlockDeletionMark{}
	if userDeletionMarks, ok := d.userDeletionMarks[userID]; ok {
		for _, m := range matchingMetas {
			if d := userDeletionMarks[m.ID]; d != nil {
				matchingDeletionMarks[m.ID] = d
			}
		}
	}

	return matchingMetas, matchingDeletionMarks, nil
}

func (d *BucketScanBlocksFinder) starting(ctx context.Context) error {
	// Before the service is in the running state it must have successfully
	// complete the initial scan.
	if err := d.scanBucket(ctx); err != nil {
		level.Error(d.logger).Log("msg", "unable to run the initial blocks scan", "err", err)
		return err
	}

	return nil
}

func (d *BucketScanBlocksFinder) scan(ctx context.Context) error {
	if err := d.scanBucket(ctx); err != nil {
		level.Error(d.logger).Log("msg", "failed to scan bucket storage to find blocks", "err", err)
	}

	// Never return error, otherwise the service terminates.
	return nil
}

func (d *BucketScanBlocksFinder) scanBucket(ctx context.Context) (returnErr error) {
	defer func(start time.Time) {
		d.scanDuration.Observe(time.Since(start).Seconds())
		if returnErr == nil {
			d.scanLastSuccess.SetToCurrentTime()
		}
	}(time.Now())

	// Discover all users first. This helps cacheability of the object store call.
	userIDs, _, err := d.usersScanner.ScanUsers(ctx)
	if err != nil {
		return err
	}

	jobsChan := make(chan string)
	resMx := sync.Mutex{}
	resMetas := map[string]bucketindex.Blocks{}
	resMetasLookup := map[string]map[ulid.ULID]*bucketindex.Block{}
	resDeletionMarks := map[string]map[ulid.ULID]*bucketindex.BlockDeletionMark{}
	resErrs := tsdb_errors.NewMulti()

	// Create a pool of workers which will synchronize metas. The pool size
	// is limited in order to avoid to concurrently sync a lot of tenants in
	// a large cluster.
	wg := &sync.WaitGroup{}
	wg.Add(d.cfg.TenantsConcurrency)

	for i := 0; i < d.cfg.TenantsConcurrency; i++ {
		go func() {
			defer wg.Done()

			for userID := range jobsChan {
				metas, deletionMarks, err := d.scanUserBlocksWithRetries(ctx, userID)

				// Build the lookup map.
				lookup := map[ulid.ULID]*bucketindex.Block{}
				for _, m := range metas {
					lookup[m.ID] = m
				}

				resMx.Lock()
				if err != nil {
					resErrs.Add(err)
				} else {
					resMetas[userID] = metas
					resMetasLookup[userID] = lookup
					resDeletionMarks[userID] = deletionMarks
				}
				resMx.Unlock()
			}
		}()
	}

	// Push a job for each user whose blocks need to be discovered.
pushJobsLoop:
	for _, userID := range userIDs {
		select {
		case jobsChan <- userID:
		// Nothing to do.
		case <-ctx.Done():
			resMx.Lock()
			resErrs.Add(ctx.Err())
			resMx.Unlock()
			break pushJobsLoop
		}
	}

	// Wait until all workers completed.
	close(jobsChan)
	wg.Wait()

	d.userMx.Lock()
	if len(resErrs) == 0 {
		// Replace the map, so that we discard tenants fully deleted from storage.
		d.userMetas = resMetas
		d.userMetasLookup = resMetasLookup
		d.userDeletionMarks = resDeletionMarks
	} else {
		// If an error occurred, we prefer to partially update the metas map instead of
		// not updating it at all. At least we'll update blocks for the successful tenants.
		for userID, metas := range resMetas {
			d.userMetas[userID] = metas
		}

		for userID, metas := range resMetasLookup {
			d.userMetasLookup[userID] = metas
		}

		for userID, deletionMarks := range resDeletionMarks {
			d.userDeletionMarks[userID] = deletionMarks
		}
	}
	d.userMx.Unlock()

	return resErrs.Err()
}

// scanUserBlocksWithRetries runs scanUserBlocks() retrying multiple times
// in case of error.
func (d *BucketScanBlocksFinder) scanUserBlocksWithRetries(ctx context.Context, userID string) (metas bucketindex.Blocks, deletionMarks map[ulid.ULID]*bucketindex.BlockDeletionMark, err error) {
	retries := util.NewBackoff(ctx, util.BackoffConfig{
		MinBackoff: time.Second,
		MaxBackoff: 30 * time.Second,
		MaxRetries: 3,
	})

	for retries.Ongoing() {
		metas, deletionMarks, err = d.scanUserBlocks(ctx, userID)
		if err == nil {
			return
		}

		retries.Wait()
	}

	return
}

func (d *BucketScanBlocksFinder) scanUserBlocks(ctx context.Context, userID string) (bucketindex.Blocks, map[ulid.ULID]*bucketindex.BlockDeletionMark, error) {
	fetcher, userBucket, deletionMarkFilter, err := d.getOrCreateMetaFetcher(userID)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "create meta fetcher for user %s", userID)
	}

	metas, partials, err := fetcher.Fetch(ctx)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "scan blocks for user %s", userID)
	}

	// In case we've found any partial block we log about it but continue cause we don't want
	// to break the scanner just because there's a spurious block.
	if len(partials) > 0 {
		logPartialBlocks(userID, partials, d.logger)
	}

	res := make(bucketindex.Blocks, 0, len(metas))
	for _, m := range metas {
		blockMeta := bucketindex.BlockFromThanosMeta(*m)

		// If the block is already known, we can get the remaining attributes from there
		// because a block is immutable.
		prevMeta := d.getBlockMeta(userID, m.ULID)
		if prevMeta != nil {
			blockMeta.UploadedAt = prevMeta.UploadedAt
		} else {
			attrs, err := userBucket.Attributes(ctx, path.Join(m.ULID.String(), metadata.MetaFilename))
			if err != nil {
				return nil, nil, errors.Wrapf(err, "read %s attributes of block %s for user %s", metadata.MetaFilename, m.ULID.String(), userID)
			}

			// Since the meta.json file is the last file of a block being uploaded and it's immutable
			// we can safely assume that the last modified timestamp of the meta.json is the time when
			// the block has completed to be uploaded.
			blockMeta.UploadedAt = attrs.LastModified.Unix()
		}

		res = append(res, blockMeta)
	}

	// The blocks scanner expects all blocks to be sorted by max time.
	sortBlocksByMaxTime(res)

	// Convert deletion marks to our own data type.
	marks := map[ulid.ULID]*bucketindex.BlockDeletionMark{}
	for id, m := range deletionMarkFilter.DeletionMarkBlocks() {
		marks[id] = bucketindex.BlockDeletionMarkFromThanosMarker(m)
	}

	return res, marks, nil
}

func (d *BucketScanBlocksFinder) getOrCreateMetaFetcher(userID string) (block.MetadataFetcher, objstore.Bucket, *block.IgnoreDeletionMarkFilter, error) {
	d.fetchersMx.Lock()
	defer d.fetchersMx.Unlock()

	if f, ok := d.fetchers[userID]; ok {
		return f.metadataFetcher, f.userBucket, f.deletionMarkFilter, nil
	}

	fetcher, userBucket, deletionMarkFilter, err := d.createMetaFetcher(userID)
	if err != nil {
		return nil, nil, nil, err
	}

	d.fetchers[userID] = userFetcher{
		metadataFetcher:    fetcher,
		deletionMarkFilter: deletionMarkFilter,
		userBucket:         userBucket,
	}

	return fetcher, userBucket, deletionMarkFilter, nil
}

func (d *BucketScanBlocksFinder) createMetaFetcher(userID string) (block.MetadataFetcher, objstore.Bucket, *block.IgnoreDeletionMarkFilter, error) {
	userLogger := util_log.WithUserID(userID, d.logger)
	userBucket := bucket.NewUserBucketClient(userID, d.bucketClient, d.cfgProvider)
	userReg := prometheus.NewRegistry()

	// The following filters have been intentionally omitted:
	// - Consistency delay filter: omitted because we should discover all uploaded blocks.
	//   The consistency delay is taken in account when running the consistency check at query time.
	// - Deduplicate filter: omitted because it could cause troubles with the consistency check if
	//   we "hide" source blocks because recently compacted by the compactor before the store-gateway instances
	//   discover and load the compacted ones.
	deletionMarkFilter := block.NewIgnoreDeletionMarkFilter(userLogger, userBucket, d.cfg.IgnoreDeletionMarksDelay, d.cfg.MetasConcurrency)
	filters := []block.MetadataFilter{deletionMarkFilter}

	f, err := block.NewMetaFetcher(
		userLogger,
		d.cfg.MetasConcurrency,
		userBucket,
		// The fetcher stores cached metas in the "meta-syncer/" sub directory.
		filepath.Join(d.cfg.CacheDir, userID),
		userReg,
		filters,
		nil,
	)
	if err != nil {
		return nil, nil, nil, err
	}

	d.fetchersMetrics.AddUserRegistry(userID, userReg)
	return f, userBucket, deletionMarkFilter, nil
}

func (d *BucketScanBlocksFinder) getBlockMeta(userID string, blockID ulid.ULID) *bucketindex.Block {
	d.userMx.RLock()
	defer d.userMx.RUnlock()

	metas, ok := d.userMetasLookup[userID]
	if !ok {
		return nil
	}

	return metas[blockID]
}

func sortBlocksByMaxTime(blocks bucketindex.Blocks) {
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].MaxTime < blocks[j].MaxTime
	})
}

func logPartialBlocks(userID string, partials map[ulid.ULID]error, logger log.Logger) {
	ids := make([]string, 0, len(partials))
	errs := make([]string, 0, len(partials))

	for id, err := range partials {
		ids = append(ids, id.String())
		errs = append(errs, err.Error())
	}

	level.Warn(logger).Log("msg", "found partial blocks", "user", userID, "blocks", strings.Join(ids, ","), "err", strings.Join(errs, ","))
}

type userFetcher struct {
	metadataFetcher    block.MetadataFetcher
	deletionMarkFilter *block.IgnoreDeletionMarkFilter
	userBucket         objstore.Bucket
}
