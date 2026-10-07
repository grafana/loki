package downloads

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func noopForEachCallback(bool, index.Index) error { return nil }

func TestTable_ForEach_RecordsOnDemand(t *testing.T) {
	tempDir := t.TempDir()
	tablePath := filepath.Join(tempDir, objectsStorageDirName, tableName)
	setupIndexesAtPath(t, "", tablePath, 0, 2)
	setupIndexesAtPath(t, "user1", tablePath, 0, 2)
	setupIndexesAtPath(t, "user2", tablePath, 0, 2)

	tbl := NewTable(tableName, t.TempDir(), buildTestStorageClient(t, tempDir), func(path string) (index.Index, error) {
		return openMockIndexFile(t, path), nil
	}, newMetrics(nil), testDownloadTimeout).(*table)
	defer tbl.Close()

	// Preload the common index and user1, as query readiness would.
	require.NoError(t, tbl.EnsureQueryReadiness(context.Background(), []string{"user1"}))

	for _, tc := range []struct {
		name         string
		user         string
		wantOnDemand bool
	}{
		{name: "preloaded user", user: "user1"},
		{name: "first query for user2 downloads its index", user: "user2", wantOnDemand: true},
		{name: "second query for user2 is served locally", user: "user2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for name, forEach := range map[string]func(context.Context, string, index.ForEachIndexCallback) error{
				"ForEach":           tbl.ForEach,
				"ForEachConcurrent": tbl.ForEachConcurrent,
			} {
				if tc.wantOnDemand && name == "ForEachConcurrent" {
					// The ForEach run has already downloaded the index.
					continue
				}
				ctx, stats := index.NewContextWithAccessStats(context.Background())
				require.NoError(t, forEach(ctx, tc.user, noopForEachCallback), name)
				require.Equal(t, tc.wantOnDemand, stats.RequestTier() == index.AccessTierOnDemand, name)
			}
		})
	}
}

func TestIndexSet_ForEach_RecordsOnDemandWhileNotReady(t *testing.T) {
	tempDir := t.TempDir()
	baseIndexSet := storage.NewIndexSet(buildTestStorageClient(t, tempDir), true)

	for name, forEach := range map[string]func(*indexSet, context.Context) error{
		"ForEach": func(is *indexSet, ctx context.Context) error {
			return is.ForEach(ctx, noopForEachCallback)
		},
		"ForEachConcurrent": func(is *indexSet, ctx context.Context) error {
			return is.ForEachConcurrent(ctx, noopForEachCallback)
		},
	} {
		t.Run(name, func(t *testing.T) {
			idxSet, err := NewIndexSet(tableName, userID, filepath.Join(t.TempDir(), tableName, userID), baseIndexSet,
				func(path string) (index.Index, error) {
					return openMockIndexFile(t, path), nil
				}, util_log.Logger, testDownloadTimeout)
			require.NoError(t, err)
			is := idxSet.(*indexSet)

			// Init has not run, so the index set is not ready: a query waits for it.
			ctx, stats := index.NewContextWithAccessStats(context.Background())
			done := make(chan error)
			go func() { done <- forEach(is, ctx) }()

			select {
			case err := <-done:
				t.Fatalf("query returned before the index set was ready: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			is.indexMtx.markReady()
			require.NoError(t, <-done)
			require.Equal(t, index.AccessTierOnDemand, stats.RequestTier())

			// Once ready, a query is not on-demand.
			ctx, stats = index.NewContextWithAccessStats(context.Background())
			require.NoError(t, forEach(is, ctx))
			require.Equal(t, index.AccessTierNone, stats.RequestTier())
		})
	}
}
