package postings_test

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestCompileMatcher_PreservesMatcherAndCompilesRegex(t *testing.T) {
	eq := labels.MustNewMatcher(labels.MatchEqual, "env", "prod")
	cm, err := postings.CompileMatcher(eq)
	require.NoError(t, err)
	require.Equal(t, eq, cm.Matcher())

	re := labels.MustNewMatcher(labels.MatchRegexp, "env", "pr.*")
	cm, err = postings.CompileMatcher(re)
	require.NoError(t, err)
	require.Equal(t, re, cm.Matcher())
}

func TestScanner_MatchLabelRows(t *testing.T) {
	ctx := t.Context()

	t.Run("returns only the rows whose value matches the matcher", func(t *testing.T) {
		secs, closer := buildLabelBloomSection(t, []labelPosting{
			{name: "env", value: "prod", streamID: 1, obj: "/o", section: 0, minTs: 10, maxTs: 20},
			{name: "env", value: "dev", streamID: 2, obj: "/o", section: 0, minTs: 30, maxTs: 40},
		}, nil)
		defer closer()

		cms := compileAllT(t, labels.MustNewMatcher(labels.MatchEqual, "env", "prod"))

		rows := matchLabelRows(ctx, t, secs, cms)
		require.Equal(t, []rowSummary{{matcher: 0, matches: true, streams: []int{1}}}, summarizeRows(rows))
	})

	t.Run("attributes each row to the matcher it satisfies", func(t *testing.T) {
		secs, closer := buildLabelBloomSection(t, []labelPosting{
			{name: "app", value: "loki", streamID: 1, obj: "/o", section: 0, minTs: 10, maxTs: 20},
			{name: "app", value: "mimir", streamID: 2, obj: "/o", section: 0, minTs: 10, maxTs: 20},
			{name: "env", value: "prod", streamID: 1, obj: "/o", section: 0, minTs: 10, maxTs: 20},
			{name: "env", value: "dev", streamID: 3, obj: "/o", section: 0, minTs: 10, maxTs: 20},
			{name: "tier", value: "gold", streamID: 9, obj: "/o", section: 0, minTs: 10, maxTs: 20},
		}, nil)
		defer closer()

		cms := compileAllT(t,
			labels.MustNewMatcher(labels.MatchEqual, "app", "loki"),
			labels.MustNewMatcher(labels.MatchRegexp, "env", "pro.*"),
			labels.MustNewMatcher(labels.MatchEqual, "tier", "silver"), // no row has this value
		)

		rows := matchLabelRows(ctx, t, secs, cms)
		require.ElementsMatch(t, []rowSummary{
			{matcher: 0, matches: true, streams: []int{1}},
			{matcher: 1, matches: true, streams: []int{1}},
		}, summarizeRows(rows))
	})

	t.Run("does not attribute a row to a matcher with the same name that it does not satisfy", func(t *testing.T) {
		secs, closer := buildLabelBloomSection(t, []labelPosting{
			{name: "app", value: "loki", streamID: 1, obj: "/o", section: 0, minTs: 10, maxTs: 20},
			{name: "app", value: "mimir", streamID: 2, obj: "/o", section: 0, minTs: 10, maxTs: 20},
		}, nil)
		defer closer()

		cms := compileAllT(t,
			labels.MustNewMatcher(labels.MatchRegexp, "app", "lo.*"),
			labels.MustNewMatcher(labels.MatchEqual, "app", "mimir"),
		)

		rows := matchLabelRows(ctx, t, secs, cms)
		require.ElementsMatch(t, []rowSummary{
			{matcher: 0, matches: true, streams: []int{1}},
			{matcher: 1, matches: true, streams: []int{2}},
		}, summarizeRows(rows))
	})
}

func TestScanner_LabelNameRows(t *testing.T) {
	ctx := t.Context()

	t.Run("attributes each row to the matchers with its name", func(t *testing.T) {
		secs, closer := buildLabelBloomSection(t, []labelPosting{
			{name: "app", value: "loki", streamID: 1, obj: "/o", section: 0, minTs: 10, maxTs: 20},
			{name: "app", value: "mimir", streamID: 2, obj: "/o", section: 0, minTs: 10, maxTs: 20},
			{name: "env", value: "prod", streamID: 1, obj: "/o", section: 0, minTs: 10, maxTs: 20},
			{name: "env", value: "dev", streamID: 3, obj: "/o", section: 0, minTs: 10, maxTs: 20},
		}, nil)
		defer closer()

		cms := compileAllT(t,
			labels.MustNewMatcher(labels.MatchEqual, "app", "loki"),
			labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
		)

		rows := labelNameRows(ctx, t, secs, cms)
		require.ElementsMatch(t, []rowSummary{
			{matcher: 0, matches: true, streams: []int{1}},
			{matcher: 0, matches: false, streams: []int{2}},
			{matcher: 1, matches: true, streams: []int{1}},
			{matcher: 1, matches: false, streams: []int{3}},
		}, summarizeRows(rows))
	})
}

