package metastore

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/memory"
)

// streamIDs expands a SectionStreams' stream bitmap to ascending stream IDs.
func streamIDs(r SectionStreams) []int64 {
	bmap := memory.BitmapFrom(r.StreamBitmap, len(r.StreamBitmap)*8, 0)
	var ids []int64
	for id := range bmap.IterValues(true) {
		ids = append(ids, int64(id))
	}
	return ids
}

func openAndSelectStreams(ctx context.Context, t *testing.T, selector *streamSelector, sections []*postings.Section) ([]SectionStreams, error) {
	t.Helper()
	if err := selector.open(ctx, sections, maxConcurrentSectionOpens); err != nil {
		return nil, err
	}
	t.Cleanup(func() { require.NoError(t, selector.close()) })
	return selector.selectStreams(ctx)
}

func TestStreamSelector_ZeroMatchers(t *testing.T) {
	r := newStreamSelector(nil, nil, time.Unix(0, 0), time.Unix(0, 100), nil)
	res, err := openAndSelectStreams(context.Background(), t, r, nil)
	require.NoError(t, err)
	require.Empty(t, res)
}

func TestStreamSelector_SingleSectionLabelMatch(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "loki", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	m := labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")
	r := newStreamSelector([]*labels.Matcher{m}, nil, time.Unix(0, 0), time.Unix(0, 1000), nil)
	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, "obj-a", res[0].Section.ObjectPath)
	require.Equal(t, int64(0), res[0].Section.SectionIndex)
	require.Equal(t, []int64{1}, streamIDs(res[0]))
}

func TestStreamSelector_MatcherANDViaBitmap(t *testing.T) {
	ctx := context.Background()
	// distributor is on {0,1}, dev is on {0,1}: the AND resolves to {0,1}.
	// worker is on {2} only, so it shares no stream with dev.
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "job", value: "worker", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "job", value: "distributor", streamID: 0, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "job", value: "distributor", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "namespace", value: "dev", streamID: 0, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "namespace", value: "dev", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := newStreamSelector([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "job", "distributor"),
		labels.MustNewMatcher(labels.MatchEqual, "namespace", "dev"),
	}, nil, time.Unix(0, 0), time.Unix(0, 1000), nil)

	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.ElementsMatch(t, []int64{0, 1}, streamIDs(res[0]))
}

// TestStreamSelector_ANDAcrossPhysicalSections guards that an AND of two matchers
// resolves a stream whose matched label rows live in different physical postings
// sections of the same object. The builder splits a logical section's labels
// across physical sections by column name, so the cross-matcher intersection
// must union each matcher's hits across all physical sections before ANDing.
func TestStreamSelector_ANDAcrossPhysicalSections(t *testing.T) {
	ctx := context.Background()
	// One logical section (obj-a, 0), stream 1: app=foo lives in physical
	// section 0, env=prod in physical section 1.
	secs, closer := buildSplitPostingsSections(t,
		[]labelPosting{{name: "app", value: "foo", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20}},
		[]labelPosting{{name: "env", value: "prod", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20}},
	)
	defer closer()
	require.Len(t, secs, 2, "fixture must produce two physical postings sections")

	r := newStreamSelector([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
	}, nil, time.Unix(0, 0), time.Unix(0, 1000), nil)

	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)
	require.Len(t, res, 1, "stream matches both labels only when sections are combined")
	require.Equal(t, "obj-a", res[0].Section.ObjectPath)
	require.Equal(t, int64(0), res[0].Section.SectionIndex)
	require.Equal(t, []int64{1}, streamIDs(res[0]))
}

func TestStreamSelector_CrossStreamNoFalsePositive(t *testing.T) {
	ctx := context.Background()
	// job=worker only on stream 2; namespace=dev only on streams 0,1.
	// {job="worker", namespace="dev"} must match NOTHING.
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "job", value: "worker", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "namespace", value: "dev", streamID: 0, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "namespace", value: "dev", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := newStreamSelector([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "job", "worker"),
		labels.MustNewMatcher(labels.MatchEqual, "namespace", "dev"),
	}, nil, time.Unix(0, 0), time.Unix(0, 1000), nil)

	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)
	require.Empty(t, res)
}

func TestStreamSelector_TimePruning(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()
	r := newStreamSelector(
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")},
		nil, time.Unix(0, 100), time.Unix(0, 200), // no overlap with [10,20]
		nil,
	)
	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)
	require.Empty(t, res)
}

