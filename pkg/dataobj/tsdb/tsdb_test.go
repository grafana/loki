package tsdb

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

func TestDataobjTSDBBuilder_AppendBuffersByDay(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	obj, objCloser := buildTestObject(t, now)
	defer objCloser.Close()

	tb := newTSDBBuilder("test-node", objstore.NewInMemBucket())
	require.NoError(t, tb.Append(ctx, obj, "tenant-a/object-001"))

	inner := tb.(*dataobjTSDBBuilder)
	require.Len(t, inner.dayBuilders, 2, "data spans two days so two builders should exist")
}

func TestDataobjTSDBBuilder_AppendAccumulatesAcrossCalls(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	obj1, closer1 := buildTestObject(t, now)
	defer closer1.Close()

	obj2, closer2 := buildTestObject(t, now)
	defer closer2.Close()

	tb := newTSDBBuilder("test-node", objstore.NewInMemBucket())
	require.NoError(t, tb.Append(ctx, obj1, "tenant-a/object-001"))
	require.NoError(t, tb.Append(ctx, obj2, "tenant-a/object-002"))

	inner := tb.(*dataobjTSDBBuilder)
	require.Len(t, inner.dayBuilders, 2, "two appends for the same days should reuse builders")
}

func TestDataobjTSDBBuilder_StoreUploadsAndResets(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	obj, objCloser := buildTestObject(t, now)
	defer objCloser.Close()

	bkt := objstore.NewInMemBucket()
	tb := newTSDBBuilder("test-node", bkt)

	require.NoError(t, tb.Append(ctx, obj, "tenant-a/object-001"))
	require.NoError(t, tb.Store(ctx))

	inner := tb.(*dataobjTSDBBuilder)
	require.Len(t, inner.dayBuilders, 0, "Store should reset buffered builders")

	uploaded := listBucket(t, ctx, bkt, "")
	require.Equal(t, 4, len(uploaded), "2 days x 2 files (tsdb + section refs)")
}

func buildTestObject(t *testing.T, now time.Time) (*dataobj.Object, io.Closer) {
	t.Helper()

	cfg := logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:          128 * 1024,
			TargetObjectSize:        4 * 1024 * 1024,
			TargetSectionSize:       2 * 1024 * 1024,
			BufferSize:              4 * 1024 * 1024,
			SectionStripeMergeLimit: 2,
		},
	}

	builder, err := logsobj.NewBuilder(cfg, scratch.NewMemory())
	require.NoError(t, err)

	require.NoError(t, builder.Append("tenant-a", logproto.Stream{
		Labels: `{app="api", env="prod"}`,
		Entries: []logproto.Entry{
			{Timestamp: now.Add(-2 * time.Minute), Line: "first"},
			{Timestamp: now.Add(-1 * time.Minute), Line: "second"},
		},
	}))
	require.NoError(t, builder.Append("tenant-a", logproto.Stream{
		Labels: `{app="worker", env="prod"}`,
		Entries: []logproto.Entry{
			{Timestamp: now.Add(-30 * time.Second), Line: "third"},
		},
	}))
	require.NoError(t, builder.Append("tenant-a", logproto.Stream{
		Labels: `{app="worker", env="prod"}`,
		Entries: []logproto.Entry{
			{Timestamp: now.Add(-24 * time.Hour), Line: "fourth"},
		},
	}))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	return obj, closer
}

func listBucket(t *testing.T, ctx context.Context, bkt objstore.Bucket, prefix string) []string {
	t.Helper()
	var out []string
	err := bkt.Iter(ctx, prefix, func(name string) error {
		out = append(out, name)
		return nil
	}, objstore.WithRecursiveIter())
	require.NoError(t, err)
	return out
}
