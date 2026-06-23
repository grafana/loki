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

// Test_DownloadFileFromStorage_PreservesDestinationOnEarlyError verifies that if the
// download fails before the destination file is touched (e.g. getFileFunc returns an
// error), a pre-existing destination file is left untouched.
func Test_DownloadFileFromStorage_PreservesDestinationOnEarlyError(t *testing.T) {
	tempDir := t.TempDir()
	dst := filepath.Join(tempDir, "dest")
	original := []byte("pre-existing content")
	require.NoError(t, os.WriteFile(dst, original, 0o666))

	err := DownloadFileFromStorage(dst, false, false, util_log.Logger, func() (io.ReadCloser, error) {
		return nil, io.ErrUnexpectedEOF
	})
	require.Error(t, err)

	b, statErr := os.ReadFile(dst)
	require.NoError(t, statErr, "pre-existing destination should be untouched on early error")
	require.Equal(t, original, b)
}

// Test_DownloadFileFromStorage_TruncatedGzip verifies that when the source gzip is
// truncated (which mimics a storage backend returning a short body, see issue #21736),
// DownloadFileFromStorage does not leave a partial destination file on disk.
func Test_DownloadFileFromStorage_TruncatedGzip(t *testing.T) {
	tempDir := t.TempDir()
	tableName := "test-table"
	require.NoError(t, util.EnsureDirectory(filepath.Join(tempDir, tableName)))

	// Write a valid source, compress it, then truncate the gzipped output to simulate
	// a short-body response from object storage.
	srcPath := filepath.Join(tempDir, tableName, "src")
	require.NoError(t, os.WriteFile(srcPath, []byte("the quick brown fox jumps over the lazy dog"), 0o666))

	gzPath := filepath.Join(tempDir, tableName, "src.gz")
	compressFile(t, srcPath, gzPath, true)

	info, err := os.Stat(gzPath)
	require.NoError(t, err)
	// Truncate the last few bytes, enough to corrupt the gzip trailer.
	require.NoError(t, os.Truncate(gzPath, info.Size()-5))

	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: tempDir})
	require.NoError(t, err)
	indexStorageClient := NewIndexStorageClient(objectClient, "")

	dst := filepath.Join(tempDir, "dest")
	err = DownloadFileFromStorage(dst, true, false, util_log.Logger, func() (io.ReadCloser, error) {
		return indexStorageClient.GetFile(context.Background(), tableName, "src.gz")
	})
	require.Error(t, err, "expected error for truncated gzip")

	_, statErr := os.Stat(dst)
	require.True(t, os.IsNotExist(statErr),
		"destination file should be removed when download fails, but it exists: stat err = %v", statErr)
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
