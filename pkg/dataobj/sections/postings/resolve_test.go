package postings_test

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

func TestStreamResolver_ZeroMatchers(t *testing.T) {
	r := postings.NewStreamResolver(nil, nil, time.Unix(0, 0), time.Unix(0, 100))
	refs, err := r.Resolve(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, refs)
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
	refs, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	require.Equal(t, int64(1), refs[0].StreamID)
	require.Equal(t, "obj-a", refs[0].ObjectPath)
	require.Equal(t, int64(0), refs[0].SectionIndex)
}

func TestStreamResolver_MatcherANDAcrossSections(t *testing.T) {
	ctx := context.Background()
	// stream 1 has app=nginx in object A and env=prod in object B.
	secsA, closeA := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closeA()
	secsB, closeB := buildResolveTestSection(t, []labelPosting{
		{name: "env", value: "prod", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closeB()
	ms := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "nginx"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
	}
	r := postings.NewStreamResolver(ms, nil, time.Unix(0, 0), time.Unix(0, 1000))
	refs, err := r.Resolve(ctx, append(append([]*postings.Section{}, secsA...), secsB...))
	require.NoError(t, err)
	require.Len(t, refs, 1)
	require.Equal(t, int64(1), refs[0].StreamID)
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
	refs, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Empty(t, refs)
}

func TestStreamResolver_MissingLabelSemantics(t *testing.T) {
	ctx := context.Background()
	// stream 1 has app=nginx (no "team" label); team!="bar" must match it
	// (missing label is treated as the empty value).
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()
	ms := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchNotEqual, "team", "bar"),
	}
	r := postings.NewStreamResolver(ms, nil, time.Unix(0, 0), time.Unix(0, 1000))
	refs, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	require.Equal(t, int64(1), refs[0].StreamID)
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
	refs1, err := r1.Resolve(ctx, append(append([]*postings.Section{}, secsA...), secsB...))
	require.NoError(t, err)
	r2 := postings.NewStreamResolver(m, nil, time.Unix(0, 0), time.Unix(0, 1000))
	refs2, err := r2.Resolve(ctx, append(append([]*postings.Section{}, secsB...), secsA...))
	require.NoError(t, err)

	require.ElementsMatch(t, refs1, refs2)
}

// TestStreamResolver_PerStreamAmbiguousLabels guards the spec's Critical
// requirement: a section shared by two streams where only one stream carries a
// predicate-colliding label yields different AmbiguousLabels per ref.
func TestStreamResolver_PerStreamAmbiguousLabels(t *testing.T) {
	ctx := context.Background()
	// Both streams match app=web. Stream 1 also has label "trace_id" (colliding
	// with the structured-metadata predicate name); stream 2 does not.
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "web", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "web", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "trace_id", value: "x", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	ms := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "web")}
	preds := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "trace_id", "x")}
	r := postings.NewStreamResolver(ms, preds, time.Unix(0, 0), time.Unix(0, 1000))
	refs, err := r.Resolve(ctx, secs)
	require.NoError(t, err)

	byStream := map[int64][]string{}
	for _, ref := range refs {
		byStream[ref.StreamID] = ref.AmbiguousLabels
	}
	// trace_id is a stream label only on stream 1, so only stream 1's ref
	// carries it as ambiguous.
	require.Equal(t, []string{"trace_id"}, byStream[1])
	require.Empty(t, byStream[2])
}
