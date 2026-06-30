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

func TestScanner_MatchLabel_PushesValuePredicate(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "env", value: "prod", streamID: 1, obj: "/o", section: 0, minTs: 10, maxTs: 20},
		{name: "env", value: "dev", streamID: 2, obj: "/o", section: 0, minTs: 30, maxTs: 40},
	}, nil)
	defer closer()

	cm, err := postings.CompileMatcher(labels.MustNewMatcher(labels.MatchEqual, "env", "prod"))
	require.NoError(t, err)

	got := scanAll(t, secs, func(sc *postings.Scanner) (map[postings.SectionRef][]postings.MatchedStreams, error) {
		return sc.MatchLabels(ctx, nil, []postings.CompiledMatcher{cm})
	})
	require.Len(t, got, 1)

	ref := postings.SectionRef{ObjectPath: "/o", SectionIndex: 0}
	perMatcher, ok := got[ref]
	require.True(t, ok)
	require.Len(t, perMatcher, 1)
	ms := perMatcher[0]

	require.Equal(t, []int{1}, bitmapIDs(ms.Matched), "only stream 1 (env=prod) is returned")
	require.True(t, ms.Has)
	require.Equal(t, int64(10), ms.MinNS)
	require.Equal(t, int64(20), ms.MaxNS, "envelope spans only the matching row")
}

func TestScanner_LabelStreams_PresentAndMatched(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "env", value: "prod", streamID: 1, obj: "/o", section: 0, minTs: 10, maxTs: 20},
		{name: "env", value: "dev", streamID: 2, obj: "/o", section: 0, minTs: 30, maxTs: 40},
	}, nil)
	defer closer()

	cm, err := postings.CompileMatcher(labels.MustNewMatcher(labels.MatchEqual, "env", "prod"))
	require.NoError(t, err)

	got := scanAll(t, secs, func(sc *postings.Scanner) (map[postings.SectionRef][]postings.LabelStreams, error) {
		return sc.LabelStreams(ctx, nil, []postings.CompiledMatcher{cm})
	})
	require.Len(t, got, 1)

	ref := postings.SectionRef{ObjectPath: "/o", SectionIndex: 0}
	perMatcher, ok := got[ref]
	require.True(t, ok)
	require.Len(t, perMatcher, 1)
	ls := perMatcher[0]

	require.Equal(t, []int{1, 2}, bitmapIDs(ls.Present), "both streams carry env (name-only scan)")
	require.Equal(t, []int{1}, bitmapIDs(ls.Matched), "only stream 1 has env=prod")
	require.True(t, ls.Has)
	require.Equal(t, int64(10), ls.MinNS)
	require.Equal(t, int64(40), ls.MaxNS, "envelope spans every row for the name")
}

// TestScanner_LabelStreams_UnionZeroExtends covers accumulating rows whose
// stream bitmaps differ in byte length. Stream 70 lives beyond the first byte,
// so its bitmap is longer than stream 1's; the union must zero-extend the
// shorter operand rather than reject the length mismatch.
func TestScanner_LabelStreams_UnionZeroExtends(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "env", value: "prod", streamID: 1, obj: "/o", section: 0, minTs: 10, maxTs: 20},
		{name: "env", value: "prod", streamID: 70, obj: "/o", section: 0, minTs: 30, maxTs: 40},
	}, nil)
	defer closer()

	cm, err := postings.CompileMatcher(labels.MustNewMatcher(labels.MatchEqual, "env", "prod"))
	require.NoError(t, err)

	got := scanAll(t, secs, func(sc *postings.Scanner) (map[postings.SectionRef][]postings.LabelStreams, error) {
		return sc.LabelStreams(ctx, nil, []postings.CompiledMatcher{cm})
	})
	require.Len(t, got, 1)

	ref := postings.SectionRef{ObjectPath: "/o", SectionIndex: 0}
	perMatcher, ok := got[ref]
	require.True(t, ok)
	require.Len(t, perMatcher, 1)
	ls := perMatcher[0]

	require.Equal(t, []int{1, 70}, bitmapIDs(ls.Present), "both streams union across differing bitmap lengths")
	require.Equal(t, []int{1, 70}, bitmapIDs(ls.Matched))
}

