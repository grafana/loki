package logproto

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

func pairs(kv ...string) []push.LabelAdapter {
	out := make([]push.LabelAdapter, 0, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		out = append(out, push.LabelAdapter{Name: kv[i], Value: kv[i+1]})
	}
	return out
}

func entry(line string, own ...string) push.Entry {
	return push.Entry{
		Timestamp:          time.Unix(0, 1),
		Line:               line,
		StructuredMetadata: pairs(own...),
	}
}

// nested builds a stream of one resource and one scope carrying the given attributes.
func nested(resAttrs, scopeAttrs []string, entries ...push.Entry) InternalStreamAdapter {
	return InternalStreamAdapter{
		Labels: `{app="a"}`,
		Hash:   7,
		ResourceLogs: []ResourceLogs{{
			Attrs: pairs(resAttrs...),
			ScopeLogs: []ScopeLogs{{
				Attrs:   pairs(scopeAttrs...),
				Entries: entries,
			}},
		}},
	}
}

func linesOf(entries []push.Entry) []string {
	out := make([]string, 0, len(entries))
	for i := range entries {
		out = append(out, entries[i].Line)
	}
	return out
}

func TestEntryCountAcrossGroupsAndScopes(t *testing.T) {
	s := InternalStreamAdapter{ResourceLogs: []ResourceLogs{
		{ScopeLogs: []ScopeLogs{
			{Entries: []push.Entry{entry("a"), entry("b")}},
			{Entries: []push.Entry{entry("c")}},
		}},
		{ScopeLogs: []ScopeLogs{
			{Entries: []push.Entry{entry("d")}},
		}},
	}}

	require.Equal(t, 4, s.EntryCount())
}

func TestSizesCountSharedAttributesOnceOrPerEntry(t *testing.T) {
	// Two entries under a resource carrying 6 bytes of attributes and a scope carrying 4.
	s := nested([]string{"res", "abc"}, []string{"sc", "de"}, entry("hello"), entry("world"))

	// Unexpanded: the two lines, plus each shared set once.
	require.Equal(t, 5+5+6+4, s.UnexpandedSize())

	// Expanded: the two lines, plus both shared sets for each of the two entries.
	require.Equal(t, (5+6+4)+(5+6+4), s.ExpandedSize())
}

func TestSizesAreEqualWhenNothingIsShared(t *testing.T) {
	// Every native push looks like this, so the two must agree or rate limiting and
	// shard-count decisions would diverge for traffic that has not changed at all.
	s := FromStream(Stream{Entries: []push.Entry{entry("hello", "own", "1"), entry("bye")}})

	require.Equal(t, s.UnexpandedSize(), s.ExpandedSize())
	require.Equal(t, 5+3+1+3, s.UnexpandedSize())
}

func TestSizeExcludesTheDetectedLevel(t *testing.T) {
	// Loki adds the level label itself, so a tenant must not be charged for it — at any
	// of the three places metadata can live.
	plain := nested(nil, nil, entry("hello"))
	ownLevel := nested(nil, nil, entry("hello", constants.LevelLabel, "info"))
	resLevel := nested([]string{constants.LevelLabel, "info"}, nil, entry("hello"))
	scopeLevel := nested(nil, []string{constants.LevelLabel, "info"}, entry("hello"))

	require.Equal(t, plain.UnexpandedSize(), ownLevel.UnexpandedSize())
	require.Equal(t, plain.UnexpandedSize(), resLevel.UnexpandedSize())
	require.Equal(t, plain.UnexpandedSize(), scopeLevel.UnexpandedSize())

	// A label the tenant did send is still charged for, so the exclusion is not blanket.
	sent := nested(nil, nil, entry("hello", "detected_thing", "info"))
	require.Greater(t, sent.UnexpandedSize(), plain.UnexpandedSize())
}

func TestAppendEffectiveMetadataSeesAllThreeLevels(t *testing.T) {
	s := nested([]string{"res", "1"}, []string{"sc", "2"}, entry("hello", "own", "3"))
	res := &s.ResourceLogs[0]
	scope := &res.ScopeLogs[0]

	got := AppendEffectiveMetadata(nil, res.Attrs, scope.Attrs, &scope.Entries[0])

	require.Equal(t, pairs("own", "3", "res", "1", "sc", "2"), got)
}

