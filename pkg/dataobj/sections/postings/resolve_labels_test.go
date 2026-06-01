package postings_test

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// TestResolveLabels_EqualMatchers_Single anchors the predicate-pushdown shape
// for a single Equal matcher. Fixture: three (column_name, label_value) rows
// — env=prod -> {1,2,3}, env=staging -> {4,5}, app=foo -> {2,3,6}. Querying
// env=prod must yield exactly {1,2,3} and labelNamesByStream[1..3] = ["env"].
func TestResolveLabels_EqualMatchers_Single(t *testing.T) {
	r := openLabelResolveFixture(t, []labelFixtureEntry{
		{name: "env", value: "prod", streamIDs: []int64{1, 2, 3}},
		{name: "env", value: "staging", streamIDs: []int64{4, 5}},
		{name: "app", value: "foo", streamIDs: []int64{2, 3, 6}},
	})

	got, names, err := r.ResolveLabels(t.Context(), []*labels.Matcher{
		equalMatcher(t, "env", "prod"),
	})
	require.NoError(t, err)
	require.Len(t, got, 3, "env=prod must yield exactly 3 streams")
	for _, id := range []int64{1, 2, 3} {
		_, ok := got[id]
		require.True(t, ok, "stream %d must be present in env=prod result", id)
	}
	require.NotNil(t, names, "labelNamesByStream must be populated when there is a match")
	for _, id := range []int64{1, 2, 3} {
		require.ElementsMatch(t, []string{"env"}, names[id], "labelNamesByStream[%d]", id)
	}
}

// TestResolveLabels_RegexFallback exercises the Go-side regex evaluation
// (). Same fixture as Test 1. Querying env=~"^pr.*" with no Equal
// matcher must yield {1,2,3} (the rows whose label_value matches the regex
// — only "prod" matches "^pr.*" among {prod, staging, foo, ...}).
func TestResolveLabels_RegexFallback(t *testing.T) {
	r := openLabelResolveFixture(t, []labelFixtureEntry{
		{name: "env", value: "prod", streamIDs: []int64{1, 2, 3}},
		{name: "env", value: "staging", streamIDs: []int64{4, 5}},
		{name: "app", value: "foo", streamIDs: []int64{2, 3, 6}},
	})

	got, _, err := r.ResolveLabels(t.Context(), []*labels.Matcher{
		regexMatcher(t, "env", "^pr.*"),
	})
	require.NoError(t, err)
	require.Len(t, got, 3, "regex env=~^pr.* must match exactly 3 streams (rows with env=prod)")
	for _, id := range []int64{1, 2, 3} {
		_, ok := got[id]
		require.True(t, ok, "stream %d must be present in regex result", id)
	}
}

// TestResolveLabels_LabelNamesByStream_Inversion stress-tests the inverted
// index: stream 2 appears under 3 different labels (env=prod, app=foo,
// region=us). Querying all 3 with AND must keep stream 2 in the result, and
// labelNamesByStream[2] must be the set {"env","app","region"} (order
// independent).
func TestResolveLabels_LabelNamesByStream_Inversion(t *testing.T) {
	r := openLabelResolveFixture(t, []labelFixtureEntry{
		{name: "env", value: "prod", streamIDs: []int64{1, 2}},
		{name: "app", value: "foo", streamIDs: []int64{2, 7}},
		{name: "region", value: "us", streamIDs: []int64{2, 8}},
	})

	got, names, err := r.ResolveLabels(t.Context(), []*labels.Matcher{
		equalMatcher(t, "env", "prod"),
		equalMatcher(t, "app", "foo"),
		equalMatcher(t, "region", "us"),
	})
	require.NoError(t, err)
	require.Len(t, got, 1, "only stream 2 appears under all 3 labels")
	_, ok := got[2]
	require.True(t, ok, "stream 2 must be the sole survivor of the 3-way AND")
	require.ElementsMatch(t, []string{"env", "app", "region"}, names[2],
		"labelNamesByStream[2] must contain all 3 contributing column names")
}

