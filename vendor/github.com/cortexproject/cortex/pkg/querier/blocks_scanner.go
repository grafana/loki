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

	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/storegateway"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
)

var (
	errBlocksScannerNotRunning = errors.New("blocks scanner is not running")
	errInvalidBlocksRange      = errors.New("invalid blocks time range")
)

type BlocksScannerConfig struct {
	ScanInterval             time.Duration
	TenantsConcurrency       int
	MetasConcurrency         int
	CacheDir                 string
	ConsistencyDelay         time.Duration
	IgnoreDeletionMarksDelay time.Duration
}

type BlocksScanner struct {
	services.Service

	cfg             BlocksScannerConfig
	logger          log.Logger
	bucketClient    objstore.Bucket
	fetchersMetrics *storegateway.MetadataFetcherMetrics

	// We reuse the metadata fetcher instance for a given tenant both because of performance
	// reasons (the fetcher keeps a in-memory cache) and being able to collect and group metrics.
	fetchersMx sync.Mutex
	fetchers   map[string]userFetcher

	// Keep the per-tenant/user metas found during the last run.
	userMx            sync.RWMutex
	userMetas         map[string][]*BlockMeta
	userMetasLookup   map[string]map[ulid.ULID]*BlockMeta
	userDeletionMarks map[string]map[ulid.ULID]*metadata.DeletionMark

	scanDuration    prometheus.Histogram
	scanLastSuccess prometheus.Gauge
}

func NewBlocksScanner(cfg BlocksScannerConfig, bucketClient objstore.Bucket, logger log.Logger, reg prometheus.Registerer) *BlocksScanner {
	d := &BlocksScanner{
		cfg:               cfg,
		logger:            logger,
		bucketClient:      bucketClient,
		fetchers:          make(map[string]userFetcher),
		userMetas:         make(map[string][]*BlockMeta),
		userMetasLookup:   make(map[string]map[ulid.ULID]*BlockMeta),
		userDeletionMarks: map[string]map[ulid.ULID]*metadata.DeletionMark{},
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
func (d *BlocksScanner) GetBlocks(userID string, minT, maxT int64) ([]*BlockMeta, map[ulid.ULID]*metadata.DeletionMark, error) {
	// We need to ensure the initial full bucket scan succeeded.
	if d.State() != services.Running {
		return nil, nil, errBlocksScannerNotRunning
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
	var matchingMetas []*BlockMeta
	for i := len(userMetas) - 1; i >= 0; i-- {
		// NOTE: Block intervals are half-open: [MinTime, MaxTime).
		if userMetas[i].MinTime <= maxT && minT < userMetas[i].MaxTime {
			matchingMetas = append(matchingMetas, userMetas[i])
		}

		// We can safely break the loop because metas are sorted by MaxTime.
		if userMetas[i].MaxTime <= minT {
			break
		}
	}

	// Filter deletion marks by matching blocks only.
	matchingDeletionMarks := map[ulid.ULID]*metadata.DeletionMark{}
	if userDeletionMarks, ok := d.userDeletionMarks[userID]; ok {
		for _, m := range matchingMetas {
			if d := userDeletionMarks[m.ULID]; d != nil {
				matchingDeletionMarks[m.ULID] = d
			}
		}
	}

	return matchingMetas, matchingDeletionMarks, nil
}

func (d *BlocksScanner) starting(ctx context.Context) error {
	// Before the service is in the running state it must have successfully
	// complete the initial scan.
	if err := d.scanBucket(ctx); err != nil {
		level.Error(d.logger).Log("msg", "unable to run the initial blocks scan", "err", err)
		return err
	}

	return nil
}

func (d *BlocksScanner) scan(ctx context.Context) error {
	if err := d.scanBucket(ctx); err != nil {
		level.Error(d.logger).Log("msg", "failed to scan bucket storage to find blocks", "err", err)
	}

	// Never return error, otherwise the service terminates.
	return nil
}

func (d *BlocksScanner) scanBucket(ctx context.Context) (returnErr error) {
	defer func(start time.Time) {
		d.scanDuration.Observe(time.Since(start).Seconds())
		if returnErr == nil {
			d.scanLastSuccess.SetToCurrentTime()
		}
	}(time.Now())

	jobsChan := make(chan string)
	resMx := sync.Mutex{}
	resMetas := map[string][]*BlockMeta{}
	resMetasLookup := map[string]map[ulid.ULID]*BlockMeta{}
	resDeletionMarks := map[string]map[ulid.ULID]*metadata.DeletionMark{}
	resErrs := tsdb_errors.MultiError{}

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
				lookup := map[ulid.ULID]*BlockMeta{}
				for _, m := range metas {
					lookup[m.ULID] = m
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

	// Iterate the bucket to discover users.
	err := d.bucketClient.Iter(ctx, "", func(s string) error {
		userID := strings.TrimSuffix(s, "/")
		select {
		case jobsChan <- userID:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	if err != nil {
		resMx.Lock()
		resErrs.Add(err)
		resMx.Unlock()
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
func (d *BlocksScanner) scanUserBlocksWithRetries(ctx context.Context, userID string) (metas []*BlockMeta, deletionMarks map[ulid.ULID]*metadata.DeletionMark, err error) {
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

func (d *BlocksScanner) scanUserBlocks(ctx context.Context, userID string) ([]*BlockMeta, map[ulid.ULID]*metadata.DeletionMark, error) {
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

	res := make([]*BlockMeta, 0, len(metas))
	for _, m := range metas {
		blockMeta := &BlockMeta{
			Meta: *m,
		}

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
			blockMeta.UploadedAt = attrs.LastModified
		}

		res = append(res, blockMeta)
	}

	// The blocks scanner expects all blocks to be sorted by max time.
	sortBlockMetasByMaxTime(res)

	return res, deletionMarkFilter.DeletionMarkBlocks(), nil
}

func (d *BlocksScanner) getOrCreateMetaFetcher(userID string) (block.MetadataFetcher, objstore.Bucket, *block.IgnoreDeletionMarkFilter, error) {
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

func (d *BlocksScanner) createMetaFetcher(userID string) (block.MetadataFetcher, objstore.Bucket, *block.IgnoreDeletionMarkFilter, error) {
	userLogger := util.WithUserID(userID, d.logger)
	userBucket := cortex_tsdb.NewUserBucketClient(userID, d.bucketClient)
	userReg := prometheus.NewRegistry()

	// The following filters have been intentionally omitted:
	// - Consistency delay filter: omitted because we should discover all uploaded blocks.
	//   The consistency delay is taken in account when running the consistency check at query time.
	// - Deduplicate filter: omitted because it could cause troubles with the consistency check if
	//   we "hide" source blocks because recently compacted by the compactor before the store-gateway instances
	//   discover and load the compacted ones.
	deletionMarkFilter := block.NewIgnoreDeletionMarkFilter(userLogger, userBucket, d.cfg.IgnoreDeletionMarksDelay)
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

func (d *BlocksScanner) getBlockMeta(userID string, blockID ulid.ULID) *BlockMeta {
	d.userMx.RLock()
	defer d.userMx.RUnlock()

	metas, ok := d.userMetasLookup[userID]
	if !ok {
		return nil
	}

	return metas[blockID]
}

func sortBlockMetasByMaxTime(metas []*BlockMeta) {
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].MaxTime < metas[j].MaxTime
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