func TestAppendEffectiveMetadataReusesTheBuffer(t *testing.T) {
	s := nested([]string{"res", "1"}, nil, entry("a", "own", "1"), entry("b", "own", "2"))
	res := &s.ResourceLogs[0]
	scope := &res.ScopeLogs[0]

	var buf []push.LabelAdapter
	for k := range scope.Entries {
		buf = AppendEffectiveMetadata(buf[:0], res.Attrs, scope.Attrs, &scope.Entries[k])
		require.Len(t, buf, 2, "the buffer must be reset, not grown, on each entry")
	}
	require.Equal(t, pairs("own", "2", "res", "1"), buf)
}

func TestFilterPrunesEmptyScopesAndGroups(t *testing.T) {
	s := InternalStreamAdapter{ResourceLogs: []ResourceLogs{
		{ScopeLogs: []ScopeLogs{
			{Entries: []push.Entry{entry("keep"), entry("drop")}},
			{Entries: []push.Entry{entry("drop")}}, // becomes empty, must be pruned
		}},
		{ScopeLogs: []ScopeLogs{
			{Entries: []push.Entry{entry("drop")}}, // whole group becomes empty
		}},
		{ScopeLogs: []ScopeLogs{
			{Entries: []push.Entry{entry("keep")}},
		}},
	}}

	dropped := s.Filter(func(_ *ResourceLogs, _ *ScopeLogs, e *push.Entry) bool {
		return e.Line != "drop"
	})

	require.Equal(t, 3, dropped)
	require.Equal(t, 2, s.EntryCount())
	require.Len(t, s.ResourceLogs, 2, "the group left with no entries must be gone")
	require.Len(t, s.ResourceLogs[0].ScopeLogs, 1, "the scope left with no entries must be gone")
	require.Equal(t, []string{"keep"}, linesOf(s.ResourceLogs[0].ScopeLogs[0].Entries))
	require.Equal(t, []string{"keep"}, linesOf(s.ResourceLogs[1].ScopeLogs[0].Entries))
}

func TestFilterKeepingNothingLeavesNoGroups(t *testing.T) {
	s := nested(nil, nil, entry("a"), entry("b"))

	require.Equal(t, 2, s.Filter(func(*ResourceLogs, *ScopeLogs, *push.Entry) bool { return false }))
	require.Empty(t, s.ResourceLogs)
	require.Zero(t, s.EntryCount())
}

func TestFilterMutationsSurviveCompaction(t *testing.T) {
	// The kept entry sits after a dropped one, so it moves. Its mutation has to move too.
	s := nested(nil, nil, entry("drop"), entry("keep"))

	s.Filter(func(_ *ResourceLogs, _ *ScopeLogs, e *push.Entry) bool {
		if e.Line == "drop" {
			return false
		}
		e.StructuredMetadata = append(e.StructuredMetadata, push.LabelAdapter{Name: "level", Value: "info"})
		return true
	})

	entries := s.ResourceLogs[0].ScopeLogs[0].Entries
	require.Len(t, entries, 1)
	require.Equal(t, "keep", entries[0].Line)
	require.Equal(t, push.LabelsAdapter(pairs("level", "info")), entries[0].StructuredMetadata)
}

func TestTruncateLinesCountsTheSuffixTowardTheLimit(t *testing.T) {
	// maxLen 8 with a 3-byte suffix keeps 5 bytes of line, so the result is exactly 8.
	s := nested(nil, nil, entry("short"), entry("far too long"), entry("also too long"))

	truncated, removed := s.TruncateLines(8, "xyz")

	require.Equal(t, 2, truncated)
	entries := s.ResourceLogs[0].ScopeLogs[0].Entries
	require.Equal(t, []string{"short", "far txyz", "also xyz"}, linesOf(entries))
	require.Len(t, entries[1].Line, 8)
	require.Equal(t, (len("far too long")-5)+(len("also too long")-5), removed)
}

func TestTruncateLinesLeavesLinesAloneWhenTheSuffixFillsTheBudget(t *testing.T) {
	s := nested(nil, nil, entry("far too long"))

	truncated, removed := s.TruncateLines(3, "xyz")

	require.Zero(t, truncated)
	require.Zero(t, removed)
	require.Equal(t, []string{"far too long"}, linesOf(s.ResourceLogs[0].ScopeLogs[0].Entries))
}

