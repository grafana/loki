package compactor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
	chunk_util "github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	gzipExtension = ".gz"
)

var errRetentionFileCountNotOne = fmt.Errorf("can't apply retention when index file count is not one")

type tableExpirationChecker interface {
	IntervalMayHaveExpiredChunks(interval model.Interval, userID string) bool
}

type IndexCompactor interface {
	// NewTableCompactor returns a new TableCompactor for compacting a table.
	// commonIndexSet refers to common index files or in other words multi-tenant index.
	// existingUserIndexSet refers to existing user specific index files in the storage.
	// makeEmptyUserIndexSetFunc can be used for creating an empty indexSet for a user
	// who does not have an index for it in existingUserIndexSet.
	// periodConfig holds the PeriodConfig for the table.
	NewTableCompactor(
		ctx context.Context,
		commonIndexSet IndexSet,
		existingUserIndexSet map[string]IndexSet,
		makeEmptyUserIndexSetFunc MakeEmptyUserIndexSetFunc,
		periodConfig config.PeriodConfig,
	) TableCompactor

	// OpenCompactedIndexFile opens a compressed index file at given path.
	OpenCompactedIndexFile(
		ctx context.Context,
		path,
		tableName,
		userID,
		workingDir string,
		periodConfig config.PeriodConfig,
		logger log.Logger,
	) (
		CompactedIndex,
		error,
	)
}

type TableCompactor interface {
	// CompactTable compacts the table.
	// After compaction is done successfully, it should set the new/updated CompactedIndex for relevant IndexSets.
	CompactTable() (err error)
}

type MakeEmptyUserIndexSetFunc func(userID string) (IndexSet, error)

type table struct {
	name               string
	workingDirectory   string
	uploadConcurrency  int
	indexStorageClient storage.Client
	indexCompactor     IndexCompactor
	tableMarker        retention.TableMarker
	expirationChecker  tableExpirationChecker
	periodConfig       config.PeriodConfig

	baseUserIndexSet, baseCommonIndexSet storage.IndexSet

	indexSets             map[string]*indexSet
	usersWithPerUserIndex []string
	logger                log.Logger

	ctx context.Context
}

func newTable(ctx context.Context, workingDirectory string, indexStorageClient storage.Client,
	indexCompactor IndexCompactor, periodConfig config.PeriodConfig,
	tableMarker retention.TableMarker, expirationChecker tableExpirationChecker,
	uploadConcurrency int,
) (*table, error) {
	err := chunk_util.EnsureDirectory(workingDirectory)
	if err != nil {
		return nil, err
	}

	table := table{
		ctx:                ctx,
		name:               filepath.Base(workingDirectory),
		workingDirectory:   workingDirectory,
		indexStorageClient: indexStorageClient,
		indexCompactor:     indexCompactor,
		tableMarker:        tableMarker,
		expirationChecker:  expirationChecker,
		periodConfig:       periodConfig,
		indexSets:          map[string]*indexSet{},
		baseUserIndexSet:   storage.NewIndexSet(indexStorageClient, true),
		baseCommonIndexSet: storage.NewIndexSet(indexStorageClient, false),
		uploadConcurrency:  uploadConcurrency,
	}
	table.logger = log.With(util_log.Logger, "table-name", table.name)

	return &table, nil
}

