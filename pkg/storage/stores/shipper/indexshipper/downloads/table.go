package downloads

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

// timeout for downloading initial files for a table to avoid leaking resources by allowing it to take all the time.
const (
	downloadTimeout        = 1 * time.Minute
	maxDownloadConcurrency = 50
)

type Table interface {
	Close()
	ForEach(ctx context.Context, userID string, callback index.ForEachIndexCallback) error
	ForEachConcurrent(ctx context.Context, userID string, callback index.ForEachIndexCallback) error
	DropUnusedIndex(ttl time.Duration, now time.Time) (bool, error)
	Sync(ctx context.Context) error
	EnsureQueryReadiness(ctx context.Context, userIDs []string) error
}

// table is a collection of multiple files created for a same table by various ingesters.
// All the public methods are concurrency safe and take care of mutexes to avoid any data race.
type table struct {
	name              string
	cacheLocation     string
	storageClient     storage.Client
	openIndexFileFunc index.OpenIndexFileFunc
	metrics           *metrics
	maxConcurrent     int

	baseUserIndexSet, baseCommonIndexSet storage.IndexSet

	logger       log.Logger
	indexSets    map[string]IndexSet
	indexSetsMtx sync.RWMutex
}

// NewTable just creates an instance of table without trying to load files from local storage or object store.
// It is used for initializing table at query time.
func NewTable(name, cacheLocation string, storageClient storage.Client, openIndexFileFunc index.OpenIndexFileFunc, metrics *metrics) Table {
	maxConcurrent := max(runtime.GOMAXPROCS(0)/2, 1)
	return &table{
		name:               name,
		cacheLocation:      cacheLocation,
		storageClient:      storageClient,
		baseUserIndexSet:   storage.NewIndexSet(storageClient, true),
		baseCommonIndexSet: storage.NewIndexSet(storageClient, false),
		logger:             log.With(util_log.Logger, "table-name", name),
		openIndexFileFunc:  openIndexFileFunc,
		metrics:            metrics,
		maxConcurrent:      maxConcurrent,
		indexSets:          map[string]IndexSet{},
	}
}

// LoadTable loads a table from local storage(syncs the table too if we have it locally) or downloads it from the shared store.
// It is used for loading and initializing table at startup. It would initialize index sets which already had files locally.
func LoadTable(name, cacheLocation string, storageClient storage.Client, openIndexFileFunc index.OpenIndexFileFunc, metrics *metrics) (Table, error) {
	err := util.EnsureDirectory(cacheLocation)
	if err != nil {
		return nil, err
	}

	dirEntries, err := os.ReadDir(cacheLocation)
	if err != nil {
		return nil, err
	}

	maxConcurrent := max(runtime.GOMAXPROCS(0)/2, 1)

	table := table{
		name:               name,
		cacheLocation:      cacheLocation,
		storageClient:      storageClient,
		baseUserIndexSet:   storage.NewIndexSet(storageClient, true),
		baseCommonIndexSet: storage.NewIndexSet(storageClient, false),
		logger:             log.With(util_log.Logger, "table-name", name),
		indexSets:          map[string]IndexSet{},
		openIndexFileFunc:  openIndexFileFunc,
		metrics:            metrics,
		maxConcurrent:      maxConcurrent,
	}

	level.Debug(table.logger).Log("msg", fmt.Sprintf("opening locally present files for table %s", name), "files", fmt.Sprint(dirEntries))

	// common index files are outside the directories and user index files are in the directories
	for _, entry := range dirEntries {
		if !entry.IsDir() {
			continue
		}

		userID := entry.Name()
		logger := loggerWithUserID(table.logger, userID)
		userIndexSet, err := NewIndexSet(name, userID, filepath.Join(cacheLocation, userID),
			table.baseUserIndexSet, openIndexFileFunc, logger)
		if err != nil {
			return nil, err
		}

		err = userIndexSet.Init(false, logger)
		if err != nil {
			return nil, err
		}

		table.indexSets[userID] = userIndexSet
	}

	commonIndexSet, err := NewIndexSet(name, "", cacheLocation, table.baseCommonIndexSet,
		openIndexFileFunc, table.logger)
	if err != nil {
		return nil, err
	}

	err = commonIndexSet.Init(false, table.logger)
	if err != nil {
		return nil, err
	}

	table.indexSets[""] = commonIndexSet

	return &table, nil
}

// Close Closes references to all the index.
func (t *table) Close() {
	t.indexSetsMtx.Lock()
	defer t.indexSetsMtx.Unlock()

	for _, userIndexSet := range t.indexSets {
		userIndexSet.Close()
	}

	t.indexSets = map[string]IndexSet{}
}