func TestEnforceTimestampOrder(t *testing.T) {
	at := func(ns int64, line string) push.Entry {
		return push.Entry{Timestamp: time.Unix(0, ns), Line: line}
	}

	for _, tc := range []struct {
		name     string
		in       []push.Entry
		adjusted int
		want     []int64
	}{
		{
			// Same timestamp and same line is de-duplicated further down, so it must be
			// left alone here.
			name:     "same timestamp and same line is untouched",
			in:       []push.Entry{at(10, "a"), at(10, "a")},
			adjusted: 0,
			want:     []int64{10, 10},
		},
		{
			name:     "same timestamp and different line is nudged",
			in:       []push.Entry{at(10, "a"), at(10, "b")},
			adjusted: 1,
			want:     []int64{10, 11},
		},
		{
			// The whole run spreads out, which only works because the tracker is seeded
			// with the first entry's timestamp rather than the zero time.
			name:     "a run of collisions spreads out",
			in:       []push.Entry{at(10, "a"), at(10, "b"), at(10, "c")},
			adjusted: 2,
			want:     []int64{10, 11, 12},
		},
		{
			name:     "distinct timestamps are untouched",
			in:       []push.Entry{at(10, "a"), at(20, "b"), at(30, "c")},
			adjusted: 0,
			want:     []int64{10, 20, 30},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := nested(nil, nil, tc.in...)

			require.Equal(t, tc.adjusted, s.EnforceTimestampOrder())

			got := make([]int64, 0, len(tc.want))
			for _, e := range s.ResourceLogs[0].ScopeLogs[0].Entries {
				got = append(got, e.Timestamp.UnixNano())
			}
			require.Equal(t, tc.want, got)
		})
	}
}

func TestEnforceTimestampOrderSpansScopes(t *testing.T) {
	// The entries are written in containment order, so a collision across a scope
	// boundary is still a collision.
	s := InternalStreamAdapter{ResourceLogs: []ResourceLogs{{ScopeLogs: []ScopeLogs{
		{Entries: []push.Entry{{Timestamp: time.Unix(0, 10), Line: "a"}}},
		{Entries: []push.Entry{{Timestamp: time.Unix(0, 10), Line: "b"}}},
	}}}}

	require.Equal(t, 1, s.EnforceTimestampOrder())
	require.Equal(t, int64(11), s.ResourceLogs[0].ScopeLogs[1].Entries[0].Timestamp.UnixNano())
}

func TestFromStreamCostsNothingPerEntry(t *testing.T) {
	flat := Stream{Labels: `{app="a"}`, Hash: 7, Entries: []push.Entry{entry("a"), entry("b")}}

	s := FromStream(flat)

	require.Equal(t, `{app="a"}`, s.Labels)
	require.Equal(t, uint64(7), s.Hash)
	require.Equal(t, 2, s.EntryCount())
	// The entries are taken as they are, not copied.
	require.Equal(t, &flat.Entries[0], &s.ResourceLogs[0].ScopeLogs[0].Entries[0])
}

func TestRoundTripThroughToStreamIsLosslessForNativePushes(t *testing.T) {
	flat := Stream{Labels: `{app="a"}`, Hash: 7, Entries: []push.Entry{
		entry("a", "own", "1"),
		entry("b"),
	}}

	converted := FromStream(flat)
	got := converted.ToStream()

	require.Equal(t, flat, got)
}

func TestToStreamResolvesSharedAttributesOntoEveryEntry(t *testing.T) {
	s := nested([]string{"res", "1"}, []string{"sc", "2"}, entry("a", "own", "3"), entry("b"))

	got := s.ToStream()

	require.Equal(t, `{app="a"}`, got.Labels)
	require.Equal(t, uint64(7), got.Hash)
	require.Equal(t, []string{"a", "b"}, linesOf(got.Entries))
	require.Equal(t, push.LabelsAdapter(pairs("own", "3", "res", "1", "sc", "2")), got.Entries[0].StructuredMetadata)
	require.Equal(t, push.LabelsAdapter(pairs("res", "1", "sc", "2")), got.Entries[1].StructuredMetadata)
}