func (t *table) compact(applyRetention bool) error {
	t.indexStorageClient.RefreshIndexTableCache(t.ctx, t.name)
	indexFiles, usersWithPerUserIndex, err := t.indexStorageClient.ListFiles(t.ctx, t.name, false)
	if err != nil {
		return err
	}

	if len(indexFiles) == 0 && len(usersWithPerUserIndex) == 0 {
		level.Info(t.logger).Log("msg", "no common index files and user index found")
		return nil
	}

	t.usersWithPerUserIndex = usersWithPerUserIndex

	level.Info(t.logger).Log("msg", "listed files", "count", len(indexFiles))

	defer func() {
		for _, is := range t.indexSets {
			is.cleanup()
		}

		if err := os.RemoveAll(t.workingDirectory); err != nil {
			level.Error(t.logger).Log("msg", fmt.Sprintf("failed to remove working directory %s", t.workingDirectory), "err", err)
		}
	}()

	t.indexSets[""], err = newCommonIndexSet(t.ctx, t.name, t.baseCommonIndexSet, t.workingDirectory, t.logger)
	if err != nil {
		return err
	}

	// userIndexSets is just for passing it to NewTableCompactor since go considers map[string]*indexSet different type than map[string]IndexSet
	userIndexSets := make(map[string]IndexSet, len(t.usersWithPerUserIndex))

	for _, userID := range t.usersWithPerUserIndex {
		var err error
		t.indexSets[userID], err = newUserIndexSet(t.ctx, t.name, userID, t.baseUserIndexSet, filepath.Join(t.workingDirectory, userID), t.logger)
		if err != nil {
			return err
		}
		userIndexSets[userID] = t.indexSets[userID]
	}

	// protect indexSets with mutex so that we are concurrency safe if the TableCompactor calls MakeEmptyUserIndexSetFunc concurrently
	indexSetsMtx := sync.Mutex{}
	tableCompactor := t.indexCompactor.NewTableCompactor(t.ctx, t.indexSets[""], userIndexSets, func(userID string) (IndexSet, error) {
		indexSetsMtx.Lock()
		defer indexSetsMtx.Unlock()

		var err error
		t.indexSets[userID], err = newUserIndexSet(t.ctx, t.name, userID, t.baseUserIndexSet, filepath.Join(t.workingDirectory, userID), t.logger)
		return t.indexSets[userID], err
	}, t.periodConfig)

	err = tableCompactor.CompactTable()
	if err != nil {
		return err
	}

	if applyRetention {
		err := t.applyRetention()
		if err != nil {
			return err
		}
	}

	return t.done()
}

func (t *table) done() error {
	userIDs := make([]string, 0, len(t.indexSets))
	for userID := range t.indexSets {
		// indexSet.done() uploads the compacted db and cleans up the source index files.
		// For user index sets, the files from common index sets are also a source of index.
		// if we cleanup common index sets first, and we fail to upload newly compacted dbs in user index sets, then we will lose data.
		// To avoid any data loss, we should call done() on common index sets at the end.
		if userID == "" {
			continue
		}

		userIDs = append(userIDs, userID)
	}

	err := concurrency.ForEachJob(t.ctx, len(userIDs), t.uploadConcurrency, func(_ context.Context, idx int) error {
		return t.indexSets[userIDs[idx]].done()
	})
	if err != nil {
		return err
	}

	if commonIndexSet, ok := t.indexSets[""]; ok {
		if err := commonIndexSet.done(); err != nil {
			return err
		}
	}

	return nil
}

// applyRetention applies retention on the index sets
func (t *table) applyRetention() error {
	tableInterval := retention.ExtractIntervalFromTableName(t.name)
	// call runRetention on the index sets which may have expired chunks
	for userID, is := range t.indexSets {
		// make sure we do not apply retention on common index set which got compacted away to per-user index
		if userID == "" && is.compactedIndex == nil && is.removeSourceObjects && !is.uploadCompactedDB {
			continue
		}

		if !t.expirationChecker.IntervalMayHaveExpiredChunks(tableInterval, userID) {
			continue
		}

		// compactedIndex is only set in indexSet when files have been compacted,
		// so we need to open the compacted index file for applying retention if compactedIndex is nil
		if is.compactedIndex == nil && len(is.ListSourceFiles()) == 1 {
			if err := t.openCompactedIndexForRetention(is); err != nil {
				return err
			}
		}

		err := is.runRetention(t.tableMarker)
		if err != nil {
			return err
		}
	}

	return nil
}

func (t *table) openCompactedIndexForRetention(idxSet *indexSet) error {
	sourceFiles := idxSet.ListSourceFiles()
	if len(sourceFiles) != 1 {
		return errRetentionFileCountNotOne
	}

	downloadedAt, err := idxSet.GetSourceFile(sourceFiles[0])
	if err != nil {
		return err
	}

	compactedIndexFile, err := t.indexCompactor.OpenCompactedIndexFile(t.ctx, downloadedAt, t.name, idxSet.userID, filepath.Join(t.workingDirectory, idxSet.userID), t.periodConfig, idxSet.logger)
	if err != nil {
		return err
	}

	idxSet.setCompactedIndex(compactedIndexFile, false, false)

	return nil
}

// tableHasUncompactedIndex returns true if we have more than "1" common index files.
// We are checking for more than "1" because earlier boltdb-shipper index type did not have per tenant index so there would be only common index files.
// In case of per tenant index, it is okay to consider it compacted since having just 1 uncompacted index file for a while should be fine.
func tableHasUncompactedIndex(ctx context.Context, tableName string, indexStorageClient storage.Client) (bool, error) {
	commonIndexFiles, _, err := indexStorageClient.ListFiles(ctx, tableName, false)
	return len(commonIndexFiles) > 1, err
}
