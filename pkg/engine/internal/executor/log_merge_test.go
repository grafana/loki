package executor

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// sortSchemaOverrides is a logsobj.TenantOverrides stub returning a fixed sort
// schema (ordered FQN sort keys) for every tenant.
type sortSchemaOverrides []string

func (s sortSchemaOverrides) SortSchemaLabels(_ string) []string { return s }

// testStream is a labeled set of entries appended to a source log object.
type testStream struct {
	labels  string
	entries []push.Entry
}

// linesAt builds count entries for a stream, one second apart starting at base.
func linesAt(base time.Time, count int) []push.Entry {
	entries := make([]push.Entry, 0, count)
	for i := range count {
		entries = append(entries, push.Entry{
			Timestamp: base.Add(time.Duration(i) * time.Second).UTC(),
			Line:      "line",
		})
	}
	return entries
}

// wideLinesAt builds count entries with ~100-byte lines so a handful of records
// exceed a small TargetObjectSize and force the merge to split its output.
func wideLinesAt(base time.Time, count int) []push.Entry {
	entries := make([]push.Entry, 0, count)
	for i := range count {
		entries = append(entries, push.Entry{
			Timestamp: base.Add(time.Duration(i) * time.Second).UTC(),
			Line:      strings.Repeat("x", 100),
		})
	}
	return entries
}

// buildSourceLogObject builds a schema-sorted log data object from the given
// per-tenant streams and uploads it to the bucket. When sortSchema is non-empty
// the object is written in SortSchemaASC order for that schema.
func buildSourceLogObject(t *testing.T, bucket objstore.Bucket, path string, sortSchema []string, byTenant map[string][]testStream) {
	t.Helper()

	cfg := logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:            2048,
			MaxPageRows:               10000,
			TargetObjectSize:          1 << 22, // 4 MiB
			TargetSectionSize:         1 << 21, // 2 MiB
			BufferSize:                2048 * 8,
			SectionStripeMergeLimit:   2,
			EstimatedCompressionRatio: 8,
		},
		DataobjSortOrder:     "timestamp-desc",
		AppendOrderedEnabled: true,
		DataobjUseSortSchema: len(sortSchema) > 0,
	}

	b, err := logsobj.NewBuilder(cfg, scratch.NewMemory(), logsobj.NewBuilderMetrics(), log.NewNopLogger(), sortSchemaOverrides(sortSchema))
	require.NoError(t, err)

	for tenant, streams := range byTenant {
		for _, s := range streams {
			require.NotEmpty(t, s.entries, "test stream must have at least one entry")
			require.NoError(t, b.Append(tenant, logproto.Stream{
				Labels:  s.labels,
				Entries: s.entries,
			}, s.entries[0].Timestamp))
		}
	}

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	defer closer.Close()

	require.NoError(t, uploadObjectToBucket(context.Background(), bucket, path, obj))
}

func TestCollectLogSources_DedupsAndResolvesLabels(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	buildSourceLogObject(t, bucket, "objA", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="a"}`, entries: linesAt(base, 3)},
			{labels: `{app="b"}`, entries: linesAt(base, 2)},
		},
	})
	buildSourceLogObject(t, bucket, "objB", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="b"}`, entries: linesAt(base.Add(time.Hour), 2)},
			{labels: `{app="c"}`, entries: linesAt(base.Add(time.Hour), 4)},
		},
	})

	c := newTestExecutorContext(t, bucket)
	node := &physical.LogMerge{
		Tenant:     tenant,
		SortSchema: sortSchema,
		Runs: []*compactionv2pb.RunRef{
			// objA is referenced twice (and by two runs) to exercise dedup.
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objA"}, {ObjectPath: "objA"}}},
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objB"}, {ObjectPath: "objA"}}},
		},
	}

	sources, err := c.collectLogSources(ctx, node)
	require.NoError(t, err)
	require.Len(t, sources, 2, "duplicate object paths must be collapsed to one source each")

	byPath := make(map[string]*logSource, len(sources))
	for _, s := range sources {
		byPath[s.path] = s
		require.NotEmpty(t, s.logsSections, "source %q must have at least one logs section", s.path)
	}

	require.Contains(t, byPath, "objA")
	require.Contains(t, byPath, "objB")

	appValues := func(s *logSource) map[string]bool {
		got := make(map[string]bool)
		for _, st := range s.streams {
			got[st.Labels.Get("app")] = true
		}
		return got
	}
	require.Equal(t, map[string]bool{"a": true, "b": true}, appValues(byPath["objA"]))
	require.Equal(t, map[string]bool{"b": true, "c": true}, appValues(byPath["objB"]))
}

