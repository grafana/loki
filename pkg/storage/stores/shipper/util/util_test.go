package util

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	util_log "github.com/cortexproject/cortex/pkg/util/log"

	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
)

func Test_GetFileFromStorage(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "get-file-from-storage")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	// write a file to storage.
	testData := []byte("test-data")
	tableName := "test-table"
	require.NoError(t, util.EnsureDirectory(filepath.Join(tempDir, tableName)))
	require.NoError(t, ioutil.WriteFile(filepath.Join(tempDir, tableName, "src"), testData, 0666))

	// try downloading the file from the storage.
	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: tempDir})
	require.NoError(t, err)

	indexStorageClient := storage.NewIndexStorageClient(objectClient, "")

	require.NoError(t, DownloadFileFromStorage(func() (io.ReadCloser, error) {
		return indexStorageClient.GetFile(context.Background(), tableName, "src")
	}, false, filepath.Join(tempDir, "dest"), false, util_log.Logger))

	// verify the contents of the downloaded file.
	b, err := ioutil.ReadFile(filepath.Join(tempDir, "dest"))
	require.NoError(t, err)

	require.Equal(t, testData, b)

	// compress the file in storage
	err = CompressFile(filepath.Join(tempDir, tableName, "src"), filepath.Join(tempDir, tableName, "src.gz"), true)
	require.NoError(t, err)

	// get the compressed file from storage
	require.NoError(t, DownloadFileFromStorage(func() (io.ReadCloser, error) {
		return indexStorageClient.GetFile(context.Background(), tableName, "src.gz")
	}, true, filepath.Join(tempDir, "dest.gz"), false, util_log.Logger))

	// verify the contents of the downloaded gz file.
	b, err = ioutil.ReadFile(filepath.Join(tempDir, "dest.gz"))
	require.NoError(t, err)

	require.Equal(t, testData, b)
}

func Test_CompressFile(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "compress-file")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	uncompressedFilePath := filepath.Join(tempDir, "test-file")
	compressedFilePath := filepath.Join(tempDir, "test-file.gz")
	decompressedFilePath := filepath.Join(tempDir, "test-file-decompressed")

	testData := []byte("test-data")

	require.NoError(t, ioutil.WriteFile(uncompressedFilePath, testData, 0666))

	require.NoError(t, CompressFile(uncompressedFilePath, compressedFilePath, true))
	require.FileExists(t, compressedFilePath)

	testutil.DecompressFile(t, compressedFilePath, decompressedFilePath)
	b, err := ioutil.ReadFile(decompressedFilePath)
	require.NoError(t, err)

	require.Equal(t, testData, b)
}

func TestDoConcurrentWork(t *testing.T) {
	maxConcurrency := 10
	for name, tc := range map[string]struct {
		workCount uint32
		failWork  bool
		cancelCtx bool
	}{
		"no work": {},
		"1 work item": {
			workCount: 1,
		},
		"100 work item": {
			workCount: 100,
		},
		"1 work item with failure": {
			workCount: 1,
			failWork:  true,
		},
		"100 work item with failure": {
			workCount: 100,
			failWork:  true,
		},
		"1 work item with cancel ctx": {
			workCount: 1,
			cancelCtx: true,
		},
		"100 work item with cancel ctx": {
			workCount: 100,
			cancelCtx: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			workDone := uint32(0)
			err := DoConcurrentWork(ctx, maxConcurrency, int(tc.workCount), util_log.Logger, func(workNum int) error {
				time.Sleep(10 * time.Millisecond)
				if workNum == 0 {
					if tc.failWork {
						return errors.New("fail work")
					}
					if tc.cancelCtx {
						cancel()
						return nil
					}
				}
				atomic.AddUint32(&workDone, 1)
				return nil
			})

			if tc.failWork || tc.cancelCtx {
				require.Error(t, err)
				require.Less(t, workDone, tc.workCount)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, tc.workCount, workDone)
			}
		})
	}
}
