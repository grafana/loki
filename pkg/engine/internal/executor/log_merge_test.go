package executor

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// sortSchemaOverrides is a logsobj.TenantOverrides stub returning a fixed sort
// schema (ordered FQN sort keys) and the default shard layout for every tenant.
type sortSchemaOverrides []string

func (s sortSchemaOverrides) SortSchemaLabels(_ string) []string { return s }

func (s sortSchemaOverrides) DataObjShardLayout(_ string) streams.ShardLayout {
	return streams.DefaultShardLayout()
}

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
	buildSourceLogObjectWithSectionSize(t, bucket, path, sortSchema, byTenant, 1<<21)
}

func buildSourceLogObjectWithSectionSize(t *testing.T, bucket objstore.Bucket, path string, sortSchema []string, byTenant map[string][]testStream, sectionSize flagext.Bytes) {
	t.Helper()

	cfg := logsobj.BuilderBaseConfig{
		TargetPageSize:            2048,
		MaxPageRows:               10000,
		TargetObjectSize:          1 << 22, // 4 MiB
		TargetSectionSize:         sectionSize,
		BufferSize:                2048 * 8,
		SectionStripeMergeLimit:   2,
		EstimatedCompressionRatio: 8,
	}
	buildSourceLogObjectWithConfig(t, bucket, path, cfg, sortSchema, byTenant)
}

// buildMultiSectionSourceLogObject builds a single log object that holds several
// logs sections per tenant by cutting sections at a tiny TargetSectionSize while
// keeping TargetObjectSize large enough to stay within one object. This lets a
// test partition one object's sections across tasks.
func buildMultiSectionSourceLogObject(t *testing.T, bucket objstore.Bucket, path string, sortSchema []string, byTenant map[string][]testStream) {
	t.Helper()

	cfg := logsobj.BuilderBaseConfig{
		TargetPageSize:            128,
		MaxPageRows:               10000,
		TargetObjectSize:          1 << 22, // 4 MiB: stays one object
		TargetSectionSize:         256,     // tiny: forces many logs sections
		BufferSize:                2048 * 8,
		SectionStripeMergeLimit:   2,
		EstimatedCompressionRatio: 8,
	}
	buildSourceLogObjectWithConfig(t, bucket, path, cfg, sortSchema, byTenant)
}

func buildSourceLogObjectWithConfig(t testing.TB, bucket objstore.Bucket, path string, cfg logsobj.BuilderBaseConfig, sortSchema []string, byTenant map[string][]testStream) {
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

	sorted, sortedCloser, err := b.FlushAndSort(context.Background())
	require.NoError(t, err)
	defer sortedCloser.Close()

	require.NoError(t, uploadObjectToBucket(context.Background(), bucket, path, sorted))
}

// sourceLogRuns gives each object its own run; overlapping objects cannot be
// concatenated. References count logs sections across all tenants.
func sourceLogRuns(t testing.TB, bucket objstore.Bucket, tenant string, paths ...string) []*compactionv2pb.RunRef {
	t.Helper()
	var runs []*compactionv2pb.RunRef
	for _, path := range paths {
		obj, err := dataobj.FromBucket(context.Background(), bucket, path, 0)
		require.NoError(t, err)
		run := &compactionv2pb.RunRef{}
		for i, section := range obj.Sections().Filter(logs.CheckSection) {
			if section.Tenant == tenant && logs.CheckSection(section) {
				run.Sections = append(run.Sections, &compactionv2pb.SectionRef{ObjectPath: path, SectionIndex: int64(i)})
			}
		}
		runs = append(runs, run)
	}
	return runs
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
		Tenant: tenant,
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
		require.NotEmpty(t, s.logSectionsByIndex, "source %q must have at least one logs section", s.path)
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
		Tenant: tenant,
		Runs:   []*compactionv2pb.RunRef{{Sections: refs}},
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
		Tenant: tenant,
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "objects/aa/bb"}}},
		},
	}

	sources, err := c.collectLogSources(ctx, node)
	require.NoError(t, err, "source objects must resolve against the unprefixed data bucket")
	require.Len(t, sources, 1)
	require.Equal(t, "objects/aa/bb", sources[0].path)
}

// TestDoLogObjectMerge_WritesCompactedLogsToDataBucket reproduces the bug where
// compacted log objects were written next to the index (through the
// index-prefixed bucket, under the indexes/ namespace) instead of to the objects/
// namespace of the unprefixed data bucket. Because a query worker resolves object
// paths recorded in an index against the unprefixed data bucket, a compacted log
// written under the index prefix is unreadable at query time. Compacted logs must
// land in the data bucket under objects/, and the index must reference that path.
// newSmallObjectExecutorContext is like newTestExecutorContext but with a tiny
// TargetObjectSize so the merge splits its output across multiple objects.
func newSmallObjectExecutorContext(t *testing.T, bucket objstore.Bucket) *Context {
	t.Helper()
	c := newTestExecutorContext(t, bucket)
	c.logsobjCfg.TargetPageSize = 512
	c.logsobjCfg.TargetObjectSize = 1000 // bytes; forces splitting
	c.logsobjCfg.TargetSectionSize = 800
	return c
}

// outputRecord is one decoded record read back from a compacted output object.
type outputRecord struct {
	app      string
	streamID int64
	ts       time.Time
}

// compactedObject holds the streams and records of one LogMerge output object.
type compactedObject struct {
	streamApp    map[int64]string
	streamLabels map[int64]labels.Labels
	records      []outputRecord
}

// readCompactedObjectsFromIndex reads the index at indexPath from the index bucket,
// extracts the embedded compacted log object paths, and loads each object from
// the data bucket. Returns the streamID->app map and records for each object.
func readCompactedObjectsFromIndex(ctx context.Context, t *testing.T, dataBucket objstore.Bucket, indexBucket objstore.Bucket, indexPath, tenant string) []compactedObject {
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
	var out []compactedObject

	for _, logPath := range logObjectPaths {
		logObj, err := dataobj.FromBucket(ctx, dataBucket, logPath, 0)
		require.NoError(t, err, "compacted log object must exist at %s", logPath)

		streamApp := make(map[int64]string)
		streamLabels := make(map[int64]labels.Labels)
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
				streamLabels[s.ID] = s.Labels.Copy()
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

		out = append(out, compactedObject{streamApp: streamApp, streamLabels: streamLabels, records: records})
	}
	return out
}

