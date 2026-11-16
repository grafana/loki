package executor

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestIndexOutput_AppendPostingsRow(t *testing.T) {
	label := func(column string, minTS, maxTS int64) postings.Row {
		return postings.Row{
			Kind: postings.KindLabel, ObjectPath: "logs/a", ColumnName: column, LabelValue: "v",
			StreamIDBitmap: []byte{1}, MinTimestamp: minTS, MaxTimestamp: maxTS,
		}
	}
	newOutput := func(t *testing.T) *indexOutput {
		t.Helper()
		out, err := newTestExecutorContext(t, objstore.NewInMemBucket()).newIndexOutput("acme", streams.Layout{})
		require.NoError(t, err)
		t.Cleanup(out.reset)
		return out
	}

	t.Run("returns an error when a row sorts before the previous row", func(t *testing.T) {
		out := newOutput(t)
		require.NoError(t, out.appendPostingsRow(label("env", 10, 20)))
		require.ErrorContains(t, out.appendPostingsRow(label("app", 10, 20)), "sorts before the previous key")
	})

	unsetBloom := postings.Row{
		Kind: postings.KindBloom, ObjectPath: "logs/a", ColumnName: "trace_id", BloomFilter: []byte{1},
		MinTimestamp: math.MaxInt64, MaxTimestamp: math.MinInt64,
	}

	t.Run("reports no time range but posting key bounds when its only row is a bloom row of a column that no record set", func(t *testing.T) {
		out := newOutput(t)
		require.NoError(t, out.appendPostingsRow(unsetBloom))

		artifact, err := out.artifact("indexes/out")
		require.NoError(t, err)
		require.NoError(t, artifact.Validate())
		require.True(t, artifact.MinTime.IsZero())
		require.Equal(t, "trace_id", artifact.PostingKeys.Min().ColumnName)
	})

	t.Run("reports the time range of the label rows after a bloom row of a column that no record set", func(t *testing.T) {
		out := newOutput(t)
		require.NoError(t, out.appendPostingsRow(unsetBloom))
		require.NoError(t, out.appendPostingsRow(label("app", 10, 20)))

		artifact, err := out.artifact("indexes/out")
		require.NoError(t, err)
		require.Equal(t, time.Unix(0, 10).UTC(), artifact.MinTime)
		require.Equal(t, time.Unix(0, 20).UTC(), artifact.MaxTime)
		require.Equal(t, "trace_id", artifact.PostingKeys.Min().ColumnName)
		require.Equal(t, "app", artifact.PostingKeys.Max().ColumnName)
	})
}