func TestToStreamDoesNotAliasSharedAttributes(t *testing.T) {
	// Each entry needs its own metadata slice: one resource's attributes are shared by
	// every entry beneath it, so writing through one entry must not touch another.
	s := nested([]string{"res", "1"}, nil, entry("a"), entry("b"))

	got := s.ToStream()
	got.Entries[0].StructuredMetadata[0].Value = "mutated"

	require.Equal(t, "1", got.Entries[1].StructuredMetadata[0].Value)
	require.Equal(t, "1", s.ResourceLogs[0].Attrs[0].Value, "the source must be untouched")
}

func TestToStreamOrdersEntriesByContainment(t *testing.T) {
	s := InternalStreamAdapter{ResourceLogs: []ResourceLogs{
		{ScopeLogs: []ScopeLogs{
			{Entries: []push.Entry{entry("a")}},
			{Entries: []push.Entry{entry("b")}},
		}},
		{ScopeLogs: []ScopeLogs{
			{Entries: []push.Entry{entry("c")}},
		}},
	}}

	require.Equal(t, []string{"a", "b", "c"}, linesOf(s.ToStream().Entries))
}

func TestToStreamIsTheExactInverseOfTheNesting(t *testing.T) {
	// Own pairs first, then the resource's, then the scope's, in that order and unsorted:
	// that is what the flattened form carried before the attributes were lifted out, and
	// ordering the stored value is sanitisation's job further down.
	s := nested([]string{"b", "resource"}, []string{"a", "scope"}, entry("line", "c", "own"))

	got := s.ToStream()

	require.Equal(t, push.LabelsAdapter(pairs("c", "own", "b", "resource", "a", "scope")),
		got.Entries[0].StructuredMetadata)
}

func TestDivideRoundRobinKeepsGroupAttributes(t *testing.T) {
	s := nested([]string{"res", "1"}, []string{"sc", "2"},
		entry("a"), entry("b"), entry("c"), entry("d"))

	parts := s.Divide(3, func(idx int, _ *push.Entry) int { return idx % 3 })

	require.Len(t, parts, 3)
	require.Equal(t, []string{"a", "d"}, linesOf(parts[0].ResourceLogs[0].ScopeLogs[0].Entries))
	require.Equal(t, []string{"b"}, linesOf(parts[1].ResourceLogs[0].ScopeLogs[0].Entries))
	require.Equal(t, []string{"c"}, linesOf(parts[2].ResourceLogs[0].ScopeLogs[0].Entries))

	for _, part := range parts {
		require.Equal(t, `{app="a"}`, part.Labels)
		require.Equal(t, pairs("res", "1"), part.ResourceLogs[0].Attrs,
			"a shard must carry the attributes that applied to its entries")
		require.Equal(t, pairs("sc", "2"), part.ResourceLogs[0].ScopeLogs[0].Attrs)
	}
}

func TestDivideReturnsEmptyPartsSoIndexesStillMean(t *testing.T) {
	s := nested(nil, nil, entry("a"), entry("b"))

	// Nothing lands in part 1, but it must still be returned so part 2 is still part 2 to
	// a caller stamping shard labels.
	parts := s.Divide(3, func(_ int, e *push.Entry) int {
		if e.Line == "a" {
			return 0
		}
		return 2
	})

	require.Len(t, parts, 3)
	require.Equal(t, 1, parts[0].EntryCount())
	require.Zero(t, parts[1].EntryCount())
	require.Empty(t, parts[1].ResourceLogs, "an empty part must not carry an empty group")
	require.Equal(t, 1, parts[2].EntryCount())
}

func TestDivideDiscardsOutOfRangeAssignments(t *testing.T) {
	s := nested(nil, nil, entry("keep"), entry("drop"), entry("also drop"))

	parts := s.Divide(1, func(_ int, e *push.Entry) int {
		switch e.Line {
		case "drop":
			return -1
		case "also drop":
			return 5
		}
		return 0
	})

	require.Len(t, parts, 1)
	require.Equal(t, []string{"keep"}, linesOf(parts[0].ResourceLogs[0].ScopeLogs[0].Entries))
}

func TestDivideAsksOncePerEntryInContainmentOrder(t *testing.T) {
	s := InternalStreamAdapter{ResourceLogs: []ResourceLogs{
		{ScopeLogs: []ScopeLogs{
			{Entries: []push.Entry{entry("a"), entry("b")}},
			{Entries: []push.Entry{entry("c")}},
		}},
		{ScopeLogs: []ScopeLogs{
			{Entries: []push.Entry{entry("d")}},
		}},
	}}

	var seen []string
	var indexes []int
	s.Divide(1, func(idx int, e *push.Entry) int {
		seen = append(seen, e.Line)
		indexes = append(indexes, idx)
		return 0
	})

	require.Equal(t, []string{"a", "b", "c", "d"}, seen)
	require.Equal(t, []int{0, 1, 2, 3}, indexes)
}

