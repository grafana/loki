package querier

import (
	"context"
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
	fetchers   map[string]block.MetadataFetcher

	// Keep the per-tenant metas found during the last run.
	metasMx sync.RWMutex
	metas   map[string][]*metadata.Meta

	scanDuration prometheus.Histogram
}

func NewBlocksScanner(cfg BlocksScannerConfig, bucketClient objstore.Bucket, logger log.Logger, reg prometheus.Registerer) *BlocksScanner {
	d := &BlocksScanner{
		cfg:             cfg,
		logger:          logger,
		bucketClient:    bucketClient,
		fetchers:        make(map[string]block.MetadataFetcher),
		metas:           make(map[string][]*metadata.Meta),
		fetchersMetrics: storegateway.NewMetadataFetcherMetrics(),
		scanDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "cortex_querier_blocks_scan_duration_seconds",
			Help:    "The total time it takes to run a full blocks scan across the storage.",
			Buckets: []float64{1, 10, 20, 30, 60, 120, 180, 240, 300, 600},
		}),
	}

	if reg != nil {
		prometheus.WrapRegistererWithPrefix("cortex_querier_", reg).MustRegister(d.fetchersMetrics)
	}

	d.Service = services.NewTimerService(cfg.ScanInterval, d.starting, d.scan, nil)

	return d
}

// GetBlocks returns known blocks for userID containing samples within the range minT
// and maxT (milliseconds, both included). Returned blocks are sorted by MaxTime descending.
func (d *BlocksScanner) GetBlocks(userID string, minT, maxT int64) ([]*metadata.Meta, error) {
	// We need to ensure the initial full bucket scan succeeded.
	if d.State() != services.Running {
		return nil, errBlocksScannerNotRunning
	}
	if maxT < minT {
		return nil, errInvalidBlocksRange
	}

	d.metasMx.RLock()
	defer d.metasMx.RUnlock()

	userMetas, ok := d.metas[userID]
	if !ok {
		return nil, nil
	}

	// Given we do expect the large majority of queries to have a time range close
	// to "now", we're going to find matching blocks iterating the list in reverse order.
	var matchingMetas []*metadata.Meta
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

	return matchingMetas, nil
}

func (d *BlocksScanner) starting(ctx context.Context) error {
	// Before the service is in the running state it must have successfully
	// complete the initial scan.
	return d.scanBucket(ctx)
}

func (d *BlocksScanner) scan(ctx context.Context) error {
	if err := d.scanBucket(ctx); err != nil {
		level.Error(d.logger).Log("msg", "failed to scan bucket storage to find blocks", "err", err)
	}

	// Never return error, otherwise the service terminates.
	return nil
}

func (d *BlocksScanner) scanBucket(ctx context.Context) error {
	defer func(start time.Time) {
		d.scanDuration.Observe(time.Since(start).Seconds())
	}(time.Now())

	jobsChan := make(chan string)
	resMx := sync.Mutex{}
	resMetas := map[string][]*metadata.Meta{}
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
				metas, err := d.scanUserBlocksWithRetries(ctx, userID)

				resMx.Lock()
				if err != nil {
					resErrs.Add(err)
				} else {
					resMetas[userID] = metas
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

	d.metasMx.Lock()
	if len(resErrs) == 0 {
		// Replace the map, so that we discard tenants fully deleted from storage.
		d.metas = resMetas
	} else {
		// If an error occurred, we prefer to partially update the metas map instead of
		// not updating it at all. At least we'll update blocks for the successful tenants.
		for userID, metas := range resMetas {
			d.metas[userID] = metas
		}
	}
	d.metasMx.Unlock()

	return resErrs.Err()
}

// scanUserBlocksWithRetries runs scanUserBlocks() retrying multiple times
// in case of error.
func (d *BlocksScanner) scanUserBlocksWithRetries(ctx context.Context, userID string) (metas []*metadata.Meta, err error) {
	retries := util.NewBackoff(ctx, util.BackoffConfig{
		MinBackoff: time.Second,
		MaxBackoff: 30 * time.Second,
		MaxRetries: 3,
	})

	for retries.Ongoing() {
		metas, err = d.scanUserBlocks(ctx, userID)
		if err == nil {
			return
		}

		retries.Wait()
	}

	return
}

func (d *BlocksScanner) scanUserBlocks(ctx context.Context, userID string) ([]*metadata.Meta, error) {
	fetcher, err := d.getOrCreateMetaFetcher(userID)
	if err != nil {
		return nil, errors.Wrapf(err, "create meta fetcher for user %s", userID)
	}

	metas, partials, err := fetcher.Fetch(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "scan blocks for user %s", userID)
	}

	// In case we've found any partial block we log about it but continue cause we don't want
	// to break the scanner just because there's a spurious block.
	if len(partials) > 0 {
		logPartialBlocks(userID, partials, d.logger)
	}

	return sortMetasByMaxTime(metas), nil
}

func (d *BlocksScanner) getOrCreateMetaFetcher(userID string) (block.MetadataFetcher, error) {
	d.fetchersMx.Lock()
	defer d.fetchersMx.Unlock()

	if fetcher, ok := d.fetchers[userID]; ok {
		return fetcher, nil
	}

	fetcher, err := d.createMetaFetcher(userID)
	if err != nil {
		return nil, err
	}

	d.fetchers[userID] = fetcher
	return fetcher, nil
}

func (d *BlocksScanner) createMetaFetcher(userID string) (block.MetadataFetcher, error) {
	userLogger := util.WithUserID(userID, d.logger)
	userBucket := cortex_tsdb.NewUserBucketClient(userID, d.bucketClient)
	userReg := prometheus.NewRegistry()

	var filters []block.MetadataFilter
	// TODO(pracucci) I'm dubious we actually need NewConsistencyDelayMetaFilter here. I think we should remove it and move the
	// 		consistency delay upwards, where we do the consistency check in the querier.
	filters = append(filters, block.NewConsistencyDelayMetaFilter(userLogger, d.cfg.ConsistencyDelay, userReg))
	filters = append(filters, block.NewIgnoreDeletionMarkFilter(userLogger, userBucket, d.cfg.IgnoreDeletionMarksDelay))
	// TODO(pracucci) is this problematic due to the consistency check?
	filters = append(filters, block.NewDeduplicateFilter())

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
		return nil, err
	}

	d.fetchersMetrics.AddUserRegistry(userID, userReg)
	return f, nil
}

func sortMetasByMaxTime(metas map[ulid.ULID]*metadata.Meta) []*metadata.Meta {
	sorted := make([]*metadata.Meta, 0, len(metas))
	for _, m := range metas {
		sorted = append(sorted, m)
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].MaxTime < sorted[j].MaxTime
	})

	return sorted
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
