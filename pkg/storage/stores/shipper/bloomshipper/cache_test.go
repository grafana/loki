package bloomshipper

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

func TestBlockDirectory_Cleanup(t *testing.T) {
	checkInterval := 50 * time.Millisecond
	timeout := 200 * time.Millisecond

	tests := map[string]struct {
		releaseQuerier                   bool
		expectDirectoryToBeDeletedWithin time.Duration
	}{
		"expect directory to be removed once all queriers are released": {
			releaseQuerier:                   true,
			expectDirectoryToBeDeletedWithin: 2 * checkInterval,
		},
		"expect directory to be force removed after timeout": {
			releaseQuerier:                   false,
			expectDirectoryToBeDeletedWithin: 2 * timeout,
		},
	}
	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			extractedBlockDirectory := t.TempDir()
			blockFilePath, _, _, _ := createBlockArchive(t)
			err := extractArchive(blockFilePath, extractedBlockDirectory)
			require.NoError(t, err)
			require.DirExists(t, extractedBlockDirectory)

			cached := BlockDirectory{
				Path:                        extractedBlockDirectory,
				removeDirectoryTimeout:      timeout,
				activeQueriersCheckInterval: checkInterval,
				logger:                      log.NewNopLogger(),
				activeQueriers:              atomic.NewInt32(0),
			}
			// acquire directory
			cached.activeQueriers.Inc()
			// start cleanup goroutine
			cached.removeDirectoryAsync()

			if tc.releaseQuerier {
				// release directory
				cached.activeQueriers.Dec()
			}

			// ensure directory does not exist any more
			require.Eventually(t, func() bool {
				return directoryDoesNotExist(extractedBlockDirectory)
			}, tc.expectDirectoryToBeDeletedWithin, 10*time.Millisecond)
		})
	}
}

func Test_ClosableBlockQuerier(t *testing.T) {
	blockFilePath, _, _, _ := createBlockArchive(t)
	extractedBlockDirectory := t.TempDir()
	err := extractArchive(blockFilePath, extractedBlockDirectory)
	require.NoError(t, err)

	cached := BlockDirectory{
		Path:                   extractedBlockDirectory,
		removeDirectoryTimeout: 100 * time.Millisecond,
		activeQueriers:         atomic.NewInt32(0),
	}

	querier := cached.BlockQuerier()
	require.Equal(t, int32(1), cached.activeQueriers.Load())
	require.NoError(t, querier.Close())
	require.Equal(t, int32(0), cached.activeQueriers.Load())

}
