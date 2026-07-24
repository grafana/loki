package compactor

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	dataobjindex "github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	statssection "github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// TestLogSectionRefs_NoPerValueSectionFanout is a reproducer for the log-compaction
// duplication bug.
//
// Root cause: an index's stats section records one row PER (data-object section,
// distinct sort-key value) — e.g. one row per service_name present in a section
// (see pkg/dataobj/index/stats_calculation.go, which aggregates per composite
// schema key). logSectionRefsFor turns each stats row into its own
// compactionv2pb.SectionRef, so a single physical log section that holds N
// distinct sort-key values yields N SectionRefs. Downstream, v2.Plan scatters
// those N refs across multiple LogMerge tasks; because a log section can only be
// merged whole, the section is re-merged once per task it lands in, duplicating
// every record it holds (observed ~38x on real data).
//
// The invariant that must hold for compaction to be duplication-free: a physical
// (ObjectPath, SectionIndex) is referenced AT MOST ONCE. When it is, CalculateRuns
// + Plan place each section in exactly one task and each record is merged once.
//
// The index here is built with the real index Calculator over a real source log
// object, so this test catches a fix on either side: a read-side fix (dedup the
// refs in logSectionRefsFor) or a write-side fix (Calculator records one stats row
// per section) both make the invariant hold.
func TestLogSectionRefs_NoPerValueSectionFanout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const tenant = "T"
	bucket := objstore.NewInMemBucket()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// One source object holding several distinct service_name values in a single
	// timestamp-sorted logs section (no schema sort) — the L0/ingester shape that
	// makes the stats section record one row per service_name for that section.
	src := buildReproSourceLogObject(t, tenant, nil, 1<<21, []reproStream{
		{labels: `{service_name="alpha"}`, entries: reproLinesAt(base, 3)},
		{labels: `{service_name="bravo"}`, entries: reproLinesAt(base, 3)},
		{labels: `{service_name="charlie"}`, entries: reproLinesAt(base, 3)},
	})
	defer src.closer.Close()

	const srcPath = "objects/repro/src-0"
	idxPath := buildReproIndex(ctx, t, bucket, map[string]*dataobj.Object{srcPath: src.obj})

	refs, _, err := logSectionRefsFor(ctx, bucket, tenant, idxPath)
	require.NoError(t, err)
	require.NotEmpty(t, refs, "expected at least one section ref from the seeded index")

	distinct := make(map[string]int)
	for _, r := range refs {
		distinct[fmt.Sprintf("%s#%d", r.ObjectPath, r.SectionIndex)]++
	}

	// Log the fan-out so a failing run is self-explanatory.
	for id, n := range distinct {
		if n > 1 {
			t.Logf("section %s is referenced %d times (per-value fan-out)", id, n)
		}
	}

	require.Equal(t, len(distinct), len(refs),
		"logSectionRefsFor must return one ref per physical (ObjectPath, SectionIndex); "+
			"got %d refs for %d distinct sections. The extra refs are per-sort-key-value "+
			"fan-out, which v2.Plan scatters across tasks and causes each section to be "+
			"merged (and its records duplicated) once per task.", len(refs), len(distinct))
}

