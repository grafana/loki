package sortmerge_test

import (
	"cmp"
	"context"
	"slices"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/sortmerge"
	"github.com/grafana/loki/v3/pkg/logproto"
)

var testBuilderConfig = logsobj.BuilderConfig{
	BuilderBaseConfig: logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		TargetObjectSize:        1 << 20, // 1 MiB
		TargetSectionSize:       8 << 10, // 8 KiB
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	},
}

const testTenant = "test"

// buildObject builds a single in-memory dataobj containing one stream with
// lineCount entries spaced 1s apart starting at base.
func buildObject(t *testing.T, labels string, base time.Time, lineCount int) (*dataobj.Object, func()) {
	t.Helper()

	b, err := logsobj.NewBuilder(testBuilderConfig, nil, logsobj.NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(t, err)

	entries := make([]push.Entry, 0, lineCount)
	for i := 0; i < lineCount; i++ {
		entries = append(entries, push.Entry{
			Timestamp: base.Add(time.Duration(i) * time.Second).UTC(),
			Line:      "line",
		})
	}
	require.NoError(t, b.Append(testTenant, logproto.Stream{
		Labels:  labels,
		Entries: entries,
	}, base))

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	return obj, func() { _ = closer.Close() }
}

func collectSections(t *testing.T, obj *dataobj.Object) []*dataobj.Section {
	t.Helper()
	var out []*dataobj.Section
	for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
		return logs.CheckSection(s) && s.Tenant == testTenant
	}) {
		out = append(out, sec)
	}
	require.NotEmpty(t, out, "expected at least one logs section for tenant")
	return out
}

// TestIterator_MergesAndOrdersByTimestamp builds two single-stream data
// objects whose timestamp ranges interleave, then verifies that
// sortmerge.Iterator merges them into a single non-increasing-timestamp
// stream (SortTimestampDESC) and emits every input record exactly once.
func TestIterator_MergesAndOrdersByTimestamp(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	objA, closeA := buildObject(t, `{app="a"}`, now, 5)
	defer closeA()
	objB, closeB := buildObject(t, `{app="b"}`, now.Add(500*time.Millisecond), 5)
	defer closeB()

	var sections []*dataobj.Section
	sections = append(sections, collectSections(t, objA)...)
	sections = append(sections, collectSections(t, objB)...)

	iter, err := sortmerge.Iterator(context.Background(), sections, logs.SortTimestampDESC)
	require.NoError(t, err)

	var (
		count int
		prev  time.Time
		first = true
	)
	for res := range iter {
		rec, err := res.Value()
		require.NoError(t, err)
		if !first {
			require.False(t, rec.Timestamp.After(prev),
				"SortTimestampDESC must emit non-increasing timestamps; got %s after %s", rec.Timestamp, prev)
		}
		prev = rec.Timestamp
		first = false
		count++
	}
	require.Equal(t, 10, count, "expected merged record count to equal sum of inputs")
}

// TestIterator_SingleSection_DegenerateIdentity asserts the iterator is a
// straight passthrough when given a single section. Catches wiring breakage
// in the open → dataset → row reader → loser tree path.
func TestIterator_SingleSection_DegenerateIdentity(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	obj, closeFn := buildObject(t, `{app="solo"}`, now, 7)
	defer closeFn()

	sections := collectSections(t, obj)

	iter, err := sortmerge.Iterator(context.Background(), sections, logs.SortStreamASC)
	require.NoError(t, err)

	var count int
	for res := range iter {
		_, err := res.Value()
		require.NoError(t, err)
		count++
	}
	require.Equal(t, 7, count, "expected single-section iterator to emit every input record")
}

// TestIterator_NoSections returns an iterator over zero sections — should
// terminate immediately without error.
func TestIterator_NoSections(t *testing.T) {
	iter, err := sortmerge.Iterator(context.Background(), nil, logs.SortStreamASC)
	require.NoError(t, err)
	for res := range iter {
		_, err := res.Value()
		t.Fatalf("expected no records from zero-section iterator; got record err=%v", err)
	}
}

type schemaOverrides []string

func (s schemaOverrides) SortSchemaLabels(_ string) []string { return s }

// buildSchemaObject builds a schema-sorted (SortSchemaASC) in-memory object for
// the given per-label entries. Labels may be appended out of sort-key order to
// exercise stream-ID remapping.
func buildSchemaObject(t *testing.T, sortSchema []string, byLabel map[string][]push.Entry) (*dataobj.Object, func()) {
	t.Helper()

	cfg := testBuilderConfig
	cfg.DataobjSortOrder = "timestamp-desc"
	cfg.AppendOrderedEnabled = true
	cfg.DataobjUseSortSchema = true

	b, err := logsobj.NewBuilder(cfg, nil, logsobj.NewBuilderMetrics(), log.NewNopLogger(), schemaOverrides(sortSchema))
	require.NoError(t, err)

	for lbls, entries := range byLabel {
		require.NoError(t, b.Append(testTenant, logproto.Stream{Labels: lbls, Entries: entries}, entries[0].Timestamp))
	}

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	return obj, func() { _ = closer.Close() }
}

