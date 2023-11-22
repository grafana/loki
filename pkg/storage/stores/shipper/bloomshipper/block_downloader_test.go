package bloomshipper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/pkg/validation"
)

func Test_blockDownloader_downloadBlocks(t *testing.T) {
	stoppedWorkersCount := atomic.NewInt32(0)
	onWorkerStopCallback = func() {
		stoppedWorkersCount.Inc()
	}
	t.Cleanup(func() {
		onWorkerStopCallback = func() {}
	})
	overrides, err := validation.NewOverrides(validation.Limits{BloomGatewayBlocksDownloadingParallelism: 20}, nil)
	require.NoError(t, err)
	workingDirectory := t.TempDir()

	blockReferences, blockClient := createFakeBlocks(t, 20)
	blockClient.responseDelay = 100 * time.Millisecond
	workersCount := 10
	downloader, err := newBlockDownloader(config.Config{
		WorkingDirectory: workingDirectory,
		BlocksDownloadingQueue: config.DownloadingQueueConfig{
			WorkersCount:              workersCount,
			MaxTasksEnqueuedPerTenant: 20,
		},
	}, blockClient, overrides, log.NewNopLogger(), prometheus.DefaultRegisterer)
	require.NoError(t, err)
	blocksCh, errorsCh := downloader.downloadBlocks(context.Background(), "fake", blockReferences)
	downloadedBlocks := make(map[string]any, len(blockReferences))
	done := make(chan bool)
	go func() {
		for i := 0; i < 20; i++ {
			select {
			case block := <-blocksCh:
				downloadedBlocks[block.BlockPath] = nil
			}
		}
		done <- true
	}()

	select {
	//20 blocks, 10 workers, fixed delay 100ms per block: the total downloading time must be ~200ms.
	case <-time.After(2 * time.Second):
		t.Fatalf("test must complete before the timeout")
	case err := <-errorsCh:
		require.NoError(t, err)
	case <-done:
	}
	require.Len(t, downloadedBlocks, 20, "all 20 block must be downloaded")

	downloader.stop()
	require.Eventuallyf(t, func() bool {
		return stoppedWorkersCount.Load() == int32(workersCount)
	}, 1*time.Second, 10*time.Millisecond, "expected all %d workers to be stopped", workersCount)
}

// creates fake blocks and returns map[block-path]Block and mockBlockClient
func createFakeBlocks(t *testing.T, count int) ([]BlockRef, *mockBlockClient) {
	mockData := make(map[string]Block, count)
	refs := make([]BlockRef, 0, count)
	for i := 0; i < count; i++ {
		archive, _, _ := createBlockArchive(t)
		block := Block{
			BlockRef: BlockRef{
				BlockPath: fmt.Sprintf("block-path-%d", i),
			},
			Data: archive,
		}
		mockData[block.BlockPath] = block
		refs = append(refs, block.BlockRef)
	}
	return refs, &mockBlockClient{mockData: mockData}
}

type mockBlockClient struct {
	responseDelay time.Duration
	mockData      map[string]Block
}

func (m *mockBlockClient) GetBlock(_ context.Context, reference BlockRef) (Block, error) {
	time.Sleep(m.responseDelay)
	block, exists := m.mockData[reference.BlockPath]
	if exists {
		return block, nil
	}

	return block, fmt.Errorf("block %s is not found in mockData", reference.BlockPath)
}

func (m *mockBlockClient) PutBlocks(_ context.Context, _ []Block) ([]Block, error) {
	panic("implement me")
}

func (m *mockBlockClient) DeleteBlocks(_ context.Context, _ []BlockRef) error {
	panic("implement me")
}

func Test_blockDownloader_extractBlock(t *testing.T) {
	blockFile, bloomFileContent, seriesFileContent := createBlockArchive(t)

	workingDir := t.TempDir()
	downloader := &blockDownloader{workingDirectory: workingDir}
	ts := time.Now().UTC()
	block := Block{
		BlockRef: BlockRef{BlockPath: "first-period-19621/tenantA/metas/ff-fff-1695272400-1695276000-aaa"},
		Data:     blockFile,
	}

	actualPath, err := downloader.extractBlock(&block, ts)

	require.NoError(t, err)
	expectedPath := filepath.Join(workingDir, block.BlockPath, strconv.FormatInt(ts.UnixMilli(), 10))
	require.Equal(t, expectedPath, actualPath,
		"expected archive to be extracted to working directory under the same path as blockPath and with timestamp suffix")
	require.FileExists(t, filepath.Join(expectedPath, v1.BloomFileName))
	require.FileExists(t, filepath.Join(expectedPath, v1.SeriesFileName))

	actualBloomFileContent, err := os.ReadFile(filepath.Join(expectedPath, v1.BloomFileName))
	require.NoError(t, err)
	require.Equal(t, bloomFileContent, string(actualBloomFileContent))

	actualSeriesFileContent, err := os.ReadFile(filepath.Join(expectedPath, v1.SeriesFileName))
	require.NoError(t, err)
	require.Equal(t, seriesFileContent, string(actualSeriesFileContent))
}

func createBlockArchive(t *testing.T) (*os.File, string, string) {
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

	blockFilePath := filepath.Join(dir, "test-block-archive")
	file, err := os.OpenFile(blockFilePath, os.O_CREATE|os.O_RDWR, 0700)
	require.NoError(t, err)
	err = v1.TarGz(file, v1.NewDirectoryBlockReader(mockBlockDir))
	require.NoError(t, err)

	blockFile, err := os.OpenFile(blockFilePath, os.O_RDONLY, 0700)
	require.NoError(t, err)
	return blockFile, bloomFileContent, seriesFileContent
}
