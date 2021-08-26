package util

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
)

func Test_GetFileFromStorage(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "get-file-from-storage")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	// write a file to storage.
	testData := []byte("test-data")
	tableName := "test-table"
	require.NoError(t, util.EnsureDirectory(filepath.Join(tempDir, tableName)))
	require.NoError(t, ioutil.WriteFile(filepath.Join(tempDir, tableName, "src"), testData, 0666))

	// try downloading the file from the storage.
	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: tempDir})
	require.NoError(t, err)

	indexStorageClient := storage.NewIndexStorageClient(objectClient, "")

	require.NoError(t, GetFileFromStorage(context.Background(), indexStorageClient, tableName, "src", filepath.Join(tempDir, "dest"), false))

	// verify the contents of the downloaded file.
	b, err := ioutil.ReadFile(filepath.Join(tempDir, "dest"))
	require.NoError(t, err)

	require.Equal(t, testData, b)

	// compress the file in storage
	err = CompressFile(filepath.Join(tempDir, tableName, "src"), filepath.Join(tempDir, tableName, "src.gz"), true)
	require.NoError(t, err)

	// get the compressed file from storage
	require.NoError(t, GetFileFromStorage(context.Background(), indexStorageClient, tableName, "src.gz", filepath.Join(tempDir, "dest.gz"), false))

	// verify the contents of the downloaded gz file.
	b, err = ioutil.ReadFile(filepath.Join(tempDir, "dest.gz"))
	require.NoError(t, err)

	require.Equal(t, testData, b)
}

func Test_CompressFile(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "compress-file")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	uncompressedFilePath := filepath.Join(tempDir, "test-file")
	compressedFilePath := filepath.Join(tempDir, "test-file.gz")
	decompressedFilePath := filepath.Join(tempDir, "test-file-decompressed")

	testData := []byte("test-data")

	require.NoError(t, ioutil.WriteFile(uncompressedFilePath, testData, 0666))

	require.NoError(t, CompressFile(uncompressedFilePath, compressedFilePath, true))
	require.FileExists(t, compressedFilePath)

	testutil.DecompressFile(t, compressedFilePath, decompressedFilePath)
	b, err := ioutil.ReadFile(decompressedFilePath)
	require.NoError(t, err)

	require.Equal(t, testData, b)
}
