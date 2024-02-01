package bloomshipper

import (
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

func Test_CachedBlock(t *testing.T) {
	tests := map[string]struct {
		releaseQuerier                   bool
		expectDirectoryToBeDeletedWithin time.Duration
	}{
		"expected block directory to be removed once all queriers are released": {
			releaseQuerier: true,
			// four times grater than activeQueriersCheckInterval
			expectDirectoryToBeDeletedWithin: 200 * time.Millisecond,
		},
		"expected block directory to be force removed after timeout": {
			releaseQuerier: false,
			// four times grater than removeDirectoryTimeout
			expectDirectoryToBeDeletedWithin: 2 * time.Second,
		},
	}
	for name, testData := range tests {
		t.Run(name, func(t *testing.T) {
			extractedBlockDirectory := t.TempDir()
			blockFilePath, _, _ := createBlockArchive(t)
			err := extractArchive(blockFilePath, extractedBlockDirectory)
			require.NoError(t, err)
			require.DirExists(t, extractedBlockDirectory)

			cached := CachedBlock{
				Directory:                   extractedBlockDirectory,
				removeDirectoryTimeout:      500 * time.Millisecond,
				activeQueriersCheckInterval: 50 * time.Millisecond,
				logger:                      log.NewLogfmtLogger(os.Stderr),
				activeQueriers:              atomic.NewInt32(1),
			}
			cached.removeDirectoryAsync()
			//ensure directory exists
			require.Never(t, func() bool {
				return directoryDoesNotExist(extractedBlockDirectory)
			}, 200*time.Millisecond, 50*time.Millisecond)

			if testData.releaseQuerier {
				cached.activeQueriers.Dec()
			}
			//ensure directory does not exist
			require.Eventually(t, func() bool {
				return directoryDoesNotExist(extractedBlockDirectory)
			}, testData.expectDirectoryToBeDeletedWithin, 50*time.Millisecond)
		})
	}
}

func Test_ClosableBlockQuerier(t *testing.T) {
	t.Run("cached", func(t *testing.T) {
		blockFilePath, _, _ := createBlockArchive(t)
		extractedBlockDirectory := t.TempDir()
		err := extractArchive(blockFilePath, extractedBlockDirectory)
		require.NoError(t, err)

		cached := CachedBlock{
			Directory:              extractedBlockDirectory,
			removeDirectoryTimeout: 100 * time.Millisecond,
			activeQueriers:         atomic.NewInt32(0),
		}

		querier := newBlockQuerierFromCache(cached)
		require.Equal(t, int32(1), cached.activeQueriers.Load())
		require.NoError(t, querier.Close())
		require.Equal(t, int32(0), cached.activeQueriers.Load())
	})

	t.Run("file system", func(t *testing.T) {
		blockFilePath, _, _ := createBlockArchive(t)
		extractedBlockDirectory := t.TempDir()
		err := extractArchive(blockFilePath, extractedBlockDirectory)
		require.NoError(t, err)

		querier := newBlockQuerierFromFS(extractedBlockDirectory)
		require.DirExists(t, extractedBlockDirectory)

		require.NoError(t, querier.Close())

		//ensure directory does not exist
		require.Eventually(t, func() bool {
			return directoryDoesNotExist(extractedBlockDirectory)
		}, 1*time.Second, 100*time.Millisecond)
	})
}
