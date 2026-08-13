package querier

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func cacheTestStream(app string) logproto.Stream {
	return logproto.Stream{
		Labels:  labels.FromStrings("app", app).String(),
		Entries: []push.Entry{{Timestamp: time.Unix(1, 0), Line: "x"}},
	}
}

func wantSet(ids ...int64) map[streamID]struct{} {
	out := make(map[streamID]struct{}, len(ids))
	for _, id := range ids {
		out[streamID(id)] = struct{}{}
	}
	return out
}

// TestDataObjCache_Get checks that get opens an object once and caches the resolved sections.
func TestDataObjCache_Get(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	buildDataObject(ctx, t, bucket, "obj", []logproto.Stream{cacheTestStream("a")})

	c := newDataObjCache(bucket, dataObjTestTenant)
	oo1, err := c.get(ctx, "obj")
	require.NoError(t, err)
	oo2, err := c.get(ctx, "obj")
	require.NoError(t, err)

	require.Same(t, oo1, oo2, "a second get returns the cached openObject")
	require.NotNil(t, oo1.streamsSecDO, "the tenant's streams section is resolved")
	require.Contains(t, oo1.logsSecDO, 0, "the tenant's logs section is at logs-relative index 0")
}

// TestDataObjCache_StreamLabels checks that streamLabels returns exactly the requested streams' labels
// (the reader pushes the IDs down), and nothing for absent or empty requests.
func TestDataObjCache_StreamLabels(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	foo, bar, baz := labels.FromStrings("app", "foo"), labels.FromStrings("app", "bar"), labels.FromStrings("app", "baz")
	ids := buildDataObject(ctx, t, bucket, "obj", []logproto.Stream{
		{Labels: foo.String(), Entries: []push.Entry{{Timestamp: time.Unix(1, 0), Line: "x"}}},
		{Labels: bar.String(), Entries: []push.Entry{{Timestamp: time.Unix(2, 0), Line: "y"}}},
		{Labels: baz.String(), Entries: []push.Entry{{Timestamp: time.Unix(3, 0), Line: "z"}}},
	})
	require.Len(t, ids, 3)

	oo, err := newDataObjCache(bucket, dataObjTestTenant).get(ctx, "obj")
	require.NoError(t, err)

	t.Run("all requested", func(t *testing.T) {
		got, err := oo.streamLabels(ctx, wantSet(ids...))
		require.NoError(t, err)
		require.Len(t, got, 3)
		byLabel := map[string]bool{}
		for _, lbls := range got {
			byLabel[lbls.String()] = true
		}
		require.True(t, byLabel[foo.String()] && byLabel[bar.String()] && byLabel[baz.String()])
	})

	t.Run("subset only", func(t *testing.T) {
		got, err := oo.streamLabels(ctx, wantSet(ids[0], ids[2]))
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.Contains(t, got, streamID(ids[0]))
		require.Contains(t, got, streamID(ids[2]))
		require.NotContains(t, got, streamID(ids[1]))
	})

	t.Run("absent id returns nothing for it", func(t *testing.T) {
		got, err := oo.streamLabels(ctx, wantSet(99999))
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("empty want short-circuits", func(t *testing.T) {
		got, err := oo.streamLabels(ctx, map[streamID]struct{}{})
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

// TestDataObjCache_ResolveSectionsCrossTenant checks that logs sections are indexed logs-relative across
// the whole object: resolveSections counts the other tenant's logs sections too, so a tenant's sections
// keep the same indices the metastore's SectionIdx assigns. Without that, both tenants would index from
// 0 and a read would fetch the wrong tenant's data.
func TestDataObjCache_ResolveSectionsCrossTenant(t *testing.T) {
	const tenantA, tenantB = "tenant-a", "tenant-b"

	cfg := logsobj.BuilderConfig{BuilderBaseConfig: logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		TargetObjectSize:        1 << 20,
		TargetSectionSize:       1, // tiny: one logs section per appended stream
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	}}
	builder, err := logsobj.NewBuilder(cfg, nil, logsobj.NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(t, err)

	// Interleave the tenants so neither tenant's logs sections sit only at the front of the object.
	appendStream := func(tenant, app string, ts int64) {
		s := logproto.Stream{
			Labels:  labels.FromStrings("app", app).String(),
			Entries: []push.Entry{{Timestamp: time.Unix(ts, 0), Line: "x"}},
		}
		require.NoError(t, builder.Append(tenant, s, time.Unix(0, 0)))
	}
	appendStream(tenantB, "b1", 1)
	appendStream(tenantA, "a1", 2)
	appendStream(tenantB, "b2", 3)
	appendStream(tenantA, "a2", 4)

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	totalLogs := obj.Sections().Count(logs.CheckSection)
	require.GreaterOrEqual(t, totalLogs, 2, "each tenant must contribute at least one logs section")

	ooA, err := newDataObjCache(nil, tenantA).resolveSections(obj)
	require.NoError(t, err)
	ooB, err := newDataObjCache(nil, tenantB).resolveSections(obj)
	require.NoError(t, err)

	require.NotNil(t, ooA.streamsSecDO, "tenant A has its own streams section")
	require.NotNil(t, ooB.streamsSecDO, "tenant B has its own streams section")
	require.NotEmpty(t, ooA.logsSecDO)
	require.NotEmpty(t, ooB.logsSecDO)

	// The two tenants' logs-relative indices are disjoint and together cover 0..totalLogs-1. If
	// resolveSections counted only its own tenant, both would start at 0 and the indices would collide.
	owner := map[int]string{}
	for idx := range ooA.logsSecDO {
		owner[idx] = tenantA
	}
	for idx := range ooB.logsSecDO {
		require.NotContains(t, owner, idx, "logs-relative index %d claimed by both tenants", idx)
		owner[idx] = tenantB
	}
	require.Len(t, owner, totalLogs, "the tenants' logs sections cover every logs-relative index once")
	for idx := 0; idx < totalLogs; idx++ {
		require.Contains(t, owner, idx, "logs-relative index %d must belong to a tenant", idx)
	}
}

// TestDataObjCache_LogsSection checks logs-section lookup by logs-relative index.
func TestDataObjCache_LogsSection(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	buildDataObject(ctx, t, bucket, "obj", []logproto.Stream{cacheTestStream("a")})

	oo, err := newDataObjCache(bucket, dataObjTestTenant).get(ctx, "obj")
	require.NoError(t, err)

	sec, err := oo.logsSection(ctx, 0)
	require.NoError(t, err)
	require.NotNil(t, sec)

	missing, err := oo.logsSection(ctx, 99)
	require.NoError(t, err)
	require.Nil(t, missing, "an out-of-range section index resolves to nil")
}
