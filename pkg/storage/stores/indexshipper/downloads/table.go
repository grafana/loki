package downloads

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/storage"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

// timeout for downloading initial files for a table to avoid leaking resources by allowing it to take all the time.
const (
	downloadTimeout        = 5 * time.Minute
	maxDownloadConcurrency = 50
	CommonTenantIdentifier = ""
)

type Table interface {
	Close()
	ForEach(ctx context.Context, userID string, doneChan <-chan struct{}, callback index.ForEachIndexCallback) error
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

	baseUserIndexSet, baseCommonIndexSet storage.IndexSet

	logger       log.Logger
	indexSets    map[string]IndexSet
	indexSetsMtx sync.RWMutex
}

// NewTable just creates an instance of table without trying to load files from local storage or object store.
// It is used for initializing table at query time.
func NewTable(name, cacheLocation string, storageClient storage.Client, openIndexFileFunc index.OpenIndexFileFunc, metrics *metrics) Table {
	table := table{
		name:               name,
		cacheLocation:      cacheLocation,
		storageClient:      storageClient,
		baseUserIndexSet:   storage.NewIndexSet(storageClient, true),
		baseCommonIndexSet: storage.NewIndexSet(storageClient, false),
		logger:             log.With(util_log.Logger, "table-name", name),
		openIndexFileFunc:  openIndexFileFunc,
		metrics:            metrics,
		indexSets:          map[string]IndexSet{},
	}

	return &table
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
	}

	level.Debug(table.logger).Log("msg", fmt.Sprintf("opening locally present files for table %s", name), "files", fmt.Sprint(dirEntries))

	// common index files are outside the directories and user index files are in the directories
	for _, entry := range dirEntries {
		if !entry.IsDir() {
			continue
		}

		userID := entry.Name()
		userIndexSet, err := NewIndexSet(name, userID, filepath.Join(cacheLocation, userID),
			table.baseUserIndexSet, openIndexFileFunc, loggerWithUserID(table.logger, userID))
		if err != nil {
			return nil, err
		}

		err = userIndexSet.Init(false)
		if err != nil {
			return nil, err
		}

		table.indexSets[userID] = userIndexSet
	}

	commonIndexSet, err := NewIndexSet(name, CommonTenantIdentifier, cacheLocation, table.baseCommonIndexSet,
		openIndexFileFunc, table.logger)
	if err != nil {
		return nil, err
	}

	err = commonIndexSet.Init(false)
	if err != nil {
		return nil, err
	}

	table.indexSets[CommonTenantIdentifier] = commonIndexSet

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

func (t *table) ForEach(ctx context.Context, userID string, doneChan <-chan struct{}, callback index.ForEachIndexCallback) error {
	ids := []string{userID, CommonTenantIdentifier}
	sets, err := t.getOrCreateIndexSets(ctx, ids, true)
	if err != nil {
		return err
	}

	// iterate through both user and common index
	for i, indexSet := range sets {

		if indexSet.Err() != nil {
			// since getOrCreateIndexSets preserves input->output positional mapping,
			// we know this indexSet corresponds to the i'th ID.
			t.cleanupBrokenIndexSet(ctx, ids[i])
			return indexSet.Err()
		}

		err = indexSet.ForEach(ctx, doneChan, callback)
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
			if userID == CommonTenantIdentifier {
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
		expiredIndexSets = append(expiredIndexSets, CommonTenantIdentifier)
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
			if userID == CommonTenantIdentifier && len(t.indexSets) != 1 {
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

// getOrCreateIndexSets gets or creates the index sets for the userIDs.
// If it does not exist, it creates a new one and initializes it in a goroutine.
// Caller can use IndexSet.AwaitReady() to wait until the IndexSet gets ready, if required.
// forQuerying must be set to true only getting the index for querying since
// it captures the amount of time it takes to download the index at query time.
// NOTE: order of inputs and outputs are preserved: if user "A" is the 0th position in the `ids`
// argument, it will be the 0th position in the returned []IndexSet as well.
func (t *table) getOrCreateIndexSets(ctx context.Context, ids []string, forQuerying bool) ([]IndexSet, error) {

	// withLock keeps track of index sets which didn't exist and therefore need
	// to be tried again under a Lock instead of RLock
	var withLock []int
	results := make([]IndexSet, len(ids))
	var found int
	var err error

	t.indexSetsMtx.RLock()
	for i, id := range ids {
		set, ok := t.indexSets[id]
		if ok {
			results[i] = set
			found++
		} else {
			withLock = append(withLock, i)
		}
	}
	t.indexSetsMtx.RUnlock()

	// happy path: all required index sets were found
	if found == len(ids) {
		return results, nil
	}

	t.indexSetsMtx.Lock()
	defer t.indexSetsMtx.Unlock()

	for i := range withLock {

		// since we're starting goroutines with references to this item
		// during iteration over the slice,
		// we must bind it within the iteration
		pos := withLock[i]
		id := ids[pos]

		set, ok := t.indexSets[id]
		if !ok {
			baseIndexSet := t.baseUserIndexSet
			if id == CommonTenantIdentifier {
				baseIndexSet = t.baseCommonIndexSet
			}

			// instantiate the index set, add it to the map
			set, err = NewIndexSet(t.name, id, filepath.Join(t.cacheLocation, id), baseIndexSet, t.openIndexFileFunc,
				loggerWithUserID(t.logger, id))
			if err != nil {
				return nil, err
			}
			t.indexSets[id] = set

			// initialize the index set in async mode, it would be upto the caller to wait for its readiness using IndexSet.AwaitReady()
			go func() {
				if forQuerying {
					start := time.Now()
					defer func() {
						duration := time.Since(start)
						t.metrics.queryTimeTableDownloadDurationSeconds.WithLabelValues(t.name).Add(duration.Seconds())
						logger := spanlogger.FromContextWithFallback(ctx, loggerWithUserID(t.logger, id))
						level.Info(logger).Log("msg", "downloaded index set at query time", "duration", duration)
					}()
				}

				err := set.Init(forQuerying)
				if err != nil {
					level.Error(t.logger).Log("msg", fmt.Sprintf("failed to init user index set %s", id), "err", err)
					t.cleanupBrokenIndexSet(ctx, id)
				}
			}()
		}

		results[pos] = set
	}

	return results, err
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
	res, err := t.getOrCreateIndexSets(ctx, []string{CommonTenantIdentifier}, false)
	if err != nil {
		return err
	}

	commonIndexSet := res[0]
	err = commonIndexSet.AwaitReady(ctx)
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
		// TODO: concurrency should be controlled globally
		// Here, we call getOrCreateIndexSets one at a time in order to run with proper concurrency since
		// we call `AwaitReady` afterwards.
		indexSet, err := t.getOrCreateIndexSets(ctx, []string{userIDs[idx]}, false)
		if err != nil {
			return err
		}
		return indexSet[0].AwaitReady(ctx)
	})
}

func loggerWithUserID(logger log.Logger, userID string) log.Logger {
	if userID == CommonTenantIdentifier {
		return logger
	}

	return log.With(logger, "user-id", userID)
}
