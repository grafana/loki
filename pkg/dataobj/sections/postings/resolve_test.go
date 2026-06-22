package postings_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/memory"
)

// streamIDs expands a SectionResult's stream bitmap to ascending stream IDs.
func streamIDs(r postings.SectionResult) []int64 {
	bmap := memory.BitmapFrom(r.StreamBitmap, len(r.StreamBitmap)*8, 0)
	var ids []int64
	for id := range bmap.IterValues(true) {
		ids = append(ids, int64(id))
	}
	return ids
}

func TestStreamResolver_ZeroMatchers(t *testing.T) {
	r := postings.NewStreamResolver(nil, nil, time.Unix(0, 0), time.Unix(0, 100))
	res, err := r.Resolve(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, res)
}

func TestStreamResolver_SingleSectionLabelMatch(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "loki", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	m := labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")
	r := postings.NewStreamResolver([]*labels.Matcher{m}, nil, time.Unix(0, 0), time.Unix(0, 1000))
	res, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, "obj-a", res[0].ObjectPath)
	require.Equal(t, int64(0), res[0].SectionIndex)
	require.Equal(t, []int64{1}, streamIDs(res[0]))
}

func TestStreamResolver_MatcherANDViaBitmap(t *testing.T) {
	ctx := context.Background()
	// distributor is on {0,1}, dev is on {0,1}: the AND resolves to {0,1}.
	// worker is on {2} only, so it shares no stream with dev.
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "job", value: "worker", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "job", value: "distributor", streamID: 0, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "job", value: "distributor", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "namespace", value: "dev", streamID: 0, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "namespace", value: "dev", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := postings.NewStreamResolver([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "job", "distributor"),
		labels.MustNewMatcher(labels.MatchEqual, "namespace", "dev"),
	}, nil, time.Unix(0, 0), time.Unix(0, 1000))

	res, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.ElementsMatch(t, []int64{0, 1}, streamIDs(res[0]))
}

func TestStreamResolver_CrossStreamNoFalsePositive(t *testing.T) {
	ctx := context.Background()
	// job=worker only on stream 2; namespace=dev only on streams 0,1.
	// {job="worker", namespace="dev"} must match NOTHING.
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "job", value: "worker", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "namespace", value: "dev", streamID: 0, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "namespace", value: "dev", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := postings.NewStreamResolver([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "job", "worker"),
		labels.MustNewMatcher(labels.MatchEqual, "namespace", "dev"),
	}, nil, time.Unix(0, 0), time.Unix(0, 1000))

	res, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Empty(t, res)
}

func TestStreamResolver_TimePruning(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()
	r := postings.NewStreamResolver(
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")},
		nil, time.Unix(0, 100), time.Unix(0, 200), // no overlap with [10,20]
	)
	res, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Empty(t, res)
}

func TestStreamResolver_MissingLabelSemantics(t *testing.T) {
	ctx := context.Background()
	// stream 1 has app=nginx (no "team" label); team!="bar" must match it.
	// app="nginx" is the positive matcher that seeds the result.
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()
	r := postings.NewStreamResolver(
		[]*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "app", "nginx"),
			labels.MustNewMatcher(labels.MatchNotEqual, "team", "bar"),
		},
		nil, time.Unix(0, 0), time.Unix(0, 1000),
	)
	res, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, []int64{1}, streamIDs(res[0]))
}

func TestStreamResolver_MissingLabelViaAndNot(t *testing.T) {
	ctx := context.Background()
	// app=loki on {0,1}; stream 2 carries job=worker. job="" matches streams
	// lacking job, so app=loki AND job="" resolves to {0,1}.
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 0, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "loki", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "job", value: "worker", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := postings.NewStreamResolver([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "loki"),
		labels.MustNewMatcher(labels.MatchEqual, "job", ""),
	}, nil, time.Unix(0, 0), time.Unix(0, 1000))

	res, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.ElementsMatch(t, []int64{0, 1}, streamIDs(res[0]))
}

