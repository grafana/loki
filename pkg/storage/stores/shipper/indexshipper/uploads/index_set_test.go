package uploads

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/testutil"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const userID = "user-id"

func TestIndexSet_Add(t *testing.T) {
	tempDir := t.TempDir()
	testStorageClient := buildTestStorageClient(t, tempDir)

	for _, userID := range []string{userID, ""} {
		t.Run(userID, func(t *testing.T) {
			indexSet, err := NewIndexSet(testTableName, userID, storage.NewIndexSet(testStorageClient, userID != ""), util_log.Logger)
			require.NoError(t, err)

			defer indexSet.Close()

			testIndexes := buildTestIndexes(t, t.TempDir(), 10)
			for _, testIndex := range testIndexes {
				indexSet.Add(testIndex)
			}

			// see if we can find all the added indexes in the table.
			indexesFound := map[string]*mockIndex{}
			err = indexSet.ForEach(func(_ bool, index index.Index) error {
				indexesFound[index.Path()] = index.(*mockIndex)
				return nil
			})
			require.NoError(t, err)

			require.Equal(t, testIndexes, indexesFound)
		})
	}
}

func TestIndexSet_Upload(t *testing.T) {
	tempDir := t.TempDir()
	testStorageClient := buildTestStorageClient(t, tempDir)

	for _, userID := range []string{userID, ""} {
		t.Run(userID, func(t *testing.T) {
			idxSet, err := NewIndexSet(testTableName, userID, storage.NewIndexSet(testStorageClient, userID != ""), util_log.Logger)
			require.NoError(t, err)

			defer idxSet.Close()

			testIndexes := buildTestIndexes(t, t.TempDir(), 5)
			for _, testIndex := range testIndexes {
				idxSet.Add(testIndex)
			}

			err = idxSet.Upload(context.Background())
			require.NoError(t, err)

			for _, testIndex := range testIndexes {
				indexPathInStorage := filepath.Join(tempDir, objectsStorageDirName, testTableName, userID, idxSet.(*indexSet).buildFileName(testIndex.Name()))
				require.FileExists(t, indexPathInStorage)

				// compare the contents of created test index and uploaded index in storage
				_, err = testIndex.Seek(0, 0)
				require.NoError(t, err)
				expectedIndexContent, err := io.ReadAll(testIndex.File)
				require.NoError(t, err)
				require.Equal(t, expectedIndexContent, readCompressedFile(t, indexPathInStorage))
			}
		})
	}
}

func TestIndexSet_Cleanup(t *testing.T) {
	dbRetainPeriod := 5 * time.Minute
	tempDir := t.TempDir()
	testStorageClient := buildTestStorageClient(t, tempDir)

	for _, userID := range []string{userID, ""} {
		t.Run(userID, func(t *testing.T) {
			idxSet, err := NewIndexSet(testTableName, userID, storage.NewIndexSet(testStorageClient, userID != ""), util_log.Logger)
			require.NoError(t, err)
			defer idxSet.Close()

			testIndexes := buildTestIndexes(t, t.TempDir(), 5)
			for _, testIndex := range testIndexes {
				idxSet.Add(testIndex)
			}

			// upload the indexes
			err = idxSet.Upload(context.Background())
			require.NoError(t, err)

			// cleanup the indexes outside the retention period
			err = idxSet.Cleanup(dbRetainPeriod)
			require.NoError(t, err)

			// all the indexes should be retained since they were just uploaded
			indexesFound := map[string]*mockIndex{}
			err = idxSet.ForEach(func(_ bool, index index.Index) error {
				indexesFound[index.Path()] = index.(*mockIndex)
				return nil
			})
			require.NoError(t, err)

			require.Equal(t, testIndexes, indexesFound)

			// change the upload time of some of the indexes to now-(retention period+minute) so that they get dropped during cleanup
			indexToCleanup := map[string]struct{}{}
			for _, testIndex := range testIndexes {
				indexToCleanup[testIndex.Path()] = struct{}{}
				idxSet.(*indexSet).indexUploadTime[testIndex.Name()] = time.Now().Add(-(dbRetainPeriod + time.Minute))
				if len(indexToCleanup) == 2 {
					break
				}
			}

			// cleanup the indexes outside the retention period
			err = idxSet.Cleanup(dbRetainPeriod)
			require.NoError(t, err)

			// get all the indexes that are retained
			indexesFound = map[string]*mockIndex{}
			err = idxSet.ForEach(func(_ bool, index index.Index) error {
				indexesFound[index.Path()] = index.(*mockIndex)
				return nil
			})
			require.NoError(t, err)

			// we should have only the indexes whose upload time was not changed above
			require.Len(t, indexesFound, len(testIndexes)-(len(indexToCleanup)))
			for _, testIndex := range testIndexes {
				if _, ok := indexToCleanup[testIndex.Path()]; ok {
					// make sure that file backing the index is dropped from local disk
					require.NoFileExists(t, testIndex.Path())
					continue
				}
				require.Contains(t, indexesFound, testIndex.Path())
			}
		})
	}
}

// readCompressedFile reads the contents of a compressed file at given path.
func readCompressedFile(t *testing.T, path string) []byte {
	tempDir := t.TempDir()
	decompressedFilePath := filepath.Join(tempDir, "decompressed")
	testutil.DecompressFile(t, path, decompressedFilePath)

	fileContent, err := os.ReadFile(decompressedFilePath)
	require.NoError(t, err)

	return fileContent
}
