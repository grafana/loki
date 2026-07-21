package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
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
	buildSourceLogObjectWithConfig(t, bucket, path, cfg, sortSchema, byTenant)
}

// buildMultiSectionSourceLogObject builds a single log object that holds several
// logs sections per tenant by cutting sections at a tiny TargetSectionSize while
// keeping TargetObjectSize large enough to stay within one object. This lets a
// test partition one object's sections across tasks.
func buildMultiSectionSourceLogObject(t *testing.T, bucket objstore.Bucket, path string, sortSchema []string, byTenant map[string][]testStream) {
	t.Helper()

	cfg := logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:            128,
			MaxPageRows:               10000,
			TargetObjectSize:          1 << 22, // 4 MiB: stays one object
			TargetSectionSize:         256,     // tiny: forces many logs sections
			BufferSize:                2048 * 8,
			SectionStripeMergeLimit:   2,
			EstimatedCompressionRatio: 8,
		},
		DataobjSortOrder:     "timestamp-desc",
		AppendOrderedEnabled: true,
		DataobjUseSortSchema: len(sortSchema) > 0,
	}
	buildSourceLogObjectWithConfig(t, bucket, path, cfg, sortSchema, byTenant)
}

func buildSourceLogObjectWithConfig(t *testing.T, bucket objstore.Bucket, path string, cfg logsobj.BuilderConfig, sortSchema []string, byTenant map[string][]testStream) {
	t.Helper()

	b, err := logsobj.NewBuilder(cfg, scratch.NewMemory(), logsobj.NewBuilderMetrics(), log.NewNopLogger(), sortSchemaOverrides(sortSchema))
	require.NoError(t, err)

	for tenant, streamSet := range byTenant {
		for _, s := range streamSet {
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
	buildSourceLogObject(t, bucket, "multi", sortSchema, map[string][]testStream{
		tenant:  {{labels: `{app="a"}`, entries: linesAt(base, 3)}},
		"other": {{labels: `{app="z"}`, entries: linesAt(base, 3)}},
	})

	// Reference tenant T's actual logs section index. Sections are ordered by
	// natsorted tenant name, so "other" may precede "T"; a task's refs always
	// name the tenant's own section indices, never another tenant's.
	tIdxs := logsSectionIndexesForTenant(ctx, t, bucket, "multi", tenant)
	require.NotEmpty(t, tIdxs)
	refs := make([]*compactionv2pb.SectionRef, 0, len(tIdxs))
	for _, idx := range tIdxs {
		refs = append(refs, &compactionv2pb.SectionRef{ObjectPath: "multi", SectionIndex: int64(idx)})
	}

	c := newTestExecutorContext(t, bucket)
	node := &physical.LogMerge{
		Tenant:     tenant,
		SortSchema: sortSchema,
		Runs:       []*compactionv2pb.RunRef{{Sections: refs}},
	}

	sources, err := c.collectLogSources(ctx, node)
	require.NoError(t, err)
	require.Len(t, sources, 1)

	apps := map[string]bool{}
	for _, st := range sources[0].streams {
		apps[st.Labels.Get("app")] = true
	}
	require.Equal(t, map[string]bool{"a": true}, apps, "other tenants' streams must be excluded")
}

// TestCollectLogSources_ReadsFromUnprefixedDataBucket reproduces the bug where
// the compaction worker was wired with only the index-prefixed bucket. Source
// log objects live at the unprefixed dataobj root, so reading them through the
// prefixed bucket prepended the index prefix and failed with "key does not
// exist". collectLogSources must read sources from dataBucket (unprefixed).
func TestCollectLogSources_ReadsFromUnprefixedDataBucket(t *testing.T) {
	ctx := context.Background()

	const tenant = "T"
	sortSchema := []string{"label:app"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Source log objects are written at the unprefixed root, exactly as the
	// uploader writes them (objects/<sha>/<sha>).
	root := objstore.NewInMemBucket()
	buildSourceLogObject(t, root, "objects/aa/bb", sortSchema, map[string][]testStream{
		tenant: {{labels: `{app="a"}`, entries: linesAt(base, 3)}},
	})

	// bucket is the index-prefixed view the compaction wiring hands the
	// executor; dataBucket is the unprefixed root for reading source objects.
	c := newTestExecutorContext(t, objstore.NewPrefixedBucket(root, "dataobj/index/v0"))
	c.dataBucket = root

	node := &physical.LogMerge{
		Tenant:     tenant,
		SortSchema: sortSchema,
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objects/aa/bb"}}},
		},
	}

	sources, err := c.collectLogSources(ctx, node)
	require.NoError(t, err, "source objects must resolve against the unprefixed data bucket")
	require.Len(t, sources, 1)
	require.Equal(t, "objects/aa/bb", sources[0].path)
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

// readCompactedObjectsFromIndex reads the index at indexPath from the index bucket,
// extracts the embedded compacted log object paths, and loads each object from
// the data bucket. Returns the streamID->app map and records for each object.
func readCompactedObjectsFromIndex(ctx context.Context, t *testing.T, dataBucket objstore.Bucket, indexBucket objstore.Bucket, indexPath, tenant string) []struct {
	streamApp map[int64]string
	records   []outputRecord
} {
	t.Helper()

	// Load the index from the index bucket
	indexObj, err := dataobj.FromBucket(ctx, indexBucket, indexPath, 0)
	require.NoError(t, err)

	// Extract the compacted log object paths from the postings section
	var logObjectPaths []string
	for _, sec := range indexObj.Sections().Filter(postings.CheckSection) {
		if sec.Tenant != tenant {
			continue
		}
		ps, err := postings.Open(ctx, sec)
		require.NoError(t, err)
		reader := postings.NewReader(postings.ReaderOptions{Columns: ps.Columns()})
		require.NoError(t, reader.Open(ctx))
		rr := postings.NewRowReader(ctx, reader)
		for rr.Next() {
			row := rr.At()
			if row.ObjectPath != "" {
				// Avoid duplicates
				found := false
				for _, p := range logObjectPaths {
					if p == row.ObjectPath {
						found = true
						break
					}
				}
				if !found {
					logObjectPaths = append(logObjectPaths, row.ObjectPath)
				}
			}
		}
		rr.Close()
	}

	// Now load each compacted log object from the data bucket
	var out []struct {
		streamApp map[int64]string
		records   []outputRecord
	}

	for _, logPath := range logObjectPaths {
		logObj, err := dataobj.FromBucket(ctx, dataBucket, logPath, 0)
		require.NoError(t, err, "compacted log object must exist at %s", logPath)

		streamApp := make(map[int64]string)
		for _, sec := range logObj.Sections().Filter(streams.CheckSection) {
			if sec.Tenant != tenant {
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
		for _, sec := range logObj.Sections().Filter(logs.CheckSection) {
			if sec.Tenant != tenant {
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
	dataBucket := objstore.NewInMemBucket()
	indexBucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}
	ta := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tb := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	tc := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)

	// Overlapping label sets across objects (a,b,c / b,c,d / c,d,e). With the
	// k-way merge (no cross-object stream dedup) every source stream becomes its
	// own output stream: 9 source streams -> 9 output streams.
	buildSourceLogObject(t, dataBucket, "objA", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="a"}`, entries: wideLinesAt(ta, 4)},
			{labels: `{app="b"}`, entries: wideLinesAt(ta, 4)},
			{labels: `{app="c"}`, entries: wideLinesAt(ta, 4)},
		},
	})
	buildSourceLogObject(t, dataBucket, "objB", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="b"}`, entries: wideLinesAt(tb, 4)},
			{labels: `{app="c"}`, entries: wideLinesAt(tb, 4)},
			{labels: `{app="d"}`, entries: wideLinesAt(tb, 4)},
		},
	})
	buildSourceLogObject(t, dataBucket, "objC", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="c"}`, entries: wideLinesAt(tc, 4)},
			{labels: `{app="d"}`, entries: wideLinesAt(tc, 4)},
			{labels: `{app="e"}`, entries: wideLinesAt(tc, 4)},
		},
	})

	c := newSmallObjectExecutorContext(t, indexBucket)
	c.dataBucket = dataBucket
	node := &physical.LogMerge{
		Tenant:     tenant,
		SortSchema: sortSchema,
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objA"}, {ObjectPath: "objB"}, {ObjectPath: "objC"}}},
		},
	}

	arts, err := c.doLogObjectMerge(ctx, node)
	require.NoError(t, err)
	require.NotEmpty(t, arts, "merge must return artifacts")
	require.Len(t, arts, 1, "log-merge produces one index artifact")

	indexPath := arts[0].Path
	objs := readCompactedObjectsFromIndex(ctx, t, dataBucket, indexBucket, indexPath, tenant)
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

func TestDoLogObjectMerge_WritesIndexOverCompactedObjects(t *testing.T) {
	ctx := context.Background()
	dataBucket := objstore.NewInMemBucket()
	indexBucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	buildSourceLogObject(t, dataBucket, "objA", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="x"}`, entries: linesAt(base, 2)},
			{labels: `{app="y"}`, entries: linesAt(base, 3)},
		},
	})

	c := newTestExecutorContext(t, indexBucket)
	c.dataBucket = dataBucket
	node := &physical.LogMerge{
		Tenant:     tenant,
		SortSchema: sortSchema,
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objA"}}},
		},
	}

	arts, err := c.doLogObjectMerge(ctx, node)
	require.NoError(t, err)
	require.Len(t, arts, 1, "produce one index")

	indexPath := arts[0].Path

	// Index exists at the content-hash path in the index bucket
	ok, err := indexBucket.Exists(ctx, indexPath)
	require.NoError(t, err)
	require.True(t, ok, "index object must be written at %s", indexPath)

	// Index contains sections for the tenant
	indexObj, err := dataobj.FromBucket(ctx, indexBucket, indexPath, 0)
	require.NoError(t, err)
	kinds := make(map[string]bool)
	for _, sec := range indexObj.Sections() {
		if sec.Tenant != tenant {
			continue
		}
		if stats.CheckSection(sec) {
			kinds["stats"] = true
		}
		if postings.CheckSection(sec) {
			kinds["postings"] = true
		}
	}
	require.Contains(t, kinds, "stats", "index must contain stats section")
	require.Contains(t, kinds, "postings", "index must contain postings section")

	// Compacted log object exists in data bucket
	objs := readCompactedObjectsFromIndex(ctx, t, dataBucket, indexBucket, indexPath, tenant)
	require.Equal(t, 1, len(objs), "single source -> single compacted object")
}