func (t *table) ForEachConcurrent(ctx context.Context, userID string, callback index.ForEachIndexCallback) error {
	g, ctx := errgroup.WithContext(ctx)
	if t.maxConcurrent == 0 {
		panic("maxConcurrent cannot be 0, downloads.table is being initialized without setting maxConcurrent")
	}
	g.SetLimit(t.maxConcurrent)

	// iterate through both user and common index
	users := []string{userID, ""}

	for i := range users {
		// bind locally within iteration before
		// sending to goroutine
		uid := users[i]

		g.Go(func() error {
			indexSet, err := t.getOrCreateIndexSet(ctx, uid, true)
			if err != nil {
				return err
			}

			if indexSet.Err() != nil {
				t.cleanupBrokenIndexSet(ctx, uid)
				return indexSet.Err()
			}

			return indexSet.ForEachConcurrent(ctx, callback)
		})
	}
	return g.Wait()
}

func (t *table) ForEach(ctx context.Context, userID string, callback index.ForEachIndexCallback) error {
	// iterate through both user and common index
	for _, uid := range []string{userID, ""} {
		indexSet, err := t.getOrCreateIndexSet(ctx, uid, true)
		if err != nil {
			return err
		}

		if indexSet.Err() != nil {
			t.cleanupBrokenIndexSet(ctx, uid)
			return indexSet.Err()
		}

		err = indexSet.ForEach(ctx, callback)
		if err != nil {
			return err
		}
	}

	return nil
}

func (t *table) findExpiredIndexSets(ttl time.Duration, now time.Time) []string {
	t.indexSetsMtx.RLock()
	defer t.indexSetsMtx.RUnlock()

	var expiredIndexSets []string
	commonIndexSetExpired := false

	for userID, userIndexSet := range t.indexSets {
		lastUsedAt := userIndexSet.LastUsedAt()
		if lastUsedAt.Add(ttl).Before(now) {
			if userID == "" {
				// add the userID for common index set at the end of the list to make sure it is the last one cleaned up
				// because we remove directories containing the index sets which in case of common index is
				// the parent directory of all the user index sets.
				commonIndexSetExpired = true
			} else {
				expiredIndexSets = append(expiredIndexSets, userID)
			}
		}
	}

	// common index set should expire only after all the user index sets have expired.
	if commonIndexSetExpired && len(expiredIndexSets) == len(t.indexSets)-1 {
		expiredIndexSets = append(expiredIndexSets, "")
	}

	return expiredIndexSets
}

// DropUnusedIndex drops the index set if it has not been queried for at least ttl duration.
// It returns true if the whole table gets dropped.
func (t *table) DropUnusedIndex(ttl time.Duration, now time.Time) (bool, error) {
	indexSetsToCleanup := t.findExpiredIndexSets(ttl, now)

	if len(indexSetsToCleanup) > 0 {
		t.indexSetsMtx.Lock()
		defer t.indexSetsMtx.Unlock()
		for _, userID := range indexSetsToCleanup {
			// additional check for cleaning up the common index set when it is the only one left.
			// This is just for safety because the index sets could change between findExpiredIndexSets and the actual cleanup.
			if userID == "" && len(t.indexSets) != 1 {
				level.Info(t.logger).Log("msg", "skipping cleanup of common index set because we possibly have unexpired user index sets left")
				continue
			}

			level.Info(t.logger).Log("msg", fmt.Sprintf("cleaning up expired index set %s", userID))
			err := t.indexSets[userID].DropAllDBs()
			if err != nil {
				return false, err
			}

			delete(t.indexSets, userID)
		}

		return len(t.indexSets) == 0, nil
	}

	return false, nil
}

// Sync downloads updated and new files from the storage relevant for the table and removes the deleted ones
func (t *table) Sync(ctx context.Context) error {
	level.Debug(t.logger).Log("msg", fmt.Sprintf("syncing files for table %s", t.name))

	t.indexSetsMtx.RLock()
	defer t.indexSetsMtx.RUnlock()

	for userID, indexSet := range t.indexSets {
		if err := indexSet.Sync(ctx); err != nil {
			return errors.Wrap(err, fmt.Sprintf("failed to sync index set %s for table %s", userID, t.name))
		}
	}

	return nil
}

