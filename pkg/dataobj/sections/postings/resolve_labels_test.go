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

func TestResolveMatchingStreamRefs_EqualMatchers_Single(t *testing.T) {
	r := openLabelResolveFixture(t, []labelFixtureEntry{
		{name: "env", value: "prod", streamIDs: []int64{1, 2, 3}},
		{name: "env", value: "staging", streamIDs: []int64{4, 5}},
		{name: "app", value: "foo", streamIDs: []int64{2, 3, 6}},
	})

	got, names, err := resolveToStreamIDs(t, r, []*labels.Matcher{
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

func TestResolveMatchingStreamRefs_RegexFallback(t *testing.T) {
	r := openLabelResolveFixture(t, []labelFixtureEntry{
		{name: "env", value: "prod", streamIDs: []int64{1, 2, 3}},
		{name: "env", value: "staging", streamIDs: []int64{4, 5}},
		{name: "app", value: "foo", streamIDs: []int64{2, 3, 6}},
	})

	got, _, err := resolveToStreamIDs(t, r, []*labels.Matcher{
		regexMatcher(t, "env", "^pr.*"),
	})
	require.NoError(t, err)
	require.Len(t, got, 3, "regex env=~^pr.* must match exactly 3 streams (rows with env=prod)")
	for _, id := range []int64{1, 2, 3} {
		_, ok := got[id]
		require.True(t, ok, "stream %d must be present in regex result", id)
	}
}

func TestResolveMatchingStreamRefs_LabelNamesByStream_Inversion(t *testing.T) {
	r := openLabelResolveFixture(t, []labelFixtureEntry{
		{name: "env", value: "prod", streamIDs: []int64{1, 2}},
		{name: "app", value: "foo", streamIDs: []int64{2, 7}},
		{name: "region", value: "us", streamIDs: []int64{2, 8}},
	})

	got, names, err := resolveToStreamIDs(t, r, []*labels.Matcher{
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

func TestResolveMatchingStreamRefs_Mixed_Equal_And_Regex_DifferentNames(t *testing.T) {
	r := openLabelResolveFixture(t, []labelFixtureEntry{
		{name: "env", value: "prod", streamIDs: []int64{1, 2, 3}},
		{name: "app", value: "foo", streamIDs: []int64{2, 4}},
		{name: "app", value: "bar", streamIDs: []int64{2, 5}},
	})

	got, names, err := resolveToStreamIDs(t, r, []*labels.Matcher{
		equalMatcher(t, "env", "prod"),
		regexMatcher(t, "app", "^foo.*"),
	})
	require.NoError(t, err)
	require.Len(t, got, 1,
		"env=prod AND app=~^foo.* must yield exactly stream 2 — previously the regex was silently dropped and 3 streams returned")
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

func TestResolveMatchingStreamRefs_MultiRegex_AND(t *testing.T) {
	r := openLabelResolveFixture(t, []labelFixtureEntry{
		{name: "env", value: "prod", streamIDs: []int64{1, 2}},
		{name: "env", value: "staging", streamIDs: []int64{3}},
		{name: "app", value: "foo", streamIDs: []int64{2, 4}},
		{name: "app", value: "bar", streamIDs: []int64{5}},
	})

	got, names, err := resolveToStreamIDs(t, r, []*labels.Matcher{
		regexMatcher(t, "env", "^pr.*"),
		regexMatcher(t, "app", "^fo.*"),
	})
	require.NoError(t, err)
	require.Len(t, got, 1,
		"env=~^pr.* AND app=~^fo.* must intersect to stream 2 — previously this returned the UNION {1,2,4}")
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

func TestResolveMatchingStreamRefs_NotEqualMatcher_AcrossNames(t *testing.T) {
	r := openLabelResolveFixture(t, []labelFixtureEntry{
		{name: "env", value: "prod", streamIDs: []int64{1, 2}},
		{name: "env", value: "dev", streamIDs: []int64{3}},
		{name: "app", value: "foo", streamIDs: []int64{2, 4}},
		{name: "app", value: "bar", streamIDs: []int64{1, 5}},
	})

	notEqual, err := labels.NewMatcher(labels.MatchNotEqual, "app", "bar")
	require.NoError(t, err)

	got, _, err := resolveToStreamIDs(t, r, []*labels.Matcher{
		equalMatcher(t, "env", "prod"),
		notEqual,
	})
	require.NoError(t, err)
	require.Len(t, got, 1,
		"env=prod AND app!=bar must yield exactly stream 2 — previously the NotEqual matcher was silently dropped")
	_, ok := got[2]
	require.True(t, ok, "stream 2 (env=prod AND app=foo) must be the sole survivor")
	_, has1 := got[1]
	require.False(t, has1, "stream 1 (env=prod AND app=bar) must NOT appear — app!=bar rejects it")
}

func TestResolveLabelStreams_ObjectScopedStreamIDs(t *testing.T) {
	r := openLabelResolveFixture(t, []labelFixtureEntry{
		{objectPath: "/obj-a", name: "app", value: "foo", streamIDs: []int64{1}},
		{objectPath: "/obj-b", name: "app", value: "bar", streamIDs: []int64{1}},
	})

	got, names, err := r.ResolveMatchingStreamRefs(t.Context(), []*labels.Matcher{
		equalMatcher(t, "app", "foo"),
	})
	require.NoError(t, err)
	require.Len(t, got, 1, "only /obj-a stream 1 matches app=foo")

	target := postings.StreamRef{ObjectPath: "/obj-a", StreamID: 1}
	_, ok := got[target]
	require.True(t, ok, "object-scoped stream ref must be present")
	_, leaked := got[postings.StreamRef{ObjectPath: "/obj-b", StreamID: 1}]
	require.False(t, leaked, "same numeric stream ID in another object must not leak into the result")
	require.ElementsMatch(t, []string{"app"}, names[target])
}

func TestResolveLabelNames(t *testing.T) {
	tests := []struct {
		name       string
		entries    []labelFixtureEntry
		streamRefs map[postings.StreamRef]struct{}
		want       []string
	}{
		{
			name: "all",
			entries: []labelFixtureEntry{
				{name: "env", value: "prod", streamIDs: []int64{1, 2}},
				{name: "app", value: "foo", streamIDs: []int64{2}},
				{name: "region", value: "us", streamIDs: []int64{1}},
			},
			want: []string{"env", "app", "region"},
		},
		{
			name: "filtered_by_stream_refs",
			entries: []labelFixtureEntry{
				{objectPath: "/obj-a", name: "app", value: "foo", streamIDs: []int64{1}},
				{objectPath: "/obj-a", name: "env", value: "prod", streamIDs: []int64{1}},
				{objectPath: "/obj-a", name: "app", value: "bar", streamIDs: []int64{2}},
				{objectPath: "/obj-b", name: "app", value: "baz", streamIDs: []int64{1}},
			},
			streamRefs: map[postings.StreamRef]struct{}{
				{ObjectPath: "/obj-a", StreamID: 1}: {},
			},
			want: []string{"app", "env"},
		},
		{
			name: "empty_stream_refs",
			entries: []labelFixtureEntry{
				{name: "env", value: "prod", streamIDs: []int64{1}},
				{name: "app", value: "foo", streamIDs: []int64{1}},
			},
			streamRefs: map[postings.StreamRef]struct{}{},
			want:       []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := openLabelResolveFixture(t, tc.entries)
			got, err := r.ResolveLabelNames(t.Context(), tc.streamRefs)
			require.NoError(t, err)
			require.ElementsMatch(t, tc.want, got)
		})
	}
}

func TestResolveLabelValues(t *testing.T) {
	tests := []struct {
		name       string
		entries    []labelFixtureEntry
		streamRefs map[postings.StreamRef]struct{}
		want       []string
	}{
		{
			name: "all",
			entries: []labelFixtureEntry{
				{name: "env", value: "prod", streamIDs: []int64{1, 2}},
				{name: "app", value: "foo", streamIDs: []int64{2}},
				{name: "app", value: "bar", streamIDs: []int64{3}},
			},
			want: []string{"prod", "foo", "bar"},
		},
		{
			name: "filtered_by_stream_refs",
			entries: []labelFixtureEntry{
				{objectPath: "/obj-a", name: "app", value: "foo", streamIDs: []int64{1}},
				{objectPath: "/obj-a", name: "env", value: "prod", streamIDs: []int64{1}},
				{objectPath: "/obj-a", name: "app", value: "bar", streamIDs: []int64{2}},
				{objectPath: "/obj-b", name: "app", value: "baz", streamIDs: []int64{1}},
			},
			streamRefs: map[postings.StreamRef]struct{}{
				{ObjectPath: "/obj-a", StreamID: 1}: {},
			},
			want: []string{"foo", "prod"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := openLabelResolveFixture(t, tc.entries)
			got, err := r.ResolveLabelValues(t.Context(), tc.streamRefs)
			require.NoError(t, err)
			require.ElementsMatch(t, tc.want, got)
		})
	}
}

func resolveToStreamIDs(tb testing.TB, r *postings.Reader, matchers []*labels.Matcher) (map[int64]struct{}, map[int64][]string, error) {
	tb.Helper()

	matchingRefs, namesByRef, err := r.ResolveMatchingStreamRefs(tb.Context(), matchers)
	if err != nil {
		return nil, nil, err
	}
	if matchingRefs == nil && namesByRef == nil {
		return nil, nil, nil
	}

	matchingStreamIDs := make(map[int64]struct{}, len(matchingRefs))
	for streamRef := range matchingRefs {
		matchingStreamIDs[streamRef.StreamID] = struct{}{}
	}

	if len(namesByRef) == 0 {
		return matchingStreamIDs, nil, nil
	}

	nameSetByStreamID := make(map[int64]map[string]struct{}, len(namesByRef))
	for streamRef, names := range namesByRef {
		nameSet := nameSetByStreamID[streamRef.StreamID]
		if nameSet == nil {
			nameSet = make(map[string]struct{}, len(names))
			nameSetByStreamID[streamRef.StreamID] = nameSet
		}
		for _, name := range names {
			nameSet[name] = struct{}{}
		}
	}

	namesByStreamID := make(map[int64][]string, len(nameSetByStreamID))
	for streamID, nameSet := range nameSetByStreamID {
		names := make([]string, 0, len(nameSet))
		for name := range nameSet {
			names = append(names, name)
		}
		namesByStreamID[streamID] = names
	}

	return matchingStreamIDs, namesByStreamID, nil
}

type labelFixtureEntry struct {
	objectPath string
	name       string
	value      string
	streamIDs  []int64
}

// openLabelResolveFixture builds an opened postings Reader from entries. It takes
// a testing.TB so it can be shared between tests and benchmarks.
func openLabelResolveFixture(tb testing.TB, entries []labelFixtureEntry) *postings.Reader {
	tb.Helper()

	pb := postings.NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 1000).UTC()
	for _, e := range entries {
		path := e.objectPath
		if path == "" {
			path = "/obj"
		}
		for _, sid := range e.streamIDs {
			pb.ObserveLabelPosting(postings.LabelObservation{
				ObjectPath:       path,
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
	require.NoError(tb, objBuilder.Append(pb))
	obj, closer, err := objBuilder.Flush()
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = closer.Close() })

	var sec *postings.Section
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		opened, openErr := postings.Open(tb.Context(), s)
		require.NoError(tb, openErr)
		sec = opened
		break
	}
	require.NotNil(tb, sec, "postings section missing from fixture")

	r := postings.NewReader(postings.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(tb, r.Open(tb.Context()))
	tb.Cleanup(func() { _ = r.Close() })
	return r
}