func TestStreamResolver_SectionWideTimestampEnvelope(t *testing.T) {
	ctx := context.Background()
	// The fixture records each observation at minTs, so the section envelope
	// spans the earliest and latest observation timestamps across all rows.
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 0, obj: "obj-a", section: 0, minTs: 100, maxTs: 100},
		{name: "app", value: "loki", streamID: 1, obj: "obj-a", section: 0, minTs: 400, maxTs: 400},
	}, nil)
	defer closer()
	r := postings.NewStreamResolver(
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "loki")},
		nil, time.Unix(0, 0), time.Unix(0, 1000),
	)
	res, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, int64(100), res[0].MinTimestamp)
	require.Equal(t, int64(400), res[0].MaxTimestamp)
}

func TestStreamResolver_OrderIndependent(t *testing.T) {
	ctx := context.Background()
	secsA, closeA := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closeA()
	secsB, closeB := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "nginx", streamID: 2, obj: "obj-b", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closeB()
	m := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")}

	r1 := postings.NewStreamResolver(m, nil, time.Unix(0, 0), time.Unix(0, 1000))
	res1, err := r1.Resolve(ctx, append(append([]*postings.Section{}, secsA...), secsB...))
	require.NoError(t, err)
	r2 := postings.NewStreamResolver(m, nil, time.Unix(0, 0), time.Unix(0, 1000))
	res2, err := r2.Resolve(ctx, append(append([]*postings.Section{}, secsB...), secsA...))
	require.NoError(t, err)

	require.ElementsMatch(t, normalize(res1), normalize(res2))
}

// normalize reduces results to a comparable "object:streamIDs" form so two
// resolutions can be compared independent of section ordering.
func normalize(results []postings.SectionResult) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		ids := streamIDs(r)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		out = append(out, fmt.Sprintf("%s:%v", r.ObjectPath, ids))
	}
	return out
}

func TestStreamResolver_SectionAmbiguousNames(t *testing.T) {
	ctx := context.Background()
	// Both streams match app=web. Stream 1 also carries label "trace_id"
	// (colliding with the structured-metadata predicate name).
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "web", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "web", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "trace_id", value: "x", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	ms := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "web")}
	preds := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "trace_id", "x")}
	r := postings.NewStreamResolver(ms, preds, time.Unix(0, 0), time.Unix(0, 1000))
	res, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.ElementsMatch(t, []int64{1, 2}, streamIDs(res[0]))
	require.ElementsMatch(t, []string{"trace_id"}, res[0].AmbiguousNames)
}

func TestStreamResolver_BloomFilters(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildResolveTestSection(t,
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
	rHit := postings.NewStreamResolver(ms,
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "trace_id", "abc")},
		time.Unix(0, 0), time.Unix(0, 1000))
	res, err := rHit.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)

	// Predicate absent from bloom -> section dropped.
	rMiss := postings.NewStreamResolver(ms,
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "trace_id", "zzz")},
		time.Unix(0, 0), time.Unix(0, 1000))
	res, err = rMiss.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Empty(t, res)
}

func TestStreamResolver_StreamLabelPredicateDropped(t *testing.T) {
	ctx := context.Background()
	// "app" is a stream label. A predicate on "app" must be dropped before
	// bloom matching, not treated as a structured-metadata bloom predicate.
	secs, closer := buildResolveTestSection(t,
		[]labelPosting{
			{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		},
		nil,
	)
	defer closer()
	ms := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")}
	r := postings.NewStreamResolver(ms,
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")},
		time.Unix(0, 0), time.Unix(0, 1000))
	res, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
}

func TestStreamResolver_DuplicateEqualPredicateNames(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildResolveTestSection(t,
		[]labelPosting{{name: "app", value: "loki", streamID: 0, obj: "obj-a", section: 0, minTs: 10, maxTs: 20}},
		[]bloomPosting{{columnName: "pod", values: []string{"a"}, streamID: 0, obj: "obj-a", section: 0}},
	)
	defer closer()
	r := postings.NewStreamResolver(
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "loki")},
		[]*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "pod", "a"),
			labels.MustNewMatcher(labels.MatchEqual, "pod", "b"),
		},
		time.Unix(0, 0), time.Unix(0, 1000),
	)
	res, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Empty(t, res) // pod=a AND pod=b -> bloom gate drops section (pod=b absent)
}