func TestStreamSelector_MissingLabelSemantics(t *testing.T) {
	ctx := context.Background()
	// stream 1 has app=nginx (no "team" label); team!="bar" must match it.
	// app="nginx" is the value-selecting matcher that seeds the result.
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()
	r := newStreamSelector(
		[]*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "app", "nginx"),
			labels.MustNewMatcher(labels.MatchNotEqual, "team", "bar"),
		},
		nil, time.Unix(0, 0), time.Unix(0, 1000),
		nil,
	)
	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, []int64{1}, streamIDs(res[0]))
}

func TestStreamSelector_MissingLabelViaAndNot(t *testing.T) {
	ctx := context.Background()
	// app=loki on {0,1}; stream 2 carries job=worker. job="" matches streams
	// lacking job, so app=loki AND job="" resolves to {0,1}.
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 0, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "loki", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "job", value: "worker", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := newStreamSelector([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "loki"),
		labels.MustNewMatcher(labels.MatchEqual, "job", ""),
	}, nil, time.Unix(0, 0), time.Unix(0, 1000), nil)

	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.ElementsMatch(t, []int64{0, 1}, streamIDs(res[0]))
}

func TestStreamSelector_SectionWideTimestampEnvelope(t *testing.T) {
	ctx := context.Background()
	// The fixture records each observation at minTs, so the section envelope
	// spans the earliest and latest observation timestamps across all rows.
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 0, obj: "obj-a", section: 0, minTs: 100, maxTs: 100},
		{name: "app", value: "loki", streamID: 1, obj: "obj-a", section: 0, minTs: 400, maxTs: 400},
	}, nil)
	defer closer()
	r := newStreamSelector(
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "loki")},
		nil, time.Unix(0, 0), time.Unix(0, 1000),
		nil,
	)
	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, int64(100), res[0].MinTimestamp)
	require.Equal(t, int64(400), res[0].MaxTimestamp)
}

func TestStreamSelector_OrderIndependent(t *testing.T) {
	ctx := context.Background()
	secsA, closeA := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closeA()
	secsB, closeB := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "nginx", streamID: 2, obj: "obj-b", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closeB()
	m := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")}

	r1 := newStreamSelector(m, nil, time.Unix(0, 0), time.Unix(0, 1000), nil)
	res1, err := openAndSelectStreams(ctx, t, r1, append(append([]*postings.Section{}, secsA...), secsB...))
	require.NoError(t, err)
	r2 := newStreamSelector(m, nil, time.Unix(0, 0), time.Unix(0, 1000), nil)
	res2, err := openAndSelectStreams(ctx, t, r2, append(append([]*postings.Section{}, secsB...), secsA...))
	require.NoError(t, err)

	require.ElementsMatch(t, normalize(res1), normalize(res2))
}

// normalize reduces results to a comparable "object:streamIDs" form so two
// resolutions can be compared independent of section ordering.
func normalize(results []SectionStreams) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		ids := streamIDs(r)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		out = append(out, fmt.Sprintf("%s:%v", r.Section.ObjectPath, ids))
	}
	return out
}

func TestStreamSelector_SectionAmbiguousNames(t *testing.T) {
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "web", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "provider", value: "idology", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()
	ms := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "web")}

	tests := []struct {
		name  string
		preds []*labels.Matcher
		want  []string
	}{
		{
			name:  "reports a stream label named by an equality predicate",
			preds: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "provider", "idology")},
			want:  []string{"provider"},
		},
		{
			name:  "reports a stream label named by a regex predicate",
			preds: []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "provider", "idol.*")},
			want:  []string{"provider"},
		},
		{
			name:  "reports a stream label named by a not-equal predicate",
			preds: []*labels.Matcher{labels.MustNewMatcher(labels.MatchNotEqual, "provider", "other")},
			want:  []string{"provider"},
		},
		{
			name: "reports a name once when several predicates share it",
			preds: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "provider", "idol.*"),
				labels.MustNewMatcher(labels.MatchNotRegexp, "provider", "oth.*"),
			},
			want: []string{"provider"},
		},
		{
			name: "reports a name once when equality and regex predicates share it",
			preds: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "provider", "idology"),
				labels.MustNewMatcher(labels.MatchRegexp, "provider", "idol.*"),
			},
			want: []string{"provider"},
		},
		{
			name:  "ignores nil predicates",
			preds: []*labels.Matcher{nil, labels.MustNewMatcher(labels.MatchRegexp, "provider", "idol.*")},
			want:  []string{"provider"},
		},
		{
			name:  "reports nothing for a name that is not a stream label",
			preds: []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "trace_id", "a.*")},
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newStreamSelector(ms, tt.preds, time.Unix(0, 0), time.Unix(0, 1000), nil)
			res, err := openAndSelectStreams(context.Background(), t, r, secs)
			require.NoError(t, err)
			require.Len(t, res, 1)
			require.ElementsMatch(t, tt.want, res[0].AmbiguousNames)
		})
	}
}

