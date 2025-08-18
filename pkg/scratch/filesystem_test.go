package scratch

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

// TestFilesystem_Remove_deletes_files ensures that calling
// [Filesystem.Remove] will cause the files on disk to be deleted.
func TestFilesystem_Remove_deletes_files(t *testing.T) {
	var (
		logger  = log.NewNopLogger()
		tempDir = t.TempDir()

		testData = []byte("test data content")
	)

	store, err := NewFilesystem(logger, tempDir)
	require.NoError(t, err)

	// Create a handle and ensure that its file exists on disk.
	handle := store.Put(testData)
	name := store.files[handle]

	require.FileExists(t, filepath.Join(tempDir, name), "data file should exist on disk")

	// Remove the handle, and then ensure the files have been deleted.
	err = store.Remove(handle)
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(tempDir, name), "data file should be deleted from disk")
}

func TestFilesystem_fallback(t *testing.T) {
	logger := log.NewNopLogger()

	// Create a Filesystem with an invalid path that will cause file
	// operations to fail.
	var (
		store    = newUncheckedFilesystem(logger, "/invalid/nonexistent/path")
		testData = []byte("test data for fallback")
	)

	handle := store.Put(testData)
	_, existsInDisk := store.files[handle]
	require.False(t, existsInDisk, "handle should not exist in disk store maps since fallback was used")

	// [diskScratchStore] should pass all calls down to the fallback store.
	require.Equal(t, testData, readData(t, store, handle))

	// Removing should remove the section from the fallback store.
	require.NoError(t, store.Remove(handle))

	_, err := store.Read(handle)
	require.ErrorAs(t, err, new(HandleNotFoundError))
	require.ErrorAs(t, store.Remove(handle), new(HandleNotFoundError))
}

func readData(t *testing.T, store Store, handle Handle) []byte {
	dataReader, err := store.Read(handle)
	require.NoError(t, err)
	defer dataReader.Close()

	actualData, err := io.ReadAll(dataReader)
	require.NoError(t, err)
	return actualData
}
