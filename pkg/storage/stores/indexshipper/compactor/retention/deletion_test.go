package retention

import (
	"context"
	"io/ioutil"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	util_storage "github.com/grafana/loki/pkg/util"
)

func TestDeletion_DeleteChunksBasedOnBlockSize_delete(t *testing.T) {
	totalFiles := 10000
	fileContent := []byte("Diego - 10") // 10 bytes content
	chunksDir := t.TempDir()
	generateFiles(t, totalFiles, fileContent, chunksDir)

	bytesFullDisk := uint64(totalFiles * 10)
	sizeBasedRetentionPercentage := 80
	remainingFilesAfterDelete := totalFiles * (sizeBasedRetentionPercentage) / 100

	// Verify whether all files are created
	files, _ := ioutil.ReadDir(".")
	require.Equal(t, totalFiles, len(files), "Number of files should be "+strconv.Itoa(totalFiles))

	// Check remainingFilesAfterDelete
	diskUsage, _ := util_storage.DiskUsage(chunksDir)
	diskUsage.UsedPercent = 100.0
	diskUsage.All = bytesFullDisk

	require.NoError(t, DeleteChunksBasedOnBlockSize(context.Background(), chunksDir, diskUsage, sizeBasedRetentionPercentage))
	files, _ = ioutil.ReadDir(".")
	require.Equal(t, remainingFilesAfterDelete, len(files), "Number of files should be "+strconv.Itoa(remainingFilesAfterDelete))
}

func TestDeletion_DeleteChunksBasedOnBlockSize_delete_none(t *testing.T) {
	totalFiles := 10000
	fileContent := []byte("Diego - 10") // 10 bytes content
	chunksDir := t.TempDir()
	generateFiles(t, totalFiles, fileContent, chunksDir)
	bytesFullDisk := uint64(totalFiles * 10)
	sizeBasedRetentionPercentage := 80
	remainingFilesAfterDelete := totalFiles

	// Verify whether all files are created
	files, _ := ioutil.ReadDir(".")
	require.Equal(t, totalFiles, len(files), "Number of files should be "+strconv.Itoa(totalFiles))

	// Check remainingFilesAfterDelete
	diskUsage, _ := util_storage.DiskUsage(chunksDir)
	diskUsage.UsedPercent = float64(sizeBasedRetentionPercentage - 10) // 70 < 80 No need to delete
	diskUsage.All = bytesFullDisk
	require.NoError(t, DeleteChunksBasedOnBlockSize(context.Background(), chunksDir, diskUsage, sizeBasedRetentionPercentage))
	files, _ = ioutil.ReadDir(".")
	require.Equal(t, remainingFilesAfterDelete, len(files), "Number of files should be "+strconv.Itoa(remainingFilesAfterDelete))
}

func generateFiles(t *testing.T, files int, content []byte, chunksDir string) {
	totalFiles := files
	fileContent := content

	file := "file_"
	require.NoError(t, os.Chdir(chunksDir))

	for i := 0; i < totalFiles; i++ {
		filename := file + strconv.Itoa(i)
		f, err := os.Create(filename)
		os.WriteFile(filename, fileContent, 0666)
		require.NoError(t, err)
		require.NoError(t, f.Close())
	}
}