// TestLogCompactionPlan_NoDuplicationForSingleNonOverlappingObject covers a narrow
// case: a SINGLE schema-sorted source object (that schema recorded in both the logs
// and stats sections). Its sections cover disjoint sort-key ranges, so the planning
// path assigns each physical section to exactly one task and nothing is duplicated.
//
// IMPORTANT — correct sorting alone is NOT sufficient to avoid duplication. When
// MULTIPLE source objects overlap in sort-key space (each spanning the window, as
// real L0/ingester objects do), their per-value refs scatter across tasks and every
// section is re-merged in several tasks even though each object is perfectly sorted.
// See TestLogCompactionPlan_DuplicatesAcrossOverlappingSortedObjects. This test only
// asserts the single-object case that genuinely stays duplication-free; it must not
// be read as "sorted input is safe".
func TestLogCompactionPlan_NoDuplicationForSingleNonOverlappingObject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const tenant = "T"
	bucket := objstore.NewInMemBucket()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Build a genuinely schema-sorted object (SortSchemaASC on service_name) with
	// enough wide records and a small section size to split it into several sections.
	schema := []string{"label:service_name"}
	var srcStreams []reproStream
	for _, svc := range []string{"alpha", "bravo", "charlie", "delta", "echo"} {
		srcStreams = append(srcStreams, reproStream{
			labels:  fmt.Sprintf(`{service_name=%q}`, svc),
			entries: reproWideLinesAt(base, 20),
		})
	}
	src := buildReproSourceLogObject(t, tenant, schema, 4096, srcStreams)
	defer src.closer.Close()

	// Premise: every logs section declares the schema we sorted by, and its records
	// are physically in that sort order (service_name ascending).
	sectionCount := assertReproLogsSchemaSorted(ctx, t, src.obj, tenant, schema)
	require.GreaterOrEqual(t, sectionCount, 2, "want the source split into multiple sorted sections")

	const srcPath = "objects/repro/sorted-0"
	idxPath := buildReproIndex(ctx, t, bucket, map[string]*dataobj.Object{srcPath: src.obj})

	// Premise: the stats section records the same sort schema as the logs sections.
	require.Equal(t, "label:service_name", firstReproStatsSortSchema(ctx, t, bucket, idxPath, tenant),
		"stats section sort schema must match the logs section schema")

	// Replay compactTenantLogs' planning path.
	refs, sortSchema, err := logSectionRefsFor(ctx, bucket, tenant, idxPath)
	require.NoError(t, err)
	runs := v2.CalculateRuns(refs)
	tasks := v2.Plan(runs, tenant, 3, sortSchema)
	require.NotEmpty(t, tasks)

	// No duplication: each physical (ObjectPath, SectionIndex) is planned into at
	// most one task. A section in >1 task would be merged (and its records emitted)
	// once per task.
	tasksBySection := make(map[string]map[int]bool)
	for ti, ts := range tasks {
		for _, run := range ts.Runs {
			for _, s := range run.Sections {
				id := fmt.Sprintf("%s#%d", s.ObjectPath, s.SectionIndex)
				if tasksBySection[id] == nil {
					tasksBySection[id] = make(map[int]bool)
				}
				tasksBySection[id][ti] = true
			}
		}
	}
	require.NotEmpty(t, tasksBySection)
	for id, taskSet := range tasksBySection {
		require.Lenf(t, taskSet, 1,
			"section %s is planned into %d tasks (%v); correctly-sorted input must place each "+
				"section in exactly one task so it is merged only once", id, len(taskSet), sortedInts(taskSet))
	}
}

// reproStream is a labeled set of entries for a source log object.
type reproStream struct {
	labels  string
	entries []push.Entry
}

// reproLinesAt builds count entries one second apart starting at base.
func reproLinesAt(base time.Time, count int) []push.Entry {
	entries := make([]push.Entry, 0, count)
	for i := range count {
		entries = append(entries, push.Entry{
			Timestamp: base.Add(time.Duration(i) * time.Second).UTC(),
			Line:      fmt.Sprintf("line-%d", i),
		})
	}
	return entries
}

// reproSpanEntries builds entries whose timestamps span [lo, hi] (lo, a midpoint,
// and hi), so the resulting section's stats row for the stream carries
// MinTimestamp=lo and MaxTimestamp=hi.
func reproSpanEntries(lo, hi time.Time) []push.Entry {
	mid := lo.Add(hi.Sub(lo) / 2)
	return []push.Entry{
		{Timestamp: lo.UTC(), Line: "lo"},
		{Timestamp: mid.UTC(), Line: "mid"},
		{Timestamp: hi.UTC(), Line: "hi"},
	}
}

// reproWideLinesAt builds count entries with ~120-byte lines so a small
// TargetSectionSize splits them across multiple sections.
func reproWideLinesAt(base time.Time, count int) []push.Entry {
	entries := make([]push.Entry, 0, count)
	for i := range count {
		entries = append(entries, push.Entry{
			Timestamp: base.Add(time.Duration(i) * time.Second).UTC(),
			Line:      fmt.Sprintf("line-%05d-%0110d", i, i),
		})
	}
	return entries
}

// reproSourceObject holds a built source log object plus the closer that must be
// held open until the Calculator has read it.
type reproSourceObject struct {
	obj    *dataobj.Object
	closer io.Closer
}

// reproSortSchemaOverrides is a logsobj.TenantOverrides stub. An empty schema
// leaves the object timestamp-sorted; a non-empty schema sorts it SortSchemaASC.
type reproSortSchemaOverrides []string

func (s reproSortSchemaOverrides) SortSchemaLabels(_ string) []string { return s }