func TestStreamSelector_NonEqualityPredicatesAndAdmission(t *testing.T) {
	secs, closer := buildLabelBloomSection(t,
		[]labelPosting{
			{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		},
		[]bloomPosting{
			{columnName: "trace_id", values: []string{"abc"}, streamID: 1, obj: "obj-a", section: 0},
		},
	)
	defer closer()
	ms := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")}
	regex := labels.MustNewMatcher(labels.MatchRegexp, "trace_id", "a.*")

	tests := []struct {
		name  string
		preds []*labels.Matcher
		want  int
	}{
		{
			name:  "keeps the section when only a regex predicate names a label that is not a stream label",
			preds: []*labels.Matcher{regex},
			want:  1,
		},
		{
			name:  "keeps the section when the equality predicate hits the bloom and a regex predicate is present",
			preds: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "trace_id", "abc"), labels.MustNewMatcher(labels.MatchRegexp, "app", "ng.*")},
			want:  1,
		},
		{
			name:  "drops the section when the equality predicate misses the bloom and a regex predicate is present",
			preds: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "trace_id", "zzz"), labels.MustNewMatcher(labels.MatchRegexp, "app", "ng.*")},
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newStreamSelector(ms, tt.preds, time.Unix(0, 0), time.Unix(0, 1000), nil)
			res, err := openAndSelectStreams(context.Background(), t, r, secs)
			require.NoError(t, err)
			require.Len(t, res, tt.want)
		})
	}
}

func TestStreamSelector_BloomFilters(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t,
		[]labelPosting{
			{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		},
		[]bloomPosting{
			{columnName: "trace_id", values: []string{"abc"}, streamID: 1, obj: "obj-a", section: 0},
		},
	)
	defer closer()
	ms := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")}

	// Predicate present in bloom -> section kept.
	rHit := newStreamSelector(ms,
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "trace_id", "abc")},
		time.Unix(0, 0), time.Unix(0, 1000), nil)
	res, err := openAndSelectStreams(ctx, t, rHit, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)

	// Predicate absent from bloom -> section dropped.
	rMiss := newStreamSelector(ms,
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "trace_id", "zzz")},
		time.Unix(0, 0), time.Unix(0, 1000), nil)
	res, err = openAndSelectStreams(ctx, t, rMiss, secs)
	require.NoError(t, err)
	require.Empty(t, res)
}

func TestStreamSelector_StreamLabelPredicateDropped(t *testing.T) {
	ctx := context.Background()
	// "app" is a stream label. A predicate on "app" must be dropped before
	// bloom matching, not treated as a structured-metadata bloom predicate.
	secs, closer := buildLabelBloomSection(t,
		[]labelPosting{
			{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		},
		nil,
	)
	defer closer()
	ms := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")}
	r := newStreamSelector(ms,
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")},
		time.Unix(0, 0), time.Unix(0, 1000), nil)
	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
}

func TestStreamSelector_PerObjectStreamIDReuse(t *testing.T) {
	ctx := context.Background()
	// Two sections in DIFFERENT objects both use stream ID 5, but only obj-b's
	// stream 5 satisfies the query.
	secsA, closeA := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "other", streamID: 5, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closeA()
	secsB, closeB := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 5, obj: "obj-b", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closeB()

	r := newStreamSelector(
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "loki")},
		nil, time.Unix(0, 0), time.Unix(0, 1000),
		nil,
	)
	res, err := openAndSelectStreams(ctx, t, r, append(append([]*postings.Section{}, secsA...), secsB...))
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, "obj-b", res[0].Section.ObjectPath)
	require.Equal(t, []int64{5}, streamIDs(res[0]))
}

// TestStreamSelector_InterleavedLogicalSections guards the production shape where
// one physical postings section holds rows for many logical (object, section)
// pairs. Stream ID 5 in obj-a/section 0 and stream ID 5 in obj-b/section 1 are
// distinct streams and must not be conflated; each logical section yields its
// own result.
func TestStreamSelector_InterleavedLogicalSections(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 5, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "loki", streamID: 5, obj: "obj-b", section: 1, minTs: 10, maxTs: 20},
		{name: "app", value: "loki", streamID: 7, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "other", streamID: 9, obj: "obj-b", section: 1, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := newStreamSelector(
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "loki")},
		nil, time.Unix(0, 0), time.Unix(0, 1000),
		nil,
	)
	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)

	byKey := map[string][]int64{}
	for _, sr := range res {
		byKey[fmt.Sprintf("%s/%d", sr.Section.ObjectPath, sr.Section.SectionIndex)] = streamIDs(sr)
	}
	require.Len(t, byKey, 2)
	require.ElementsMatch(t, []int64{5, 7}, byKey["obj-a/0"])
	require.Equal(t, []int64{5}, byKey["obj-b/1"])
}

