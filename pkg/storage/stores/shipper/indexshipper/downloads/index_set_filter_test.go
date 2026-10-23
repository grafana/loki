package downloads

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// evenIndexFiles keeps the files whose index number is even, standing in for
// the files of the shards an index gateway owns.
func evenIndexFiles(files []storage.IndexFile) ([]storage.IndexFile, error) {
	var kept []storage.IndexFile
	for _, f := range files {
		n, err := strconv.Atoi(f.Name[strings.LastIndex(f.Name, "-")+1:])
		if err != nil {
			return nil, err
		}
		if n%2 == 0 {
			kept = append(kept, f)
		}
	}
	return kept, nil
}

// TestIndexSet_FileFilter checks that the file filter applies to index sets
// kept ready for queries only: one downloaded on demand holds every file, and
// drops the files the filter leaves out once it is marked query ready.
func TestIndexSet_FileFilter(t *testing.T) {
	tempDir := t.TempDir()
	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	setupIndexesAtPath(t, userID, filepath.Join(objectStoragePath, tableName, userID), 0, 10)
	all := buildListOfExpectedIndexes(userID, 0, 10)
	var even []string
	for i, name := range all {
		if i%2 == 0 {
			even = append(even, name)
		}
	}

	storageClient := buildTestStorageClient(t, tempDir)
	baseIndexSet := storage.NewIndexSet(storageClient, true)
	newSet := func(t *testing.T, queryReady bool) *indexSet {
		t.Helper()
		set, err := newIndexSet(tableName, userID, filepath.Join(t.TempDir(), tableName, userID), baseIndexSet,
			func(path string) (index.Index, error) {
				return openMockIndexFile(t, path), nil
			}, util_log.Logger, testDownloadTimeout, queryReady)
		require.NoError(t, err)
		set.fileFilter = evenIndexFiles
		t.Cleanup(set.Close)
		return set
	}
	names := func(set *indexSet) []string {
		var got []string
		require.NoError(t, set.ForEach(context.Background(), func(_ bool, idx index.Index) error {
			got = append(got, filepath.Base(idx.Path()))
			return nil
		}))
		return got
	}

	t.Run("kept ready for queries from the start downloads only the filtered files", func(t *testing.T) {
		set := newSet(t, true)
		require.NoError(t, set.Init(false, util_log.Logger))
		require.ElementsMatch(t, even, names(set))
	})

	t.Run("downloaded on demand holds every file until it is query ready", func(t *testing.T) {
		set := newSet(t, false)
		require.NoError(t, set.Init(true, util_log.Logger))
		require.ElementsMatch(t, all, names(set))

		// a sync while still on demand keeps everything
		require.NoError(t, set.Sync(context.Background()))
		require.ElementsMatch(t, all, names(set))

		// once query ready, the next sync drops the files the filter leaves out
		set.MarkQueryReady()
		require.NoError(t, set.Sync(context.Background()))
		require.ElementsMatch(t, even, names(set))
	})

	t.Run("a filter error fails the sync", func(t *testing.T) {
		set := newSet(t, true)
		set.fileFilter = func([]storage.IndexFile) ([]storage.IndexFile, error) {
			return nil, context.DeadlineExceeded
		}
		require.ErrorIs(t, set.Init(false, util_log.Logger), context.DeadlineExceeded)
	})
}

func TestTableQueryReadinessRefreshesOwnedFiles(t *testing.T) {
	tempDir := t.TempDir()
	setupReconcileTable(t, tempDir, "u1")
	keep := "u1-0"
	filter := func(_, _ string, files []storage.IndexFile) ([]storage.IndexFile, error) {
		var owned []storage.IndexFile
		for _, file := range files {
			if file.Name == keep {
				owned = append(owned, file)
			}
		}
		return owned, nil
	}
	tbl := newReconcileTable(t, tempDir, newOpenRecorder(), tableOptions{fileFilter: filter})
	ctx := context.Background()
	require.NoError(t, tbl.EnsureQueryReadiness(ctx, []string{"u1"}))
	require.ElementsMatch(t, []string{"0", "1", "u1-0"}, forEachNames(t, tbl, "u1"))
	keep = "u1-1"
	// Reconciliation must refresh an already-query-ready tenant immediately,
	// without waiting for the independent periodic sync loop.
	require.NoError(t, tbl.EnsureQueryReadiness(ctx, []string{"u1"}))
	require.ElementsMatch(t, []string{"0", "1", "u1-1"}, forEachNames(t, tbl, "u1"))
}
