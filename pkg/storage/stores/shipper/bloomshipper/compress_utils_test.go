package bloomshipper

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

func DirExists(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		panic(fmt.Sprintf("error running os.Lstat(%q): %s", path, err))
	}
	if !info.IsDir() {
		panic(fmt.Sprintf("%q is not a directory", path))
	}
	return true
}

const testArchiveFileName = "test-block-archive"

func createBlockArchive(t *testing.T) (string, io.Reader, string, string) {
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

	_, _ = file.Seek(0, 0)

	return blockFilePath, file, bloomFileContent, seriesFileContent
}