// TestStreamSelector_SecondMatcherOnlyKeyDropped guards that a logical section
// first seen by a later value-selecting matcher (never seeded by the first) is
// dropped rather than crashing on a nil running result. obj-a/0 has both app and team;
// obj-b/1 has only team, so the app matcher never seeds it.
func TestStreamSelector_SecondMatcherOnlyKeyDropped(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "team", value: "x", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "team", value: "x", streamID: 2, obj: "obj-b", section: 1, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := newStreamSelector([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "loki"),
		labels.MustNewMatcher(labels.MatchEqual, "team", "x"),
	}, nil, time.Unix(0, 0), time.Unix(0, 1000), nil)

	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, "obj-a", res[0].Section.ObjectPath)
	require.Equal(t, int64(0), res[0].Section.SectionIndex)
	require.Equal(t, []int64{1}, streamIDs(res[0]))
}

// TestStreamSelector_MatchAllRegexpDoesNotPanic guards that a `.*` regex matcher,
// which LogQL admits as a value-selecting (everything-matching) matcher, is
// resolved rather than rejected as filter-only.
func TestStreamSelector_MatchAllRegexpDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "nginx", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := newStreamSelector(
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "app", ".*")},
		nil, time.Unix(0, 0), time.Unix(0, 1000),
		nil,
	)
	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.ElementsMatch(t, []int64{1, 2}, streamIDs(res[0]))
}

// TestStreamSelector_OnlyFiltersErrors guards that a query carrying only filters
// (which LogQL rejects before stream selection) returns an error rather than
// silently dropping streams.
func TestStreamSelector_OnlyFiltersErrors(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := newStreamSelector(
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchNotEqual, "team", "bar")},
		nil, time.Unix(0, 0), time.Unix(0, 1000),
		nil,
	)
	_, err := openAndSelectStreams(ctx, t, r, secs)
	require.Error(t, err)
}

func TestStreamSelector_TimePruningConservativeEnvelope(t *testing.T) {
	ctx := context.Background()
	// Two rows for the same section: one inside the window, one outside. The
	// section envelope [10,1000] overlaps [900,1100], so both streams survive
	// the pre-filter. This is the intended conservative behavior: timeOverlap is
	// a sound pre-filter, exact timestamp pruning happens downstream.
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "loki", streamID: 2, obj: "obj-a", section: 0, minTs: 1000, maxTs: 1000},
	}, nil)
	defer closer()

	r := newStreamSelector(
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "loki")},
		nil, time.Unix(0, 900), time.Unix(0, 1100),
		nil,
	)
	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.ElementsMatch(t, []int64{1, 2}, streamIDs(res[0]))
}

// TestStreamSelector_MatchNotRegexp guards the filter negative-regexp
// path: env!~"prod.*" matches streams whose env fails the regex AND streams
// lacking env entirely. app=web seeds {1,2,3}; stream 1 has env=production
// (dropped), stream 2 has env=dev (kept), stream 3 lacks env (kept).
func TestStreamSelector_MatchNotRegexp(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		{name: "app", value: "web", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "web", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "web", streamID: 3, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "env", value: "production", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "env", value: "dev", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := newStreamSelector([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "web"),
		labels.MustNewMatcher(labels.MatchNotRegexp, "env", "prod.*"),
	}, nil, time.Unix(0, 0), time.Unix(0, 1000), nil)

	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.ElementsMatch(t, []int64{2, 3}, streamIDs(res[0]))
}