func TestDoLogObjectMerge_IndexCoversAllSplitObjects(t *testing.T) {
	ctx := context.Background()
	dataBucket := objstore.NewInMemBucket()
	indexBucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}
	ta := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tb := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	// Two objects with overlapping label sets to force multiple output objects
	buildSourceLogObject(t, dataBucket, "objA", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="a"}`, entries: wideLinesAt(ta, 5)},
			{labels: `{app="b"}`, entries: wideLinesAt(ta, 5)},
		},
	})
	buildSourceLogObject(t, dataBucket, "objB", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="b"}`, entries: wideLinesAt(tb, 5)},
			{labels: `{app="c"}`, entries: wideLinesAt(tb, 5)},
		},
	})

	c := newSmallObjectExecutorContext(t, indexBucket)
	c.dataBucket = dataBucket
	node := &physical.LogMerge{
		Tenant:     tenant,
		SortSchema: sortSchema,
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objA"}, {ObjectPath: "objB"}}},
		},
	}

	arts, err := c.doLogObjectMerge(ctx, node)
	require.NoError(t, err)
	require.Len(t, arts, 1, "produce one index")

	indexPath := arts[0].Path
	objs := readCompactedObjectsFromIndex(ctx, t, dataBucket, indexBucket, indexPath, tenant)

	// With small TargetObjectSize, the merge should split into multiple objects
	require.GreaterOrEqual(t, len(objs), 2, "merge should split into multiple objects")

	// Index postings section must reference all compacted log objects by path
	indexObj, err := dataobj.FromBucket(ctx, indexBucket, indexPath, 0)
	require.NoError(t, err)

	referencedPaths := make(map[string]bool)
	for _, sec := range indexObj.Sections().Filter(postings.CheckSection) {
		if sec.Tenant != tenant {
			continue
		}
		ps, err := postings.Open(ctx, sec)
		require.NoError(t, err)
		reader := postings.NewReader(postings.ReaderOptions{Columns: ps.Columns()})
		require.NoError(t, reader.Open(ctx))
		rr := postings.NewRowReader(ctx, reader)
		for rr.Next() {
			row := rr.At()
			if row.ObjectPath != "" {
				referencedPaths[row.ObjectPath] = true
			}
		}
		rr.Close()
	}

	// All compacted objects must be referenced in the index
	require.Equal(t, len(objs), len(referencedPaths), "index must reference all compacted objects")
}