// requireSortedByLayout checks that the records of o are in the order of the
// default shard layout with sortSchema: shard, schema key, hash, stream ID,
// and timestamp descending.
func requireSortedByLayout(t *testing.T, o compactedObject, sortSchema []string, objIdx int) {
	t.Helper()
	for i := 1; i < len(o.records); i++ {
		prev, curr := o.records[i-1], o.records[i]
		prevSchemaKey, err := logsobj.ComputeSchemaKey(o.streamLabels[prev.streamID], sortSchema)
		require.NoError(t, err)
		prevKey := streams.DefaultShardLayout().NewSortKey(o.streamLabels[prev.streamID], prevSchemaKey)

		currSchemaKey, err := logsobj.ComputeSchemaKey(o.streamLabels[curr.streamID], sortSchema)
		require.NoError(t, err)
		currKey := streams.DefaultShardLayout().NewSortKey(o.streamLabels[curr.streamID], currSchemaKey)

		require.LessOrEqual(t, streams.CompareSortKey(prevKey, currKey), 0, "stream order must be non-decreasing within object %d", objIdx)
		if streams.CompareSortKey(prevKey, currKey) == 0 {
			require.LessOrEqual(t, prev.streamID, curr.streamID, "streamIDs must be non-decreasing within a stream-order group")
			if prev.streamID == curr.streamID {
				require.False(t, curr.ts.After(prev.ts), "timestamps must be non-increasing within a stream")
			}
		}
	}
}

func TestBuildGlobalStreamTable_SameLabelsShareID(t *testing.T) {
	sortSchema := []string{"label:app"}
	ls := labels.FromStrings("app", "auth")
	other := labels.FromStrings("app", "web")
	sources := []*logSource{
		{
			path: "a",
			streams: map[int64]streams.Stream{
				2: {ID: 2, Labels: ls, ShardBucket: int64(streams.DefaultShardLayout().BucketFromLabels(ls))},
				7: {ID: 7, Labels: other, ShardBucket: int64(streams.DefaultShardLayout().BucketFromLabels(other))},
			},
		},
		{
			path: "b",
			streams: map[int64]streams.Stream{
				5: {ID: 5, Labels: ls.Copy(), ShardBucket: int64(streams.DefaultShardLayout().BucketFromLabels(ls))},
			},
		},
	}

	table, err := buildGlobalStreamTable(sources, streams.NewLayout(streams.DefaultShardLayout(), sortSchema))
	require.NoError(t, err)

	aID, err := table.Resolve(0, 2)
	require.NoError(t, err)
	bID, err := table.Resolve(1, 5)
	require.NoError(t, err)
	require.Equal(t, aID, bID, "same labels across objects must share one global ID")
	aID2, err := table.Resolve(0, 7)
	require.NoError(t, err)
	require.NotEqual(t, aID, aID2)

	count := table.Size()
	require.Equal(t, count, 2)
	for id := int64(2); id <= int64(count); id++ {
		prev := table.ByID(id - 1)
		curr := table.ByID(id)
		require.Negative(t, streams.CompareSortKey(prev, curr),
			"global stream IDs must increase in SortKey order")
	}
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

	// Overlapping label sets across objects (a,b,c / b,c,d / c,d,e). The
	// logsobj output builder deduplicates the 9 source stream occurrences into
	// 5 logical streams.
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
		Tenant: tenant,
		Runs:   sourceLogRuns(t, dataBucket, tenant, "objA", "objB", "objC"),
	}

	arts, err := c.doLogObjectMerge(ctx, node)
	require.NoError(t, err)
	require.NoError(t, arts.Validate())

	indexPath := arts.Path
	objs := readCompactedObjectsFromIndex(ctx, t, dataBucket, indexBucket, indexPath, tenant)
	require.GreaterOrEqual(t, len(objs), 2, "output must be split into multiple objects")

	totalStreams := 0
	totalRecords := 0
	distinctApps := make(map[string]bool)
	for objIdx, o := range objs {
		totalRecords += len(o.records)
		totalStreams += len(o.streamApp)
		for _, app := range o.streamApp {
			distinctApps[app] = true
		}
		requireSortedByLayout(t, o, sortSchema, objIdx)
	}

	// Cross-object dedup: 9 source stream occurrences => 5 logical streams.
	require.Equal(t, 5, totalStreams)
	// The 5 distinct label sets are all present.
	require.Equal(t, map[string]bool{"a": true, "b": true, "c": true, "d": true, "e": true}, distinctApps)
	// 9 stream-appends x 4 entries = 36 records.
	require.Equal(t, 36, totalRecords)
}