func TestCollectLogSources_ExcludesOtherTenants(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// One multi-tenant object: only tenant T's sections should be collected.
	buildSourceLogObject(t, bucket, "objMulti", sortSchema, map[string][]testStream{
		tenant:  {{labels: `{app="a"}`, entries: linesAt(base, 3)}},
		"other": {{labels: `{app="z"}`, entries: linesAt(base, 3)}},
	})

	c := newTestExecutorContext(t, bucket)
	node := &physical.LogMerge{
		Tenant:     tenant,
		SortSchema: sortSchema,
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objMulti"}}},
		},
	}

	sources, err := c.collectLogSources(ctx, node)
	require.NoError(t, err)
	require.Len(t, sources, 1)

	apps := make(map[string]bool)
	for _, st := range sources[0].streams {
		apps[st.Labels.Get("app")] = true
	}
	require.Equal(t, map[string]bool{"a": true}, apps, "other tenants' streams must be excluded")
}

// newSmallObjectExecutorContext is like newTestExecutorContext but with a tiny
// TargetObjectSize so the merge splits its output across multiple objects.
func newSmallObjectExecutorContext(t *testing.T, bucket objstore.Bucket) *Context {
	t.Helper()
	c := newTestExecutorContext(t, bucket)
	c.indexobjCfg.TargetPageSize = 512
	c.indexobjCfg.TargetObjectSize = 1000 // bytes; forces splitting
	c.indexobjCfg.TargetSectionSize = 800
	return c
}

// outputRecord is one decoded record read back from a compacted output object.
type outputRecord struct {
	app      string
	streamID int64
	ts       time.Time
}

// readCompactedObjects loads every compacted log object the merge wrote for node
// (paths logMergeOutputPath(node.OutputIndexPath, 0..)), stopping at the first
// missing index. For each object it returns the streamID->app map and the
// records in stored (schema-sorted) order.
func readCompactedObjects(ctx context.Context, t *testing.T, bucket objstore.Bucket, node *physical.LogMerge) []struct {
	streamApp map[int64]string
	records   []outputRecord
} {
	t.Helper()

	var out []struct {
		streamApp map[int64]string
		records   []outputRecord
	}
	for i := 0; ; i++ {
		path := logMergeOutputPath(node.OutputIndexPath, i)
		ok, err := bucket.Exists(ctx, path)
		require.NoError(t, err)
		if !ok {
			break
		}

		obj, err := dataobj.FromBucket(ctx, bucket, path, 0)
		require.NoError(t, err)

		streamApp := make(map[int64]string)
		for _, sec := range obj.Sections().Filter(streams.CheckSection) {
			if sec.Tenant != node.Tenant {
				continue
			}
			ss, err := streams.Open(ctx, sec)
			require.NoError(t, err)
			for res := range streams.IterSection(ctx, ss) {
				s, err := res.Value()
				require.NoError(t, err)
				streamApp[s.ID] = s.Labels.Get("app")
			}
		}

		var records []outputRecord
		for _, sec := range obj.Sections().Filter(logs.CheckSection) {
			if sec.Tenant != node.Tenant {
				continue
			}
			ls, err := logs.Open(ctx, sec)
			require.NoError(t, err)
			for res := range logs.IterSection(ctx, ls) {
				rec, err := res.Value()
				require.NoError(t, err)
				records = append(records, outputRecord{
					app:      streamApp[rec.StreamID],
					streamID: rec.StreamID,
					ts:       rec.Timestamp,
				})
			}
		}

		out = append(out, struct {
			streamApp map[int64]string
			records   []outputRecord
		}{streamApp: streamApp, records: records})
	}
	return out
}