func compileAllT(t *testing.T, matchers ...*labels.Matcher) []postings.CompiledMatcher {
	t.Helper()
	cms := make([]postings.CompiledMatcher, len(matchers))
	for i, m := range matchers {
		cm, err := postings.CompileMatcher(m)
		require.NoError(t, err)
		cms[i] = cm
	}
	return cms
}

// scannerFactory returns a function that opens the readers of a section for the given matchers,
// filters and predicates and returns a scanner over them.
func scannerFactory(ctx context.Context, t *testing.T, matchers []postings.CompiledMatcher, filters []postings.CompiledMatcher, predicates []*labels.Matcher) func(*postings.Section) *postings.Scanner {
	t.Helper()
	return func(sec *postings.Section) *postings.Scanner {
		readers, err := postings.NewScannerReaders(sec, matchers, filters, predicates, nil, nil, nil)
		require.NoError(t, err)
		require.NoError(t, readers.Open(ctx))
		t.Cleanup(func() { require.NoError(t, readers.Close()) })
		return postings.NewScanner(sec, readers)
	}
}

// bitmapIDs expands a bitmap's set bits to ascending ints for assertions.
func bitmapIDs(b *memory.Bitmap) []int {
	if b == nil {
		return nil
	}
	var out []int
	for id := range b.IterValues(true) {
		out = append(out, id)
	}
	return out
}

func TestScanner_MatcherHits(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t,
		[]labelPosting{{name: "env", value: "prod", streamID: 1, obj: "/o", section: 0, minTs: 1, maxTs: 1}},
		[]bloomPosting{{columnName: "trace_id", values: []string{"abc"}, streamID: 1, obj: "/o", section: 0}},
	)
	defer closer()

	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "trace_id", "abc")}
	ref := postings.SectionRef{ObjectPath: "/o", SectionIndex: 0}

	var hitFound bool
	for _, sec := range secs {
		hits, _, err := scannerFactory(ctx, t, nil, nil, matchers)(sec).MatcherHits(ctx, matchers)
		require.NoError(t, err)
		if _, ok := hits[ref][postings.PredicateValue{Name: "trace_id", Value: "abc"}]; ok {
			hitFound = true
		}
	}
	require.True(t, hitFound, "bloom for trace_id=abc must test positive")
}

// TestScanner_MatcherHits_MultiBloomSinglePass verifies the single OR-of-ANDs
// scan over 3+ distinct-named matchers attributes each hit to its own
// (name,value) via the row's ColumnName, isolates branches (no cross-name
// bleed), and excludes a matcher whose value is absent from the bloom.
func TestScanner_MatcherHits_MultiBloomSinglePass(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, nil, []bloomPosting{
		{columnName: "trace_id", values: []string{"abc"}, streamID: 1, obj: "/o", section: 0},
		{columnName: "span_id", values: []string{"xyz"}, streamID: 1, obj: "/o", section: 0},
		{columnName: "user_id", values: []string{"u1"}, streamID: 1, obj: "/o", section: 0},
		{columnName: "region", values: []string{"eu"}, streamID: 1, obj: "/o", section: 0},
	})
	defer closer()

	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "trace_id", "abc"), // present
		labels.MustNewMatcher(labels.MatchEqual, "span_id", "xyz"),  // present
		labels.MustNewMatcher(labels.MatchEqual, "user_id", "u1"),   // present
		labels.MustNewMatcher(labels.MatchEqual, "region", "us"),    // name present, value absent
		labels.MustNewMatcher(labels.MatchEqual, "absent", "x"),     // name absent entirely
	}
	ref := postings.SectionRef{ObjectPath: "/o", SectionIndex: 0}

	got := make(map[postings.PredicateValue]struct{})
	for _, sec := range secs {
		hits, _, err := scannerFactory(ctx, t, nil, nil, matchers)(sec).MatcherHits(ctx, matchers)
		require.NoError(t, err)
		for pv := range hits[ref] {
			got[pv] = struct{}{}
		}
	}
	require.Equal(t, map[postings.PredicateValue]struct{}{
		{Name: "trace_id", Value: "abc"}: {},
		{Name: "span_id", Value: "xyz"}:  {},
		{Name: "user_id", Value: "u1"}:   {},
	}, got, "only present (name,value) pairs attribute; wrong-value and absent-name matchers are excluded")
}

