package downloads

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

// trackedIndex records whether an index reader was closed.
type trackedIndex struct {
	*mockIndex
	closed bool
}

func (i *trackedIndex) Close() error {
	i.closed = true
	return i.mockIndex.Close()
}

// openRecorder opens readers that expose their close state to tests.
type openRecorder struct{}

func newOpenRecorder() *openRecorder { return &openRecorder{} }
func (r *openRecorder) open(t *testing.T) index.OpenIndexFileFunc {
	return func(path string) (index.Index, error) {
		return &trackedIndex{mockIndex: openMockIndexFile(t, path)}, nil
	}
}

// setupReconcileTable puts 2 common files and 2 files for each user in table
// tableName in object storage under tempDir.
func setupReconcileTable(t *testing.T, tempDir string, users ...string) {
	tablePath := filepath.Join(tempDir, objectsStorageDirName, tableName)
	setupIndexesAtPath(t, "", tablePath, 0, 2)
	for _, u := range users {
		setupIndexesAtPath(t, u, filepath.Join(tablePath, u), 0, 2)
	}
}

func newReconcileTable(t *testing.T, tempDir string, rec *openRecorder) *table {
	tbl := NewTable(tableName, filepath.Join(tempDir, cacheDirName, tableName), buildTestStorageClient(t, tempDir),
		rec.open(t), newMetrics(nil), testDownloadTimeout).(*table)
	t.Cleanup(tbl.Close)
	return tbl
}

func forEachNames(t *testing.T, tbl *table, userID string) []string {
	var names []string
	require.NoError(t, tbl.ForEach(context.Background(), userID, func(_ bool, idx index.Index) error {
		names = append(names, idx.Name())
		return nil
	}))
	slices.Sort(names)
	return names
}

func TestTable_QueryReadyProvenance(t *testing.T) {
	tempDir := t.TempDir()
	setupReconcileTable(t, tempDir, "u1", "u2")
	tbl := newReconcileTable(t, tempDir, newOpenRecorder())
	require.NoError(t, tbl.EnsureQueryReadiness(context.Background(), []string{"u1"}))
	require.True(t, tbl.indexSets["u1"].QueryReady())
	require.True(t, tbl.indexSets[""].QueryReady())
	require.NotEmpty(t, forEachNames(t, tbl, "u2"))
	require.False(t, tbl.indexSets["u2"].QueryReady())
	require.NoError(t, tbl.EnsureQueryReadiness(context.Background(), []string{"u2"}))
	require.True(t, tbl.indexSets["u2"].QueryReady())
}

func TestTable_DropNotOwned(t *testing.T) {
	tempDir := t.TempDir()
	setupReconcileTable(t, tempDir, "u1", "u2", "u3")
	tbl := newReconcileTable(t, tempDir, newOpenRecorder())
	ctx := context.Background()

	require.NoError(t, tbl.EnsureQueryReadiness(ctx, []string{"u1", "u2"}))
	forEachNames(t, tbl, "u3") // on demand

	t.Run("a filter error drops nothing", func(t *testing.T) {
		err := tbl.DropNotOwned(ctx, func(string, []string) ([]string, error) { return nil, errors.New("ring unavailable") })
		require.Error(t, err)
		require.Len(t, tbl.indexSets, 4)
	})

	evicted := tbl.indexSets["u1"]
	var candidates []string
	require.NoError(t, tbl.DropNotOwned(ctx, func(table string, tenants []string) ([]string, error) {
		require.Equal(t, tableName, table)
		candidates = tenants
		return []string{"u1"}, nil
	}))

	// Only query ready user index sets are candidates: not the common index
	// set, nor u3 downloaded on demand.
	require.Equal(t, []string{"u1", "u2"}, candidates)
	require.NotContains(t, tbl.indexSets, "u1")
	require.Contains(t, tbl.indexSets, "u2")
	require.Contains(t, tbl.indexSets, "u3")
	require.Contains(t, tbl.indexSets, "")
	_, err := os.Stat(filepath.Join(tempDir, cacheDirName, tableName, "u1"))
	require.True(t, os.IsNotExist(err), "the evicted index set's files are deleted")

	// A query that got hold of the index set before it was evicted fails, so
	// that the client retries elsewhere, rather than finding it empty.
	err = evicted.ForEach(ctx, func(bool, index.Index) error { return nil })
	require.ErrorIs(t, err, errIndexSetEvicted)
	err = evicted.ForEachConcurrent(ctx, func(bool, index.Index) error { return nil })
	require.ErrorIs(t, err, errIndexSetEvicted)

	// A later query downloads it again, on demand.
	require.Equal(t, []string{"0", "1", "u1-0", "u1-1"}, forEachNames(t, tbl, "u1"))
	require.False(t, tbl.indexSets["u1"].QueryReady())
}