// TestStreamSelector_LogicalSectionAcrossPhysicalSections guards that one
// logical section whose label rows are split across several physical postings
// sections is read as a single section: each matcher's hits must union across
// every physical section before the per-section result is assembled. app=web
// lives in physical section 0, env=prod in section 1, region=us in section 2;
// stream 1 satisfies all three only when the physical sections are combined.
func TestStreamSelector_LogicalSectionAcrossPhysicalSections(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildSplitPostingsSections(t,
		[]labelPosting{
			{name: "app", value: "web", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
			{name: "app", value: "web", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		},
		[]labelPosting{
			{name: "env", value: "prod", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
			{name: "env", value: "prod", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		},
		[]labelPosting{
			{name: "region", value: "us", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		},
	)
	defer closer()
	require.Len(t, secs, 3, "fixture must produce three physical postings sections")

	r := newStreamSelector([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "web"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
		labels.MustNewMatcher(labels.MatchEqual, "region", "us"),
	}, nil, time.Unix(0, 0), time.Unix(0, 1000), nil)

	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)
	require.Len(t, res, 1, "the logical section resolves to one result across physical sections")
	require.Equal(t, "obj-a", res[0].Section.ObjectPath)
	require.Equal(t, int64(0), res[0].Section.SectionIndex)
	require.Equal(t, []int64{1}, streamIDs(res[0]))
}

// TestStreamSelector_MixedMatcherTypes guards an AND that mixes every matcher
// type in a single query: value-selecting MatchEqual/MatchRegexp seed the result,
// filter MatchNotEqual/MatchNotRegexp prune it. Only stream 1 satisfies
// app=web AND job=~"api.*" AND team!="sre" AND region!~"eu.*".
func TestStreamSelector_MixedMatcherTypes(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildLabelBloomSection(t, []labelPosting{
		// stream 1: matches all four.
		{name: "app", value: "web", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "job", value: "api-server", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "team", value: "platform", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "region", value: "us-east", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		// stream 2: team=sre fails team!="sre".
		{name: "app", value: "web", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "job", value: "api-worker", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "team", value: "sre", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		// stream 3: region=eu-west fails region!~"eu.*".
		{name: "app", value: "web", streamID: 3, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "job", value: "api-gateway", streamID: 3, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "region", value: "eu-west", streamID: 3, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		// stream 4: job=cron fails job=~"api.*".
		{name: "app", value: "web", streamID: 4, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "job", value: "cron", streamID: 4, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := newStreamSelector([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "web"),
		labels.MustNewMatcher(labels.MatchRegexp, "job", "api.*"),
		labels.MustNewMatcher(labels.MatchNotEqual, "team", "sre"),
		labels.MustNewMatcher(labels.MatchNotRegexp, "region", "eu.*"),
	}, nil, time.Unix(0, 0), time.Unix(0, 1000), nil)

	res, err := openAndSelectStreams(ctx, t, r, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, []int64{1}, streamIDs(res[0]))
}

// BenchmarkStreamSelector_SelectStreams measures selectStreams over a section
// carrying many streams, each with several labels, matched by a mix of
// value-selecting matchers and filters. Section construction is excluded from the timed
// loop so the benchmark isolates selection cost.
func BenchmarkStreamSelector_SelectStreams(b *testing.B) {
	ctx := context.Background()
	const nStreams = 1000
	names := []string{"app", "job", "namespace", "tenant"}
	values := []string{"loki", "worker", "distributor", "dev", "prod"}

	var lps []labelPosting
	for id := int64(0); id < nStreams; id++ {
		lps = append(lps, labelPosting{
			name: "svc", value: values[id%int64(len(values))],
			streamID: id, obj: "obj-0", section: 0, minTs: 10, maxTs: 1000,
		})
		for i, name := range names {
			if (id+int64(i))%3 != 0 {
				continue
			}
			lps = append(lps, labelPosting{
				name: name, value: values[(id+int64(i))%int64(len(values))],
				streamID: id, obj: "obj-0", section: 0, minTs: 10, maxTs: 1000,
			})
		}
	}

	secs, closer := buildLabelBloomSection(b, lps, nil)
	defer closer()

	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "svc", "loki"),
		labels.MustNewMatcher(labels.MatchRegexp, "app", "lo.*"),
		labels.MustNewMatcher(labels.MatchNotEqual, "namespace", "dev"),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := newStreamSelector(matchers, nil, time.Unix(0, 0), time.Unix(0, 2000), nil)
		if err := r.open(ctx, secs, maxConcurrentSectionOpens); err != nil {
			b.Fatal(err)
		}
		if _, err := r.selectStreams(ctx); err != nil {
			b.Fatal(err)
		}
		if err := r.close(); err != nil {
			b.Fatal(err)
		}
	}
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
// returned for the selector to scan. bloomsIn may be nil for label-only fixtures.
//
// When a label posting's maxTs exceeds its minTs, a second observation is
// recorded at maxTs so the posting's aggregated timestamp envelope spans
// [minTs, maxTs] rather than collapsing to a single point.
func buildLabelBloomSection(t testing.TB, labelsIn []labelPosting, bloomsIn []bloomPosting) ([]*postings.Section, func()) {
	t.Helper()
	ctx := context.Background()
	b := postings.NewBuilder(nil, 0, 0, 1<<20)

	for _, lp := range labelsIn {
		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: lp.obj, SectionIndex: lp.section, ColumnName: lp.name, LabelValue: lp.value,
			StreamID: lp.streamID, Timestamp: time.Unix(0, lp.minTs).UTC(), UncompressedSize: 0, ShardBucket: lp.shardBucket,
		})
		if lp.maxTs > lp.minTs {
			b.ObserveLabelPosting(postings.LabelObservation{
				ObjectPath: lp.obj, SectionIndex: lp.section, ColumnName: lp.name, LabelValue: lp.value,
				StreamID: lp.streamID, Timestamp: time.Unix(0, lp.maxTs).UTC(), UncompressedSize: 0, ShardBucket: lp.shardBucket,
			})
		}
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

// openPostingsSections flushes the builders into a single object, each builder
// producing its own physical postings section, and opens every postings section
// the object contains.
func openPostingsSections(ctx context.Context, t testing.TB, builders ...*postings.Builder) ([]*postings.Section, func()) {
	t.Helper()
	objBuilder := dataobj.NewBuilder(nil)
	for _, b := range builders {
		require.NoError(t, objBuilder.Append(b))
	}
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)

	var secs []*postings.Section
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		sec, err := postings.Open(ctx, s)
		require.NoError(t, err)
		secs = append(secs, sec)
	}
	require.NotEmpty(t, secs, "no postings section in object")
	return secs, func() { _ = closer.Close() }
}

// buildSplitPostingsSections builds one object with len(perSection) physical
// postings sections, the i-th holding perSection[i]'s label postings. It lets a
// test place labels for the same logical (object, section) into different
// physical postings sections.
func buildSplitPostingsSections(t testing.TB, perSection ...[]labelPosting) ([]*postings.Section, func()) {
	t.Helper()
	builders := make([]*postings.Builder, 0, len(perSection))
	for _, lps := range perSection {
		b := postings.NewBuilder(nil, 0, 0, 1<<20)
		for _, lp := range lps {
			b.ObserveLabelPosting(postings.LabelObservation{
				ObjectPath: lp.obj, SectionIndex: lp.section, ColumnName: lp.name, LabelValue: lp.value,
				StreamID: lp.streamID, Timestamp: time.Unix(0, lp.minTs).UTC(), UncompressedSize: 0, ShardBucket: lp.shardBucket,
			})
		}
		builders = append(builders, b)
	}
	return openPostingsSections(context.Background(), t, builders...)
}

func TestStreamSelector_ShardBucketRange(t *testing.T) {
	ctx := t.Context()

	// bucketedStream is one stream of the fixture, with the shard bucket that the index records for it.
	type bucketedStream struct {
		id      int64
		section int64
		bucket  uint32
		labels  [][2]string

		// outsideWindow is true when every line of the stream lies outside the query window.
		outsideWindow bool
	}

	// bucketedStreams spreads ten streams over two logs sections and the buckets 0 to 31. Section 0
	// holds the buckets 0 to 15 and section 1 holds the buckets 18 to 31. Streams 4 and 8 have no
	// line inside the query window.
	bucketedStreams := []bucketedStream{
		{id: 1, section: 0, bucket: 0, labels: [][2]string{{"app", "a"}, {"env", "prod"}}},
		{id: 2, section: 0, bucket: 3, labels: [][2]string{{"app", "a"}, {"env", "dev"}, {"team", "x"}}},
		{id: 3, section: 0, bucket: 7, labels: [][2]string{{"app", "b"}, {"env", "prod"}}},
		{id: 4, section: 0, bucket: 12, labels: [][2]string{{"app", "b"}, {"env", "dev"}}, outsideWindow: true},
		{id: 5, section: 0, bucket: 15, labels: [][2]string{{"app", "c"}, {"env", "prod"}, {"team", "y"}}},
		{id: 6, section: 1, bucket: 18, labels: [][2]string{{"app", "c"}, {"env", "dev"}}},
		{id: 7, section: 1, bucket: 21, labels: [][2]string{{"app", "a"}, {"env", "prod"}, {"team", "x"}}},
		{id: 8, section: 1, bucket: 24, labels: [][2]string{{"app", "b"}, {"env", "dev"}, {"team", "y"}}, outsideWindow: true},
		{id: 9, section: 1, bucket: 27, labels: [][2]string{{"app", "c"}, {"env", "prod"}}},
		{id: 10, section: 1, bucket: 31, labels: [][2]string{{"app", "a"}, {"env", "dev"}}},
	}

	bucketedPostings := func() []labelPosting {
		var out []labelPosting
		for _, stream := range bucketedStreams {
			minTs, maxTs := int64(10), int64(20)
			if stream.outsideWindow {
				minTs, maxTs = 5000, 6000
			}
			for _, label := range stream.labels {
				out = append(out, labelPosting{
					name: label[0], value: label[1], streamID: stream.id,
					obj: "obj-a", section: stream.section, minTs: minTs, maxTs: maxTs, shardBucket: stream.bucket,
				})
			}
		}
		return out
	}

	bucketOf := make(map[int64]uint32, len(bucketedStreams))
	for _, stream := range bucketedStreams {
		bucketOf[stream.id] = stream.bucket
	}

	// selected returns the streams the selector resolves, by logs section. A section with no
	// stream is absent.
	selected := func(t *testing.T, matchers []*labels.Matcher, bucketRange *postings.ShardBucketRange) map[int64][]int64 {
		t.Helper()

		secs, closer := buildLabelBloomSection(t, bucketedPostings(), nil)
		defer closer()

		selector := newStreamSelector(matchers, nil, time.Unix(0, 0), time.Unix(0, 1000), bucketRange)
		results, err := openAndSelectStreams(ctx, t, selector, secs)
		require.NoError(t, err)

		out := map[int64][]int64{}
		for _, result := range results {
			out[result.Section.SectionIndex] = streamIDs(result)
		}
		return out
	}

	// inRange returns the IDs of the streams inside the range. Stream IDs are unique in the fixture.
	inRange := func(sections map[int64][]int64, bucketRange postings.ShardBucketRange) map[int64]struct{} {
		out := map[int64]struct{}{}
		for _, ids := range sections {
			for _, id := range ids {
				if bucket := bucketOf[id]; bucket >= bucketRange.From && bucket <= bucketRange.To {
					out[id] = struct{}{}
				}
			}
		}
		return out
	}

	// expected returns the IDs of the streams that pass three tests. Their labels match every
	// matcher. They have a line inside the query window. They lie inside the range. It reads the
	// fixture, not a selector, so it is independent of the code under test.
	expected := func(matchers []*labels.Matcher, bucketRange postings.ShardBucketRange) map[int64]struct{} {
		out := map[int64]struct{}{}
		for _, stream := range bucketedStreams {
			if stream.outsideWindow || stream.bucket < bucketRange.From || stream.bucket > bucketRange.To {
				continue
			}
			var pairs []string
			for _, label := range stream.labels {
				pairs = append(pairs, label[0], label[1])
			}
			streamLabels := labels.FromStrings(pairs...)

			matches := true
			for _, matcher := range matchers {
				matches = matches && matcher.Matches(streamLabels.Get(matcher.Name))
			}
			if matches {
				out[stream.id] = struct{}{}
			}
		}
		return out
	}

	ranges := []postings.ShardBucketRange{
		{From: 0, To: 0},
		{From: 3, To: 3},
		{From: 13, To: 14},
		{From: 7, To: 12},
		{From: 0, To: 15},
		{From: 16, To: 31},
		{From: 5, To: 20},
		{From: 31, To: 31},
		{From: 0, To: 31},
	}

	matcherSets := []struct {
		name     string
		matchers []*labels.Matcher
	}{
		{
			name:     "an equality matcher",
			matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "env", "prod")},
		},
		{
			name:     "a regular expression matcher",
			matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "app", "a|b")},
		},
		{
			name: "two equality matchers",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "a"),
				labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
			},
		},
		{
			name: "a not-equal matcher",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "app", ".+"),
				labels.MustNewMatcher(labels.MatchNotEqual, "env", "prod"),
			},
		},
		{
			name: "a not-regular-expression matcher",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "app", ".+"),
				labels.MustNewMatcher(labels.MatchNotRegexp, "env", "pr.*"),
			},
		},
		{
			name: "a matcher for a missing label",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "app", ".+"),
				labels.MustNewMatcher(labels.MatchEqual, "team", ""),
			},
		},
		{
			name: "a matcher for a present label",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "app", ".+"),
				labels.MustNewMatcher(labels.MatchNotEqual, "team", ""),
			},
		},
		{
			name: "a mix of positive and negative matchers",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "app", ".+"),
				labels.MustNewMatcher(labels.MatchRegexp, "team", "x|y"),
				labels.MustNewMatcher(labels.MatchNotEqual, "env", "dev"),
			},
		},
	}

	for _, test := range matcherSets {
		t.Run(fmt.Sprintf("%s resolves every matching stream inside every bucket range, and no stream inside it that the unpruned selection lacks", test.name), func(t *testing.T) {
			everything := postings.ShardBucketRange{From: 0, To: streams.ShardFactor - 1}
			require.NotEmpty(t, expected(test.matchers, everything), "the matchers must select a stream, or the comparison proves nothing")

			unpruned := selected(t, test.matchers, nil)
			for _, bucketRange := range ranges {
				pruned := inRange(selected(t, test.matchers, &bucketRange), bucketRange)

				require.Subset(t, pruned, expected(test.matchers, bucketRange), "a matching stream is missing for buckets %d to %d", bucketRange.From, bucketRange.To)
				require.Subset(t, inRange(unpruned, bucketRange), pruned, "a stream the unpruned selection lacks appears for buckets %d to %d", bucketRange.From, bucketRange.To)
			}
		})
	}

	t.Run("a range of bucket 0 resolves section 0 with every env=prod stream of its row, and not section 1", func(t *testing.T) {
		matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "env", "prod")}

		got := selected(t, matchers, &postings.ShardBucketRange{From: 0, To: 0})

		require.Equal(t, map[int64][]int64{0: {1, 3, 5}}, got)
	})

	t.Run("a range that keeps only the row of a stream with no line in the window resolves no stream", func(t *testing.T) {
		secs, closer := buildLabelBloomSection(t, []labelPosting{
			{name: "app", value: "a", streamID: 1, obj: "obj-a", section: 0, minTs: 5000, maxTs: 6000, shardBucket: 0},
			{name: "app", value: "b", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20, shardBucket: 20},
		}, nil)
		defer closer()
		matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "app", "a|b")}

		unpruned, err := openAndSelectStreams(ctx, t, newStreamSelector(matchers, nil, time.Unix(0, 0), time.Unix(0, 1000), nil), secs)
		require.NoError(t, err)
		require.Len(t, unpruned, 1)
		require.Equal(t, []int64{1, 2}, streamIDs(unpruned[0]), "the unpruned selection keeps the stream, because the envelope of the matcher overlaps the window")

		pruned, err := openAndSelectStreams(ctx, t, newStreamSelector(matchers, nil, time.Unix(0, 0), time.Unix(0, 1000), &postings.ShardBucketRange{From: 0, To: 0}), secs)
		require.NoError(t, err)
		require.Empty(t, pruned)
	})

	t.Run("a range prunes every physical postings section of an index object", func(t *testing.T) {
		build := func() ([]*postings.Section, func()) {
			return buildSplitPostingsSections(t,
				[]labelPosting{{name: "app", value: "foo", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20, shardBucket: 3}},
				[]labelPosting{{name: "app", value: "foo", streamID: 1, obj: "obj-a", section: 1, minTs: 10, maxTs: 20, shardBucket: 20}},
			)
		}
		matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

		sectionsFor := func(bucketRange *postings.ShardBucketRange) []int64 {
			secs, closer := build()
			defer closer()
			results, err := openAndSelectStreams(ctx, t, newStreamSelector(matchers, nil, time.Unix(0, 0), time.Unix(0, 1000), bucketRange), secs)
			require.NoError(t, err)

			var out []int64
			for _, result := range results {
				out = append(out, result.Section.SectionIndex)
			}
			slices.Sort(out)
			return out
		}

		require.Equal(t, []int64{0, 1}, sectionsFor(nil))
		require.Equal(t, []int64{0}, sectionsFor(&postings.ShardBucketRange{From: 3, To: 3}))
		require.Equal(t, []int64{1}, sectionsFor(&postings.ShardBucketRange{From: 20, To: 20}))
	})

	t.Run("a metadata predicate keeps working with a range", func(t *testing.T) {
		const bucket = 3
		secs, closer := buildLabelBloomSection(t,
			[]labelPosting{
				{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20, shardBucket: bucket},
				{name: "env", value: "prod", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20, shardBucket: bucket},
			},
			[]bloomPosting{{columnName: "trace_id", values: []string{"abc"}, streamID: 1, obj: "obj-a", section: 0}},
		)
		defer closer()

		matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")}
		bucketRange := &postings.ShardBucketRange{From: bucket, To: bucket}
		sectionsFor := func(predicate *labels.Matcher) []SectionStreams {
			selector := newStreamSelector(matchers, []*labels.Matcher{predicate}, time.Unix(0, 0), time.Unix(0, 1000), bucketRange)
			results, err := openAndSelectStreams(ctx, t, selector, secs)
			require.NoError(t, err)
			return results
		}

		require.Len(t, sectionsFor(labels.MustNewMatcher(labels.MatchEqual, "trace_id", "abc")), 1, "a bloom hit keeps the section")
		require.Empty(t, sectionsFor(labels.MustNewMatcher(labels.MatchEqual, "trace_id", "zzz")), "a bloom miss drops the section")
		require.Len(t, sectionsFor(labels.MustNewMatcher(labels.MatchEqual, "env", "prod")), 1, "a predicate on a stream label keeps the section")
	})

	t.Run("a range outside every row resolves no section", func(t *testing.T) {
		matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "env", "prod")}

		got := selected(t, matchers, &postings.ShardBucketRange{From: 16, To: 17})

		require.Empty(t, got)
	})
}
