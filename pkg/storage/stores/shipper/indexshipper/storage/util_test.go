package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	gzip "github.com/klauspost/pgzip"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func Test_GetFileFromStorage(t *testing.T) {
	tempDir := t.TempDir()

	// write a file to storage.
	testData := []byte("test-data")
	tableName := "test-table"
	require.NoError(t, util.EnsureDirectory(filepath.Join(tempDir, tableName)))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, tableName, "src"), testData, 0o666))

	// try downloading the file from the storage.
	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: tempDir})
	require.NoError(t, err)

	indexStorageClient := NewIndexStorageClient(objectClient, "")

	require.NoError(t, DownloadFileFromStorage(filepath.Join(tempDir, "dest"), false,
		false, util_log.Logger, func() (io.ReadCloser, error) {
			return indexStorageClient.GetFile(context.Background(), tableName, "src")
		}))

	// verify the contents of the downloaded file.
	b, err := os.ReadFile(filepath.Join(tempDir, "dest"))
	require.NoError(t, err)

	require.Equal(t, testData, b)

	// compress the file in storage
	compressFile(t, filepath.Join(tempDir, tableName, "src"), filepath.Join(tempDir, tableName, "src.gz"), true)

	// get the compressed file from storage
	require.NoError(t, DownloadFileFromStorage(filepath.Join(tempDir, "dest.gz"), true,
		false, util_log.Logger, func() (io.ReadCloser, error) {
			return indexStorageClient.GetFile(context.Background(), tableName, "src.gz")
		}))

	// verify the contents of the downloaded gz file.
	b, err = os.ReadFile(filepath.Join(tempDir, "dest.gz"))
	require.NoError(t, err)

	require.Equal(t, testData, b)
}

func compressFile(t *testing.T, src, dest string, sync bool) {
	uncompressedFile, err := os.Open(src)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, uncompressedFile.Close())
	}()

	compressedFile, err := os.Create(dest)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, compressedFile.Close())
	}()

	compressedWriter := gzip.NewWriter(compressedFile)

	_, err = io.Copy(compressedWriter, uncompressedFile)
	require.NoError(t, err)

	err = compressedWriter.Close()
	require.NoError(t, err)

	if sync {
		require.NoError(t, compressedFile.Sync())
	}
}