// buildReproSourceLogObject builds a log data object for one tenant from the given
// streams. A non-empty sortSchema writes it in SortSchemaASC order for that schema;
// an empty sortSchema leaves it timestamp-sorted. It is a local copy of the executor
// test's builder (that package's helpers are not importable here).
func buildReproSourceLogObject(t *testing.T, tenant string, sortSchema []string, targetSectionSize int, streams []reproStream) reproSourceObject {
	t.Helper()

	cfg := logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:            2048,
			MaxPageRows:               10000,
			TargetObjectSize:          1 << 22, // 4 MiB
			TargetSectionSize:         flagext.Bytes(targetSectionSize),
			BufferSize:                2048 * 8,
			SectionStripeMergeLimit:   2,
			EstimatedCompressionRatio: 8,
		},
		DataobjSortOrder:     "timestamp-desc",
		AppendOrderedEnabled: true,
		DataobjUseSortSchema: len(sortSchema) > 0,
	}

	b, err := logsobj.NewBuilder(cfg, scratch.NewMemory(), logsobj.NewBuilderMetrics(), log.NewNopLogger(), reproSortSchemaOverrides(sortSchema))
	require.NoError(t, err)

	for _, s := range streams {
		require.NotEmpty(t, s.entries)
		require.NoError(t, b.Append(tenant, logproto.Stream{Labels: s.labels, Entries: s.entries}, s.entries[0].Timestamp))
	}

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	return reproSourceObject{obj: obj, closer: closer}
}

// buildReproIndex runs the real index Calculator over the given source objects and
// uploads the resulting index object, returning its path. Using the real
// Calculator (not a hand-built stats section) is what makes the reproducer catch a
// write-side fix as well as a read-side one.
func buildReproIndex(ctx context.Context, t *testing.T, bucket objstore.Bucket, sources map[string]*dataobj.Object) string {
	t.Helper()

	var cfg Config
	cfg.RegisterFlags(flag.NewFlagSet("repro", flag.PanicOnError))

	ib, err := indexobj.NewBuilder(cfg.IndexobjBuilder, scratch.NewMemory())
	require.NoError(t, err)

	calc := dataobjindex.NewCalculator(ib)
	// Iterate in sorted path order for deterministic index contents.
	paths := make([]string, 0, len(sources))
	for path := range sources {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		require.NoError(t, calc.Calculate(ctx, log.NewNopLogger(), sources[path], path))
	}

	idxObj, closer, err := calc.Flush()
	require.NoError(t, err)
	defer closer.Close()

	const idxPath = "indexes/tenants/T/repro-index"
	reader, err := idxObj.Reader(ctx)
	require.NoError(t, err)
	defer reader.Close()

	raw, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, bucket.Upload(ctx, idxPath, io.NopCloser(bytes.NewReader(raw))))
	return idxPath
}

// assertReproLogsSchemaSorted asserts every tenant logs section in obj declares
// wantSchema and holds its records in ascending service_name order (the primary
// schema sort key), and returns the number of logs sections checked.
func assertReproLogsSchemaSorted(ctx context.Context, t *testing.T, obj *dataobj.Object, tenant string, wantSchema []string) int {
	t.Helper()

	service := make(map[int64]string)
	for _, sec := range obj.Sections().Filter(streams.CheckSection) {
		if sec.Tenant != tenant {
			continue
		}
		ss, err := streams.Open(ctx, sec)
		require.NoError(t, err)
		for res := range streams.IterSection(ctx, ss) {
			s, err := res.Value()
			require.NoError(t, err)
			service[s.ID] = s.Labels.Get("service_name")
		}
	}

	count := 0
	for _, sec := range obj.Sections().Filter(logs.CheckSection) {
		if sec.Tenant != tenant {
			continue
		}
		ls, err := logs.Open(ctx, sec)
		require.NoError(t, err)

		got, err := ls.SchemaLabels()
		require.NoError(t, err)
		require.Equal(t, wantSchema, got, "logs section must declare the schema it was sorted by")

		prev := ""
		first := true
		for res := range logs.IterSection(ctx, ls) {
			rec, err := res.Value()
			require.NoError(t, err)
			cur := service[rec.StreamID]
			if !first {
				require.LessOrEqualf(t, prev, cur, "records must be in ascending service_name order; %q came after %q", cur, prev)
			}
			prev, first = cur, false
		}
		count++
	}
	return count
}

