package bloomshipper

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

func Test_blockDownloader_extractBlock(t *testing.T) {
	blockFilePath, _, bloomFileContent, seriesFileContent := createBlockArchive(t)
	blockFile, err := os.OpenFile(blockFilePath, os.O_RDONLY, 0700)
	require.NoError(t, err)

	workingDir := t.TempDir()

	err = extractBlock(blockFile, workingDir, nil)
	require.NoError(t, err)

	require.FileExists(t, filepath.Join(workingDir, v1.BloomFileName))
	require.FileExists(t, filepath.Join(workingDir, v1.SeriesFileName))

	actualBloomFileContent, err := os.ReadFile(filepath.Join(workingDir, v1.BloomFileName))
	require.NoError(t, err)
	require.Equal(t, bloomFileContent, string(actualBloomFileContent))

	actualSeriesFileContent, err := os.ReadFile(filepath.Join(workingDir, v1.SeriesFileName))
	require.NoError(t, err)
	require.Equal(t, seriesFileContent, string(actualSeriesFileContent))
}

func directoryDoesNotExist(path string) bool {
	_, err := os.Lstat(path)
	return err != nil
}

const testArchiveFileName = "test-block-archive"

func createBlockArchive(t *testing.T) (string, string, string, string) {
	dir := t.TempDir()
	mockBlockDir := filepath.Join(dir, "mock-block-dir")
	err := os.MkdirAll(mockBlockDir, 0777)
	require.NoError(t, err)
	bloomFile, err := os.Create(filepath.Join(mockBlockDir, v1.BloomFileName))
	require.NoError(t, err)
	bloomFileContent := uuid.NewString()
	_, err = io.Copy(bloomFile, bytes.NewReader([]byte(bloomFileContent)))
	require.NoError(t, err)

	seriesFile, err := os.Create(filepath.Join(mockBlockDir, v1.SeriesFileName))
	require.NoError(t, err)
	seriesFileContent := uuid.NewString()
	_, err = io.Copy(seriesFile, bytes.NewReader([]byte(seriesFileContent)))
	require.NoError(t, err)

	blockFilePath := filepath.Join(dir, testArchiveFileName)
	file, err := os.OpenFile(blockFilePath, os.O_CREATE|os.O_RDWR, 0700)
	require.NoError(t, err)
	err = v1.TarGz(file, v1.NewDirectoryBlockReader(mockBlockDir))
	require.NoError(t, err)

	return blockFilePath, mockBlockDir, bloomFileContent, seriesFileContent
}