func TestTableManager_DropFilter(t *testing.T) {
	tableRange := config.TableRange{Start: 0, End: math.MaxInt64, PeriodConfig: &config.PeriodConfig{
		IndexTables: config.IndexPeriodicTableConfig{PeriodicTableConfig: config.PeriodicTableConfig{
			Prefix: indexTablePrefix, Period: indexTablePeriod,
		}},
	}}
	activeTable := buildTableName(0)

	for name, tc := range map[string]struct {
		dropFilter  TenantFilter
		expectUsers []string
	}{
		"no drop filter keeps everything": {
			expectUsers: []string{"", "u1", "u2"},
		},
		"drop filter evicts what it returns": {
			dropFilter: func(table string, tenants []string) ([]string, error) {
				if table != activeTable {
					return nil, nil
				}
				return slices.DeleteFunc(slices.Clone(tenants), func(u string) bool { return u != "u1" }), nil
			},
			expectUsers: []string{"", "u2"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			tempDir := t.TempDir()
			tablePath := filepath.Join(tempDir, objectsStorageDirName, activeTable)
			setupIndexesAtPath(t, "", tablePath, 0, 1)
			for _, u := range []string{"u1", "u2"} {
				setupIndexesAtPath(t, u, filepath.Join(tablePath, u), 0, 1)
			}

			cfg := Config{
				CacheDir:        filepath.Join(tempDir, cacheDirName),
				SyncInterval:    time.Hour,
				CacheTTL:        time.Hour,
				DownloadTimeout: time.Minute,
				Limits:          &mockLimits{queryReadyIndexNumDaysDefault: 1},
				DropFilter:      tc.dropFilter,
			}
			tm, err := NewTableManager(cfg, func(s string) (index.Index, error) {
				return openMockIndexFile(t, s), nil
			}, buildTestStorageClient(t, tempDir), nil, tableRange, nil, log.NewNopLogger())
			require.NoError(t, err)
			defer tm.Stop()

			tbl := tm.(*tableManager).tables[activeTable].(*table)
			users := make([]string, 0, len(tbl.indexSets))
			for u := range tbl.indexSets {
				users = append(users, u)
			}
			slices.Sort(users)
			require.Equal(t, tc.expectUsers, users)
		})
	}
}

func TestTable_PromotionKeepsServing(t *testing.T) {
	tempDir := t.TempDir()
	setupReconcileTable(t, tempDir, "u1")
	tbl := newReconcileTable(t, tempDir, newOpenRecorder())
	ctx := context.Background()
	forEachNames(t, tbl, "u1") // on demand

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				var (
					namesMtx sync.Mutex
					names    []string
				)
				err := tbl.ForEachConcurrent(ctx, "u1", func(_ bool, idx index.Index) error {
					if ti, ok := idx.(*trackedIndex); ok && ti.closed {
						return errors.New("query got a closed reader")
					}
					r, err := idx.Reader()
					if err != nil {
						return err
					}
					namesMtx.Lock()
					names = append(names, idx.Name())
					namesMtx.Unlock()
					return r.Close()
				})
				if err != nil || len(names) != 4 {
					t.Errorf("query during promotion: err=%v names=%v", err, names)
					return
				}
			}
		}()
	}

	require.NoError(t, tbl.EnsureQueryReadiness(ctx, []string{"u1"}))
	close(stop)
	wg.Wait()
	require.True(t, tbl.indexSets["u1"].QueryReady())
}

// syncPause blocks a sync partway through so that a test can evict the index
// set at that point.
type syncPause struct {
	paused  chan struct{}
	release chan struct{}
}

func newSyncPause() *syncPause {
	return &syncPause{paused: make(chan struct{}), release: make(chan struct{})}
}

func (p *syncPause) block() {
	p.paused <- struct{}{}
	<-p.release
}

// pausingOpener blocks the first time it opens file, and keeps the reader it
// opened for it.
type pausingOpener struct {
	t      *testing.T
	file   string
	pause  *syncPause
	opened *trackedIndex
}

func (o *pausingOpener) open(path string) (index.Index, error) {
	idx := &trackedIndex{mockIndex: openMockIndexFile(o.t, path)}
	if filepath.Base(path) == o.file && o.opened == nil {
		o.opened = idx
		o.pause.block()
	}
	return idx, nil
}

// pausingClient blocks the first listing of a user's files once armed.
type pausingClient struct {
	storage.Client
	armed atomic.Bool
	pause *syncPause
}

func (c *pausingClient) ListUserFiles(ctx context.Context, tableName, userID string, bypassCache bool) ([]storage.IndexFile, error) {
	if c.armed.CompareAndSwap(true, false) {
		c.pause.block()
	}
	return c.Client.ListUserFiles(ctx, tableName, userID, bypassCache)
}

// newEvictRaceTable returns a table with u1 query ready, and adds u1-2 to
// object storage for the next sync to download.
func newEvictRaceTable(t *testing.T, tempDir string, client storage.Client, open index.OpenIndexFileFunc) *table {
	setupReconcileTable(t, tempDir, "u1")
	tbl := NewTable(tableName, filepath.Join(tempDir, cacheDirName, tableName), client, open, newMetrics(nil), testDownloadTimeout).(*table)
	t.Cleanup(tbl.Close)
	require.NoError(t, tbl.EnsureQueryReadiness(context.Background(), []string{"u1"}))

	setupIndexesAtPath(t, "u1", filepath.Join(tempDir, objectsStorageDirName, tableName, "u1"), 2, 3)
	tbl.storageClient.RefreshIndexTableCache(context.Background(), tableName)
	return tbl
}