func TestDoLogObjectMerge_MergesAndSplits(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}
	ta := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tb := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	tc := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)

	// Overlapping label sets across objects (a,b,c / b,c,d / c,d,e). With the
	// k-way merge (no cross-object stream dedup) every source stream becomes its
	// own output stream: 9 source streams -> 9 output streams.
	buildSourceLogObject(t, bucket, "objA", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="a"}`, entries: wideLinesAt(ta, 4)},
			{labels: `{app="b"}`, entries: wideLinesAt(ta, 4)},
			{labels: `{app="c"}`, entries: wideLinesAt(ta, 4)},
		},
	})
	buildSourceLogObject(t, bucket, "objB", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="b"}`, entries: wideLinesAt(tb, 4)},
			{labels: `{app="c"}`, entries: wideLinesAt(tb, 4)},
			{labels: `{app="d"}`, entries: wideLinesAt(tb, 4)},
		},
	})
	buildSourceLogObject(t, bucket, "objC", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="c"}`, entries: wideLinesAt(tc, 4)},
			{labels: `{app="d"}`, entries: wideLinesAt(tc, 4)},
			{labels: `{app="e"}`, entries: wideLinesAt(tc, 4)},
		},
	})

	c := newSmallObjectExecutorContext(t, bucket)
	node := &physical.LogMerge{
		Tenant:          tenant,
		SortSchema:      sortSchema,
		OutputIndexPath: "out/index",
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objA"}, {ObjectPath: "objB"}, {ObjectPath: "objC"}}},
		},
	}

	require.NoError(t, c.doLogObjectMerge(ctx, node))

	objs := readCompactedObjects(ctx, t, bucket, node)
	require.GreaterOrEqual(t, len(objs), 2, "output must be split into multiple objects")

	totalStreams := 0
	totalRecords := 0
	distinctApps := make(map[string]bool)
	for objIdx, o := range objs {
		totalStreams += len(o.streamApp)
		totalRecords += len(o.records)
		for _, app := range o.streamApp {
			distinctApps[app] = true
		}

		// Each object is schema-sorted by [app ASC, streamID ASC, timestamp DESC].
		for i := 1; i < len(o.records); i++ {
			prev, curr := o.records[i-1], o.records[i]
			require.LessOrEqual(t, prev.app, curr.app, "apps must be non-decreasing within object %d", objIdx)
			if prev.app == curr.app {
				require.LessOrEqual(t, prev.streamID, curr.streamID, "streamIDs must be non-decreasing within an app")
				if prev.streamID == curr.streamID {
					require.False(t, curr.ts.After(prev.ts), "timestamps must be non-increasing within a stream")
				}
			}
		}
	}

	// No cross-object dedup: 9 source streams => 9 output streams.
	require.Equal(t, 9, totalStreams)
	// The 5 distinct label sets are all present.
	require.Equal(t, map[string]bool{"a": true, "b": true, "c": true, "d": true, "e": true}, distinctApps)
	// 9 stream-appends x 4 entries = 36 records.
	require.Equal(t, 36, totalRecords)
}

func TestDoLogObjectMerge_ExistingOutputShortCircuits(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	buildSourceLogObject(t, bucket, "objA", sortSchema, map[string][]testStream{
		tenant: {{labels: `{app="a"}`, entries: linesAt(base, 3)}},
	})

	c := newTestExecutorContext(t, bucket)
	node := &physical.LogMerge{
		Tenant:          tenant,
		SortSchema:      sortSchema,
		OutputIndexPath: "out/index",
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objA"}}},
		},
	}

	// Pre-seed the output index path: the merge must short-circuit and not build.
	require.NoError(t, bucket.Upload(ctx, node.OutputIndexPath, strings.NewReader("sentinel")))

	require.NoError(t, c.doLogObjectMerge(ctx, node))

	got, err := bucket.Get(ctx, node.OutputIndexPath)
	require.NoError(t, err)
	buf := new(strings.Builder)
	_, err = io.Copy(buf, got)
	require.NoError(t, err)
	require.NoError(t, got.Close())
	require.Equal(t, "sentinel", buf.String(), "existing output must be left untouched")

	ok, err := bucket.Exists(ctx, logMergeOutputPath(node.OutputIndexPath, 0))
	require.NoError(t, err)
	require.False(t, ok, "compacted log objects must not be written when output index already exists")
}

// collectIndexSections opens the index object at node.OutputIndexPath and
// returns the postings/stats section kinds present for the tenant plus the set
// of object paths referenced by KindLabel postings rows.
func collectIndexSections(ctx context.Context, t *testing.T, bucket objstore.Bucket, node *physical.LogMerge) (kinds map[string]bool, postingsPaths map[string]bool) {
	t.Helper()

	obj, err := dataobj.FromBucket(ctx, bucket, node.OutputIndexPath, 0)
	require.NoError(t, err)

	kinds = make(map[string]bool)
	postingsPaths = make(map[string]bool)

	for _, sec := range obj.Sections() {
		if sec.Tenant != node.Tenant {
			continue
		}
		switch {
		case stats.CheckSection(sec):
			kinds["stats"] = true
		case postings.CheckSection(sec):
			kinds["postings"] = true

			ps, err := postings.Open(ctx, sec)
			require.NoError(t, err)
			func() {
				rr := postings.NewRowReader(ctx, ps, nil)
				defer rr.Close()
				for rr.Next() {
					if row := rr.At(); row.ObjectPath != "" {
						postingsPaths[row.ObjectPath] = true
					}
				}
				require.NoError(t, rr.Err())
			}()
		}
	}
	return kinds, postingsPaths
}

func TestDoLogObjectMerge_WritesIndexOverCompactedObjects(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	buildSourceLogObject(t, bucket, "objA", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="a"}`, entries: linesAt(base, 3)},
			{labels: `{app="b"}`, entries: linesAt(base, 2)},
		},
	})
	buildSourceLogObject(t, bucket, "objB", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="c"}`, entries: linesAt(base.Add(time.Hour), 4)},
		},
	})

	c := newTestExecutorContext(t, bucket)
	node := &physical.LogMerge{
		Tenant:          tenant,
		SortSchema:      sortSchema,
		OutputIndexPath: "index/out",
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objA"}, {ObjectPath: "objB"}}},
		},
	}

	require.NoError(t, c.doLogObjectMerge(ctx, node))

	ok, err := bucket.Exists(ctx, node.OutputIndexPath)
	require.NoError(t, err)
	require.True(t, ok, "index object must be written at OutputIndexPath")

	kinds, postingsPaths := collectIndexSections(ctx, t, bucket, node)
	require.True(t, kinds["stats"], "index must contain a stats section")
	require.True(t, kinds["postings"], "index must contain a postings section")

	compacted := make(map[string]bool)
	for i := 0; ; i++ {
		p := logMergeOutputPath(node.OutputIndexPath, i)
		exists, err := bucket.Exists(ctx, p)
		require.NoError(t, err)
		if !exists {
			break
		}
		compacted[p] = true
	}
	require.NotEmpty(t, compacted, "at least one compacted object must exist")
	require.NotEmpty(t, postingsPaths, "postings must reference at least one object path")
	for p := range postingsPaths {
		require.True(t, compacted[p], "postings object_path %q must be a compacted object", p)
		require.False(t, p == "objA" || p == "objB", "postings must not reference source object paths")
	}
}

func TestDoLogObjectMerge_IndexCoversAllSplitObjects(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}
	ta := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tb := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	tc := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)

	buildSourceLogObject(t, bucket, "objA", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="a"}`, entries: wideLinesAt(ta, 4)},
			{labels: `{app="b"}`, entries: wideLinesAt(ta, 4)},
			{labels: `{app="c"}`, entries: wideLinesAt(ta, 4)},
		},
	})
	buildSourceLogObject(t, bucket, "objB", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="b"}`, entries: wideLinesAt(tb, 4)},
			{labels: `{app="c"}`, entries: wideLinesAt(tb, 4)},
			{labels: `{app="d"}`, entries: wideLinesAt(tb, 4)},
		},
	})
	buildSourceLogObject(t, bucket, "objC", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="c"}`, entries: wideLinesAt(tc, 4)},
			{labels: `{app="d"}`, entries: wideLinesAt(tc, 4)},
			{labels: `{app="e"}`, entries: wideLinesAt(tc, 4)},
		},
	})

	c := newSmallObjectExecutorContext(t, bucket)
	node := &physical.LogMerge{
		Tenant:          tenant,
		SortSchema:      sortSchema,
		OutputIndexPath: "index/out",
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objA"}, {ObjectPath: "objB"}, {ObjectPath: "objC"}}},
		},
	}

	require.NoError(t, c.doLogObjectMerge(ctx, node))

	compacted := make(map[string]bool)
	for i := 0; ; i++ {
		p := logMergeOutputPath(node.OutputIndexPath, i)
		exists, err := bucket.Exists(ctx, p)
		require.NoError(t, err)
		if !exists {
			break
		}
		compacted[p] = true
	}
	require.GreaterOrEqual(t, len(compacted), 2, "output must be split into multiple objects")

	_, postingsPaths := collectIndexSections(ctx, t, bucket, node)
	require.Equal(t, compacted, postingsPaths, "postings must reference every compacted object")
}

