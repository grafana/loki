package dataobj

import (
	"io"
	"math"
	"path/filepath"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

func Test_sectionScratchStore(t *testing.T) {
	t.Run("impl=memoryScratchStore", func(t *testing.T) {
		testScratchStore(t, func() sectionScratchStore { return newMemoryScratchStore() })
	})

	t.Run("impl=diskScratchStore", func(t *testing.T) {
		testScratchStore(t, func() sectionScratchStore {
			logger := log.NewNopLogger()
			store, err := newDiskScratchStore(logger, t.TempDir())
			require.NoError(t, err)
			return store
		})
	})

	t.Run("impl=observableScratchStore", func(t *testing.T) {
		testScratchStore(t, func() sectionScratchStore {
			return newObservableScratchStore(
				newBuilderMetrics(),
				newMemoryScratchStore(),
			)
		})
	})
}

func testScratchStore(t *testing.T, makeStore func() sectionScratchStore) {
	var (
		exampleSectionType = SectionType{Namespace: "test", Kind: "data"}

		exampleData     = []byte("test data content")
		exampleMetadata = []byte("test metadata content")
	)

	t.Run("Put", func(t *testing.T) {
		store := makeStore()

		require.NotPanics(t, func() {
			_ = store.Put(exampleSectionType, exampleData, exampleMetadata)
		})
	})

	t.Run("Info", func(t *testing.T) {
		store := makeStore()

		handle := store.Put(exampleSectionType, exampleData, exampleMetadata)

		expectInfo := sectionInfo{
			Type:         exampleSectionType,
			DataSize:     len(exampleData),
			MetadataSize: len(exampleMetadata),
		}

		info, err := store.Info(handle)
		require.NoError(t, err)
		require.Equal(t, expectInfo, info)
	})

	t.Run("Info (invalid handle)", func(t *testing.T) {
		store := makeStore()

		invalidHandle := sectionHandle(math.MaxUint64)
		_, err := store.Info(invalidHandle)
		require.ErrorIs(t, err, errSectionHandleNotFound)
	})

	t.Run("ReadData", func(t *testing.T) {
		store := makeStore()

		handle := store.Put(exampleSectionType, exampleData, exampleMetadata)
		reader, err := store.ReadData(handle)
		require.NoError(t, err)
		defer reader.Close()

		actualData, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, exampleData, actualData)
	})

	t.Run("ReadData (invalid handle)", func(t *testing.T) {
		store := makeStore()

		invalidHandle := sectionHandle(math.MaxUint64)
		_, err := store.ReadData(invalidHandle)
		require.ErrorIs(t, err, errSectionHandleNotFound)
	})

	t.Run("ReadMetadata", func(t *testing.T) {
		store := makeStore()

		handle := store.Put(exampleSectionType, exampleData, exampleMetadata)
		reader, err := store.ReadMetadata(handle)
		require.NoError(t, err)
		defer reader.Close()

		actualMetadata, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, exampleMetadata, actualMetadata)
	})

	t.Run("ReadMetadata (invalid handle)", func(t *testing.T) {
		store := makeStore()

		invalidHandle := sectionHandle(math.MaxUint64)
		_, err := store.ReadMetadata(invalidHandle)
		require.ErrorIs(t, err, errSectionHandleNotFound)
	})

	t.Run("Remove", func(t *testing.T) {
		store := makeStore()

		handle := store.Put(exampleSectionType, exampleData, exampleMetadata)
		_, err := store.Info(handle)
		require.NoError(t, err)

		require.NoError(t, store.Remove(handle))

		// Verify everything returns errSectionHandleNotFound
		_, err = store.Info(handle)
		require.ErrorIs(t, err, errSectionHandleNotFound)
		_, err = store.ReadData(handle)
		require.ErrorIs(t, err, errSectionHandleNotFound)
		_, err = store.ReadMetadata(handle)
		require.ErrorIs(t, err, errSectionHandleNotFound)
	})

	t.Run("Remove (invalid handle)", func(t *testing.T) {
		store := makeStore()

		invalidHandle := sectionHandle(math.MaxUint64)
		require.ErrorIs(t, store.Remove(invalidHandle), errSectionHandleNotFound)
	})
}

