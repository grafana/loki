package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func labelPostingsRow(column string) postings.Row {
	return postings.Row{
		Kind: postings.KindLabel, ObjectPath: "logs/a", ColumnName: column, LabelValue: "v",
		StreamIDBitmap: []byte{1}, MinTimestamp: 10, MaxTimestamp: 20,
	}
}

// newSplittingContext returns an executor context whose index objects are
// full after a few postings rows.
func newSplittingContext(t *testing.T, bucket objstore.Bucket) *Context {
	t.Helper()
	c := newTestExecutorContext(t, bucket)
	c.indexobjCfg.TargetObjectSize = 4096
	c.indexobjCfg.TargetSectionSize = 4096
	return c
}

// splitOutputLayout is the layout of the outputs that the split tests write.
var splitOutputLayout = streams.NewLayout(streams.DefaultShardLayout(), []string{"label:service"})

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
			require.NoError(t, out.appendPostingsRow(t.Context(), labelPostingsRow(column)))
		}

		artifact, err := out.currentArtifact("indexes/out")
		require.NoError(t, err)
		require.Equal(t, "app", artifact.PostingKeys.Min().ColumnName)
		require.Equal(t, "zone", artifact.PostingKeys.Max().ColumnName)
	})

	t.Run("includes the key of a bloom row in its posting key bounds", func(t *testing.T) {
		out := newOutput(t)
		require.NoError(t, out.appendPostingsRow(t.Context(), postings.Row{
			Kind: postings.KindBloom, ObjectPath: "logs/a", ColumnName: "trace_id", BloomFilter: []byte{1},
		}))
		require.NoError(t, out.appendPostingsRow(t.Context(), labelPostingsRow("app")))

		artifact, err := out.currentArtifact("indexes/out")
		require.NoError(t, err)
		require.Equal(t, postings.Key{Kind: postings.KindBloom, ColumnName: "trace_id"}, artifact.PostingKeys.Min())
		require.Equal(t, "app", artifact.PostingKeys.Max().ColumnName)
	})

	t.Run("accepts a row with the same key as the previous row", func(t *testing.T) {
		out := newOutput(t)
		require.NoError(t, out.appendPostingsRow(t.Context(), labelPostingsRow("app")))
		row := labelPostingsRow("app")
		row.ObjectPath = "logs/b"
		require.NoError(t, out.appendPostingsRow(t.Context(), row))

		artifact, err := out.currentArtifact("indexes/out")
		require.NoError(t, err)
		require.Equal(t, "app", artifact.PostingKeys.Min().ColumnName)
		require.Equal(t, "app", artifact.PostingKeys.Max().ColumnName)
	})

	t.Run("returns an error when a postings row follows a stats row", func(t *testing.T) {
		out, err := newTestExecutorContext(t, objstore.NewInMemBucket()).newIndexOutput("acme", splitOutputLayout)
		require.NoError(t, err)
		t.Cleanup(out.reset)
		require.NoError(t, out.appendStat(stats.Stat{ObjectPath: "logs/a", SortSchema: "label:service", Labels: map[string]string{"service": "a"}}))
		require.ErrorContains(t, out.appendPostingsRow(t.Context(), labelPostingsRow("app")), "after a stats row")
	})

	t.Run("returns an error when a row sorts before the previous row", func(t *testing.T) {
		out := newOutput(t)
		require.NoError(t, out.appendPostingsRow(t.Context(), labelPostingsRow("env")))
		require.ErrorContains(t, out.appendPostingsRow(t.Context(), labelPostingsRow("app")), "sorts before the previous key")
	})

	t.Run("returns an error when a row sorts before the keys of an index object that it already uploaded", func(t *testing.T) {
		out, err := newSplittingContext(t, objstore.NewInMemBucket()).newIndexOutput("acme", streams.Layout{})
		require.NoError(t, err)
		t.Cleanup(out.reset)
		for i := 0; len(out.artifacts) == 0; i++ {
			require.NoError(t, out.appendPostingsRow(t.Context(), labelPostingsRow(fmt.Sprintf("c%04d", i))))
		}
		require.ErrorContains(t, out.appendPostingsRow(t.Context(), labelPostingsRow("a")), "sorts before the previous key")
	})
}