func TestDoLogObjectMerge_EmptyRunsErrors(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	c := newTestExecutorContext(t, bucket)
	node := &physical.LogMerge{Tenant: "T", SortSchema: []string{"label:app"}, OutputIndexPath: "index/out"}

	require.Error(t, c.doLogObjectMerge(ctx, node), "empty runs must error so the coordinator skips the ToC swap")

	ok, err := bucket.Exists(ctx, node.OutputIndexPath)
	require.NoError(t, err)
	require.False(t, ok, "no index object should be written when there are no sources")

	ok, err = bucket.Exists(ctx, logMergeOutputPath(node.OutputIndexPath, 0))
	require.NoError(t, err)
	require.False(t, ok, "no compacted object should be written for an empty merge")
}

func TestDoLogObjectMerge_ZeroOutputObjectsErrors(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	sortSchema := []string{"label:app"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// The object holds only tenant "other" data. The node targets tenant "T",
	// so a source is collected but no records for "T" are produced.
	buildSourceLogObject(t, bucket, "objA", sortSchema, map[string][]testStream{
		"other": {{labels: `{app="a"}`, entries: linesAt(base, 3)}},
	})

	c := newTestExecutorContext(t, bucket)
	node := &physical.LogMerge{
		Tenant:          "T",
		SortSchema:      sortSchema,
		OutputIndexPath: "index/out",
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objA"}}},
		},
	}

	require.Error(t, c.doLogObjectMerge(ctx, node), "zero output objects must error")

	ok, err := bucket.Exists(ctx, node.OutputIndexPath)
	require.NoError(t, err)
	require.False(t, ok, "no index object should be written when no output was produced")
}

