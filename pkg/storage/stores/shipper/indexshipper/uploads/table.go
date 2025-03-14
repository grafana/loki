package uploads

import (
	"context"
	"sync"
	"time"

	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	// a temp file is created during uploads with name of the db + tempFileSuffix
	tempFileSuffix = ".temp"
)

type Table interface {
	Name() string
	AddIndex(userID string, idx index.Index) error
	ForEach(userID string, callback index.ForEachIndexCallback) error
	Upload(ctx context.Context) error
	Cleanup(indexRetainPeriod time.Duration) error
	Stop()
}

// table is a collection of one or more index files created by the ingester or compacted by the compactor.
// A table would provide a logical grouping for index by a period. This period is controlled by `schema_config.configs.index.period` config.
// It contains index for all the users sending logs to Loki.
// All the public methods are concurrency safe and take care of mutexes to avoid any data race.
type table struct {
	name                                 string
	baseUserIndexSet, baseCommonIndexSet storage.IndexSet
	logger                               log.Logger

	indexSet    map[string]IndexSet
	indexSetMtx sync.RWMutex
}

// NewTable create a new table instance.
func NewTable(name string, storageClient storage.Client) Table {
	return &table{
		name:               name,
		baseUserIndexSet:   storage.NewIndexSet(storageClient, true),
		baseCommonIndexSet: storage.NewIndexSet(storageClient, false),
		logger:             log.With(util_log.Logger, "table-name", name),
		indexSet:           make(map[string]IndexSet),
	}
}

// Name returns the name of the table.
func (lt *table) Name() string {
	return lt.name
}

// AddIndex adds a new index to the table.
func (lt *table) AddIndex(userID string, idx index.Index) error {
	lt.indexSetMtx.Lock()
	defer lt.indexSetMtx.Unlock()

	if _, ok := lt.indexSet[userID]; !ok {
		baseIndexSet := lt.baseUserIndexSet
		if userID == "" {
			baseIndexSet = lt.baseCommonIndexSet
		}
		idxSet, err := NewIndexSet(lt.name, userID, baseIndexSet, loggerWithUserID(lt.logger, userID))
		if err != nil {
			return err
		}

		lt.indexSet[userID] = idxSet
	}

	lt.indexSet[userID].Add(idx)
	return nil
}

// ForEach iterates over all the indexes belonging to the user.
func (lt *table) ForEach(userID string, callback index.ForEachIndexCallback) error {
	lt.indexSetMtx.RLock()
	defer lt.indexSetMtx.RUnlock()

	// TODO(owen-d): refactor? Uploads mgr never has user indices,
	// only common (multitenant) ones.
	// iterate through both user and common index
	for _, uid := range []string{userID, ""} {
		idxSet, ok := lt.indexSet[uid]
		if !ok {
			continue
		}

		if err := idxSet.ForEach(callback); err != nil {
			return err
		}
	}
	return nil
}

// Upload uploads the index to object storage.
func (lt *table) Upload(ctx context.Context) error {
	lt.indexSetMtx.RLock()
	defer lt.indexSetMtx.RUnlock()

	for _, indexSet := range lt.indexSet {
		if err := indexSet.Upload(ctx); err != nil {
			return err
		}
	}

	return nil
}

// Cleanup removes indexes which are already uploaded and have been retained for period longer than indexRetainPeriod since they were uploaded.
func (lt *table) Cleanup(indexRetainPeriod time.Duration) error {
	lt.indexSetMtx.RLock()
	defer lt.indexSetMtx.RUnlock()

	for _, indexSet := range lt.indexSet {
		if err := indexSet.Cleanup(indexRetainPeriod); err != nil {
			return err
		}
	}

	return nil
}

// Stop closes all the open dbs.
func (lt *table) Stop() {
	lt.indexSetMtx.Lock()
	defer lt.indexSetMtx.Unlock()

	for _, indexSet := range lt.indexSet {
		indexSet.Close()
	}
}

func loggerWithUserID(logger log.Logger, userID string) log.Logger {
	if userID == "" {
		return logger
	}

	return log.With(logger, "user-id", userID)
}