// TestScanner_MatcherHits_AttributesByValueNotName verifies the OR scan keys
// hits on the exact value, not just the column name: a section holding the
// right column but the wrong value yields no hit for that matcher.
func TestScanner_MatcherHits_AttributesByValueNotName(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, nil, []bloomPosting{
		{columnName: "app", values: []string{"loki"}, streamID: 1, obj: "/o", section: 0},
	})
	defer closer()

	ref := postings.SectionRef{ObjectPath: "/o", SectionIndex: 0}
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "mimir")}

	for _, sec := range secs {
		hits, _, err := scannerFactory(ctx, t, nil, nil, matchers)(sec).MatcherHits(ctx, matchers)
		require.NoError(t, err)
		_, hit := hits[ref][postings.PredicateValue{Name: "app", Value: "mimir"}]
		require.False(t, hit, "matching column name but wrong value must not attribute a hit")
	}
}

// TestScanner_MatcherHits_DuplicateNameRejected verifies the single-pass scan
// guards its distinct-name invariant rather than silently mis-attributing.
func TestScanner_MatcherHits_DuplicateNameRejected(t *testing.T) {
	secs, closer := buildLabelBloomSection(t, nil, []bloomPosting{
		{columnName: "app", values: []string{"foo"}, streamID: 1, obj: "/o", section: 0},
	})
	defer closer()

	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "app", "bar"),
	}
	for _, sec := range secs {
		_, err := postings.NewScannerReaders(sec, nil, nil, matchers, nil, nil, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "duplicate equal-predicate name")
	}
}

// TestScanner_MatcherHits_LabelNamesSinglePass verifies the consolidated
// label-name scan resolves every matcher's name that appears as a stream label,
// across multiple names, in one pass.
func TestScanner_MatcherHits_LabelNamesSinglePass(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "env", value: "prod", streamID: 1, obj: "/o", section: 0, minTs: 1, maxTs: 1},
		{name: "app", value: "loki", streamID: 1, obj: "/o", section: 0, minTs: 1, maxTs: 1},
	}, nil)
	defer closer()

	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
		labels.MustNewMatcher(labels.MatchEqual, "app", "loki"),
		labels.MustNewMatcher(labels.MatchEqual, "absent", "x"),
	}
	ref := postings.SectionRef{ObjectPath: "/o", SectionIndex: 0}

	got := make(map[string]struct{})
	for _, sec := range secs {
		_, ambiguous, err := scannerFactory(ctx, t, nil, nil, matchers)(sec).MatcherHits(ctx, matchers)
		require.NoError(t, err)
		for name := range ambiguous[ref] {
			got[name] = struct{}{}
		}
	}
	require.Equal(t, map[string]struct{}{"env": {}, "app": {}}, got,
		"both present label names resolve in one pass; absent name does not appear")
}

func TestScanner_MatcherHits_PredicateTypes(t *testing.T) {
	ctx := context.Background()
	ref := postings.SectionRef{ObjectPath: "/o", SectionIndex: 0}
	secs, closer := buildLabelBloomSection(t,
		[]labelPosting{{name: "provider", value: "idology", streamID: 1, obj: "/o", section: 0, minTs: 1, maxTs: 1}},
		[]bloomPosting{{columnName: "trace_id", values: []string{"abc"}, streamID: 1, obj: "/o", section: 0}},
	)
	defer closer()

	collect := func(t *testing.T, predicates []*labels.Matcher) (map[postings.PredicateValue]struct{}, map[string]struct{}) {
		t.Helper()
		bloom := make(map[postings.PredicateValue]struct{})
		names := make(map[string]struct{})
		for _, sec := range secs {
			hits, ambiguous, err := scannerFactory(ctx, t, nil, nil, predicates)(sec).MatcherHits(ctx, predicates)
			require.NoError(t, err)
			for pv := range hits[ref] {
				bloom[pv] = struct{}{}
			}
			for name := range ambiguous[ref] {
				names[name] = struct{}{}
			}
		}
		return bloom, names
	}

	t.Run("reports stream label names and no bloom hits when every predicate is non-equality", func(t *testing.T) {
		bloom, names := collect(t, []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchRegexp, "provider", "idol.*"),
			labels.MustNewMatcher(labels.MatchNotEqual, "trace_id", "zzz"),
		})
		require.Empty(t, bloom)
		require.Equal(t, map[string]struct{}{"provider": {}}, names)
	})

	t.Run("does not attribute a bloom hit to a regex predicate sharing a bloom column name", func(t *testing.T) {
		bloom, _ := collect(t, []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "trace_id", "abc")})
		require.Empty(t, bloom)
	})

	t.Run("reports bloom hits for equality predicates and label names for all predicates when types are mixed", func(t *testing.T) {
		bloom, names := collect(t, []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "trace_id", "abc"),
			labels.MustNewMatcher(labels.MatchNotRegexp, "provider", "oth.*"),
		})
		require.Equal(t, map[postings.PredicateValue]struct{}{{Name: "trace_id", Value: "abc"}: {}}, bloom)
		require.Equal(t, map[string]struct{}{"provider": {}}, names)
	})

	t.Run("accepts predicates of different types that share a name", func(t *testing.T) {
		for _, sec := range secs {
			_, err := postings.NewScannerReaders(sec, nil, nil, []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "trace_id", "abc"),
				labels.MustNewMatcher(labels.MatchRegexp, "trace_id", "a.*"),
			}, nil, nil, nil)
			require.NoError(t, err)
		}
	})

	t.Run("accepts two non-equality predicates that share a name", func(t *testing.T) {
		for _, sec := range secs {
			_, err := postings.NewScannerReaders(sec, nil, nil, []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "provider", "a.*"),
				labels.MustNewMatcher(labels.MatchNotEqual, "provider", "b"),
			}, nil, nil, nil)
			require.NoError(t, err)
		}
	})
}

