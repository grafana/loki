package deletion

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
)

// A chunk whose bounds straddle midnight is indexed in both daily tables, so the
// compactor filters it once per table. The deleted line counters belong to the
// delete request rather than to the table, so the second table must not add the
// same lines again: the request would report more deleted lines than the chunk
// holds, and the excess equals the part of the chunk that crosses midnight.
func TestDeleteRequestBatch_ExpiredDoesNotCountCrossTableLinesTwice(t *testing.T) {
	const userID = "user"

	lbls := mustParseLabel(`{foo="bar"}`)
	midnight := model.TimeFromUnix((model.Now().Unix() / 86400) * 86400)

	chunk := retention.Chunk{
		ChunkID: "chunk-1",
		From:    midnight.Add(-5 * time.Minute),
		Through: midnight.Add(4 * time.Minute),
	}
	lines := int(chunk.Through-chunk.From)/int(time.Minute/time.Millisecond) + 1

	metrics := newDeleteRequestsManagerMetrics(prometheus.NewPedanticRegistry())
	batch := newDeleteRequestBatch(metrics)
	dr, err := newDeleteRequest(deletionproto.DeleteRequest{
		RequestID: "req-1",
		UserID:    userID,
		Query:     lbls.String() + ` |~ "secret=[A-Za-z0-9]"`,
		StartTime: midnight.Add(-24 * time.Hour),
		EndTime:   midnight.Add(24 * time.Hour),
	}, metrics.deletedLinesTotal)
	require.NoError(t, err)
	batch.addDeleteRequest(dr)

	// The table before midnight and the one after it both index the whole chunk
	// and filter every line of it.
	day := midnight.Unix() / 86400
	for _, tableName := range []string{
		fmt.Sprintf("index_%05d", day-1),
		fmt.Sprintf("index_%05d", day),
	} {
		tableInterval := retention.ExtractIntervalFromTableName(tableName)
		expired, filterFunc := batch.expired([]byte(userID), chunk, lbls, tableInterval, func(*deleteRequest) bool { return false })
		require.True(t, expired, "the chunk is covered by the request in table %s", tableName)
		require.NotNil(t, filterFunc)

		for ts := chunk.From; ts <= chunk.Through; ts = ts.Add(time.Minute) {
			filterFunc(ts.Time(), "secret=abc123def456", labels.EmptyLabels())
		}
	}

	require.EqualValues(t, lines, dr.DeletedLines.Load(),
		"every line of the chunk is deleted exactly once, even though two tables rewrote it")
	require.Equal(t, float64(lines), testutil.ToFloat64(metrics.deletedLinesTotal))
}
