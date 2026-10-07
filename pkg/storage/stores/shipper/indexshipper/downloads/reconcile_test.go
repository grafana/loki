package downloads

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
)

// trackedIndex is a mockIndex that records whether it was opened as query
// ready and whether it was closed.
type trackedIndex struct {
	*mockIndex
	queryReady bool
	closed     bool
}

func (i *trackedIndex) Close() error {
	i.closed = true
	return i.mockIndex.Close()
}

// openRecorder records every index file opened through its open func.
type openRecorder struct {
	mtx    sync.Mutex
	opened map[string][]*trackedIndex // by file name, in order of opening
}

func newOpenRecorder() *openRecorder {
	return &openRecorder{opened: map[string][]*trackedIndex{}}
}

func (r *openRecorder) open(t *testing.T) index.OpenIndexFileFunc {
	return func(path string, opts index.OpenOptions) (index.Index, error) {
		idx := &trackedIndex{mockIndex: openMockIndexFile(t, path), queryReady: opts.QueryReady}
		r.mtx.Lock()
		defer r.mtx.Unlock()
		name := filepath.Base(path)
		r.opened[name] = append(r.opened[name], idx)
		return idx, nil
	}
}

// opens returns the QueryReady flag of every open of the file, in order.
func (r *openRecorder) opens(name string) []bool {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	var out []bool
	for _, idx := range r.opened[name] {
		out = append(out, idx.queryReady)
	}
	return out
}

func (r *openRecorder) instances(name string) []*trackedIndex {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	return slices.Clone(r.opened[name])
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

func newReconcileTable(t *testing.T, tempDir string, rec *openRecorder, opts tableOptions) *table {
	tbl := newTable(tableName, filepath.Join(tempDir, cacheDirName, tableName), buildTestStorageClient(t, tempDir),
		rec.open(t), newMetrics(nil), testDownloadTimeout, opts).(*table)
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

func TestTable_QueryReadyOpenOptions(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		t.Run(map[bool]string{false: "without reopen", true: "with reopen"}[reopen], func(t *testing.T) {
			tempDir := t.TempDir()
			setupReconcileTable(t, tempDir, "u1", "u2")
			rec := newOpenRecorder()
			tbl := newReconcileTable(t, tempDir, rec, tableOptions{reopenOnQueryReady: reopen})
			ctx := context.Background()

			// Index sets created for query readiness, common included, are
			// opened as query ready.
			require.NoError(t, tbl.EnsureQueryReadiness(ctx, []string{"u1"}))
			require.Equal(t, []bool{true}, rec.opens("u1-0"))
			require.Equal(t, []bool{true}, rec.opens("0"))

			// An index set downloaded on demand by a query is not.
			require.Equal(t, []string{"0", "1", "u2-0", "u2-1"}, forEachNames(t, tbl, "u2"))
			require.Equal(t, []bool{false}, rec.opens("u2-0"))
			require.False(t, tbl.indexSets["u2"].QueryReady())
			require.True(t, tbl.indexSets["u1"].QueryReady())
			require.True(t, tbl.indexSets[""].QueryReady())

			// Once it must be query ready, it is marked. Its files are
			// reopened only with reopenOnQueryReady.
			require.NoError(t, tbl.EnsureQueryReadiness(ctx, []string{"u1", "u2"}))
			require.True(t, tbl.indexSets["u2"].QueryReady())
			if reopen {
				require.Equal(t, []bool{false, true}, rec.opens("u2-0"))
				require.True(t, rec.instances("u2-0")[0].closed, "the old reader is closed")
				require.False(t, rec.instances("u2-0")[1].closed)
			} else {
				require.Equal(t, []bool{false}, rec.opens("u2-0"))
			}
			require.Equal(t, []string{"0", "1", "u2-0", "u2-1"}, forEachNames(t, tbl, "u2"))

			// Files synced later into a query ready index set, such as
			// compaction output, are opened as query ready.
			setupIndexesAtPath(t, "u2", filepath.Join(tempDir, objectsStorageDirName, tableName, "u2"), 2, 3)
			tbl.storageClient.RefreshIndexTableCache(ctx, tableName)
			require.NoError(t, tbl.Sync(ctx))
			require.Equal(t, []bool{true}, rec.opens("u2-2"))
		})
	}
}

func TestLoadTable_NotQueryReadyUntilMarked(t *testing.T) {
	tempDir := t.TempDir()
	setupReconcileTable(t, tempDir, "u1")
	ctx := context.Background()

	// Download the table, then close it, leaving its files on local disk as
	// after a restart.
	first := newReconcileTable(t, tempDir, newOpenRecorder(), tableOptions{})
	require.NoError(t, first.EnsureQueryReadiness(ctx, []string{"u1"}))
	first.Close()

	rec := newOpenRecorder()
	loaded, err := loadTable(tableName, filepath.Join(tempDir, cacheDirName, tableName), buildTestStorageClient(t, tempDir),
		rec.open(t), newMetrics(nil), testDownloadTimeout, tableOptions{reopenOnQueryReady: true})
	require.NoError(t, err)
	tbl := loaded.(*table)
	defer tbl.Close()

	require.False(t, tbl.indexSets["u1"].QueryReady())
	require.False(t, tbl.indexSets[""].QueryReady())
	require.Equal(t, []bool{false}, rec.opens("u1-0"))

	// The preload marks what this instance owns, and reopens it.
	require.NoError(t, tbl.EnsureQueryReadiness(ctx, []string{"u1"}))
	require.True(t, tbl.indexSets["u1"].QueryReady())
	require.True(t, tbl.indexSets[""].QueryReady())
	require.Equal(t, []bool{false, true}, rec.opens("u1-0"))
	require.Equal(t, []bool{false, true}, rec.opens("0"))
}

func TestTable_DropNotOwned(t *testing.T) {
	tempDir := t.TempDir()
	setupReconcileTable(t, tempDir, "u1", "u2", "u3")
	tbl := newReconcileTable(t, tempDir, newOpenRecorder(), tableOptions{})
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
			tm, err := NewTableManager(cfg, func(s string, _ index.OpenOptions) (index.Index, error) {
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
	tbl := newReconcileTable(t, tempDir, newOpenRecorder(), tableOptions{reopenOnQueryReady: true})
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