func TestDoLogObjectMerge_DeduplicatesConflictingSourceStreamOrder(t *testing.T) {
	ctx := context.Background()
	dataBucket := objstore.NewInMemBucket()
	indexBucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := early.Add(time.Hour)

	// Same schema key, different local stream ID order. Unique-label global IDs
	// make the tree timestamp-merge each full-label stream instead of treating
	// the two objects as distinct streams.
	buildSourceLogObject(t, dataBucket, "objA", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="a",instance="1"}`, entries: linesAt(early, 1)},
			{labels: `{app="a",instance="2"}`, entries: linesAt(early.Add(time.Second), 1)},
		},
	})
	buildSourceLogObject(t, dataBucket, "objB", sortSchema, map[string][]testStream{
		tenant: {
			{labels: `{app="a",instance="2"}`, entries: linesAt(late, 1)},
			{labels: `{app="a",instance="1"}`, entries: linesAt(late.Add(time.Second), 1)},
		},
	})

	c := newTestExecutorContext(t, indexBucket)
	c.dataBucket = dataBucket
	node := &physical.LogMerge{
		Tenant: tenant,
		Runs:   sourceLogRuns(t, dataBucket, tenant, "objA", "objB"),
	}

	arts, err := c.doLogObjectMerge(ctx, node)
	require.NoError(t, err)
	require.NoError(t, arts.Validate())

	objs := readCompactedObjectsFromIndex(ctx, t, dataBucket, indexBucket, arts.Path, tenant)
	require.Len(t, objs, 1)
	require.Len(t, objs[0].streamApp, 2, "two full-label streams must remain after cross-object deduplication")
	require.Len(t, objs[0].records, 4)

	counts := make(map[int64]int)
	for i, record := range objs[0].records {
		counts[record.streamID]++
		if i == 0 {
			continue
		}
		prev := objs[0].records[i-1]
		require.LessOrEqual(t, prev.streamID, record.streamID)
		if prev.streamID == record.streamID {
			require.False(t, record.ts.After(prev.ts), "timestamps must be descending within the deduplicated stream")
		}
	}
	require.Equal(t, map[int64]int{1: 2, 2: 2}, counts)
}

type recordingLogMergeObserver struct {
	stats      []LogMergeObservedStats
	inputBytes []int64
}

func (o *recordingLogMergeObserver) ObserveLogMerge(_ string, stats LogMergeObservedStats, _ time.Duration) {
	o.stats = append(o.stats, stats)
}

func (o *recordingLogMergeObserver) ObserveLogMergeInputBytes(bytes int64) {
	o.inputBytes = append(o.inputBytes, bytes)
}

func TestDoLogObjectMerge_ReportsInputBytesAndDuplicates(t *testing.T) {
	ctx := context.Background()
	dataBucket := objstore.NewInMemBucket()
	indexBucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	entry := func(line, traceID string) push.Entry {
		return push.Entry{Timestamp: ts, Line: line, StructuredMetadata: push.LabelsAdapter{{Name: "trace_id", Value: traceID}}}
	}

	buildSourceLogObject(t, dataBucket, "objA", sortSchema, map[string][]testStream{
		tenant: {{labels: `{app="a"}`, entries: []push.Entry{entry("line", "1")}}},
	})
	buildSourceLogObject(t, dataBucket, "objB", sortSchema, map[string][]testStream{
		tenant: {{labels: `{app="a"}`, entries: []push.Entry{entry("line", "1"), entry("line", "22")}}},
	})

	observer := &recordingLogMergeObserver{}
	c := newTestExecutorContext(t, indexBucket)
	c.dataBucket = dataBucket
	c.logMergeObserver = observer
	node := &physical.LogMerge{
		Tenant: tenant,
		Runs:   sourceLogRuns(t, dataBucket, tenant, "objA", "objB"),
	}

	_, err := c.doLogObjectMerge(ctx, node)
	require.NoError(t, err)

	wantBytes := int64(len("line1") + len("line1") + len("line22"))
	require.Equal(t, []int64{wantBytes}, observer.inputBytes, "records below one batch must be reported in one report")
	require.Len(t, observer.stats, 1)
	require.Equal(t, logMergeOutcomeSuccess, observer.stats[0].Outcome)
	require.Equal(t, int64(len("line1")+len("line1")+len("line22")), observer.stats[0].InputBytes)
	require.Equal(t, 1, observer.stats[0].DuplicateRecords)
}

func TestDoLogObjectMerge_CountsDuplicatesByStreamLabels(t *testing.T) {
	const tenant = "T"
	sortSchema := []string{"label:app"}
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	entries := []push.Entry{{Timestamp: ts, Line: "same line"}}

	// mergeDuplicates merges objA and objB and returns the duplicate count plus
	// each object's local stream ID for every label set.
	mergeDuplicates := func(t *testing.T, objA, objB []testStream) (int, map[string]map[string]int64) {
		ctx := context.Background()
		dataBucket := objstore.NewInMemBucket()
		buildSourceLogObject(t, dataBucket, "objA", sortSchema, map[string][]testStream{tenant: objA})
		buildSourceLogObject(t, dataBucket, "objB", sortSchema, map[string][]testStream{tenant: objB})

		observer := &recordingLogMergeObserver{}
		c := newTestExecutorContext(t, objstore.NewInMemBucket())
		c.dataBucket = dataBucket
		c.logMergeObserver = observer
		node := &physical.LogMerge{Tenant: tenant, Runs: sourceLogRuns(t, dataBucket, tenant, "objA", "objB")}

		sources, err := c.collectLogSources(ctx, node)
		require.NoError(t, err)
		localIDs := make(map[string]map[string]int64)
		for _, src := range sources {
			localIDs[src.path] = make(map[string]int64)
			for id, stream := range src.streams {
				localIDs[src.path][stream.Labels.String()] = id
			}
		}

		_, err = c.doLogObjectMerge(ctx, node)
		require.NoError(t, err)
		require.Len(t, observer.stats, 1)
		return observer.stats[0].DuplicateRecords, localIDs
	}

	t.Run("counts a duplicate when the stream has a different local ID in each object", func(t *testing.T) {
		duplicates, localIDs := mergeDuplicates(t,
			[]testStream{{labels: `{app="a"}`, entries: entries}, {labels: `{app="b"}`, entries: entries}},
			[]testStream{{labels: `{app="a"}`, entries: entries}},
		)

		require.NotEqual(t, localIDs["objA"][`{app="a"}`], localIDs["objB"][`{app="a"}`])
		require.Equal(t, 1, duplicates)
	})

	t.Run("does not count records from different streams that share a local ID", func(t *testing.T) {
		duplicates, localIDs := mergeDuplicates(t,
			[]testStream{{labels: `{app="a"}`, entries: entries}},
			[]testStream{{labels: `{app="b"}`, entries: entries}},
		)

		require.Equal(t, localIDs["objA"][`{app="a"}`], localIDs["objB"][`{app="b"}`])
		require.Equal(t, 0, duplicates)
	})
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
		Tenant: tenant,
		Runs:   sourceLogRuns(t, dataBucket, tenant, "objA"),
	}

	arts, err := c.doLogObjectMerge(ctx, node)
	require.NoError(t, err)
	require.NoError(t, arts.Validate())

	indexPath := arts.Path

	// Index exists at the content-hash path in the index bucket
	ok, err := indexBucket.Exists(ctx, indexPath)
	require.NoError(t, err)
	require.True(t, ok, "index object must be written at %s", indexPath)

	// Index contains sections for the tenant only
	indexObj, err := dataobj.FromBucket(ctx, indexBucket, indexPath, 0)
	require.NoError(t, err)
	require.Equal(t, []string{tenant}, indexObj.Tenants())
	kinds := make(map[string]bool)
	for _, sec := range indexObj.Sections() {
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
		Tenant: tenant,
		Runs:   sourceLogRuns(t, dataBucket, tenant, "objA", "objB"),
	}

	arts, err := c.doLogObjectMerge(ctx, node)
	require.NoError(t, err)
	require.NoError(t, arts.Validate())

	indexPath := arts.Path
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
	node := &physical.LogMerge{Tenant: "T"}

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
		Tenant: "T",
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
		Tenant: tenant,
		Runs:   sourceLogRuns(t, dataBucket, tenant, "objA"),
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
	var arts v2.ResultArtifact
	require.NoError(t, arts.FromRecordBatch(rec))
	require.NoError(t, arts.Validate())

	indexPath := arts.Path
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
			Tenant: tenant,
			Runs:   []*compactionv2pb.RunRef{{Sections: refs}},
		}
		arts, err := c.doLogObjectMerge(ctx, node)
		require.NoError(t, err)
		require.NoError(t, arts.Validate())

		got := make(map[appTS]int)
		for _, o := range readCompactedObjectsFromIndex(ctx, t, dataBucket, indexBucket, arts.Path, tenant) {
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

// appSortPrefix returns the sort prefix of the stream {app="<app>"} under the
// default shard layout and sortSchema.
func appSortPrefix(t *testing.T, app string, sortSchema []string) streams.SortPrefix {
	t.Helper()
	prefix, err := logsobj.StreamSortPrefix(labels.FromStrings("app", app), streams.NewLayout(streams.DefaultShardLayout(), sortSchema))
	require.NoError(t, err)
	return prefix
}

func appSortPrefixProto(t *testing.T, app string, sortSchema []string) *compactionv2pb.KeyRangePrefix {
	t.Helper()
	return &compactionv2pb.KeyRangePrefix{ShardBucket: appSortPrefix(t, app, sortSchema).ShardBucket, SchemaValues: []string{app}}
}

var (
	minSortPrefixProto   = v2.FullKeyRangeProto().Start
	sortPrefixLimitProto = v2.FullKeyRangeProto().End
)

func prefixRange(t *testing.T, start, end streams.SortPrefix) v2.KeyRange[streams.SortPrefix] {
	t.Helper()
	r, err := v2.NewKeyRange(start, end, streams.SortPrefix.Compare)
	require.NoError(t, err)
	return r
}

func TestDoLogObjectMerge(t *testing.T) {
	ctx := context.Background()
	const tenant = "T"
	sortSchema := []string{"label:app"}

	mergeRecords := func(t *testing.T, dataBucket objstore.Bucket, node *physical.LogMerge) map[appTS]int {
		t.Helper()
		indexBucket := objstore.NewInMemBucket()
		c := newTestExecutorContext(t, indexBucket)
		c.dataBucket = dataBucket
		arts, err := c.doLogObjectMerge(ctx, node)
		require.NoError(t, err)
		require.NoError(t, arts.Validate())

		got := make(map[appTS]int)
		for _, o := range readCompactedObjectsFromIndex(ctx, t, dataBucket, indexBucket, arts.Path, tenant) {
			for _, r := range o.records {
				got[appTS{app: r.app, ts: r.ts.UnixNano()}]++
			}
		}
		return got
	}

	t.Run("splits shared sections across tasks without losing or duplicating records", func(t *testing.T) {
		dataBucket := objstore.NewInMemBucket()
		baseDay := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		apps := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
		streamSet := make([]testStream, 0, len(apps))
		for i, app := range apps {
			streamSet = append(streamSet, testStream{
				labels:  fmt.Sprintf(`{app=%q}`, app),
				entries: wideLinesAt(baseDay.Add(time.Duration(i)*24*time.Hour), 6),
			})
		}
		buildMultiSectionSourceLogObject(t, dataBucket, "obj", sortSchema, map[string][]testStream{tenant: streamSet})

		slices.SortFunc(apps, func(a, b string) int {
			return appSortPrefix(t, a, sortSchema).Compare(appSortPrefix(t, b, sortSchema))
		})
		cutApp := apps[len(apps)/2]
		cut := appSortPrefix(t, cutApp, sortSchema)
		runs := sourceLogRuns(t, dataBucket, tenant, "obj")

		below := mergeRecords(t, dataBucket, &physical.LogMerge{
			Tenant:   tenant,
			Runs:     runs,
			KeyRange: &compactionv2pb.KeyRange{Start: minSortPrefixProto, End: appSortPrefixProto(t, cutApp, sortSchema)},
		})
		above := mergeRecords(t, dataBucket, &physical.LogMerge{
			Tenant:   tenant,
			Runs:     runs,
			KeyRange: &compactionv2pb.KeyRange{Start: appSortPrefixProto(t, cutApp, sortSchema), End: sortPrefixLimitProto},
		})

		for k := range below {
			require.Negative(t, appSortPrefix(t, k.app, sortSchema).Compare(cut), "record %+v is not below the cut", k)
			_, dup := above[k]
			require.False(t, dup, "record %+v produced by both tasks", k)
		}
		for k := range above {
			require.GreaterOrEqual(t, appSortPrefix(t, k.app, sortSchema).Compare(cut), 0, "record %+v is below the cut", k)
		}

		union := make(map[appTS]int, len(below)+len(above))
		for k, v := range below {
			union[k] += v
		}
		for k, v := range above {
			union[k] += v
		}
		require.Equal(t, readLogObjectRecords(ctx, t, dataBucket, "obj", tenant), union)
	})

	t.Run("skips the sections of an object with no stream in range", func(t *testing.T) {
		dataBucket := objstore.NewInMemBucket()
		day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		buildSourceLogObject(t, dataBucket, "objA", sortSchema, map[string][]testStream{
			tenant: {{labels: `{app="a"}`, entries: wideLinesAt(day, 4)}},
		})
		buildSourceLogObject(t, dataBucket, "objB", sortSchema, map[string][]testStream{
			tenant: {{labels: `{app="b"}`, entries: wideLinesAt(day, 3)}},
		})
		keyRange := &compactionv2pb.KeyRange{Start: minSortPrefixProto, End: appSortPrefixProto(t, "b", sortSchema)}
		if appSortPrefix(t, "a", sortSchema).Compare(appSortPrefix(t, "b", sortSchema)) > 0 {
			keyRange = &compactionv2pb.KeyRange{Start: appSortPrefixProto(t, "a", sortSchema), End: sortPrefixLimitProto}
		}
		c := newTestExecutorContext(t, objstore.NewInMemBucket())
		c.dataBucket = dataBucket

		inputs, err := c.prepareLogMergeInputs(ctx, &physical.LogMerge{
			Tenant:   tenant,
			Runs:     sourceLogRuns(t, dataBucket, tenant, "objA", "objB"),
			KeyRange: keyRange,
		})
		require.NoError(t, err)

		require.Len(t, inputs.runs, 1, "the run of objB must be skipped")
	})

	t.Run("checks the bytes it reads against the input bytes of the task", func(t *testing.T) {
		dataBucket := objstore.NewInMemBucket()
		day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		// wideLinesAt writes 100-byte lines without metadata, so app a holds
		// 400 input bytes and app b 300.
		buildSourceLogObject(t, dataBucket, "objA", sortSchema, map[string][]testStream{
			tenant: {{labels: `{app="a"}`, entries: wideLinesAt(day, 4)}, {labels: `{app="b"}`, entries: wideLinesAt(day, 3)}},
		})
		keyRange := &compactionv2pb.KeyRange{Start: minSortPrefixProto, End: appSortPrefixProto(t, "b", sortSchema)}
		if appSortPrefix(t, "a", sortSchema).Compare(appSortPrefix(t, "b", sortSchema)) > 0 {
			keyRange = &compactionv2pb.KeyRange{Start: appSortPrefixProto(t, "a", sortSchema), End: sortPrefixLimitProto}
		}
		merge := func(inputBytes int64) error {
			c := newTestExecutorContext(t, objstore.NewInMemBucket())
			c.dataBucket = dataBucket
			_, err := c.doLogObjectMerge(ctx, &physical.LogMerge{
				Tenant:     tenant,
				Runs:       sourceLogRuns(t, dataBucket, tenant, "objA"),
				KeyRange:   keyRange,
				InputBytes: inputBytes,
			})
			return err
		}

		require.NoError(t, merge(400), "succeeds when the bytes match")
		require.NoError(t, merge(0), "skips the check when the input bytes are unknown")
		require.ErrorContains(t, merge(700), "read 400 input bytes, but the index stats hold 700 bytes")
	})

	t.Run("keeps no record of an object with no stream in range", func(t *testing.T) {
		dataBucket := objstore.NewInMemBucket()
		day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		buildSourceLogObject(t, dataBucket, "objA", sortSchema, map[string][]testStream{
			tenant: {{labels: `{app="a"}`, entries: wideLinesAt(day, 4)}},
		})
		buildSourceLogObject(t, dataBucket, "objB", sortSchema, map[string][]testStream{
			tenant: {{labels: `{app="b"}`, entries: wideLinesAt(day, 4)}},
		})

		keyRange := &compactionv2pb.KeyRange{Start: minSortPrefixProto, End: appSortPrefixProto(t, "b", sortSchema)}
		if appSortPrefix(t, "a", sortSchema).Compare(appSortPrefix(t, "b", sortSchema)) > 0 {
			keyRange = &compactionv2pb.KeyRange{Start: appSortPrefixProto(t, "a", sortSchema), End: sortPrefixLimitProto}
		}
		got := mergeRecords(t, dataBucket, &physical.LogMerge{
			Tenant:   tenant,
			Runs:     sourceLogRuns(t, dataBucket, tenant, "objA", "objB"),
			KeyRange: keyRange,
		})

		require.Equal(t, readLogObjectRecords(ctx, t, dataBucket, "objA", tenant), got)
	})
}

func TestDropStreamsOutsideRange(t *testing.T) {
	sortSchema := []string{"label:app"}
	layout := streams.NewLayout(streams.DefaultShardLayout(), sortSchema)
	newStreams := func() map[int64]streams.Stream {
		return map[int64]streams.Stream{
			1: {ID: 1, Labels: labels.FromStrings("app", "a")},
			2: {ID: 2, Labels: labels.FromStrings("app", "b")},
		}
	}
	a, b := appSortPrefix(t, "a", sortSchema), appSortPrefix(t, "b", sortSchema)
	low, high := a, b
	lowID, highID := int64(1), int64(2)
	if a.Compare(b) > 0 {
		low, high = b, a
		lowID, highID = 2, 1
	}

	t.Run("drops no stream for the full range", func(t *testing.T) {
		byID := newStreams()
		dropped, err := dropStreamsOutsideRange(byID, layout, prefixRange(t, streams.MinSortPrefix(), streams.SortPrefixLimit()))
		require.NoError(t, err)
		require.Empty(t, dropped)
		require.Len(t, byID, 2)
	})

	t.Run("drops the streams above the range and returns their IDs", func(t *testing.T) {
		byID := newStreams()
		dropped, err := dropStreamsOutsideRange(byID, layout, prefixRange(t, streams.MinSortPrefix(), high))
		require.NoError(t, err)
		require.Equal(t, []int64{highID}, dropped)
		require.Contains(t, byID, lowID)
		require.NotContains(t, byID, highID)
	})

	t.Run("drops the streams below the range and returns their IDs", func(t *testing.T) {
		byID := newStreams()
		dropped, err := dropStreamsOutsideRange(byID, layout, prefixRange(t, high, streams.SortPrefixLimit()))
		require.NoError(t, err)
		require.Equal(t, []int64{lowID}, dropped)
		require.Contains(t, byID, highID)
	})

	t.Run("drops every stream when no stream is in range", func(t *testing.T) {
		byID := newStreams()
		dropped, err := dropStreamsOutsideRange(byID, layout, prefixRange(t, streams.MinSortPrefix(), low))
		require.NoError(t, err)
		require.ElementsMatch(t, []int64{1, 2}, dropped)
		require.Empty(t, byID)
	})
}

func TestSortPrefixRange(t *testing.T) {
	bounds := func(r v2.KeyRange[streams.SortPrefix]) [2]streams.SortPrefix {
		return [2]streams.SortPrefix{r.Start(), r.End()}
	}
	full := [2]streams.SortPrefix{streams.MinSortPrefix(), streams.SortPrefixLimit()}
	at := func(bucket uint32, values ...string) *compactionv2pb.KeyRangePrefix {
		return &compactionv2pb.KeyRangePrefix{ShardBucket: bucket, SchemaValues: values}
	}

	t.Run("returns the full range when the range is missing", func(t *testing.T) {
		got, err := sortPrefixRange(nil)
		require.NoError(t, err)
		require.Equal(t, full, bounds(got))
	})

	t.Run("returns the range with both bounds", func(t *testing.T) {
		got, err := sortPrefixRange(&compactionv2pb.KeyRange{Start: at(1, "a"), End: at(2, "b")})
		require.NoError(t, err)
		require.Equal(t, [2]streams.SortPrefix{streams.NewSortPrefix(1, []string{"a"}), streams.NewSortPrefix(2, []string{"b"})}, bounds(got))
	})

	t.Run("accepts the full range from the coordinator", func(t *testing.T) {
		got, err := sortPrefixRange(&compactionv2pb.KeyRange{Start: minSortPrefixProto, End: sortPrefixLimitProto})
		require.NoError(t, err)
		require.Equal(t, full, bounds(got))
	})

	errorCases := []struct {
		name string
		r    *compactionv2pb.KeyRange
	}{
		{name: "returns an error when the start is missing", r: &compactionv2pb.KeyRange{End: at(2)}},
		{name: "returns an error when the end is missing", r: &compactionv2pb.KeyRange{Start: at(1)}},
		{name: "returns an error when the start equals the end", r: &compactionv2pb.KeyRange{Start: at(1, "a"), End: at(1, "a")}},
		{name: "returns an error when the start is above the end", r: &compactionv2pb.KeyRange{Start: at(2), End: at(1)}},
		{name: "returns an error when the end is above the sort prefix limit", r: &compactionv2pb.KeyRange{Start: at(0), End: at(streams.SortPrefixLimit().ShardBucket, "a")}},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sortPrefixRange(tc.r)
			require.Error(t, err)
		})
	}
}

// streamAgg is a per-output-stream aggregate read back from a compacted object's
// streams section.
type streamAgg struct {
	app              string
	rows             int
	uncompressedSize int64
	minTS, maxTS     time.Time
}

// readCompactedStreamAggs returns, for every compacted object referenced by the
// index, the streams-section aggregate paired with the aggregate recomputed from
// the logs records that object actually holds. Each element is one output stream.
func readCompactedStreamAggs(ctx context.Context, t *testing.T, dataBucket, indexBucket objstore.Bucket, indexPath, tenant string) (declared, actual []streamAgg) {
	t.Helper()

	indexObj, err := dataobj.FromBucket(ctx, indexBucket, indexPath, 0)
	require.NoError(t, err)

	var logObjectPaths []string
	seen := make(map[string]struct{})
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
			if row.ObjectPath == "" {
				continue
			}
			if _, dup := seen[row.ObjectPath]; dup {
				continue
			}
			seen[row.ObjectPath] = struct{}{}
			logObjectPaths = append(logObjectPaths, row.ObjectPath)
		}
		rr.Close()
	}

	for _, logPath := range logObjectPaths {
		logObj, err := dataobj.FromBucket(ctx, dataBucket, logPath, 0)
		require.NoError(t, err, "compacted log object must exist at %s", logPath)

		type streamMeta struct {
			app              string
			rows             int
			uncompressedSize int64
			minTS, maxTS     time.Time
		}
		declaredByID := make(map[int64]streamMeta)
		for _, sec := range logObj.Sections().Filter(streams.CheckSection) {
			if sec.Tenant != tenant {
				continue
			}
			ss, err := streams.Open(ctx, sec)
			require.NoError(t, err)
			for res := range streams.IterSection(ctx, ss) {
				s, err := res.Value()
				require.NoError(t, err)
				declaredByID[s.ID] = streamMeta{
					app:              s.Labels.Get("app"),
					rows:             s.Rows,
					uncompressedSize: s.UncompressedSize,
					minTS:            s.MinTimestamp,
					maxTS:            s.MaxTimestamp,
				}
			}
		}

		actualByID := make(map[int64]streamMeta)
		for _, sec := range logObj.Sections().Filter(logs.CheckSection) {
			if sec.Tenant != tenant {
				continue
			}
			ls, err := logs.Open(ctx, sec)
			require.NoError(t, err)
			for res := range logs.IterSection(ctx, ls) {
				rec, err := res.Value()
				require.NoError(t, err)
				m := actualByID[rec.StreamID]
				size := int64(len(rec.Line))
				rec.Metadata.Range(func(l labels.Label) { size += int64(len(l.Value)) })
				m.uncompressedSize += size
				m.rows++
				if m.minTS.IsZero() || rec.Timestamp.Before(m.minTS) {
					m.minTS = rec.Timestamp
				}
				if rec.Timestamp.After(m.maxTS) {
					m.maxTS = rec.Timestamp
				}
				actualByID[rec.StreamID] = m
			}
		}

		for id, d := range declaredByID {
			a := actualByID[id]
			declared = append(declared, streamAgg(d))
			actual = append(actual, streamAgg{app: d.app, rows: a.rows, uncompressedSize: a.uncompressedSize, minTS: a.minTS, maxTS: a.maxTS})
		}
	}
	return declared, actual
}

// TestDoLogObjectMerge_StreamMetadataMatchesMergedRecords asserts that when a
// single stream is split across logs sections assigned to different tasks, each
// task's streams section reports aggregates (rows, uncompressed size, timestamp
// range) covering only the records that task actually merged, not the full
// source stream. Copying the source aggregate verbatim inflates every task's
// stream metadata to span data it never wrote.
func TestDoLogObjectMerge_StreamMetadataMatchesMergedRecords(t *testing.T) {
	ctx := context.Background()
	dataBucket := objstore.NewInMemBucket()
	indexBucket := objstore.NewInMemBucket()

	const tenant = "T"
	sortSchema := []string{"label:app"}

	// A few streams, each with many wide records at distinct timestamps. With a
	// tiny section size the records of a single app straddle section boundaries,
	// so partitioning sections across tasks splits a stream across tasks.
	baseDay := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	apps := []string{"a", "b", "c", "d"}
	streamSet := make([]testStream, 0, len(apps))
	for i, app := range apps {
		streamSet = append(streamSet, testStream{
			labels:  fmt.Sprintf(`{app=%q}`, app),
			entries: wideLinesAt(baseDay.Add(time.Duration(i)*24*time.Hour), 12),
		})
	}
	buildMultiSectionSourceLogObject(t, dataBucket, "obj", sortSchema, map[string][]testStream{
		tenant: streamSet,
	})

	idxs := logsSectionIndexesForTenant(ctx, t, dataBucket, "obj", tenant)
	require.GreaterOrEqual(t, len(idxs), 2, "test needs multiple logs sections to partition across tasks")

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

	checkGroup := func(group []int) {
		refs := make([]*compactionv2pb.SectionRef, 0, len(group))
		for _, idx := range group {
			refs = append(refs, &compactionv2pb.SectionRef{ObjectPath: "obj", SectionIndex: int64(idx)})
		}
		c := newTestExecutorContext(t, indexBucket)
		c.dataBucket = dataBucket
		node := &physical.LogMerge{
			Tenant: tenant,
			Runs:   []*compactionv2pb.RunRef{{Sections: refs}},
		}
		arts, err := c.doLogObjectMerge(ctx, node)
		require.NoError(t, err)
		require.NoError(t, arts.Validate())

		declared, actual := readCompactedStreamAggs(ctx, t, dataBucket, indexBucket, arts.Path, tenant)
		require.NotEmpty(t, declared, "expected at least one output stream")
		require.Equal(t, actual, declared,
			"streams section metadata must match the records actually merged, not the full source stream")
	}

	checkGroup(group1)
	checkGroup(group2)
}

func TestDoLogObjectMerge_PlannedRuns(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	const tenant = "T"
	schema := []string{"label:app"}
	base := time.Unix(100, 0)
	var sections []v2.Section[streams.SortKey]
	// The duplicate label forces two overlapping runs. Other objects can be
	// concatenated using the planner's strict shard/schema-prefix comparison.
	for i, app := range []string{"a", "b", "c", "d", "b"} {
		path := fmt.Sprintf("source-%d", i)
		lbls := labels.FromStrings("app", app)
		buildSourceLogObject(t, bucket, path, schema, map[string][]testStream{
			tenant: {{labels: lbls.String(), entries: linesAt(base.Add(time.Duration(i)*time.Hour), 2)}},
		})
		key, err := logsobj.ComputeSchemaKey(lbls, schema)
		require.NoError(t, err)
		sortKey := streams.DefaultShardLayout().NewSortKey(lbls, key)
		for _, ref := range sourceLogRuns(t, bucket, tenant, path)[0].Sections {
			sections = append(sections, v2.Section[streams.SortKey]{Ref: ref, Min: sortKey, Max: sortKey})
		}
	}
	runs := v2.CalculateRuns(sections, func(a, b streams.SortKey) int {
		return cmp.Or(cmp.Compare(a.ShardBucket, b.ShardBucket), strings.Compare(a.SchemaKey, b.SchemaKey))
	})
	require.Len(t, runs, 2)
	var refs []*compactionv2pb.RunRef
	for _, run := range runs {
		refs = append(refs, &compactionv2pb.RunRef{Sections: run.Sections()})
	}
	require.True(t, len(refs[0].Sections) > 1 || len(refs[1].Sections) > 1)
	c := newTestExecutorContext(t, bucket)
	arts, err := c.doLogObjectMerge(ctx, &physical.LogMerge{Tenant: tenant, Runs: refs})
	require.NoError(t, err)
	require.NoError(t, arts.Validate())
	objects := readCompactedObjectsFromIndex(ctx, t, bucket, bucket, arts.Path, tenant)
	require.Len(t, objects, 1)
	require.Len(t, objects[0].records, 10)
	require.Len(t, objects[0].streamLabels, 4)
}

func TestLogMergeInputs_SectionReferences(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	const tenant = "T"
	schema := []string{"label:app"}
	var sourceStreams []testStream
	for i := range 8 {
		sourceStreams = append(sourceStreams, testStream{
			labels: fmt.Sprintf(`{app="app%d"}`, i), entries: wideLinesAt(time.Unix(100, 0), 20),
		})
	}
	buildSourceLogObjectWithSectionSize(t, bucket, "source", schema, map[string][]testStream{tenant: sourceStreams}, 256)
	refs := sourceLogRuns(t, bucket, tenant, "source")[0].Sections
	require.Greater(t, len(refs), 1, "fixture must contain multiple logs sections")
	c := newTestExecutorContext(t, bucket)
	node := &physical.LogMerge{Tenant: tenant, Runs: []*compactionv2pb.RunRef{
		{Sections: []*compactionv2pb.SectionRef{refs[1], refs[1]}},
		{Sections: []*compactionv2pb.SectionRef{refs[1]}},
	}}
	inputs, err := c.prepareLogMergeInputs(ctx, node)
	require.NoError(t, err)
	require.Len(t, inputs.sources, 1)
	require.Len(t, inputs.sources[0].logSectionsByIndex, len(refs))
	for i, section := range inputs.sources[0].logSectionsByIndex {
		if int64(i) == refs[1].SectionIndex {
			require.NotNil(t, section)
		} else {
			require.Nil(t, section, "unassigned section descriptors must be nil")
		}
	}
	require.Len(t, inputs.runs, 1)
	require.Len(t, inputs.runs[0], 1, "duplicate references must not duplicate records")
	require.Same(t, inputs.sources[0].logSectionsByIndex[refs[1].SectionIndex], inputs.runs[0][0].Section)
	require.Equal(t, inputs.sources[0].remap, inputs.runs[0][0].Remap)
	require.NotNil(t, inputs.runs[0][0].Remap)

	t.Run("merge selected section once", func(t *testing.T) {
		arts, err := c.doLogObjectMerge(ctx, node)
		require.NoError(t, err)
		objects := readCompactedObjectsFromIndex(ctx, t, bucket, bucket, arts.Path, tenant)
		require.Len(t, objects, 1)
		require.NotEmpty(t, objects[0].records)
		require.Less(t, len(objects[0].records), 160)
	})
	t.Run("concatenate all sections in storage order", func(t *testing.T) {
		all := &physical.LogMerge{Tenant: tenant, Runs: sourceLogRuns(t, bucket, tenant, "source")}
		arts, err := c.doLogObjectMerge(ctx, all)
		require.NoError(t, err)
		objects := readCompactedObjectsFromIndex(ctx, t, bucket, bucket, arts.Path, tenant)
		require.Len(t, objects, 1)
		require.Len(t, objects[0].records, 160)
	})

	for _, index := range []int64{-1, int64(len(refs))} {
		t.Run(fmt.Sprintf("invalid-index-%d", index), func(t *testing.T) {
			node.Runs = []*compactionv2pb.RunRef{{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "source", SectionIndex: index}}}}
			_, err := c.prepareLogMergeInputs(ctx, node)
			require.Error(t, err)
		})
	}
}

func TestDoLogObjectMerge_StreamsLayout(t *testing.T) {
	const tenant = "T"
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// uploadRawSource uploads an object with one stream, whose streams section
	// uses shardLayout and records layout if it is non-nil.
	uploadRawSource := func(t *testing.T, bucket objstore.Bucket, path string, shardLayout streams.ShardLayout, layout *streams.Layout) {
		t.Helper()
		streamBuilder := streams.NewBuilder(nil, 2048, 10000, shardLayout)
		if layout != nil {
			streamBuilder.SetLayout(*layout)
		}
		streamBuilder.SetTenant(tenant)
		id := streamBuilder.Record(labels.FromStrings("app", "a"), base, 4)
		logBuilder := logs.NewBuilder(nil, logs.BuilderOptions{
			PageSizeHint: 2048, BufferSize: 2048, StripeMergeLimit: 2, SortOrder: logs.SortStreamASC,
		})
		logBuilder.SetTenant(tenant)
		logBuilder.Append(logs.Record{StreamID: id, Timestamp: base, Line: []byte("line")})

		objBuilder := dataobj.NewBuilder(nil)
		require.NoError(t, objBuilder.Append(streamBuilder))
		require.NoError(t, objBuilder.Append(logBuilder))
		obj, closer, err := objBuilder.Flush()
		require.NoError(t, err)
		defer closer.Close()
		require.NoError(t, uploadObjectToBucket(t.Context(), bucket, path, obj))
	}
	newContext := func(t *testing.T, dataBucket objstore.Bucket) *Context {
		t.Helper()
		c := newTestExecutorContext(t, objstore.NewInMemBucket())
		c.dataBucket = dataBucket
		return c
	}

	t.Run("writes every output object of a split merge with the layout of the sources", func(t *testing.T) {
		dataBucket := objstore.NewInMemBucket()
		var streamsA, streamsB []testStream
		for i := range 4 {
			streamsA = append(streamsA, testStream{labels: fmt.Sprintf(`{app="a%d",cluster="c%d"}`, i, i), entries: wideLinesAt(base, 4)})
			streamsB = append(streamsB, testStream{labels: fmt.Sprintf(`{app="b%d",cluster="d%d"}`, i, i), entries: wideLinesAt(base, 4)})
		}
		buildSourceLogObject(t, dataBucket, "objA", []string{"label:cluster"}, map[string][]testStream{tenant: streamsA})
		buildSourceLogObject(t, dataBucket, "objB", []string{"label:cluster"}, map[string][]testStream{tenant: streamsB})
		c := newSmallObjectExecutorContext(t, objstore.NewInMemBucket())
		c.dataBucket = dataBucket

		_, err := c.doLogObjectMerge(t.Context(), &physical.LogMerge{Tenant: tenant, Runs: sourceLogRuns(t, dataBucket, tenant, "objA", "objB")})
		require.NoError(t, err)

		layouts := outputLayouts(t, dataBucket, "objA", "objB")
		require.Greater(t, len(layouts), 1, "the merge must split its output")
		want := streams.NewLayout(streams.DefaultShardLayout(), []string{"label:cluster"})
		for _, got := range layouts {
			require.True(t, want.Equal(got), "got layout %+v", got)
		}
	})

	t.Run("returns an error and no output when a source records no layout", func(t *testing.T) {
		dataBucket := objstore.NewInMemBucket()
		uploadRawSource(t, dataBucket, "objA", streams.DefaultShardLayout(), nil)
		c := newContext(t, dataBucket)

		arts, err := c.doLogObjectMerge(t.Context(), &physical.LogMerge{Tenant: tenant, Runs: sourceLogRuns(t, dataBucket, tenant, "objA")})
		require.ErrorContains(t, err, "records no layout")
		require.Nil(t, arts)
	})

	t.Run("returns an error and no output when the sources have a non-default shard layout", func(t *testing.T) {
		four, err := streams.ShardLayoutFromBuckets(4)
		require.NoError(t, err)
		layout := streams.NewLayout(four, nil)
		dataBucket := objstore.NewInMemBucket()
		uploadRawSource(t, dataBucket, "objA", four, &layout)
		c := newContext(t, dataBucket)

		arts, err := c.doLogObjectMerge(t.Context(), &physical.LogMerge{Tenant: tenant, Runs: sourceLogRuns(t, dataBucket, tenant, "objA")})
		require.ErrorContains(t, err, "supports only")
		require.Nil(t, arts)
	})

	t.Run("returns an error and no output when sources record different schemas", func(t *testing.T) {
		dataBucket := objstore.NewInMemBucket()
		buildSourceLogObject(t, dataBucket, "objA", []string{"label:cluster"}, map[string][]testStream{
			tenant: {{labels: `{app="a",cluster="c"}`, entries: linesAt(base, 2)}},
		})
		buildSourceLogObject(t, dataBucket, "objB", []string{"label:app"}, map[string][]testStream{
			tenant: {{labels: `{app="a"}`, entries: linesAt(base, 2)}},
		})

		c := newTestExecutorContext(t, objstore.NewInMemBucket())
		c.dataBucket = dataBucket
		arts, err := c.doLogObjectMerge(t.Context(), &physical.LogMerge{
			Tenant: tenant,
			Runs:   sourceLogRuns(t, dataBucket, tenant, "objA", "objB"),
		})
		require.ErrorContains(t, err, "different stream layout")
		require.Nil(t, arts)
	})
}

func TestDoLogObjectMerge_LegacySources(t *testing.T) {
	const tenant = "T"
	clusterLayout := streams.NewLayout(streams.DefaultShardLayout(), []string{"label:cluster"})

	// uploadLegacy uploads the legacy fixture name from
	// pkg/dataobj/testdata/legacy, whose logs sections record the layout in
	// their own sort info.
	uploadLegacy := func(t *testing.T, bucket objstore.Bucket, name string) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "dataobj", "testdata", "legacy", name+".dataobj"))
		require.NoError(t, err)
		require.NoError(t, bucket.Upload(t.Context(), name, bytes.NewReader(data)))
	}
	merge := func(t *testing.T, dataBucket objstore.Bucket, paths ...string) (*v2.ResultArtifact, error) {
		t.Helper()
		c := newTestExecutorContext(t, objstore.NewInMemBucket())
		c.dataBucket = dataBucket
		return c.doLogObjectMerge(t.Context(), &physical.LogMerge{Tenant: tenant, Runs: sourceLogRuns(t, dataBucket, tenant, paths...)})
	}
	// mergeOutputs merges paths and returns the output objects.
	mergeOutputs := func(t *testing.T, dataBucket objstore.Bucket, paths ...string) []compactedObject {
		t.Helper()
		indexBucket := objstore.NewInMemBucket()
		c := newTestExecutorContext(t, indexBucket)
		c.dataBucket = dataBucket
		arts, err := c.doLogObjectMerge(t.Context(), &physical.LogMerge{Tenant: tenant, Runs: sourceLogRuns(t, dataBucket, tenant, paths...)})
		require.NoError(t, err)
		objs := readCompactedObjectsFromIndex(t.Context(), t, dataBucket, indexBucket, arts.Path, tenant)
		require.NotEmpty(t, objs)
		for i, o := range objs {
			requireSortedByLayout(t, o, clusterLayout.SchemaLabels(), i)
		}
		return objs
	}
	t.Run("merges legacy sources in the layout of their logs sections and records it in the output", func(t *testing.T) {
		dataBucket := objstore.NewInMemBucket()
		uploadLegacy(t, dataBucket, "legacy-cluster-a")
		uploadLegacy(t, dataBucket, "legacy-cluster-b")

		var records int
		for _, o := range mergeOutputs(t, dataBucket, "legacy-cluster-a", "legacy-cluster-b") {
			records += len(o.records)
		}
		require.Equal(t, 8, records, "each fixture holds 4 records")

		layouts := outputLayouts(t, dataBucket, "legacy-cluster-a", "legacy-cluster-b")
		require.NotEmpty(t, layouts)
		for _, layout := range layouts {
			require.True(t, clusterLayout.Equal(layout), "got layout %+v", layout)
		}
	})

	t.Run("merges a legacy source with a new source of the same layout in sort order", func(t *testing.T) {
		dataBucket := objstore.NewInMemBucket()
		uploadLegacy(t, dataBucket, "legacy-cluster-a")
		buildSourceLogObject(t, dataBucket, "new", []string{"label:cluster"}, map[string][]testStream{
			tenant: {{labels: `{app="z",cluster="c-z"}`, entries: linesAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 2)}},
		})

		var records int
		for _, o := range mergeOutputs(t, dataBucket, "legacy-cluster-a", "new") {
			records += len(o.records)
		}
		require.Equal(t, 6, records, "the fixture holds 4 records and the new source 2")

		layouts := outputLayouts(t, dataBucket, "legacy-cluster-a", "new")
		require.NotEmpty(t, layouts)
		for _, layout := range layouts {
			require.True(t, clusterLayout.Equal(layout), "got layout %+v", layout)
		}
	})

	t.Run("returns an error and no output when a legacy source has another layout", func(t *testing.T) {
		dataBucket := objstore.NewInMemBucket()
		uploadLegacy(t, dataBucket, "legacy-cluster-a")
		uploadLegacy(t, dataBucket, "legacy-app")

		arts, err := merge(t, dataBucket, "legacy-cluster-a", "legacy-app")
		require.ErrorContains(t, err, "different stream layout")
		require.Nil(t, arts)
	})
}

func TestLogObjectWriter_StartNewObject(t *testing.T) {
	t.Run("builds objects with the layout of the sources, including a non-default shard layout", func(t *testing.T) {
		fourBuckets, err := streams.ShardLayoutFromBuckets(4)
		require.NoError(t, err)
		layout := streams.NewLayout(fourBuckets, []string{"label:cluster"})

		c := newTestExecutorContext(t, objstore.NewInMemBucket())
		w := &logObjectWriter{c: c, layout: layout, builderMetrics: c.builderMetrics}
		require.NoError(t, w.startNewObject())

		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		require.NoError(t, w.logsBuilder.Append("T", logproto.Stream{
			Labels:  `{cluster="c1"}`,
			Entries: []push.Entry{{Timestamp: now, Line: "line"}},
		}, now))
		obj, closer, err := w.logsBuilder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		require.Equal(t, 1, obj.Sections().Count(streams.CheckSection))
		for _, sec := range obj.Sections().Filter(streams.CheckSection) {
			opened, err := streams.Open(t.Context(), sec)
			require.NoError(t, err)
			got, ok, err := opened.Layout()
			require.NoError(t, err)
			require.True(t, ok, "the streams section must record a layout")
			require.Equal(t, layout, got)
		}
	})
}

// outputLayouts returns the layout that each streams section of the objects in
// dataBucket records, except for the objects at sources.
func outputLayouts(t *testing.T, dataBucket objstore.Bucket, sources ...string) []streams.Layout {
	t.Helper()
	var layouts []streams.Layout
	require.NoError(t, dataBucket.Iter(t.Context(), "", func(path string) error {
		if slices.Contains(sources, path) {
			return nil
		}
		obj, err := dataobj.FromBucket(t.Context(), dataBucket, path, 0)
		require.NoError(t, err)
		for _, sec := range obj.Sections().Filter(streams.CheckSection) {
			opened, err := streams.Open(t.Context(), sec)
			require.NoError(t, err)
			layout, ok, err := opened.Layout()
			require.NoError(t, err)
			require.True(t, ok)
			layouts = append(layouts, layout)
		}
		return nil
	}, objstore.WithRecursiveIter()))
	return layouts
}