// TestScanner_MatchLabels_MultiMatcherAttribution verifies the single OR scan
// attributes each row to the right matcher position, including a regex matcher
// (which forces the in-memory Matches re-test) and a matcher whose value is
// absent (empty result, not a misattribution).
func TestScanner_MatchLabels_MultiMatcherAttribution(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 1, obj: "/o", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "mimir", streamID: 2, obj: "/o", section: 0, minTs: 10, maxTs: 20},
		{name: "env", value: "prod", streamID: 1, obj: "/o", section: 0, minTs: 10, maxTs: 20},
		{name: "env", value: "dev", streamID: 3, obj: "/o", section: 0, minTs: 10, maxTs: 20},
		{name: "tier", value: "gold", streamID: 9, obj: "/o", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	cms := compileAllT(t,
		labels.MustNewMatcher(labels.MatchEqual, "app", "loki"),    // -> stream 1
		labels.MustNewMatcher(labels.MatchRegexp, "env", "pro.*"),  // regex -> stream 1
		labels.MustNewMatcher(labels.MatchEqual, "tier", "silver"), // value absent -> empty
	)

	got := scanAll(t, secs, func(sc *postings.Scanner) (map[postings.SectionRef][]postings.MatchedStreams, error) {
		return sc.MatchLabels(ctx, nil, cms)
	})
	require.Len(t, got, 1)

	ref := postings.SectionRef{ObjectPath: "/o", SectionIndex: 0}
	perMatcher := got[ref]
	require.Len(t, perMatcher, 3)

	require.Equal(t, []int{1}, bitmapIDs(perMatcher[0].Matched), "app=loki -> stream 1 only")
	require.Equal(t, []int{1}, bitmapIDs(perMatcher[1].Matched), "env=~pro.* -> stream 1 only (regex re-test)")
	require.Nil(t, perMatcher[2].Matched, "tier=silver absent -> no matched streams")
}

// TestScanner_MatchLabels_SharedNameDisambiguation guards the in-memory Matches
// re-test: two matchers on the same name (legal for differing matcher types)
// produce OR branches that share name==app. A row must attribute only to the
// matcher whose value it actually satisfies, not to every same-name matcher.
func TestScanner_MatchLabels_SharedNameDisambiguation(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 1, obj: "/o", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "mimir", streamID: 2, obj: "/o", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	cms := compileAllT(t,
		labels.MustNewMatcher(labels.MatchRegexp, "app", "lo.*"), // -> stream 1 (loki)
		labels.MustNewMatcher(labels.MatchEqual, "app", "mimir"), // -> stream 2
	)

	got := scanAll(t, secs, func(sc *postings.Scanner) (map[postings.SectionRef][]postings.MatchedStreams, error) {
		return sc.MatchLabels(ctx, nil, cms)
	})
	require.Len(t, got, 1)

	ref := postings.SectionRef{ObjectPath: "/o", SectionIndex: 0}
	perMatcher := got[ref]
	require.Len(t, perMatcher, 2)

	require.Equal(t, []int{1}, bitmapIDs(perMatcher[0].Matched), "app=~lo.* -> stream 1 only, not mimir's stream 2")
	require.Equal(t, []int{2}, bitmapIDs(perMatcher[1].Matched), "app=mimir -> stream 2 only, not loki's stream 1")
}

