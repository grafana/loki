package uploads

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

const objectsStorageDirName = "objects"

func buildTestStorageClient(t *testing.T, path string) storage.Client {
	objectStoragePath := filepath.Join(path, objectsStorageDirName)
	fsObjectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	return storage.NewIndexStorageClient(fsObjectClient, "")
}

type stopFunc func()

func buildTestTableManager(t *testing.T, testDir string) (TableManager, stopFunc) {
	storageClient := buildTestStorageClient(t, testDir)

	cfg := Config{
		UploadInterval: time.Hour,
		// Ensure we retain longer than any test run so UploadTables' cleanup doesn't
		// delete indexes that the test just added
		DBRetainPeriod: time.Hour,
	}
	tm, err := NewTableManager(cfg, storageClient, nil, log.NewNopLogger())
	require.NoError(t, err)

	return tm, func() {
		tm.Stop()
		require.NoError(t, os.RemoveAll(testDir))
	}
}

func TestTableManager_UploadTables(t *testing.T) {
	testDir := t.TempDir()

	tm, stopFunc := buildTestTableManager(t, testDir)
	defer stopFunc()

	const tableName = "table-1"
	const userID = "user-1"

	userIndexPath := filepath.Join(testDir, tableName, userID)
	require.NoError(t, os.MkdirAll(userIndexPath, 0755))

	testIndexes := buildTestIndexes(t, userIndexPath, 3)
	for _, testIndex := range testIndexes {
		require.NoError(t, tm.AddIndex(tableName, userID, testIndex))
	}

	// Synchronously upload all tables and ensure it surfaces success.
	require.NoError(t, tm.UploadTables(context.Background()))

	// The indexes should now be present in object storage.
	uploadedDir := filepath.Join(testDir, objectsStorageDirName, tableName, userID)
	entries, err := os.ReadDir(uploadedDir)
	require.NoError(t, err)
	require.Len(t, entries, len(testIndexes))
}

func TestTableManager(t *testing.T) {
	testDir := t.TempDir()

	testTableManager, stopFunc := buildTestTableManager(t, testDir)
	defer stopFunc()

	for tableIdx := 0; tableIdx < 2; tableIdx++ {
		tableName := "table-" + strconv.Itoa(tableIdx)
		t.Run(tableName, func(t *testing.T) {
			for userIdx := 0; userIdx < 2; userIdx++ {
				userID := "user-" + strconv.Itoa(userIdx)
				t.Run(userID, func(t *testing.T) {
					userIndexPath := filepath.Join(testDir, tableName, userID)
					require.NoError(t, os.MkdirAll(userIndexPath, 0755))

					// build some test indexes and add them to the table.
					testIndexes := buildTestIndexes(t, userIndexPath, 5)
					for _, testIndex := range testIndexes {
						require.NoError(t, testTableManager.AddIndex(tableName, userID, testIndex))
					}

					// see if we can find all the added indexes in the table.
					indexesFound := map[string]*mockIndex{}
					err := testTableManager.ForEach(tableName, userID, func(_ bool, index index.Index) error {
						indexesFound[index.Path()] = index.(*mockIndex)
						return nil
					})
					require.NoError(t, err)

					require.Equal(t, testIndexes, indexesFound)
				})
			}
		})
	}
}

// TestTableManager_ConcurrentUploadTables covers /flush/tenant and the periodic
// upload loop calling UploadTables at the same time. Overlapping uploads of the
// same index used to share a temp file, so one of them could upload 0 bytes.
func TestTableManager_ConcurrentUploadTables(t *testing.T) {
	const (
		tableName = "table-1"
		userID    = "user-1"
	)

	testDir := t.TempDir()
	client := &concurrencyDetector{Client: buildTestStorageClient(t, testDir)}

	tm, err := NewTableManager(Config{UploadInterval: time.Hour, DBRetainPeriod: time.Hour}, client, nil, log.NewNopLogger())
	require.NoError(t, err)
	defer tm.Stop()

	userIndexPath := filepath.Join(testDir, tableName, userID)
	require.NoError(t, os.MkdirAll(userIndexPath, 0o755))
	for _, idx := range buildTestIndexes(t, userIndexPath, 1) {
		require.NoError(t, tm.AddIndex(tableName, userID, idx))
	}

	require.NoError(t, runConcurrently(func() error { return tm.UploadTables(context.Background()) }))

	require.False(t, client.concurrent.Load(), "uploads ran concurrently")
	require.Equal(t, int32(1), client.uploads.Load(), "index should be uploaded exactly once")
}

// concurrencyDetector is a storage.Client that records whether two uploads
// ever run at the same time. Each upload holds for a moment so that an
// overlapping upload has time to arrive.
type concurrencyDetector struct {
	storage.Client

	mtx        sync.Mutex // held while an upload is in flight
	concurrent atomic.Bool
	uploads    atomic.Int32
}

func (d *concurrencyDetector) PutUserFile(ctx context.Context, tableName, userID, fileName string, file io.Reader) error {
	d.uploads.Add(1)

	if !d.mtx.TryLock() {
		d.concurrent.Store(true)
		return d.Client.PutUserFile(ctx, tableName, userID, fileName, file)
	}
	defer d.mtx.Unlock()

	// Uploads to the local test store finish almost instantly, so without a
	// pause a second upload would rarely arrive while this one is in flight,
	// even when nothing stops them from overlapping.
	time.Sleep(100 * time.Millisecond)
	return d.Client.PutUserFile(ctx, tableName, userID, fileName, file)
}

// runConcurrently calls upload twice at the same time and returns any errors.
func runConcurrently(upload func() error) error {
	var (
		wg   sync.WaitGroup
		errs [2]error
	)
	for i := range errs {
		wg.Go(func() { errs[i] = upload() })
	}
	wg.Wait()
	return errors.Join(errs[:]...)
}