// planGlobalStreams reads the streams sections of the given objects and assigns
// disjoint global stream IDs in (sortKey, sourceIdx, sourceStreamID) order, returning
// the flattened logs sections plus the per-section remap and globalSortKeys the
// merge needs. This mirrors what the compactor executor does.
func planGlobalStreams(ctx context.Context, t *testing.T, objs []*dataobj.Object, sortSchema []string) ([]*dataobj.Section, []map[int64]int64, []string) {
	t.Helper()

	type entry struct {
		sourceIdx      int
		sourceStreamID int64
		sortKey        string
	}
	var all []entry
	perObjLogs := make([][]*dataobj.Section, len(objs))

	for sourceIdx, obj := range objs {
		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
			return streams.CheckSection(s) && s.Tenant == testTenant
		}) {
			ss, err := streams.Open(ctx, sec)
			require.NoError(t, err)
			for res := range streams.IterSection(ctx, ss) {
				stream, err := res.Value()
				require.NoError(t, err)
				key, err := logsobj.ComputeSortKey(stream.Labels, sortSchema)
				require.NoError(t, err)
				all = append(all, entry{sourceIdx: sourceIdx, sourceStreamID: stream.ID, sortKey: key})
			}
		}
		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
			return logs.CheckSection(s) && s.Tenant == testTenant
		}) {
			perObjLogs[sourceIdx] = append(perObjLogs[sourceIdx], sec)
		}
	}

	slices.SortFunc(all, func(a, b entry) int {
		if r := cmp.Compare(a.sortKey, b.sortKey); r != 0 {
			return r
		}
		if r := cmp.Compare(a.sourceIdx, b.sourceIdx); r != 0 {
			return r
		}
		return cmp.Compare(a.sourceStreamID, b.sourceStreamID)
	})

	remapsByObj := make([]map[int64]int64, len(objs))
	for i := range remapsByObj {
		remapsByObj[i] = map[int64]int64{}
	}
	globalSortKeys := make([]string, len(all)+1)
	for i, e := range all {
		gid := int64(i + 1)
		globalSortKeys[gid] = e.sortKey
		remapsByObj[e.sourceIdx][e.sourceStreamID] = gid
	}

	var sections []*dataobj.Section
	var remaps []map[int64]int64
	for sourceIdx := range objs {
		for _, sec := range perObjLogs[sourceIdx] {
			sections = append(sections, sec)
			remaps = append(remaps, remapsByObj[sourceIdx])
		}
	}
	return sections, remaps, globalSortKeys
}

// TestIteratorWithStreamRemap_MergesAcrossObjects merges two schema-sorted
// objects with independent stream-ID spaces and overlapping labels, and verifies
// the output carries global stream IDs and is globally ordered by
// [sortKey ASC, globalStreamID ASC, timestamp DESC].
func TestIteratorWithStreamRemap_MergesAcrossObjects(t *testing.T) {
	ctx := context.Background()
	sortSchema := []string{"label:app"}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	ent := func(base time.Time, n int) []push.Entry {
		out := make([]push.Entry, 0, n)
		for i := range n {
			out = append(out, push.Entry{Timestamp: base.Add(time.Duration(i) * time.Second).UTC(), Line: "x"})
		}
		return out
	}

	// Append labels out of sort order (b before a) so remapping must reorder.
	objA, closeA := buildSchemaObject(t, sortSchema, map[string][]push.Entry{
		`{app="b"}`: ent(t0, 2),
		`{app="a"}`: ent(t0, 2),
	})
	defer closeA()
	objB, closeB := buildSchemaObject(t, sortSchema, map[string][]push.Entry{
		`{app="a"}`: ent(t0.Add(time.Hour), 2),
		`{app="c"}`: ent(t0.Add(time.Hour), 2),
	})
	defer closeB()

	sections, remaps, globalSortKeys := planGlobalStreams(ctx, t, []*dataobj.Object{objA, objB}, sortSchema)

	iter, err := sortmerge.IteratorWithStreamRemap(ctx, sections, remaps, globalSortKeys, sortSchema)
	require.NoError(t, err)

	var (
		count int
		prev  logs.Record
		first = true
	)
	for res := range iter {
		rec, err := res.Value()
		require.NoError(t, err)

		// StreamID must be a valid global ID, and SortKey must be annotated from it.
		require.Greater(t, rec.StreamID, int64(0))
		require.Less(t, int(rec.StreamID), len(globalSortKeys))
		require.Equal(t, globalSortKeys[rec.StreamID], rec.SortKey)

		if !first {
			require.LessOrEqual(t, recOrder(prev, rec), 0,
				"output must be globally sorted by [sortKey, globalStreamID, ts DESC]")
		}
		prev, first = rec, false
		count++
	}
	// objA (2 streams x 2) + objB (2 streams x 2) = 8 records; no dedup.
	require.Equal(t, 8, count)
}

// recOrder compares records by [sortKey ASC, streamID ASC, timestamp DESC].
func recOrder(a, b logs.Record) int {
	if r := cmp.Compare(a.SortKey, b.SortKey); r != 0 {
		return r
	}
	if r := cmp.Compare(a.StreamID, b.StreamID); r != 0 {
		return r
	}
	return cmp.Compare(b.Timestamp.UnixNano(), a.Timestamp.UnixNano())
}
