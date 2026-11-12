package ingester

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/tsdb/wlog"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// Entries are only written with their record timestamp, in a format older versions can't read,
// when V4 records are enabled.
func TestWALWrapperLogRecordTimestamp(t *testing.T) {
	ingestedAt := time.UnixMilli(1_700_000_000_123)

	for _, tc := range []struct {
		enabled     bool
		wantVersion wal.RecordType
		want        time.Time
	}{
		{enabled: true, wantVersion: wal.WALRecordEntriesV4, want: ingestedAt},
		{enabled: false, wantVersion: wal.WALRecordEntriesV3, want: time.Time{}},
	} {
		t.Run(fmt.Sprintf("enabled=%t", tc.enabled), func(t *testing.T) {
			cfg := WALConfig{
				Enabled:            true,
				Dir:                t.TempDir(),
				CheckpointDuration: 5 * time.Minute,
				RecordV4Enabled:    tc.enabled,
			}
			w, err := newWAL(cfg, prometheus.NewRegistry(), newIngesterMetrics(nil, constants.Loki), newIngesterSeriesIter(nil))
			require.NoError(t, err)

			rec := &wal.Record{}
			rec.Reset()
			rec.UserID = "test"
			rec.IngestedAt = ingestedAt
			rec.AddEntries(1, 1, logproto.Entry{Timestamp: time.Unix(1, 0), Line: "a"})
			require.NoError(t, w.Log(rec))
			require.NoError(t, w.Stop())

			segments, err := wlog.NewSegmentsReader(cfg.Dir)
			require.NoError(t, err)
			defer segments.Close()
			reader := wlog.NewReader(segments)

			require.True(t, reader.Next())
			b := reader.Record()
			require.Equal(t, tc.wantVersion, wal.RecordType(b[0]))

			decoded := &wal.Record{}
			require.NoError(t, wal.DecodeRecord(b, decoded))
			require.True(t, tc.want.Equal(decoded.IngestedAt), "got %s, want %s", decoded.IngestedAt, tc.want)
			require.Len(t, decoded.RefEntries, 1)

			require.False(t, reader.Next())
			require.NoError(t, reader.Err())
		})
	}
}
