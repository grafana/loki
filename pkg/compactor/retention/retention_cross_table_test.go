package retention

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// A chunk whose bounds straddle midnight is indexed in both daily tables. The
// source chunk may only be marked for deletion while processing the last table
// that indexes it; removing it from the first one leaves the second table
// pointing at an object that no longer exists, and every read that touches that
// index entry fails to download the chunk.
func TestMarkForDelete_KeepsSourceChunkUntilLastTable(t *testing.T) {
	userID := "user"
	schema := allSchemas[3] // v12, 24h index period
	now := model.Now()
	todaysTableInterval := ExtractIntervalFromTableName(schema.config.IndexTables.TableFor(now))

	// Half an hour on either side of midnight, so the chunk lands in two tables.
	crossing := createChunk(t, userID, labels.FromStrings("foo", "1"),
		todaysTableInterval.Start.Add(-30*time.Minute),
		todaysTableInterval.Start.Add(30*time.Minute))

	// Drop the lines before midnight, keep the ones after it.
	filterFunc := func(ts time.Time, _ string, _ labels.Labels) bool {
		return ts.Before(todaysTableInterval.Start.Time())
	}

	store := newTestStore(t)
	require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{crossing}))
	store.Stop()

	tables := store.indexTables()
	require.Len(t, tables, 2, "a chunk crossing midnight must be indexed in two daily tables")

	deletedByTable := make([]int64, len(tables))
	for i, table := range tables {
		expirationChecker := newMockExpirationChecker(map[string]chunkExpiry{
			getChunkID(crossing.ChunkRef): {isExpired: true, filterFunc: filterFunc},
		}, nil)
		marker := &noopWriter{}

		_, _, err := markForDelete(context.Background(), 0, table.name, marker, table, expirationChecker,
			newTestChunkRewriter(store.chunkClient, table.name, table), util_log.Logger)
		require.NoError(t, err)
		deletedByTable[i] = marker.count
	}

	require.Equal(t, []int64{0, 1}, deletedByTable,
		"the source chunk belongs to the last table that indexes it, so only that table may mark it for deletion")
}