func TestDoLogObjectMerge_EmptyRunsErrors(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	c := newTestExecutorContext(t, bucket)
	node := &physical.LogMerge{Tenant: "T", SortSchema: []string{"label:app"}}

	_, err := c.doLogObjectMerge(ctx, node)
	require.Error(t, err, "empty runs must error so the coordinator skips the ToC swap")
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
		Tenant:     "T",
		SortSchema: sortSchema,
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objA"}}},
		},
	}

	_, err := c.doLogObjectMerge(ctx, node)
	require.Error(t, err, "zero output objects must error")
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
	dataBucket := objstore.NewInMemBucket()
	indexBucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	buildSourceLogObject(t, dataBucket, "objA", sortSchema, map[string][]testStream{
		tenant: {{labels: `{app="a"}`, entries: linesAt(base, 3)}},
	})

	c := newTestExecutorContext(t, indexBucket)
	c.dataBucket = dataBucket
	c.scratchStore = removeFailingStore{c.scratchStore}
	node := &physical.LogMerge{
		Tenant:     tenant,
		SortSchema: sortSchema,
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objA"}}},
		},
	}

	_, err := c.doLogObjectMerge(ctx, node)
	require.Error(t, err, "a failing closer must surface as an error, not be dropped")
	require.ErrorContains(t, err, "scratch remove failed")

	// No index should be written when a compacted object fails to close.
	// (The index path is content-addressed, so assert nothing landed in the
	// index bucket.)
	require.NoError(t, indexBucket.Iter(ctx, "", func(string) error {
		return fmt.Errorf("unexpected object written to index bucket")
	}))
}
func TestExecuteLogMerge_ContentHashAndRecord(t *testing.T) {
	ctx := context.Background()
	dataBucket := objstore.NewInMemBucket()
	indexBucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	buildSourceLogObject(t, dataBucket, "objA", sortSchema, map[string][]testStream{
		tenant: {{labels: `{app="a"}`, entries: linesAt(base, 2)}},
	})

	c := newTestExecutorContext(t, indexBucket)
	c.dataBucket = dataBucket
	node := &physical.LogMerge{
		Tenant:     tenant,
		SortSchema: sortSchema,
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objA"}}},
		},
	}

	// executeLogMerge emits a result record batch
	pipeline := c.executeLogMerge(node)
	require.NotNil(t, pipeline)

	// Open the pipeline first
	err := pipeline.Open(ctx)
	require.NoError(t, err)
	defer pipeline.Close()

	// The pipeline should contain a result record when read
	rec, err := pipeline.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec, "pipeline must yield a result record")

	// Deserialize the result artifacts
	arts, err := v2.ReadResultRecord(rec)
	require.NoError(t, err)
	require.Len(t, arts, 1, "produce one index artifact")

	indexPath := arts[0].Path
	// Index path follows content-hash format: indexes/tenants/<tenant>/<h2>/<hrest>
	require.Contains(t, indexPath, "indexes/tenants/T/", "index path must follow content-hash format")

	// Index exists in the index bucket
	ok, err := indexBucket.Exists(ctx, indexPath)
	require.NoError(t, err)
	require.True(t, ok, "index must exist at %s", indexPath)

	// Read EOF to close the pipeline
	_, err = pipeline.Read(ctx)
	require.ErrorIs(t, err, io.EOF)
}