// TestResolveLabels_Mixed_Equal_And_Regex_DifferentNames is the // regression pin. Pre-fix, the predicate pushdown included only
// Equal-matcher Names — a mixed query like {env="prod", app=~"foo.*"}
// read only rows where column_name="env"; the regex on "app" never saw
// any rows because the predicate filtered them out, so the regex was
// silently ignored.
//
// Fixture:
// - env=prod -> {1, 2, 3}
// - app=foo -> {2, 4}
// - app=bar -> {2, 5}
//
// Query: {env="prod", app=~"^foo.*"}
//
// Correct result: streams that match BOTH env=prod AND app matching ^foo.* —
// stream 2 matches env=prod AND has app=foo (which matches ^foo.*) so
// stream 2 qualifies. Streams 1 and 3 have env=prod but no app=foo*
// entry — they must NOT appear. Stream 4 has app=foo but no env=prod —
// it must NOT appear.
func TestResolveLabels_Mixed_Equal_And_Regex_DifferentNames(t *testing.T) {
	r := openLabelResolveFixture(t, []labelFixtureEntry{
		{name: "env", value: "prod", streamIDs: []int64{1, 2, 3}},
		{name: "app", value: "foo", streamIDs: []int64{2, 4}},
		{name: "app", value: "bar", streamIDs: []int64{2, 5}},
	})

	got, names, err := r.ResolveLabels(t.Context(), []*labels.Matcher{
		equalMatcher(t, "env", "prod"),
		regexMatcher(t, "app", "^foo.*"),
	})
	require.NoError(t, err)
	require.Len(t, got, 1,
		"env=prod AND app=~^foo.* must yield exactly stream 2 — pre- fix the regex was silently dropped and 3 streams returned")
	_, ok := got[2]
	require.True(t, ok, "stream 2 (env=prod AND app=foo) must be the sole survivor")
	_, has1 := got[1]
	require.False(t, has1, "stream 1 (env=prod only, no app=foo*) must NOT appear")
	_, has3 := got[3]
	require.False(t, has3, "stream 3 (env=prod only, no app=foo*) must NOT appear")
	_, has4 := got[4]
	require.False(t, has4, "stream 4 (app=foo but no env=prod) must NOT appear")

	require.ElementsMatch(t, []string{"env", "app"}, names[2],
		"labelNamesByStream[2] must record both contributing columns")
}

// TestResolveLabels_MultiRegex_AND is the regression pin. Pre-fix,
// the regex-only path returned the UNION across regex matchers (a
// single regexUnionStreams map populated by every row that satisfied
// at least one regex). The contract is the AND-intersection.
//
// Fixture:
// - env=prod -> {1, 2}
// - env=staging -> {3}
// - app=foo -> {2, 4}
// - app=bar -> {5}
//
// Query: {env=~"^pr.*", app=~"^fo.*"}
//
// Correct result: streams matching BOTH regexes. env=prod matches ^pr.*
// — contributes {1,2}. app=foo matches ^fo.* — contributes {2,4}.
// Intersection = {2}. Pre- fix, the UNION {1,2,4} would be
// returned.
func TestResolveLabels_MultiRegex_AND(t *testing.T) {
	r := openLabelResolveFixture(t, []labelFixtureEntry{
		{name: "env", value: "prod", streamIDs: []int64{1, 2}},
		{name: "env", value: "staging", streamIDs: []int64{3}},
		{name: "app", value: "foo", streamIDs: []int64{2, 4}},
		{name: "app", value: "bar", streamIDs: []int64{5}},
	})

	got, names, err := r.ResolveLabels(t.Context(), []*labels.Matcher{
		regexMatcher(t, "env", "^pr.*"),
		regexMatcher(t, "app", "^fo.*"),
	})
	require.NoError(t, err)
	require.Len(t, got, 1,
		"env=~^pr.* AND app=~^fo.* must intersect to stream 2 — pre- fix this returned the UNION {1,2,4}")
	_, ok := got[2]
	require.True(t, ok, "stream 2 (env=prod AND app=foo) must be the sole survivor")
	_, has1 := got[1]
	require.False(t, has1, "stream 1 (env=prod only) must NOT appear in AND result")
	_, has4 := got[4]
	require.False(t, has4, "stream 4 (app=foo only) must NOT appear in AND result")
	_, has3 := got[3]
	require.False(t, has3, "stream 3 (env=staging) must NOT appear")
	_, has5 := got[5]
	require.False(t, has5, "stream 5 (app=bar) must NOT appear")

	require.ElementsMatch(t, []string{"env", "app"}, names[2],
		"labelNamesByStream[2] must record both contributing columns")
}