// getOrCreateIndexSet gets or creates the index set for the userID.
// If it does not exist, it creates a new one and initializes it in a goroutine.
// Caller can use IndexSet.AwaitReady() to wait until the IndexSet gets ready, if required.
// forQuerying must be set to true only getting the index for querying since
// it captures the amount of time it takes to download the index at query time.
func (t *table) getOrCreateIndexSet(ctx context.Context, id string, forQuerying bool) (IndexSet, error) {
	logger := spanlogger.FromContextWithFallback(ctx, loggerWithUserID(t.logger, id))

	t.indexSetsMtx.RLock()
	indexSet, ok := t.indexSets[id]
	t.indexSetsMtx.RUnlock()
	if ok {
		return indexSet, nil
	}

	t.indexSetsMtx.Lock()
	defer t.indexSetsMtx.Unlock()

	indexSet, ok = t.indexSets[id]
	if ok {
		return indexSet, nil
	}

	var err error
	baseIndexSet := t.baseUserIndexSet
	if id == "" {
		baseIndexSet = t.baseCommonIndexSet
	}

	// instantiate the index set, add it to the map
	indexSet, err = NewIndexSet(t.name, id, filepath.Join(t.cacheLocation, id), baseIndexSet, t.openIndexFileFunc, loggerWithUserID(t.logger, id))
	if err != nil {
		return nil, err
	}
	t.indexSets[id] = indexSet

	// initialize the index set in async mode
	// it is up to the caller to wait for its readiness using IndexSet.AwaitReady()
	go func() {
		start := time.Now()
		err := indexSet.Init(forQuerying, logger)
		duration := time.Since(start)

		level.Info(logger).Log("msg", "init index set", "duration", duration, "success", err == nil)

		if forQuerying {
			t.metrics.queryTimeTableDownloadDurationSeconds.WithLabelValues(t.name).Add(duration.Seconds())
			t.metrics.queryWaitTime.WithLabelValues(t.name).Observe(duration.Seconds())
			level.Info(logger).Log("msg", "downloaded index set at query time", "duration", duration)
		}

		if err != nil {
			level.Error(logger).Log("msg", "failed to init user index set", "err", err)
			t.cleanupBrokenIndexSet(ctx, id)
		}
	}()

	return indexSet, nil
}

// cleanupBrokenIndexSet if an indexSet with given id exists and is really broken i.e Err() returns a non-nil error
func (t *table) cleanupBrokenIndexSet(ctx context.Context, id string) {
	t.indexSetsMtx.Lock()
	defer t.indexSetsMtx.Unlock()

	indexSet, ok := t.indexSets[id]
	if !ok || indexSet.Err() == nil {
		return
	}

	level.Error(util_log.WithContext(ctx, t.logger)).Log("msg", fmt.Sprintf("index set %s has some problem, cleaning it up", id), "err", indexSet.Err())
	if err := indexSet.DropAllDBs(); err != nil {
		level.Error(t.logger).Log("msg", fmt.Sprintf("failed to cleanup broken index set %s", id), "err", err)
	}

	delete(t.indexSets, id)
}

// EnsureQueryReadiness ensures that we have downloaded the common index as well as user index for the provided userIDs.
// When ensuring query readiness for a table, we will always download common index set because it can include index for one of the provided user ids.
func (t *table) EnsureQueryReadiness(ctx context.Context, userIDs []string) error {
	commonIndexSet, err := t.getOrCreateIndexSet(ctx, "", false)
	if err != nil {
		return err
	}
	err = commonIndexSet.AwaitReady(ctx, "ensure query readiness")
	if err != nil {
		return err
	}

	commonIndexSet.UpdateLastUsedAt()

	missingUserIDs := make([]string, 0, len(userIDs))
	t.indexSetsMtx.RLock()
	for _, userID := range userIDs {
		if userIndexSet, ok := t.indexSets[userID]; !ok {
			missingUserIDs = append(missingUserIDs, userID)
		} else {
			userIndexSet.UpdateLastUsedAt()
		}
	}
	t.indexSetsMtx.RUnlock()

	return t.downloadUserIndexes(ctx, missingUserIDs)
}

// downloadUserIndexes downloads user specific index files concurrently.
func (t *table) downloadUserIndexes(ctx context.Context, userIDs []string) error {
	return concurrency.ForEachJob(ctx, len(userIDs), maxDownloadConcurrency, func(ctx context.Context, idx int) error {
		indexSet, err := t.getOrCreateIndexSet(ctx, userIDs[idx], false)
		if err != nil {
			return err
		}

		return indexSet.AwaitReady(ctx, "download user indexes")
	})
}

func loggerWithUserID(logger log.Logger, userID string) log.Logger {
	if userID == "" {
		return logger
	}

	return log.With(logger, "user-id", userID)
}