// firstReproStatsSortSchema returns the SortSchema recorded on the first stats row
// of the tenant's stats section in the index at idxPath.
func firstReproStatsSortSchema(ctx context.Context, t *testing.T, bucket objstore.Bucket, idxPath, tenant string) string {
	t.Helper()

	obj, err := dataobj.FromBucket(ctx, bucket, idxPath, 0)
	require.NoError(t, err)
	for _, sec := range obj.Sections().Filter(statssection.CheckSection) {
		if sec.Tenant != tenant {
			continue
		}
		ss, err := statssection.Open(ctx, sec)
		require.NoError(t, err)
		reader := statssection.NewRowReader(ctx, ss)
		defer reader.Close()
		if reader.Next() {
			return reader.At().SortSchema
		}
		require.NoError(t, reader.Err())
	}
	t.Fatalf("no stats section for tenant %q in %s", tenant, idxPath)
	return ""
}

// sortedInts returns the keys of set in ascending order, for stable failure output.
func sortedInts(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// TestLogCompactionPlan_DuplicatesAcrossOverlappingSortedObjects reproduces the
// duplication seen on real data even when the source is correctly schema-sorted.
//
// Each source object is internally sorted by the full schema, and that schema is
// recorded in both the logs and stats sections — yet duplication still occurs.
// The reason: several source objects overlap in sort-key space (each holds the
// same services at interleaved times, like L0/ingester objects that each span the
// whole window). logSectionRefsFor still emits one ref per (section, sort-key
// value), and CalculateRuns spreads a single object's per-value refs across many
// runs (a service's refs from different objects cannot share a run), so v2.Plan
// assigns the same physical (ObjectPath, SectionIndex) to multiple tasks. Because
// a task merges a section whole, the section — and every record it holds — is
// re-emitted once per task, exactly the ~34x duplication measured on comp-poc-b.
//
// This is why "correctly sorted" is not sufficient: it is cross-object OVERLAP,
// not per-object sort order, that the current planning path mishandles.
func TestLogCompactionPlan_DuplicatesAcrossOverlappingSortedObjects(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const tenant = "T"
	bucket := objstore.NewInMemBucket()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	schema := []string{"label:service_name", "label:cluster", "label:namespace", "label:job"}
	services := []string{"s0", "s1", "s2", "s3", "s4", "s5"}

	const (
		nObjects = 12
		window   = 1800 * time.Second // every stream spans ~the whole window
	)
	sources := make(map[string]*dataobj.Object, nObjects)
	var closers []io.Closer
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()

	for i := range nObjects {
		var streams []reproStream
		for j, svc := range services {
			// Each (object, service) stream spans ~the whole window, so the same
			// service from every object overlaps in time and forces parallel runs.
			// Its exact min/max vary per (object, service) — decorrelated across
			// services — so an object's rank differs per service and its section's
			// per-value refs scatter into different runs (hence different tasks).
			// Each object is still internally schema-sorted.
			lo := time.Duration((i*13+j*29)%60) * time.Second
			hi := window - time.Duration((i*17+j*23)%60)*time.Second
			streams = append(streams, reproStream{
				labels:  fmt.Sprintf(`{service_name=%q, cluster="c", namespace="n", job="job"}`, svc),
				entries: reproSpanEntries(base.Add(lo), base.Add(hi)),
			})
		}
		src := buildReproSourceLogObject(t, tenant, schema, 1<<21, streams)
		closers = append(closers, src.closer)
		sources[fmt.Sprintf("objects/repro/ovl-%02d", i)] = src.obj
	}

	idxPath := buildReproIndex(ctx, t, bucket, sources)

	refs, sortSchema, err := logSectionRefsFor(ctx, bucket, tenant, idxPath)
	require.NoError(t, err)
	runs := v2.CalculateRuns(refs)
	tasks := v2.Plan(runs, tenant, 3, sortSchema)
	require.NotEmpty(t, tasks)

	tasksBySection := make(map[string]map[int]bool)
	for ti, ts := range tasks {
		for _, run := range ts.Runs {
			for _, s := range run.Sections {
				id := fmt.Sprintf("%s#%d", s.ObjectPath, s.SectionIndex)
				if tasksBySection[id] == nil {
					tasksBySection[id] = make(map[int]bool)
				}
				tasksBySection[id][ti] = true
			}
		}
	}

	maxTasks, worst := 1, ""
	for id, set := range tasksBySection {
		if len(set) > maxTasks {
			maxTasks, worst = len(set), id
		}
	}
	t.Logf("sections=%d tasks=%d max tasks-per-section=%d (%s)", len(tasksBySection), len(tasks), maxTasks, worst)

	require.Equalf(t, 1, maxTasks,
		"source section %s is planned into %d tasks; a whole-section merge repeated per task "+
			"duplicates its records %dx — even though every source object is correctly schema-sorted",
		worst, maxTasks, maxTasks)
}
