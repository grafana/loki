package compactor

import (
	"strconv"
	"testing"
	"time"
	"unsafe"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	ingesterclient "github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
)

// unsafeGetString is like yolostring but with a meaningful name
func unsafeGetString(buf []byte) string {
	return *((*string)(unsafe.Pointer(&buf))) // #nosec G103 -- we know the string is not mutated
}

func createChunk(t testing.TB, chunkFormat byte, headBlockFmt chunkenc.HeadBlockFmt, userID string, lbs labels.Labels, from model.Time, through model.Time) chunk.Chunk {
	t.Helper()
	const (
		targetSize = 1500 * 1024
		blockSize  = 256 * 1024
	)
	labelsBuilder := labels.NewBuilder(lbs)
	labelsBuilder.Set(labels.MetricName, "logs")
	metric := labelsBuilder.Labels()
	fp := ingesterclient.Fingerprint(lbs)
	chunkEnc := chunkenc.NewMemChunk(chunkFormat, compression.Snappy, headBlockFmt, blockSize, targetSize)

	for ts := from; !ts.After(through); ts = ts.Add(1 * time.Minute) {
		dup, err := chunkEnc.Append(&logproto.Entry{
			Timestamp: ts.Time(),
			Line:      ts.String(),
		})
		require.False(t, dup)
		require.NoError(t, err)
	}

	require.NoError(t, chunkEnc.Close())
	c := chunk.NewChunk(userID, fp, metric, chunkenc.NewFacade(chunkEnc, blockSize, targetSize), from, through)
	require.NoError(t, c.Encode())
	return c
}

// ExtractIntervalFromTableName gives back the time interval for which the table is expected to hold the chunks index.
func ExtractIntervalFromTableName(tableName string) model.Interval {
	interval := model.Interval{
		Start: 0,
		End:   model.Now(),
	}
	tableNumber, err := strconv.ParseInt(tableName[len(tableName)-5:], 10, 64)
	if err != nil {
		return interval
	}

	interval.Start = model.TimeFromUnix(tableNumber * 86400)
	// subtract a millisecond here so that interval only covers a single table since adding 24 hours ends up covering the start time of next table as well.
	interval.End = interval.Start.Add(24*time.Hour) - 1
	return interval
}