func TestStreamResolver_PerObjectStreamIDReuse(t *testing.T) {
	ctx := context.Background()
	// Two sections in DIFFERENT objects both use stream ID 5, but only obj-b's
	// stream 5 satisfies the query.
	secsA, closeA := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "other", streamID: 5, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closeA()
	secsB, closeB := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 5, obj: "obj-b", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closeB()

	r := postings.NewStreamResolver(
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "loki")},
		nil, time.Unix(0, 0), time.Unix(0, 1000),
	)
	res, err := r.Resolve(ctx, append(append([]*postings.Section{}, secsA...), secsB...))
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, "obj-b", res[0].ObjectPath)
	require.Equal(t, []int64{5}, streamIDs(res[0]))
}

// TestStreamResolver_InterleavedLogicalSections guards the production shape where
// one physical postings section holds rows for many logical (object, section)
// pairs. Stream ID 5 in obj-a/section 0 and stream ID 5 in obj-b/section 1 are
// distinct streams and must not be conflated; each logical section yields its
// own result.
func TestStreamResolver_InterleavedLogicalSections(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 5, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "loki", streamID: 5, obj: "obj-b", section: 1, minTs: 10, maxTs: 20},
		{name: "app", value: "loki", streamID: 7, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "other", streamID: 9, obj: "obj-b", section: 1, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := postings.NewStreamResolver(
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "loki")},
		nil, time.Unix(0, 0), time.Unix(0, 1000),
	)
	res, err := r.Resolve(ctx, secs)
	require.NoError(t, err)

	byKey := map[string][]int64{}
	for _, sr := range res {
		byKey[fmt.Sprintf("%s/%d", sr.ObjectPath, sr.SectionIndex)] = streamIDs(sr)
	}
	require.Len(t, byKey, 2)
	require.ElementsMatch(t, []int64{5, 7}, byKey["obj-a/0"])
	require.Equal(t, []int64{5}, byKey["obj-b/1"])
}

// TestStreamResolver_SecondMatcherOnlyKeyDropped guards that a logical section
// first seen by a later positive matcher (never seeded by the first) is dropped
// rather than crashing on a nil running result. obj-a/0 has both app and team;
// obj-b/1 has only team, so the app matcher never seeds it.
func TestStreamResolver_SecondMatcherOnlyKeyDropped(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "team", value: "x", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "team", value: "x", streamID: 2, obj: "obj-b", section: 1, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := postings.NewStreamResolver([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "loki"),
		labels.MustNewMatcher(labels.MatchEqual, "team", "x"),
	}, nil, time.Unix(0, 0), time.Unix(0, 1000))

	res, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, "obj-a", res[0].ObjectPath)
	require.Equal(t, int64(0), res[0].SectionIndex)
	require.Equal(t, []int64{1}, streamIDs(res[0]))
}

// TestStreamResolver_MatchAllRegexpDoesNotPanic guards that a `.*` regex matcher,
// which LogQL admits as a positive (everything-matching) matcher, is resolved
// rather than rejected as empty-capable-only.
func TestStreamResolver_MatchAllRegexpDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "nginx", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := postings.NewStreamResolver(
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "app", ".*")},
		nil, time.Unix(0, 0), time.Unix(0, 1000),
	)
	res, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.ElementsMatch(t, []int64{1, 2}, streamIDs(res[0]))
}

// TestStreamResolver_NoPositiveMatcherErrors guards that a query carrying only
// empty-capable matchers (which LogQL rejects before the resolver) returns an
// error rather than silently dropping streams.
func TestStreamResolver_NoPositiveMatcherErrors(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "loki", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	r := postings.NewStreamResolver(
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchNotEqual, "team", "bar")},
		nil, time.Unix(0, 0), time.Unix(0, 1000),
	)
	_, err := r.Resolve(ctx, secs)
	require.Error(t, err)
}
