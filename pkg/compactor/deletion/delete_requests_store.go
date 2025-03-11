package deletion

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

type DeleteRequestsStoreDBType string

const (
	DeleteRequestsStoreDBTypeBoltDB DeleteRequestsStoreDBType = "boltdb"
	DeleteRequestsStoreDBTypeSQLite DeleteRequestsStoreDBType = "sqlite"

	deleteRequestsWorkingDirName = "delete_requests"
)

var SupportedDeleteRequestsStoreDBTypes = []DeleteRequestsStoreDBType{DeleteRequestsStoreDBTypeBoltDB, DeleteRequestsStoreDBTypeSQLite}

type DeleteRequestsStore interface {
	AddDeleteRequest(ctx context.Context, userID, query string, startTime, endTime model.Time, shardByInterval time.Duration) (string, error)
	addDeleteRequestWithID(ctx context.Context, requestID, userID, query string, startTime, endTime model.Time, shardByInterval time.Duration) error
	GetAllRequests(ctx context.Context) ([]DeleteRequest, error)
	GetAllDeleteRequestsForUser(ctx context.Context, userID string, forQuerytimeFiltering bool) ([]DeleteRequest, error)
	RemoveDeleteRequest(ctx context.Context, userID string, requestID string) error
	GetDeleteRequest(ctx context.Context, userID, requestID string) (DeleteRequest, error)
	GetCacheGenerationNumber(ctx context.Context, userID string) (string, error)
	MergeShardedRequests(ctx context.Context) error

	// ToDo(Sandeep): To keep changeset smaller, below 2 methods treat a single shard as individual request. This can be refactored later in a separate PR.
	MarkShardAsProcessed(ctx context.Context, req DeleteRequest) error
	GetUnprocessedShards(ctx context.Context) ([]DeleteRequest, error)

	Stop()
}

func NewDeleteRequestsStore(
	deleteRequestsStoreDBType DeleteRequestsStoreDBType,
	workingDirectory string,
	indexStorageClient storage.Client,
	backupDeleteRequestStoreDBType DeleteRequestsStoreDBType,
	indexUpdatePropagationMaxDelay time.Duration,
) (DeleteRequestsStore, error) {
	return newDeleteRequestsStore(
		deleteRequestsStoreDBType,
		workingDirectory,
		indexStorageClient,
		backupDeleteRequestStoreDBType,
		indexUpdatePropagationMaxDelay,
	)
}

func newDeleteRequestsStore(
	deleteRequestsStoreDBType DeleteRequestsStoreDBType,
	workingDirectory string,
	indexStorageClient storage.Client,
	backupDeleteRequestStoreDBType DeleteRequestsStoreDBType,
	indexUpdatePropagationMaxDelay time.Duration,
) (DeleteRequestsStore, error) {
	workingDirectory = filepath.Join(workingDirectory, deleteRequestsWorkingDirName)
	store, err := createDeleteRequestsStore(deleteRequestsStoreDBType, workingDirectory, indexStorageClient, indexUpdatePropagationMaxDelay)
	if err != nil {
		return nil, err
	}

	if deleteRequestsStoreDBType == DeleteRequestsStoreDBTypeSQLite {
		deleteRequestsStoreSQLite := store.(*deleteRequestsStoreSQLite)
		sqliteStoreIsEmpty, err := deleteRequestsStoreSQLite.isEmpty(context.Background())
		if err != nil {
			return nil, err
		}

		// copy data from boltdb to sqlite only if the sqlite store is empty
		if sqliteStoreIsEmpty {
			boltdbStore, err := newDeleteRequestsStoreBoltDB(workingDirectory, indexStorageClient)
			if err != nil {
				return nil, err
			}

			shards, cacheGen, err := boltdbStore.getAllData(context.Background())
			if err != nil {
				return nil, err
			}
			boltdbStore.Stop()

			err = deleteRequestsStoreSQLite.copyData(context.Background(), shards, cacheGen)
			if err != nil {
				return nil, err
			}
		}
	} else {
		// we want to cleanup SQLite DB for the scenario when SQLite is rolled back to boltDB and back to SQLite again
		// because we only copy the data from boltDB to SQLite when the db is empty.
		// If the SQLite is left un-empty, we will skip copying data from boltDB next time we move to SQLite again.
		if err := cleanupSQLiteDB(workingDirectory, indexStorageClient); err != nil {
			return nil, err
		}
	}

	if backupDeleteRequestStoreDBType != "" && deleteRequestsStoreDBType != backupDeleteRequestStoreDBType {
		backupStore, err := createDeleteRequestsStore(backupDeleteRequestStoreDBType, workingDirectory, indexStorageClient, indexUpdatePropagationMaxDelay)
		if err != nil {
			return nil, err
		}

		store = newDeleteRequestsStoreTee(store, backupStore)
	}

	return store, nil
}

