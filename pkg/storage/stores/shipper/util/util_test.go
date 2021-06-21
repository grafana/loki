package util

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
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
	require.NoError(t, ioutil.WriteFile(filepath.Join(tempDir, "src"), testData, 0666))

	// try downloading the file from the storage.
	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: tempDir})
	require.NoError(t, err)

	require.NoError(t, GetFileFromStorage(context.Background(), objectClient, "src", filepath.Join(tempDir, "dest"), false))

	// verify the contents of the downloaded file.
	b, err := ioutil.ReadFile(filepath.Join(tempDir, "dest"))
	require.NoError(t, err)

	require.Equal(t, testData, b)

	// compress the file in storage
	err = CompressFile(filepath.Join(tempDir, "src"), filepath.Join(tempDir, "src.gz"), true)
	require.NoError(t, err)

	// get the compressed file from storage
	require.NoError(t, GetFileFromStorage(context.Background(), objectClient, "src.gz", filepath.Join(tempDir, "dest.gz"), false))

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

func TestRemoveDirectories(t *testing.T) {
	tests := []struct {
		name     string
		incoming []chunk.StorageObject
		expected []chunk.StorageObject
	}{
		{
			name: "no trailing slash",
			incoming: []chunk.StorageObject{
				{Key: "obj1"},
				{Key: "obj2"},
				{Key: "obj3"},
			},
			expected: []chunk.StorageObject{
				{Key: "obj1"},
				{Key: "obj2"},
				{Key: "obj3"},
			},
		},
		{
			name: "one trailing slash",
			incoming: []chunk.StorageObject{
				{Key: "obj1"},
				{Key: "obj2/"},
				{Key: "obj3"},
			},
			expected: []chunk.StorageObject{
				{Key: "obj1"},
				{Key: "obj3"},
			},
		},
		{
			name: "only trailing slash",
			incoming: []chunk.StorageObject{
				{Key: "obj1"},
				{Key: "obj2"},
				{Key: "/"},
			},
			expected: []chunk.StorageObject{
				{Key: "obj1"},
				{Key: "obj2"},
			},
		},
		{
			name: "all trailing slash",
			incoming: []chunk.StorageObject{
				{Key: "/"},
				{Key: "/"},
				{Key: "/"},
			},
			expected: []chunk.StorageObject{},
		},
		{
			name: "internal slash",
			incoming: []chunk.StorageObject{
				{Key: "test/test1"},
				{Key: "te/st"},
				{Key: "/sted"},
			},
			expected: []chunk.StorageObject{
				{Key: "test/test1"},
				{Key: "te/st"},
				{Key: "/sted"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, RemoveDirectories(test.incoming))
		})
	}
}