// removeFailingStore wraps a scratch.Store and fails every Remove, simulating a
// scratch-cleanup failure surfaced through a flushed object's closer.
type removeFailingStore struct {
	scratch.Store
}

func (removeFailingStore) Remove(scratch.Handle) error {
	return errors.New("scratch remove failed")
}

func TestDoLogObjectMerge_CompactedObjectCloseErrorPropagates(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	buildSourceLogObject(t, bucket, "objA", sortSchema, map[string][]testStream{
		tenant: {{labels: `{app="a"}`, entries: linesAt(base, 3)}},
	})

	c := newTestExecutorContext(t, bucket)
	c.scratchStore = removeFailingStore{c.scratchStore}
	node := &physical.LogMerge{
		Tenant:          tenant,
		SortSchema:      sortSchema,
		OutputIndexPath: "index/out",
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objA"}}},
		},
	}

	err := c.doLogObjectMerge(ctx, node)
	require.Error(t, err, "a failing closer must surface as an error, not be dropped")
	require.ErrorContains(t, err, "scratch remove failed")

	ok, err := bucket.Exists(ctx, node.OutputIndexPath)
	require.NoError(t, err)
	require.False(t, ok, "no index should be written when a compacted object fails to close")

	// The compacted object is uploaded before its closer runs, so it lands even
	// though the close failure aborts the merge before the index is built.
	ok, err = bucket.Exists(ctx, logMergeOutputPath(node.OutputIndexPath, 0))
	require.NoError(t, err)
	require.True(t, ok, "the compacted object is uploaded before the failing close")
}