func createDeleteRequestsStore(
	DeleteRequestsStoreDBType DeleteRequestsStoreDBType,
	workingDirectory string,
	indexStorageClient storage.Client,
	indexUpdatePropagationMaxDelay time.Duration,
) (DeleteRequestsStore, error) {
	switch DeleteRequestsStoreDBType {
	case DeleteRequestsStoreDBTypeBoltDB:
		return newDeleteRequestsStoreBoltDB(workingDirectory, indexStorageClient)
	case DeleteRequestsStoreDBTypeSQLite:
		return newDeleteRequestsStoreSQLite(workingDirectory, indexStorageClient, indexUpdatePropagationMaxDelay)
	default:
		return nil, fmt.Errorf("unexpected delete requests store DB type %s. Supported types: (%s, %s)", DeleteRequestsStoreDBType, DeleteRequestsStoreDBTypeBoltDB, DeleteRequestsStoreDBTypeSQLite)
	}
}

type deleteRequestsStoreTee struct {
	primaryStore, backupStore DeleteRequestsStore
}

func newDeleteRequestsStoreTee(primaryStore, backupStore DeleteRequestsStore) DeleteRequestsStore {
	return deleteRequestsStoreTee{
		primaryStore: primaryStore,
		backupStore:  backupStore,
	}
}

func (d deleteRequestsStoreTee) AddDeleteRequest(ctx context.Context, userID, query string, startTime, endTime model.Time, shardByInterval time.Duration) (string, error) {
	reqID, err := d.primaryStore.AddDeleteRequest(ctx, userID, query, startTime, endTime, shardByInterval)
	if err != nil {
		return "", err
	}

	// Use request ID from primary store to have request with same ID in backup store.
	if err := d.backupStore.addDeleteRequestWithID(ctx, reqID, userID, query, startTime, endTime, shardByInterval); err != nil {
		return "", err
	}

	return reqID, nil
}

func (d deleteRequestsStoreTee) addDeleteRequestWithID(ctx context.Context, requestID, userID, query string, startTime, endTime model.Time, shardByInterval time.Duration) error {
	if err := d.primaryStore.addDeleteRequestWithID(ctx, requestID, userID, query, startTime, endTime, shardByInterval); err != nil {
		return err
	}

	return d.backupStore.addDeleteRequestWithID(ctx, requestID, userID, query, startTime, endTime, shardByInterval)
}

func (d deleteRequestsStoreTee) GetAllRequests(ctx context.Context) ([]DeleteRequest, error) {
	return d.primaryStore.GetAllRequests(ctx)
}

func (d deleteRequestsStoreTee) GetAllDeleteRequestsForUser(ctx context.Context, userID string, forQuerytimeFiltering bool) ([]DeleteRequest, error) {
	return d.primaryStore.GetAllDeleteRequestsForUser(ctx, userID, forQuerytimeFiltering)
}

func (d deleteRequestsStoreTee) RemoveDeleteRequest(ctx context.Context, userID string, requestID string) error {
	if err := d.primaryStore.RemoveDeleteRequest(ctx, userID, requestID); err != nil {
		return err
	}

	return d.backupStore.RemoveDeleteRequest(ctx, userID, requestID)
}

func (d deleteRequestsStoreTee) GetDeleteRequest(ctx context.Context, userID, requestID string) (DeleteRequest, error) {
	return d.primaryStore.GetDeleteRequest(ctx, userID, requestID)
}

func (d deleteRequestsStoreTee) GetCacheGenerationNumber(ctx context.Context, userID string) (string, error) {
	return d.primaryStore.GetCacheGenerationNumber(ctx, userID)
}

func (d deleteRequestsStoreTee) MergeShardedRequests(ctx context.Context) error {
	if err := d.primaryStore.MergeShardedRequests(ctx); err != nil {
		return err
	}

	return d.backupStore.MergeShardedRequests(ctx)
}

func (d deleteRequestsStoreTee) MarkShardAsProcessed(ctx context.Context, req DeleteRequest) error {
	if err := d.primaryStore.MarkShardAsProcessed(ctx, req); err != nil {
		return err
	}

	return d.backupStore.MarkShardAsProcessed(ctx, req)
}

func (d deleteRequestsStoreTee) GetUnprocessedShards(ctx context.Context) ([]DeleteRequest, error) {
	return d.primaryStore.GetUnprocessedShards(ctx)
}

func (d deleteRequestsStoreTee) Stop() {
	d.primaryStore.Stop()
	d.backupStore.Stop()
}
