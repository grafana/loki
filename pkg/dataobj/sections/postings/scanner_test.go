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

	got := scanAll(t, secs, func(sc *postings.Scanner) (map[postings.SectionRef]postings.MatchedStreams, error) {
		return sc.MatchLabel(ctx, cm)
	})
	require.Len(t, got, 1)

	ref := postings.SectionRef{ObjectPath: "/o", SectionIndex: 0}
	ms, ok := got[ref]
	require.True(t, ok)

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

	got := scanAll(t, secs, func(sc *postings.Scanner) (map[postings.SectionRef]postings.LabelStreams, error) {
		return sc.LabelStreams(ctx, cm)
	})
	require.Len(t, got, 1)

	ref := postings.SectionRef{ObjectPath: "/o", SectionIndex: 0}
	ls, ok := got[ref]
	require.True(t, ok)

	require.Equal(t, []int{1, 2}, bitmapIDs(ls.Present), "both streams carry env (name-only scan)")
	require.Equal(t, []int{1}, bitmapIDs(ls.Matched), "only stream 1 has env=prod")
	require.True(t, ls.Has)
	require.Equal(t, int64(10), ls.MinNS)
	require.Equal(t, int64(40), ls.MaxNS, "envelope spans every row for the name")
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
