package uploads

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

const objectsStorageDirName = "objects"

func buildTestStorageClient(t *testing.T, path string) storage.Client {
	objectStoragePath := filepath.Join(path, objectsStorageDirName)
	fsObjectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	return storage.NewIndexStorageClient(fsObjectClient, "")
}

type stopFunc func()

func buildTestTableManager(t *testing.T, testDir string) (TableManager, stopFunc) {
	storageClient := buildTestStorageClient(t, testDir)

	cfg := Config{
		UploadInterval: time.Hour,
	}
	tm, err := NewTableManager(cfg, storageClient, nil, log.NewNopLogger())
	require.NoError(t, err)

	return tm, func() {
		tm.Stop()
		require.NoError(t, os.RemoveAll(testDir))
	}
}

func TestTableManager_UploadTables(t *testing.T) {
	testDir := t.TempDir()

	tm, stopFunc := buildTestTableManager(t, testDir)
	defer stopFunc()

	const tableName = "table-1"
	const userID = "user-1"

	userIndexPath := filepath.Join(testDir, tableName, userID)
	require.NoError(t, os.MkdirAll(userIndexPath, 0755))

	testIndexes := buildTestIndexes(t, userIndexPath, 3)
	for _, testIndex := range testIndexes {
		require.NoError(t, tm.AddIndex(tableName, userID, testIndex))
	}

	// Synchronously upload all tables and ensure it surfaces success.
	require.NoError(t, tm.UploadTables(context.Background()))

	// The indexes should now be present in object storage.
	uploadedDir := filepath.Join(testDir, objectsStorageDirName, tableName, userID)
	entries, err := os.ReadDir(uploadedDir)
	require.NoError(t, err)
	require.Len(t, entries, len(testIndexes))
}

func TestTableManager(t *testing.T) {
	testDir := t.TempDir()

	testTableManager, stopFunc := buildTestTableManager(t, testDir)
	defer stopFunc()

	for tableIdx := 0; tableIdx < 2; tableIdx++ {
		tableName := "table-" + strconv.Itoa(tableIdx)
		t.Run(tableName, func(t *testing.T) {
			for userIdx := 0; userIdx < 2; userIdx++ {
				userID := "user-" + strconv.Itoa(userIdx)
				t.Run(userID, func(t *testing.T) {
					userIndexPath := filepath.Join(testDir, tableName, userID)
					require.NoError(t, os.MkdirAll(userIndexPath, 0755))

					// build some test indexes and add them to the table.
					testIndexes := buildTestIndexes(t, userIndexPath, 5)
					for _, testIndex := range testIndexes {
						require.NoError(t, testTableManager.AddIndex(tableName, userID, testIndex))
					}

					// see if we can find all the added indexes in the table.
					indexesFound := map[string]*mockIndex{}
					err := testTableManager.ForEach(tableName, userID, func(_ bool, index index.Index) error {
						indexesFound[index.Path()] = index.(*mockIndex)
						return nil
					})
					require.NoError(t, err)

					require.Equal(t, testIndexes, indexesFound)
				})
			}
		})
	}
}