// Test_diskScratchStore_Remove_deletes_files ensures that calling
// [diskScratchStore.Remove] will cause the files on disk to be deleted.
func Test_diskScratchStore_Remove_deletes_files(t *testing.T) {
	var (
		logger  = log.NewNopLogger()
		tempDir = t.TempDir()

		testSectionType = SectionType{Namespace: "test", Kind: "data"}
		testData        = []byte("test data content")
		testMetadata    = []byte("test metadata content")
	)

	store, err := newDiskScratchStore(logger, tempDir)
	require.NoError(t, err)

	// Create a section and ensure that the files exist on disk.
	handle := store.Put(testSectionType, testData, testMetadata)
	dataFilePath := store.dataFiles[handle]
	metadataFilePath := store.metadataFiles[handle]

	require.FileExists(t, filepath.Join(tempDir, dataFilePath), "data file should exist on disk")
	require.FileExists(t, filepath.Join(tempDir, metadataFilePath), "metadata file should exist on disk")

	// Remove the section, and then ensure the files have been deleted.
	err = store.Remove(handle)
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(tempDir, dataFilePath), "data file should be deleted from disk")
	require.NoFileExists(t, filepath.Join(tempDir, metadataFilePath), "metadata file should be deleted from disk")
}

func Test_diskScratchStore_Fallback(t *testing.T) {
	logger := log.NewNopLogger()

	// Create a diskScratchStore with an invalid path that will cause file
	// operations to fail. We bypass [newDiskScratchStore], since it checks if
	// the path is valid.
	store := &diskScratchStore{
		logger:        logger,
		fallback:      newMemoryScratchStore(),
		path:          "/invalid/nonexistent/path", // This path doesn't exist and can't be created
		infos:         make(map[sectionHandle]sectionInfo),
		dataFiles:     make(map[sectionHandle]string),
		metadataFiles: make(map[sectionHandle]string),
	}

	var (
		testSectionType = SectionType{Namespace: "test", Kind: "fallback"}
		testData        = []byte("test data for fallback")
		testMetadata    = []byte("test metadata for fallback")
	)

	handle := store.Put(testSectionType, testData, testMetadata)
	_, existsInDisk := store.infos[handle]
	require.False(t, existsInDisk, "section should not exist in disk store maps since fallback was used")

	// [diskScratchStore] should pass all calls down to the fallback store.
	info, err := store.Info(handle)
	require.NoError(t, err)
	require.Equal(t, testSectionType, info.Type)
	require.Equal(t, len(testData), info.DataSize)
	require.Equal(t, len(testMetadata), info.MetadataSize)
	require.Equal(t, testData, readData(t, store, handle))
	require.Equal(t, testMetadata, readMetadata(t, store, handle))

	// Removing should remove the section from the fallback store.
	require.NoError(t, store.Remove(handle))
	_, err = store.Info(handle)
	require.ErrorIs(t, err, errSectionHandleNotFound)
}

func readData(t *testing.T, store sectionScratchStore, handle sectionHandle) []byte {
	dataReader, err := store.ReadData(handle)
	require.NoError(t, err)
	defer dataReader.Close()

	actualData, err := io.ReadAll(dataReader)
	require.NoError(t, err)
	return actualData
}

func readMetadata(t *testing.T, store sectionScratchStore, handle sectionHandle) []byte {
	metadataReader, err := store.ReadMetadata(handle)
	require.NoError(t, err)
	defer metadataReader.Close()

	actualMetadata, err := io.ReadAll(metadataReader)
	require.NoError(t, err)
	return actualMetadata
}
