package executor

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func labelPostingsRow(column string) postings.Row {
	return postings.Row{
		Kind: postings.KindLabel, ObjectPath: "logs/a", ColumnName: column, LabelValue: "v",
		StreamIDBitmap: []byte{1}, MinTimestamp: 10, MaxTimestamp: 20,
	}
}

func TestIndexOutput_AppendPostingsRow(t *testing.T) {
	newOutput := func(t *testing.T) *indexOutput {
		t.Helper()
		out, err := newTestExecutorContext(t, objstore.NewInMemBucket()).newIndexOutput("acme", streams.Layout{})
		require.NoError(t, err)
		t.Cleanup(out.reset)
		return out
	}

	t.Run("reports the keys of the first and the last row as its posting key bounds", func(t *testing.T) {
		out := newOutput(t)
		for _, column := range []string{"app", "env", "zone"} {
			require.NoError(t, out.appendPostingsRow(labelPostingsRow(column)))
		}

		artifact, err := out.artifact("indexes/out")
		require.NoError(t, err)
		require.Equal(t, "app", artifact.PostingKeys.Min().ColumnName)
		require.Equal(t, "zone", artifact.PostingKeys.Max().ColumnName)
	})

	t.Run("includes the key of a bloom row in its posting key bounds", func(t *testing.T) {
		out := newOutput(t)
		require.NoError(t, out.appendPostingsRow(postings.Row{
			Kind: postings.KindBloom, ObjectPath: "logs/a", ColumnName: "trace_id", BloomFilter: []byte{1},
		}))
		require.NoError(t, out.appendPostingsRow(labelPostingsRow("app")))

		artifact, err := out.artifact("indexes/out")
		require.NoError(t, err)
		require.Equal(t, postings.Key{Kind: postings.KindBloom, ColumnName: "trace_id"}, artifact.PostingKeys.Min())
		require.Equal(t, "app", artifact.PostingKeys.Max().ColumnName)
	})

	t.Run("accepts a row with the same key as the previous row", func(t *testing.T) {
		out := newOutput(t)
		require.NoError(t, out.appendPostingsRow(labelPostingsRow("app")))
		row := labelPostingsRow("app")
		row.ObjectPath = "logs/b"
		require.NoError(t, out.appendPostingsRow(row))
	})

	t.Run("returns an error when a row sorts before the previous row", func(t *testing.T) {
		out := newOutput(t)
		require.NoError(t, out.appendPostingsRow(labelPostingsRow("env")))
		require.ErrorContains(t, out.appendPostingsRow(labelPostingsRow("app")), "sorts before the previous key")
	})
}

func TestIndexOutput_Upload(t *testing.T) {
	t.Run("reports only the rows appended after a successful upload", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		out, err := newTestExecutorContext(t, bucket).newIndexOutput("acme", streams.Layout{})
		require.NoError(t, err)
		t.Cleanup(out.reset)
		require.NoError(t, out.appendPostingsRow(labelPostingsRow("env")))
		_, _, _, err = out.upload(t.Context(), bucket)
		require.NoError(t, err)

		require.NoError(t, out.appendPostingsRow(labelPostingsRow("app")))
		artifact, err := out.artifact("indexes/out")
		require.NoError(t, err)
		require.Equal(t, "app", artifact.PostingKeys.Min().ColumnName)
		require.Equal(t, "app", artifact.PostingKeys.Max().ColumnName)
	})
}
