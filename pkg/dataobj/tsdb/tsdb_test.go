package tsdb

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

func TestDataobjTSDBBuilder_Build(t *testing.T) {
	ctx := t.Context()

	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

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

	obj, objCloser, err := builder.Flush()
	require.NoError(t, err)
	defer objCloser.Close()

	// Build TSDB + section refs from the sorted object.
	tsdbBuilder := newTSDBBuilder("test-node", objstore.NewInMemBucket())
	outputs, err := tsdbBuilder.(*dataobjTSDBBuilder).build(ctx, obj, "tenant-a/object-001")
	require.NoError(t, err)

	require.Equal(t, 2, len(outputs))
	sort.Slice(outputs, func(i, j int) bool {
		return outputs[i].id.Path() < outputs[j].id.Path()
	})
	require.True(t, strings.HasPrefix(outputs[0].id.Path(), "index_20467/"))
	require.True(t, strings.HasPrefix(outputs[1].id.Path(), "index_20468/"))

	for _, out := range outputs {
		require.NotNil(t, out.id)
		require.Greater(t, len(out.tsdbData), 0)
		require.Greater(t, len(out.sectionRefData), 0)
	}
}
