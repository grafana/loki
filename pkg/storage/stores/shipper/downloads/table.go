package downloads

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"
	"go.etcd.io/bbolt"

	chunk_util "github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

// timeout for downloading initial files for a table to avoid leaking resources by allowing it to take all the time.
const (
	downloadTimeout        = 5 * time.Minute
	maxDownloadConcurrency = 50
)

type BoltDBIndexClient interface {
	QueryWithCursor(_ context.Context, c *bbolt.Cursor, query index.Query, callback index.QueryPagesCallback) error
}

type Table interface {
	Close()
	MultiQueries(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error
	DropUnusedIndex(ttl time.Duration, now time.Time) (bool, error)
	Sync(ctx context.Context) error
	EnsureQueryReadiness(ctx context.Context, userIDs []string) error
}

// table is a collection of multiple files created for a same table by various ingesters.
// All the public methods are concurrency safe and take care of mutexes to avoid any data race.
type table struct {
	name              string
	cacheLocation     string
	metrics           *metrics
	storageClient     storage.Client
	boltDBIndexClient BoltDBIndexClient

	baseUserIndexSet, baseCommonIndexSet storage.IndexSet

	logger       log.Logger
	indexSets    map[string]IndexSet
	indexSetsMtx sync.RWMutex
}

// NewTable just creates an instance of table without trying to load files from local storage or object store.
// It is used for initializing table at query time.
func NewTable(name, cacheLocation string, storageClient storage.Client, boltDBIndexClient BoltDBIndexClient, metrics *metrics) Table {
	table := table{
		name:               name,
		cacheLocation:      cacheLocation,
		metrics:            metrics,
		storageClient:      storageClient,
		baseUserIndexSet:   storage.NewIndexSet(storageClient, true),
		baseCommonIndexSet: storage.NewIndexSet(storageClient, false),
		logger:             log.With(util_log.Logger, "table-name", name),
		boltDBIndexClient:  boltDBIndexClient,
		indexSets:          map[string]IndexSet{},
	}

	return &table
}

// LoadTable loads a table from local storage(syncs the table too if we have it locally) or downloads it from the shared store.
// It is used for loading and initializing table at startup. It would initialize index sets which already had files locally.
func LoadTable(name, cacheLocation string, storageClient storage.Client, boltDBIndexClient BoltDBIndexClient, metrics *metrics) (Table, error) {
	err := chunk_util.EnsureDirectory(cacheLocation)
	if err != nil {
		return nil, err
	}

	filesInfo, err := ioutil.ReadDir(cacheLocation)
	if err != nil {
		return nil, err
	}

	table := table{
		name:               name,
		cacheLocation:      cacheLocation,
		metrics:            metrics,
		storageClient:      storageClient,
		baseUserIndexSet:   storage.NewIndexSet(storageClient, true),
		baseCommonIndexSet: storage.NewIndexSet(storageClient, false),
		logger:             log.With(util_log.Logger, "table-name", name),
		boltDBIndexClient:  boltDBIndexClient,
		indexSets:          map[string]IndexSet{},
	}

	level.Debug(table.logger).Log("msg", fmt.Sprintf("opening locally present files for table %s", name), "files", fmt.Sprint(filesInfo))

	// common index files are outside the directories and user index files are in the directories
	for _, fileInfo := range filesInfo {
		if !fileInfo.IsDir() {
			continue
		}

		userID := fileInfo.Name()
		userIndexSet, err := NewIndexSet(name, userID, filepath.Join(cacheLocation, userID), table.baseUserIndexSet, boltDBIndexClient, loggerWithUserID(table.logger, userID))
		if err != nil {
			return nil, err
		}

		err = userIndexSet.Init(false)
		if err != nil {
			return nil, err
		}

		table.indexSets[userID] = userIndexSet
	}

	commonIndexSet, err := NewIndexSet(name, "", cacheLocation, table.baseCommonIndexSet, boltDBIndexClient, table.logger)
	if err != nil {
		return nil, err
	}

	err = commonIndexSet.Init(false)
	if err != nil {
		return nil, err
	}

	table.indexSets[""] = commonIndexSet

	return &table, nil
}

// Close Closes references to all the dbs.
func (t *table) Close() {
	t.indexSetsMtx.Lock()
	defer t.indexSetsMtx.Unlock()

	for _, userIndexSet := range t.indexSets {
		userIndexSet.Close()
	}

	t.indexSets = map[string]IndexSet{}
}

// MultiQueries runs multiple queries without having to take lock multiple times for each query.
func (t *table) MultiQueries(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}

	// query both user and common index
	for _, uid := range []string{userID, ""} {
		indexSet, err := t.getOrCreateIndexSet(ctx, uid, true)
		if err != nil {
			return err
		}

		if indexSet.Err() != nil {
			level.Error(util_log.WithContext(ctx, t.logger)).Log("msg", fmt.Sprintf("index set %s has some problem, cleaning it up", uid), "err", indexSet.Err())
			if err := indexSet.DropAllDBs(); err != nil {
				level.Error(t.logger).Log("msg", fmt.Sprintf("failed to cleanup broken index set %s", uid), "err", err)
			}

			t.indexSetsMtx.Lock()
			delete(t.indexSets, userID)
			t.indexSetsMtx.Unlock()

			return indexSet.Err()
		}

		err = indexSet.MultiQueries(ctx, queries, callback)
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
			if errors.Is(err, errIndexListCacheTooStale) {
				level.Info(t.logger).Log("msg", "we have hit stale list cache, refreshing it and running sync again")
				t.storageClient.RefreshIndexListCache(ctx)

				err = indexSet.Sync(ctx)
				if err == nil {
					continue
				}
			}
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
	indexSet, err = NewIndexSet(t.name, id, filepath.Join(t.cacheLocation, id), baseIndexSet, t.boltDBIndexClient, loggerWithUserID(t.logger, id))
	if err != nil {
		return nil, err
	}
	t.indexSets[id] = indexSet

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

		err := indexSet.Init(forQuerying)
		if err != nil {
			level.Error(t.logger).Log("msg", fmt.Sprintf("failed to init user index set %s", id), "err", err)
		}
	}()

	return indexSet, nil
}

// EnsureQueryReadiness ensures that we have downloaded the common index as well as user index for the provided userIDs.
// When ensuring query readiness for a table, we will always download common index set because it can include index for one of the provided user ids.
func (t *table) EnsureQueryReadiness(ctx context.Context, userIDs []string) error {
	commonIndexSet, err := t.getOrCreateIndexSet(ctx, "", false)
	if err != nil {
		return err
	}
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
		indexSet, err := t.getOrCreateIndexSet(ctx, userIDs[idx], false)
		if err != nil {
			return err
		}

		return indexSet.AwaitReady(ctx)
	})
}

func loggerWithUserID(logger log.Logger, userID string) log.Logger {
	if userID == "" {
		return logger
	}

	return log.With(logger, "user-id", userID)
}