type labelPosting struct {
	name, value  string
	streamID     int64
	obj          string
	section      int64
	minTs, maxTs int64 // unix nanos
	shardBucket  uint32
}

type bloomPosting struct {
	columnName string
	values     []string
	streamID   int64
	obj        string
	section    int64
}

// buildLabelBloomSection builds the opened postings section(s) for an object
// holding the given explicit label and bloom postings. The builder emits bloom
// rows and label rows into separate sections, so every postings section is
// returned for the scan. bloomsIn may be nil for label-only fixtures.
//
// When a label posting's maxTs exceeds its minTs, a second observation is
// recorded at maxTs so the posting's aggregated timestamp envelope spans
// [minTs, maxTs] rather than collapsing to a single point.
func buildLabelBloomSection(t *testing.T, labelsIn []labelPosting, bloomsIn []bloomPosting) ([]*postings.Section, func()) {
	t.Helper()
	ctx := context.Background()
	b := postings.NewBuilder(nil, 0, 0, 1<<20)

	for _, lp := range labelsIn {
		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: lp.obj, SectionIndex: lp.section, ColumnName: lp.name, LabelValue: lp.value,
			StreamID: lp.streamID, UncompressedSize: 0, ShardBucket: lp.shardBucket,
			Time: postings.At(time.Unix(0, lp.minTs).UTC()).Extend(time.Unix(0, lp.maxTs).UTC()),
		})
	}
	for _, bp := range bloomsIn {
		b.PrepareBloomColumn(bp.obj, bp.section, bp.columnName, 1000, 0)
		for _, v := range bp.values {
			require.NoError(t, b.ObserveBloomPosting(postings.BloomObservation{
				ObjectPath: bp.obj, SectionIndex: bp.section, ColumnName: bp.columnName,
				Value: v, StreamID: bp.streamID, Timestamp: time.Unix(0, 0).UTC(),
			}))
		}
	}

	return openPostingsSections(ctx, t, b)
}

// rowSummary is the part of a postings.LabelRow that the tests compare.
type rowSummary struct {
	matcher int
	matches bool
	streams []int
}

func summarizeRows(rows []postings.LabelRow) []rowSummary {
	out := make([]rowSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowSummary{matcher: row.Matcher, matches: row.Matches, streams: streamIDsOf(row)})
	}
	return out
}

func streamIDsOf(row postings.LabelRow) []int {
	bitmap := memory.BitmapFrom(row.StreamIDBitmap, len(row.StreamIDBitmap)*8, 0)
	return bitmapIDs(&bitmap)
}

// matchLabelRows scans every section with the matchers as value matchers.
func matchLabelRows(ctx context.Context, t *testing.T, secs []*postings.Section, cms []postings.CompiledMatcher) []postings.LabelRow {
	t.Helper()
	var rows []postings.LabelRow
	for _, sec := range secs {
		got, err := scannerFactory(ctx, t, cms, nil, nil)(sec).MatchLabelRows(ctx, cms)
		require.NoError(t, err)
		rows = append(rows, got...)
	}
	return rows
}

// labelNameRows scans every section with the matchers as filter matchers.
func labelNameRows(ctx context.Context, t *testing.T, secs []*postings.Section, cms []postings.CompiledMatcher) []postings.LabelRow {
	t.Helper()
	var rows []postings.LabelRow
	for _, sec := range secs {
		got, err := scannerFactory(ctx, t, nil, cms, nil)(sec).LabelNameRows(ctx, cms)
		require.NoError(t, err)
		rows = append(rows, got...)
	}
	return rows
}