// syncEvictingU1 syncs tbl, evicts u1 once pause blocks the sync, and returns
// the sync's error.
func syncEvictingU1(t *testing.T, tbl *table, pause *syncPause) error {
	syncErr := make(chan error, 1)
	go func() { syncErr <- tbl.Sync(context.Background()) }()
	<-pause.paused
	require.NoError(t, tbl.DropNotOwned(context.Background(), func(string, []string) ([]string, error) { return []string{"u1"}, nil }))
	close(pause.release)
	return <-syncErr
}

// TestTable_EvictDuringSync_ClosesOpenedFiles evicts u1 after a sync has opened
// a new file for it but before the sync adds it to the set. Nothing would ever
// close a file added to the evicted set.
func TestTable_EvictDuringSync_ClosesOpenedFiles(t *testing.T) {
	tempDir := t.TempDir()
	pause := newSyncPause()
	opener := &pausingOpener{t: t, file: buildIndexFilename("u1", 2), pause: pause}
	tbl := newEvictRaceTable(t, tempDir, buildTestStorageClient(t, tempDir), opener.open)
	evicted := tbl.indexSets["u1"].(*indexSet)

	require.NoError(t, syncEvictingU1(t, tbl, pause))
	require.True(t, opener.opened.closed)
	require.Empty(t, evicted.index)
}

// TestTable_EvictDuringSync_DeletedFileDoesNotFailSync evicts u1 at the same
// point while the sync also has a file deleted from storage to remove, which
// the eviction already removed. A failed table sync skips the tables after it.
func TestTable_EvictDuringSync_DeletedFileDoesNotFailSync(t *testing.T) {
	tempDir := t.TempDir()
	pause := newSyncPause()
	opener := &pausingOpener{t: t, file: buildIndexFilename("u1", 2), pause: pause}
	tbl := newEvictRaceTable(t, tempDir, buildTestStorageClient(t, tempDir), opener.open)

	userPath := filepath.Join(tempDir, objectsStorageDirName, tableName, "u1")
	require.NoError(t, os.Remove(filepath.Join(userPath, buildIndexFilename("u1", 0))))
	tbl.storageClient.RefreshIndexTableCache(context.Background(), tableName)

	require.NoError(t, syncEvictingU1(t, tbl, pause))
}

// TestTable_EvictDuringListing_DoesNotFailSync evicts u1 while a sync lists its
// files, so the download that follows targets the directory the eviction
// removed. A failed table sync skips the tables after it.
func TestTable_EvictDuringListing_DoesNotFailSync(t *testing.T) {
	tempDir := t.TempDir()
	pause := newSyncPause()
	client := &pausingClient{Client: buildTestStorageClient(t, tempDir), pause: pause}
	tbl := newEvictRaceTable(t, tempDir, client, newOpenRecorder().open(t))
	client.armed.Store(true)

	require.NoError(t, syncEvictingU1(t, tbl, pause))
}

// expiryHook is a fake index set that lets a test run code in the middle of
// DropUnusedIndex. findExpiredIndexSets calls LastUsedAt on every index set
// while it holds the table's read lock, so hook runs inside that window. The
// fake itself never expires.
type expiryHook struct {
	IndexSet
	once sync.Once
	hook func()
}

func (h *expiryHook) LastUsedAt() time.Time {
	h.once.Do(h.hook)
	return time.Now()
}
func (h *expiryHook) QueryReady() bool { return false }
func (h *expiryHook) Close()           {}

// TestTable_DropUnusedIndexRacingEvict evicts an expired index set after the
// TTL cleanup found it expired but before the cleanup takes the write lock.
func TestTable_DropUnusedIndexRacingEvict(t *testing.T) {
	tempDir := t.TempDir()
	setupReconcileTable(t, tempDir, "u1")
	tbl := newReconcileTable(t, tempDir, newOpenRecorder())
	ctx := context.Background()
	require.NoError(t, tbl.EnsureQueryReadiness(ctx, []string{"u1"}))

	now := time.Now()
	tbl.indexSets["u1"].(*indexSet).lastUsedAt = now.Add(-2 * time.Hour)

	evictErr := make(chan error, 1)
	tbl.indexSets["hook"] = &expiryHook{hook: func() {
		go func() {
			evictErr <- tbl.DropNotOwned(ctx, func(string, []string) ([]string, error) { return []string{"u1"}, nil })
		}()
		// Return once the eviction waits for the write lock, so that it runs
		// as soon as the TTL cleanup releases its read lock.
		for tbl.indexSetsMtx.TryRLock() {
			tbl.indexSetsMtx.RUnlock()
			runtime.Gosched()
		}
	}}

	require.NotPanics(t, func() {
		dropped, err := tbl.DropUnusedIndex(time.Hour, now)
		require.NoError(t, err)
		require.False(t, dropped)
	})
	require.NoError(t, <-evictErr)
	require.NotContains(t, tbl.indexSets, "u1")
}