// appTS uniquely identifies a source record by its stream's app label and
// nanosecond timestamp. The section-partition test builds records so every
// (app, ts) pair is globally unique.
type appTS struct {
	app string
	ts  int64
}

// logsSectionIndexesForTenant enumerates the SectionIndex values of a log
// object's logs sections that belong to the tenant, using the same
// Filter(logs.CheckSection) enumeration the index builder used to assign them.
func logsSectionIndexesForTenant(ctx context.Context, t *testing.T, bucket objstore.Bucket, path, tenant string) []int {
	t.Helper()
	obj, err := dataobj.FromBucket(ctx, bucket, path, 0)
	require.NoError(t, err)

	var idxs []int
	for i, sec := range obj.Sections().Filter(logs.CheckSection) {
		if sec.Tenant == tenant {
			idxs = append(idxs, i)
		}
	}
	return idxs
}

// readLogObjectRecords returns the multiset of (app, ts) records a log object
// holds for the tenant, resolving stream IDs to their app label.
func readLogObjectRecords(ctx context.Context, t *testing.T, bucket objstore.Bucket, path, tenant string) map[appTS]int {
	t.Helper()
	obj, err := dataobj.FromBucket(ctx, bucket, path, 0)
	require.NoError(t, err)

	streamApp := make(map[int64]string)
	for _, sec := range obj.Sections().Filter(streams.CheckSection) {
		if sec.Tenant != tenant {
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

	out := make(map[appTS]int)
	for _, sec := range obj.Sections().Filter(logs.CheckSection) {
		if sec.Tenant != tenant {
			continue
		}
		ls, err := logs.Open(ctx, sec)
		require.NoError(t, err)
		for res := range logs.IterSection(ctx, ls) {
			rec, err := res.Value()
			require.NoError(t, err)
			out[appTS{app: streamApp[rec.StreamID], ts: rec.Timestamp.UnixNano()}]++
		}
	}
	return out
}

// TestDoLogObjectMerge_HonorsPerTaskSectionSelection asserts that when one
// object's sections are partitioned across two tasks, each task merges only the
// sections it was assigned: the two outputs are disjoint and their union equals
// the whole source. Before the fix the executor merged every section for every
// task, so both tasks produced identical outputs (overlap == whole).
func TestDoLogObjectMerge_HonorsPerTaskSectionSelection(t *testing.T) {
	ctx := context.Background()
	dataBucket := objstore.NewInMemBucket()
	indexBucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}

	// One stream per app, each on its own day, so every (app, ts) pair is
	// globally unique and records can be matched exactly across tasks.
	baseDay := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	apps := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	streamSet := make([]testStream, 0, len(apps))
	for i, app := range apps {
		streamSet = append(streamSet, testStream{
			labels:  fmt.Sprintf(`{app=%q}`, app),
			entries: wideLinesAt(baseDay.Add(time.Duration(i)*24*time.Hour), 6),
		})
	}
	buildMultiSectionSourceLogObject(t, dataBucket, "obj", sortSchema, map[string][]testStream{
		tenant: streamSet,
	})

	idxs := logsSectionIndexesForTenant(ctx, t, dataBucket, "obj", tenant)
	require.GreaterOrEqual(t, len(idxs), 2, "test needs multiple logs sections to partition across tasks")

	// Partition the sections into two disjoint groups.
	var group1, group2 []int
	for i, idx := range idxs {
		if i%2 == 0 {
			group1 = append(group1, idx)
		} else {
			group2 = append(group2, idx)
		}
	}
	require.NotEmpty(t, group1)
	require.NotEmpty(t, group2)

	runFor := func(group []int) map[appTS]int {
		refs := make([]*compactionv2pb.SectionRef, 0, len(group))
		for _, idx := range group {
			refs = append(refs, &compactionv2pb.SectionRef{ObjectPath: "obj", SectionIndex: int64(idx)})
		}
		c := newTestExecutorContext(t, indexBucket)
		c.dataBucket = dataBucket
		node := &physical.LogMerge{
			Tenant:     tenant,
			SortSchema: sortSchema,
			Runs:       []*compactionv2pb.RunRef{{Sections: refs}},
		}
		arts, err := c.doLogObjectMerge(ctx, node)
		require.NoError(t, err)
		require.Len(t, arts, 1)

		got := make(map[appTS]int)
		for _, o := range readCompactedObjectsFromIndex(ctx, t, dataBucket, indexBucket, arts[0].Path, tenant) {
			for _, r := range o.records {
				got[appTS{app: r.app, ts: r.ts.UnixNano()}]++
			}
		}
		return got
	}

	got1 := runFor(group1)
	got2 := runFor(group2)

	// No overlap: a source record must be merged by exactly one task.
	for k := range got1 {
		_, dup := got2[k]
		require.False(t, dup, "record %+v produced by both tasks; sections were not partitioned", k)
	}

	// Union == whole: no record omitted or duplicated across the two tasks.
	union := make(map[appTS]int, len(got1)+len(got2))
	for k, v := range got1 {
		union[k] += v
	}
	for k, v := range got2 {
		union[k] += v
	}
	whole := readLogObjectRecords(ctx, t, dataBucket, "obj", tenant)
	require.Equal(t, whole, union, "union of per-task outputs must equal the whole source")
}