func TestIndexOutput_Finish(t *testing.T) {
	ctx := t.Context()

	appendRows := func(t *testing.T, out *indexOutput, columns, logObjects []string) {
		t.Helper()
		for _, column := range columns {
			require.NoError(t, out.appendPostingsRow(ctx, labelPostingsRow(column)))
		}
		for _, object := range logObjects {
			require.NoError(t, out.appendStat(stats.Stat{
				ObjectPath: object, SortSchema: "label:service", Labels: map[string]string{"service": object},
				MinTimestamp: 10, MaxTimestamp: 20, RowCount: 1, UncompressedSize: 100,
			}))
		}
	}
	columns := func(n int) []string {
		var out []string
		for i := range n {
			out = append(out, fmt.Sprintf("c%04d", i))
		}
		return out
	}

	t.Run("returns one index object when the output never gets full", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		out, err := newTestExecutorContext(t, bucket).newIndexOutput("acme", splitOutputLayout)
		require.NoError(t, err)
		t.Cleanup(out.reset)
		appendRows(t, out, columns(50), []string{"logs/a"})

		artifacts, _, err := out.finish(ctx)
		require.NoError(t, err)
		require.Len(t, artifacts, 1)
	})

	t.Run("cuts index objects with disjoint key ranges in key order that together hold every postings row", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		out, err := newSplittingContext(t, bucket).newIndexOutput("acme", splitOutputLayout)
		require.NoError(t, err)
		t.Cleanup(out.reset)
		appendRows(t, out, columns(300), []string{"logs/a"})

		artifacts, sizes, err := out.finish(ctx)
		require.NoError(t, err)
		require.Greater(t, len(artifacts), 1)
		var uploadedBytes int64
		for _, artifact := range artifacts {
			attrs, err := bucket.Attributes(ctx, artifact.Path)
			require.NoError(t, err)
			uploadedBytes += attrs.Size
		}
		require.Equal(t, uploadedBytes, sizes.compressed, "the compressed size counts every uploaded index object")
		require.Greater(t, sizes.uncompressed, int64(4096*(len(artifacts)-1)), "the uncompressed size counts every full index object")

		var got []string
		for i, artifact := range artifacts {
			if i > 0 {
				require.Negative(t, artifacts[i-1].PostingKeys.Max().Compare(artifact.PostingKeys.Min()), "key ranges must be disjoint and in order")
			}
			rows := readAllPostingsRowsFromBucket(ctx, t, bucket, artifact.Path)
			require.NotEmpty(t, rows, "every index object holds postings")
			for _, row := range rows {
				require.GreaterOrEqual(t, row.Key().Compare(artifact.PostingKeys.Min()), 0)
				require.LessOrEqual(t, row.Key().Compare(artifact.PostingKeys.Max()), 0)
				got = append(got, row.ColumnName)
			}
		}
		require.Equal(t, columns(300), got)
	})

	t.Run("never cuts inside one posting key", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		out, err := newSplittingContext(t, bucket).newIndexOutput("acme", splitOutputLayout)
		require.NoError(t, err)
		t.Cleanup(out.reset)
		for i := range 300 {
			row := labelPostingsRow("app")
			row.ObjectPath = fmt.Sprintf("logs/%04d", i)
			require.NoError(t, out.appendPostingsRow(ctx, row))
		}
		require.NoError(t, out.appendPostingsRow(ctx, labelPostingsRow("env")))

		artifacts, _, err := out.finish(ctx)
		require.NoError(t, err)
		require.Len(t, artifacts, 2)
		require.Len(t, readAllPostingsRowsFromBucket(ctx, t, bucket, artifacts[0].Path), 300)
		require.Equal(t, "env", artifacts[1].PostingKeys.Min().ColumnName)
	})

	t.Run("puts every stats row in the last index object and records the layout in every index object", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		out, err := newSplittingContext(t, bucket).newIndexOutput("acme", splitOutputLayout)
		require.NoError(t, err)
		t.Cleanup(out.reset)
		appendRows(t, out, columns(300), []string{"logs/a", "logs/b"})

		artifacts, _, err := out.finish(ctx)
		require.NoError(t, err)
		require.Greater(t, len(artifacts), 1)
		for i, artifact := range artifacts {
			obj := openObjectFromBucket(ctx, t, bucket, artifact.Path)
			layout, ok, err := indexobj.TenantLayout(ctx, obj, "acme")
			require.NoError(t, err)
			require.True(t, ok)
			require.True(t, layout.Equal(splitOutputLayout))

			statsRows := readAllStatsRowsFromBucket(t, bucket, artifact.Path)
			if i == len(artifacts)-1 {
				require.Len(t, statsRows, 2)
			} else {
				require.Empty(t, statsRows)
			}
		}
	})

	t.Run("writes no stats section in any index object when the output has the zero layout", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		out, err := newSplittingContext(t, bucket).newIndexOutput("acme", streams.Layout{})
		require.NoError(t, err)
		t.Cleanup(out.reset)
		appendRows(t, out, columns(300), nil)

		artifacts, _, err := out.finish(ctx)
		require.NoError(t, err)
		require.Greater(t, len(artifacts), 1)
		for _, artifact := range artifacts {
			require.Zero(t, openObjectFromBucket(ctx, t, bucket, artifact.Path).Sections().Count(stats.CheckSection))
		}
	})

	t.Run("reports only the rows appended after a finish", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		out, err := newTestExecutorContext(t, bucket).newIndexOutput("acme", splitOutputLayout)
		require.NoError(t, err)
		t.Cleanup(out.reset)
		appendRows(t, out, []string{"env"}, []string{"logs/a"})
		_, _, err = out.finish(ctx)
		require.NoError(t, err)

		require.NoError(t, out.appendPostingsRow(ctx, labelPostingsRow("app")))
		artifacts, _, err := out.finish(ctx)
		require.NoError(t, err)
		require.Len(t, artifacts, 1)
		require.Equal(t, "app", artifacts[0].PostingKeys.Min().ColumnName)
		require.Equal(t, "app", artifacts[0].PostingKeys.Max().ColumnName)
	})

	t.Run("returns the error of an upload at a cut", func(t *testing.T) {
		bucket := &failingUploadBucket{Bucket: objstore.NewInMemBucket(), failOn: 1}
		out, err := newSplittingContext(t, bucket).newIndexOutput("acme", splitOutputLayout)
		require.NoError(t, err)
		t.Cleanup(out.reset)

		var appendErr error
		for i := 0; appendErr == nil && i < 300; i++ {
			appendErr = out.appendPostingsRow(ctx, labelPostingsRow(fmt.Sprintf("c%04d", i)))
		}
		require.ErrorIs(t, appendErr, errUploadFailed)
	})
}

var errUploadFailed = errors.New("upload failed")

// failingUploadBucket fails its failOn-th upload, counted from 1.
type failingUploadBucket struct {
	objstore.Bucket
	failOn, uploads int
}

func (b *failingUploadBucket) Upload(ctx context.Context, name string, r io.Reader, opts ...objstore.ObjectUploadOption) error {
	b.uploads++
	if b.uploads == b.failOn {
		return errUploadFailed
	}
	return b.Bucket.Upload(ctx, name, r, opts...)
}