// TestResolveLabels_NotEqualMatcher_AcrossNames asserts the fix
// extends to NotEqual matchers (not just regex). Fixture:
// - env=prod -> {1, 2}
// - env=dev -> {3}
// - app=foo -> {2, 4}
//
// Query: {env="prod", app!="bar"}
//
// NotEqual matchers must NOT silently filter to env-only rows. Streams
// matching env=prod AND not having app=bar — the only env=prod stream
// with any app value is 2 (app=foo, which is != "bar"). Stream 1
// (env=prod, no app row) — app!="bar" is satisfied vacuously per
// label-matcher semantics: the row's label_value is the empty string,
// but Prometheus label matchers consider absent labels as matching
// label="" — env!="bar" is true for an absent label. However in the
// inverted-index world, a stream that has NO app row simply does not
// surface for app-targeted matchers — so this test specifically
// asserts the row-level evaluation drives the result.
//
// To keep the test focused on the -style mixed-name shape, we
// query {env="prod", app!="bar"} against a fixture where only stream 2
// has an app row matching app!="bar". The test asserts stream 2 is
// returned (the intersection), and pre-fix would have returned all
// env=prod streams because the app!=bar matcher was silently dropped.
func TestResolveLabels_NotEqualMatcher_AcrossNames(t *testing.T) {
	r := openLabelResolveFixture(t, []labelFixtureEntry{
		{name: "env", value: "prod", streamIDs: []int64{1, 2}},
		{name: "env", value: "dev", streamIDs: []int64{3}},
		{name: "app", value: "foo", streamIDs: []int64{2, 4}},
		{name: "app", value: "bar", streamIDs: []int64{1, 5}},
	})

	notEqual, err := labels.NewMatcher(labels.MatchNotEqual, "app", "bar")
	require.NoError(t, err)

	got, _, err := r.ResolveLabels(t.Context(), []*labels.Matcher{
		equalMatcher(t, "env", "prod"),
		notEqual,
	})
	require.NoError(t, err)
	// stream 1 has env=prod AND app=bar → app!="bar" REJECTS stream 1.
	// stream 2 has env=prod AND app=foo → app!="bar" accepts stream 2.
	// stream 4 has app=foo but no env=prod → env=prod rejects.
	// stream 5 has app=bar but no env=prod → env=prod rejects.
	require.Len(t, got, 1,
		"env=prod AND app!=bar must yield exactly stream 2 — pre- fix the NotEqual matcher was silently dropped")
	_, ok := got[2]
	require.True(t, ok, "stream 2 (env=prod AND app=foo) must be the sole survivor")
	_, has1 := got[1]
	require.False(t, has1, "stream 1 (env=prod AND app=bar) must NOT appear — app!=bar rejects it")
}

// ---------------------------------------------------------------------------
// Test helpers (postings_test scope)
// ---------------------------------------------------------------------------

// labelFixtureEntry describes one logical (column_name, label_value) row
// to insert into the postings section fixture. streamIDs are the stream IDs
// that should belong to the row's stream_id_bitmap — the helper issues one
// ObserveLabelPosting per streamID, and the label_aggregator unions them
// into a single row per (name, value) tuple.
type labelFixtureEntry struct {
	name      string
	value     string
	streamIDs []int64
}

// openLabelResolveFixture builds a single dataobj.Object containing a
// postings section with one KindLabel row per (name, value) entry, opens it
// via postings.Open (no parent back-pointer needed for ResolveLabels), and
// returns an opened Reader ready for ResolveLabels calls. t.Cleanup wires
// up the close path.
func openLabelResolveFixture(t *testing.T, entries []labelFixtureEntry) *postings.Reader {
	t.Helper()

	pb := postings.NewBuilder(nil, 0, 0)

	// One observation per (name, value, streamID). The aggregator unions
	// streamIDs sharing the same (objectPath, sectionIndex, name, value)
	// into a single posting row whose stream_id_bitmap covers all of them.
	ts := time.Unix(0, 1000).UTC()
	for _, e := range entries {
		for _, sid := range e.streamIDs {
			pb.ObserveLabelPosting(postings.LabelObservation{
				ObjectPath:       "/obj",
				SectionIndex:     0,
				ColumnName:       e.name,
				LabelValue:       e.value,
				StreamID:         sid,
				Timestamp:        ts,
				UncompressedSize: 1,
			})
		}
	}

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(pb))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	var sec *postings.Section
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		opened, openErr := postings.Open(t.Context(), s)
		require.NoError(t, openErr)
		sec = opened
		break
	}
	require.NotNil(t, sec, "postings section missing from fixture")

	r := postings.NewReader(postings.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(t.Context()))
	t.Cleanup(func() { _ = r.Close() })
	return r
}
