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

			blockDir := BlockDirectory{
				Path:                        extractedBlockDirectory,
				removeDirectoryTimeout:      timeout,
				activeQueriersCheckInterval: checkInterval,
				logger:                      log.NewNopLogger(),
				refCount:                    atomic.NewInt32(0),
			}
			// acquire directory
			blockDir.refCount.Inc()
			// start cleanup goroutine
			blockDir.removeDirectoryAsync()

			if tc.releaseQuerier {
				// release directory
				blockDir.refCount.Dec()
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

	blockDir := BlockDirectory{
		Path:                   extractedBlockDirectory,
		removeDirectoryTimeout: 100 * time.Millisecond,
		refCount:               atomic.NewInt32(0),
	}

	querier := blockDir.BlockQuerier()
	require.Equal(t, int32(1), blockDir.refCount.Load())
	require.NoError(t, querier.Close())
	require.Equal(t, int32(0), blockDir.refCount.Load())

}
