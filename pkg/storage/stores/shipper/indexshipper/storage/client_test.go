package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
)

func TestIndexStorageClient(t *testing.T) {
	tempDir := t.TempDir()

	storageKeyPrefix := "prefix/"
	tablesToSetup := map[string][]string{
		"table1": {"a", "b"},
		"table2": {"b", "c", "d"},
	}

	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: tempDir})
	require.NoError(t, err)

	for tableName, files := range tablesToSetup {
		require.NoError(t, util.EnsureDirectory(filepath.Join(tempDir, storageKeyPrefix, tableName)))
		for _, file := range files {
			err := os.WriteFile(filepath.Join(tempDir, storageKeyPrefix, tableName, file), []byte(tableName+file), 0o666)
			require.NoError(t, err)
		}
	}

	indexStorageClient := NewIndexStorageClient(objectClient, storageKeyPrefix)

	verifyFiles := func() {
		tables, err := indexStorageClient.ListTables(context.Background())
		require.NoError(t, err)
		require.Len(t, tables, len(tablesToSetup))
		for _, table := range tables {
			expectedFiles, ok := tablesToSetup[table]
			require.True(t, ok)

			indexStorageClient.RefreshIndexTableCache(context.Background(), table)
			filesInStorage, _, err := indexStorageClient.ListFiles(context.Background(), table, false)
			require.NoError(t, err)
			require.Len(t, filesInStorage, len(expectedFiles))

			for i, fileInStorage := range filesInStorage {
				require.Equal(t, expectedFiles[i], fileInStorage.Name)
				readCloser, err := indexStorageClient.GetFile(context.Background(), table, fileInStorage.Name)
				require.NoError(t, err)

				b, err := io.ReadAll(readCloser)
				require.NoError(t, readCloser.Close())
				require.NoError(t, err)
				require.EqualValues(t, []byte(table+fileInStorage.Name), b)
			}
		}
	}

	// verify the files using indexStorageClient
	verifyFiles()

	// delete a file and verify them again
	require.NoError(t, indexStorageClient.DeleteFile(context.Background(), "table2", "d"))
	tablesToSetup["table2"] = tablesToSetup["table2"][:2]
	verifyFiles()

	// add a file and verify them again
	require.NoError(t, indexStorageClient.PutFile(context.Background(), "table2", "e", bytes.NewReader([]byte("table2"+"e"))))
	tablesToSetup["table2"] = append(tablesToSetup["table2"], "e")
	verifyFiles()
}
