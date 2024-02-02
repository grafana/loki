package bloomshipper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/validation"
)

func Test_blockDownloader_downloadBlocks(t *testing.T) {
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
	downloadedBlocks := make(map[BlockRef]any, len(blockReferences))
	done := make(chan bool)
	go func() {
		for i := 0; i < 20; i++ {
			block := <-blocksCh
			downloadedBlocks[block.BlockRef] = nil
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

	// We want all workers to be connected to the queue
	require.Equal(t, workersCount, int(downloader.queue.GetConnectedConsumersMetric()))

	downloader.stop()

	// We want all workers to be disconnected from the queue
	require.Equal(t, 0, int(downloader.queue.GetConnectedConsumersMetric()))
}

func Test_blockDownloader_downloadBlock(t *testing.T) {
	tests := map[string]struct {
		cacheEnabled                bool
		expectedTotalGetBlocksCalls int32
	}{
		"cache disabled": {
			cacheEnabled:                false,
			expectedTotalGetBlocksCalls: 40,
		},
		"cache enabled": {
			cacheEnabled:                true,
			expectedTotalGetBlocksCalls: 20,
		},
	}
	for name, testData := range tests {
		t.Run(name, func(t *testing.T) {
			overrides, err := validation.NewOverrides(validation.Limits{BloomGatewayBlocksDownloadingParallelism: 20}, nil)
			require.NoError(t, err)
			workingDirectory := t.TempDir()

			blockReferences, blockClient := createFakeBlocks(t, 20)
			workersCount := 10
			downloader, err := newBlockDownloader(config.Config{
				WorkingDirectory: workingDirectory,
				BlocksDownloadingQueue: config.DownloadingQueueConfig{
					WorkersCount:              workersCount,
					MaxTasksEnqueuedPerTenant: 20,
				},
				BlocksCache: config.BlocksCacheConfig{
					EmbeddedCacheConfig: cache.EmbeddedCacheConfig{
						Enabled:      testData.cacheEnabled,
						MaxSizeItems: 20,
					},
					RemoveDirectoryGracefulPeriod: 1 * time.Second,
				},
			}, blockClient, overrides, log.NewNopLogger(), prometheus.NewRegistry())
			t.Cleanup(downloader.stop)
			require.NoError(t, err)

			blocksCh, errorsCh := downloader.downloadBlocks(context.Background(), "fake", blockReferences)
			downloadedBlocks := make(map[BlockRef]any, len(blockReferences))
			done := make(chan bool)
			go func() {
				for i := 0; i < 20; i++ {
					block := <-blocksCh
					downloadedBlocks[block.BlockRef] = nil
				}
				done <- true
			}()

			select {
			case <-time.After(2 * time.Second):
				t.Fatalf("test must complete before the timeout")
			case err := <-errorsCh:
				require.NoError(t, err)
			case <-done:
			}
			require.Len(t, downloadedBlocks, 20, "all 20 block must be downloaded")
			require.Equal(t, int32(20), blockClient.getBlockCalls.Load())

			blocksCh, errorsCh = downloader.downloadBlocks(context.Background(), "fake", blockReferences)
			downloadedBlocks = make(map[BlockRef]any, len(blockReferences))
			done = make(chan bool)
			go func() {
				for i := 0; i < 20; i++ {
					block := <-blocksCh
					downloadedBlocks[block.BlockRef] = nil
				}
				done <- true
			}()

			select {
			case <-time.After(2 * time.Second):
				t.Fatalf("test must complete before the timeout")
			case err := <-errorsCh:
				require.NoError(t, err)
			case <-done:
			}
			require.Len(t, downloadedBlocks, 20, "all 20 block must be downloaded")
			require.Equal(t, testData.expectedTotalGetBlocksCalls, blockClient.getBlockCalls.Load())
		})
	}
}

func Test_blockDownloader_downloadBlock_deduplication(t *testing.T) {
	tests := map[string]struct {
		cacheEnabled                bool
		expectedTotalGetBlocksCalls int32
	}{
		"requests to blockClient must be deduplicated by blockPath if cache is enabled": {
			cacheEnabled:                true,
			expectedTotalGetBlocksCalls: 1,
		},
		"requests to blockClient must NOT be deduplicated by blockPath if cache is disabled": {
			cacheEnabled:                false,
			expectedTotalGetBlocksCalls: 10,
		},
	}
	for name, testData := range tests {
		t.Run(name, func(t *testing.T) {

			overrides, err := validation.NewOverrides(validation.Limits{BloomGatewayBlocksDownloadingParallelism: 20}, nil)
			require.NoError(t, err)
			workingDirectory := t.TempDir()

			blockReferences, blockClient := createFakeBlocks(t, 1)
			workersCount := 10
			downloader, err := newBlockDownloader(config.Config{
				WorkingDirectory: workingDirectory,
				BlocksDownloadingQueue: config.DownloadingQueueConfig{
					WorkersCount:              workersCount,
					MaxTasksEnqueuedPerTenant: 20,
				},
				BlocksCache: config.BlocksCacheConfig{
					EmbeddedCacheConfig: cache.EmbeddedCacheConfig{
						Enabled:      testData.cacheEnabled,
						MaxSizeItems: 20,
					},
					RemoveDirectoryGracefulPeriod: 1 * time.Second,
				},
			}, blockClient, overrides, log.NewNopLogger(), prometheus.NewRegistry())
			t.Cleanup(downloader.stop)
			require.NoError(t, err)

			var blocksDownloadedCount atomic.Uint32
			mutex := sync.Mutex{}
			multiError := util.MultiError{}
			waitGroup := sync.WaitGroup{}
			for i := 0; i < 10; i++ {
				waitGroup.Add(1)
				go func() {
					defer waitGroup.Done()
					blocksCh, errCh := downloader.downloadBlocks(context.Background(), "fake", blockReferences)
					var err error
					select {
					case <-blocksCh:
						blocksDownloadedCount.Inc()
					case downloaderErr := <-errCh:
						err = downloaderErr
					case <-time.After(1 * time.Second):
						err = fmt.Errorf("timeout in the test waiting for a single block to be downloaded")
					}
					if err == nil {
						return
					}
					mutex.Lock()
					defer mutex.Unlock()
					multiError.Add(err)
				}()
			}
			waitGroup.Wait()

			require.NoError(t, multiError.Err())
			require.Equal(t, uint32(10), blocksDownloadedCount.Load())
			require.Equal(t, testData.expectedTotalGetBlocksCalls, blockClient.getBlockCalls.Load())
		})
	}
}

// creates fake blocks and returns map[block-path]Block and mockBlockClient
func createFakeBlocks(t *testing.T, count int) ([]BlockRef, *mockBlockClient) {
	mockData := make(map[BlockRef]blockSupplier, count)
	refs := make([]BlockRef, 0, count)
	for i := 0; i < count; i++ {
		archivePath, blockDir, _, _ := createBlockArchive(t)
		_, err := os.OpenFile(archivePath, os.O_RDONLY, 0700)
		//ensure file can be opened
		require.NoError(t, err)
		blockRef := BlockRef{
			Ref: Ref{
				TenantID:       "",
				TableName:      "",
				Bounds:         v1.NewBounds(model.Fingerprint(i), model.Fingerprint(i+1)),
				StartTimestamp: 0,
				EndTimestamp:   0,
				Checksum:       0,
			},
		}

		mockData[blockRef] = func() BlockDirectory {
			return NewBlockDirectory(blockRef, blockDir, log.NewNopLogger())
		}
		refs = append(refs, blockRef)
	}
	return refs, &mockBlockClient{mockData: mockData}
}

type blockSupplier func() BlockDirectory

type mockBlockClient struct {
	responseDelay time.Duration
	mockData      map[BlockRef]blockSupplier
	getBlockCalls atomic.Int32
	defaultKeyResolver
}

func (m *mockBlockClient) GetBlocks(_ context.Context, _ []BlockRef) ([]BlockDirectory, error) {
	panic("implement me")
}

func (m *mockBlockClient) GetBlock(_ context.Context, reference BlockRef) (BlockDirectory, error) {
	m.getBlockCalls.Inc()
	time.Sleep(m.responseDelay)
	supplier, exists := m.mockData[reference]
	if exists {
		return supplier(), nil
	}

	return BlockDirectory{}, fmt.Errorf("block %s is not found in mockData", reference)
}

func (m *mockBlockClient) PutBlock(_ context.Context, _ Block) error {
	panic("implement me")
}

func (m *mockBlockClient) DeleteBlocks(_ context.Context, _ []BlockRef) error {
	panic("implement me")
}

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
