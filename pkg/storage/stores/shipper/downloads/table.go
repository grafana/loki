package downloads

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/tenant"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/pkg/errors"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	util_log "github.com/grafana/loki/pkg/util/log"
)

// timeout for downloading initial files for a table to avoid leaking resources by allowing it to take all the time.
const (
	downloadTimeout        = 5 * time.Minute
	maxDownloadConcurrency = 50
)

var defaultBucketName = []byte("index")

type BoltDBIndexClient interface {
	QueryWithCursor(_ context.Context, c *bbolt.Cursor, query chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error
}

type StorageClient interface {
	ListTables(ctx context.Context) ([]string, error)
	ListFiles(ctx context.Context, tableName string) ([]storage.IndexFile, error)
	GetFile(ctx context.Context, tableName, fileName string) (io.ReadCloser, error)
	GetUserFile(ctx context.Context, tableName, userID, fileName string) (io.ReadCloser, error)
	IsFileNotFoundErr(err error) bool
}

// Table is a collection of multiple files created for a same table by various ingesters.
// All the public methods are concurrency safe and take care of mutexes to avoid any data race.
type Table struct {
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

// NewTable just creates an instance of Table without trying to load files from local storage or object store.
// It is used for initializing table at query time.
func NewTable(name, cacheLocation string, storageClient storage.Client, boltDBIndexClient BoltDBIndexClient, metrics *metrics) *Table {
	table := Table{
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
func LoadTable(name, cacheLocation string, storageClient storage.Client, boltDBIndexClient BoltDBIndexClient, metrics *metrics) (*Table, error) {
	err := chunk_util.EnsureDirectory(cacheLocation)
	if err != nil {
		return nil, err
	}

	filesInfo, err := ioutil.ReadDir(cacheLocation)
	if err != nil {
		return nil, err
	}

	table := Table{
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

	for _, fileInfo := range filesInfo {
		if !fileInfo.IsDir() {
			continue
		}

		userIndexSet, err := NewIndexSet(name, fileInfo.Name(), filepath.Join(cacheLocation, fileInfo.Name()),
			table.baseUserIndexSet, boltDBIndexClient, table.logger, metrics)
		if err != nil {
			return nil, err
		}

		err = userIndexSet.Init()
		if err != nil {
			return nil, err
		}

		table.indexSets[fileInfo.Name()] = userIndexSet
	}

	commonIndexSet, err := NewIndexSet(name, "", cacheLocation, table.baseCommonIndexSet,
		boltDBIndexClient, table.logger, metrics)
	if err != nil {
		return nil, err
	}

	err = commonIndexSet.Init()
	if err != nil {
		return nil, err
	}

	table.indexSets[""] = commonIndexSet

	return &table, nil
}

// Close Closes references to all the dbs.
func (t *Table) Close() {
	t.indexSetsMtx.Lock()
	defer t.indexSetsMtx.Unlock()

	for _, userIndexSet := range t.indexSets {
		userIndexSet.Close()
	}

	t.indexSets = map[string]IndexSet{}
}

// MultiQueries runs multiple queries without having to take lock multiple times for each query.
func (t *Table) MultiQueries(ctx context.Context, queries []chunk.IndexQuery, callback chunk_util.Callback) error {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}

	for _, uid := range []string{userID, ""} {
		indexSet, err := t.getOrCreateIndexSet(uid, true)
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

// DropUnusedIndex drops the index set if it has not been queried for at least ttl duration.
// It returns true if the whole table gets dropped.
func (t *Table) DropUnusedIndex(ttl time.Duration, now time.Time) (bool, error) {
	var cleanedUpIndexSets []string

	t.indexSetsMtx.RLock()
	for userID, userIndexSet := range t.indexSets {
		lastUsedAt := userIndexSet.LastUsedAt()
		if lastUsedAt.Add(ttl).Before(now) {
			cleanedUpIndexSets = append(cleanedUpIndexSets, userID)
		}
	}
	t.indexSetsMtx.RUnlock()

	if len(cleanedUpIndexSets) > 0 {
		t.indexSetsMtx.Lock()
		defer t.indexSetsMtx.Unlock()
		for _, userID := range cleanedUpIndexSets {
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
func (t *Table) Sync(ctx context.Context) error {
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

func (t *Table) getOrCreateIndexSet(id string, async bool) (IndexSet, error) {
	t.indexSetsMtx.RLock()
	indexSet, ok := t.indexSets[id]
	t.indexSetsMtx.RUnlock()
	if ok {
		return indexSet, nil
	}

	t.indexSetsMtx.Lock()

	indexSet, ok = t.indexSets[id]
	if !ok {
		var err error
		baseIndexSet := t.baseUserIndexSet
		if id == "" {
			baseIndexSet = t.baseCommonIndexSet
		}

		indexSet, err = NewIndexSet(t.name, id, filepath.Join(t.cacheLocation, id), baseIndexSet, t.boltDBIndexClient, t.logger, t.metrics)
		if err != nil {
			return nil, err
		}
		t.indexSets[id] = indexSet

	}
	t.indexSetsMtx.Unlock()

	// We want to do the initialization of table in async mode while serving the queries to honor their timeouts while
	// for background tasks like ensuring query readiness, we want to do the initialization in synchronous mode.
	// The idea here is that we want to create an instance of indexSet first and set its reference so that
	// no other goroutine creates another instance of indexSet.
	if async {
		go func() {
			err := indexSet.Init()
			if err != nil {
				level.Error(t.logger).Log("msg", fmt.Sprintf("failed to init user index set %s", id), "err", err)
			}
		}()
	} else {
		err := indexSet.Init()
		if err != nil {
			return nil, err
		}
	}

	return indexSet, nil
}

func (t *Table) EnsureQueryReadiness(ctx context.Context) error {
	_, userIDs, err := t.storageClient.ListFiles(ctx, t.name)
	if err != nil {
		return err
	}

	commonIndexSet, err := t.getOrCreateIndexSet("", false)
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
func (t *Table) downloadUserIndexes(ctx context.Context, userIDs []string) error {
	return concurrency.ForEach(ctx, concurrency.CreateJobsFromStrings(userIDs), maxDownloadConcurrency, func(ctx context.Context, userID interface{}) error {
		_, err := t.getOrCreateIndexSet(userID.(string), false)
		return err
	})
}
