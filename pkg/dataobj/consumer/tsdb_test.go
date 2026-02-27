package consumer

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

func TestDataobjTSDBBuilder_Build(t *testing.T) {
	ctx := t.Context()

	// Build a real object first.
	builder, err := logsobj.NewBuilder(testBuilderCfg, scratch.NewMemory())
	require.NoError(t, err)

	now := time.Now()
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

	obj, objCloser, err := builder.Flush()
	require.NoError(t, err)
	defer objCloser.Close()

	// Sort it the same way the flush path does.
	sortFactory := logsobj.NewBuilderFactory(testBuilderCfg, scratch.NewMemory())
	sorter := logsobj.NewSorter(sortFactory, nil)
	sortedObj, sortedCloser, err := sorter.Sort(ctx, obj)
	require.NoError(t, err)
	defer sortedCloser.Close()

	// Build TSDB + section refs from the sorted object.
	tsdbBuilder := &dataobjTSDBBuilder{}
	id, tsdbData, sectionRefData, err := tsdbBuilder.build(ctx, sortedObj, "tenant-a/object-001")
	require.NoError(t, err)

	require.NotNil(t, id)
	require.Greater(t, len(tsdbData), 0)
	require.Greater(t, len(sectionRefData), 0)
}

func TestDataobjTSDBBuilder_Build_MultiTenant(t *testing.T) {
	ctx := t.Context()

	objpath := "/Users/benclive/Downloads/dataobj_objects_08_9e4caa3d7699158353559c0f0778d6435ddf56defad004a71a2959"

	objfile, err := os.ReadFile(objpath)
	require.NoError(t, err)

	obj, err := dataobj.FromReaderAt(bytes.NewReader(objfile), int64(len(objfile)))
	require.NoError(t, err)

	tsdbBuilder := &dataobjTSDBBuilder{}
	id, tsdbData, sectionRefData, err := tsdbBuilder.build(ctx, obj, "tenant-a/object-001")
	require.NoError(t, err)

	require.NotNil(t, id)
	require.Greater(t, len(tsdbData), 0)
	require.Greater(t, len(sectionRefData), 0)

	bkt, err := filesystem.NewBucket("/Users/benclive/dev/loki/dataobj")
	require.NoError(t, err)

	err = store(ctx, bkt, id, tsdbData, sectionRefData)
	require.NoError(t, err)
}
