package tsdb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

func TestDataobjTSDBBuilder_Build(t *testing.T) {
	ctx := t.Context()

	testBuilderCfg := logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:          128 * 1024,
			TargetObjectSize:        4 * 1024 * 1024,
			TargetSectionSize:       2 * 1024 * 1024,
			BufferSize:              4 * 1024 * 1024,
			SectionStripeMergeLimit: 2,
		},
	}

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