func TestDivideKeepsEntriesFromDifferentSourceGroupsApart(t *testing.T) {
	// Two resources with different attributes. Entries from both land in one part, and
	// must not be merged into one group or each would inherit the other's attributes.
	s := InternalStreamAdapter{ResourceLogs: []ResourceLogs{
		{Attrs: pairs("host", "one"), ScopeLogs: []ScopeLogs{{Entries: []push.Entry{entry("a")}}}},
		{Attrs: pairs("host", "two"), ScopeLogs: []ScopeLogs{{Entries: []push.Entry{entry("b")}}}},
	}}

	parts := s.Divide(1, func(int, *push.Entry) int { return 0 })

	require.Len(t, parts[0].ResourceLogs, 2)
	require.Equal(t, pairs("host", "one"), parts[0].ResourceLogs[0].Attrs)
	require.Equal(t, []string{"a"}, linesOf(parts[0].ResourceLogs[0].ScopeLogs[0].Entries))
	require.Equal(t, pairs("host", "two"), parts[0].ResourceLogs[1].Attrs)
	require.Equal(t, []string{"b"}, linesOf(parts[0].ResourceLogs[1].ScopeLogs[0].Entries))
}

func TestDivideCollectsEntriesOfOneScopeTogether(t *testing.T) {
	// Alternate two parts across four entries of one scope: each part should end up with
	// one group holding two entries, not two groups holding one each.
	s := nested([]string{"res", "1"}, nil, entry("a"), entry("b"), entry("c"), entry("d"))

	parts := s.Divide(2, func(idx int, _ *push.Entry) int { return idx % 2 })

	for _, part := range parts {
		require.Len(t, part.ResourceLogs, 1)
		require.Len(t, part.ResourceLogs[0].ScopeLogs, 1)
		require.Len(t, part.ResourceLogs[0].ScopeLogs[0].Entries, 2)
	}
}

func TestSortByTimestampIsAFullSortForANativePush(t *testing.T) {
	at := func(ns int64) push.Entry {
		return push.Entry{Timestamp: time.Unix(0, ns), Line: "line"}
	}
	s := FromStream(Stream{Entries: []push.Entry{at(30), at(10), at(20)}})

	s.SortByTimestamp()

	got := make([]int64, 0, 3)
	for _, e := range s.ResourceLogs[0].ScopeLogs[0].Entries {
		got = append(got, e.Timestamp.UnixNano())
	}
	require.Equal(t, []int64{10, 20, 30}, got)
}

func TestSortByTimestampIsStable(t *testing.T) {
	same := func(line string) push.Entry {
		return push.Entry{Timestamp: time.Unix(0, 10), Line: line}
	}
	s := FromStream(Stream{Entries: []push.Entry{same("a"), same("b"), same("c")}})

	s.SortByTimestamp()

	require.Equal(t, []string{"a", "b", "c"}, linesOf(s.ResourceLogs[0].ScopeLogs[0].Entries))
}

func TestSortByTimestampOrdersWithinEachScopeOnly(t *testing.T) {
	// An entry cannot leave the scope whose attributes apply to it, so the result is
	// ordered per scope and not across them.
	at := func(ns int64) push.Entry {
		return push.Entry{Timestamp: time.Unix(0, ns), Line: "line"}
	}
	s := InternalStreamAdapter{ResourceLogs: []ResourceLogs{{ScopeLogs: []ScopeLogs{
		{Entries: []push.Entry{at(30), at(10)}},
		{Entries: []push.Entry{at(40), at(20)}},
	}}}}

	s.SortByTimestamp()

	first := s.ResourceLogs[0].ScopeLogs[0].Entries
	second := s.ResourceLogs[0].ScopeLogs[1].Entries
	require.Equal(t, int64(10), first[0].Timestamp.UnixNano())
	require.Equal(t, int64(30), first[1].Timestamp.UnixNano())
	require.Equal(t, int64(20), second[0].Timestamp.UnixNano())
	require.Equal(t, int64(40), second[1].Timestamp.UnixNano())
}