// TestScanner_LabelStreams_MultiMatcherAttribution verifies the name-only OR
// scan attributes Present (all streams with the name) and Matched (value subset)
// to the correct matcher position across multiple matchers.
func TestScanner_LabelStreams_MultiMatcherAttribution(t *testing.T) {
	ctx := context.Background()
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

	got := scanAll(t, secs, func(sc *postings.Scanner) (map[postings.SectionRef][]postings.LabelStreams, error) {
		return sc.LabelStreams(ctx, nil, cms)
	})
	require.Len(t, got, 1)

	ref := postings.SectionRef{ObjectPath: "/o", SectionIndex: 0}
	perMatcher := got[ref]
	require.Len(t, perMatcher, 2)

	require.Equal(t, []int{1, 2}, bitmapIDs(perMatcher[0].Present), "app present on streams 1,2")
	require.Equal(t, []int{1}, bitmapIDs(perMatcher[0].Matched), "app=loki on stream 1")
	require.Equal(t, []int{1, 3}, bitmapIDs(perMatcher[1].Present), "env present on streams 1,3")
	require.Equal(t, []int{1}, bitmapIDs(perMatcher[1].Matched), "env=prod on stream 1")
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

// scanAll runs scan over each section and returns the single non-empty result
// map (the builder splits label and bloom rows across sections).
func scanAll[T any](t *testing.T, secs []*postings.Section, scan func(*postings.Scanner) (map[postings.SectionRef]T, error)) map[postings.SectionRef]T {
	t.Helper()
	var got map[postings.SectionRef]T
	for _, sec := range secs {
		m, err := scan(postings.NewScanner(sec))
		require.NoError(t, err)
		if len(m) > 0 {
			got = m
		}
	}
	return got
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
		hits, _, err := postings.NewScanner(sec).MatcherHits(ctx, matchers)
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
		hits, _, err := postings.NewScanner(sec).MatcherHits(ctx, matchers)
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
		hits, _, err := postings.NewScanner(sec).MatcherHits(ctx, matchers)
		require.NoError(t, err)
		_, hit := hits[ref][postings.PredicateValue{Name: "app", Value: "mimir"}]
		require.False(t, hit, "matching column name but wrong value must not attribute a hit")
	}
}

// TestScanner_MatcherHits_DuplicateNameRejected verifies the single-pass scan
// guards its distinct-name invariant rather than silently mis-attributing.
func TestScanner_MatcherHits_DuplicateNameRejected(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, nil, []bloomPosting{
		{columnName: "app", values: []string{"foo"}, streamID: 1, obj: "/o", section: 0},
	})
	defer closer()

	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "app", "bar"),
	}
	for _, sec := range secs {
		_, _, err := postings.NewScanner(sec).MatcherHits(ctx, matchers)
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
		_, ambiguous, err := postings.NewScanner(sec).MatcherHits(ctx, matchers)
		require.NoError(t, err)
		for name := range ambiguous[ref] {
			got[name] = struct{}{}
		}
	}
	require.Equal(t, map[string]struct{}{"env": {}, "app": {}}, got,
		"both present label names resolve in one pass; absent name does not appear")
}

type labelPosting struct {
	name, value  string
	streamID     int64
	obj          string
	section      int64
	minTs, maxTs int64 // unix nanos
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
			StreamID: lp.streamID, Timestamp: time.Unix(0, lp.minTs).UTC(), UncompressedSize: 0,
		})
		if lp.maxTs > lp.minTs {
			b.ObserveLabelPosting(postings.LabelObservation{
				ObjectPath: lp.obj, SectionIndex: lp.section, ColumnName: lp.name, LabelValue: lp.value,
				StreamID: lp.streamID, Timestamp: time.Unix(0, lp.maxTs).UTC(), UncompressedSize: 0,
			})
		}
	}
	for _, bp := range bloomsIn {
		b.PrepareBloomColumn(bp.obj, bp.section, bp.columnName, 1000)
		for _, v := range bp.values {
			require.NoError(t, b.ObserveBloomPosting(postings.BloomObservation{
				ObjectPath: bp.obj, SectionIndex: bp.section, ColumnName: bp.columnName,
				Value: v, StreamID: bp.streamID, Timestamp: time.Unix(0, 0).UTC(),
			}))
		}
	}

	return openPostingsSections(ctx, t, b)
}
