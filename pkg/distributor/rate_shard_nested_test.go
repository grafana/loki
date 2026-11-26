package distributor

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
	lokiring "github.com/grafana/loki/v3/pkg/util/ring"

	"github.com/grafana/loki/pkg/push"
)

// shardLabels is what the caller has in hand when it shards: the stream's labels, parsed by
// validation before it got here.
var shardLabels = mustParseLabels(`{app="a"}`)

// shardNestedForTest also assigns labels to exercise output identities and contents.
func shardNestedForTest(stream logproto.InternalStreamAdapter, lbls labels.Labels, shards, startShard int) []logproto.InternalStreamAdapter {
	return packedShardsForTest(stream, lbls, shards, startShard, 0)
}

func packedShardsForTest(stream logproto.InternalStreamAdapter, lbls labels.Labels, shards, startShard int, target uint64) []logproto.InternalStreamAdapter {
	size := 0
	if target > 0 {
		size = expandedShardSize(stream)
	}
	var out []logproto.InternalStreamAdapter
	if target > 0 {
		out = packNested(stream, shards, size, target)
	} else {
		out = shardNested(stream, shards)
	}
	labelShards(out, lbls, shards, startShard)
	return out
}

func shardEntryCounts(out []logproto.InternalStreamAdapter) []int {
	var counts []int
	for _, shard := range out {
		counts = append(counts, shard.EntryCount())
	}
	return counts
}

func buildEntry(line string) push.Entry {
	return push.Entry{Timestamp: time.Unix(0, 1).UTC(), Line: line}
}

func buildNestedAttrs(pairs ...string) []push.LabelAdapter {
	var out []push.LabelAdapter
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, push.LabelAdapter{Name: pairs[i], Value: pairs[i+1]})
	}
	return out
}

// buildGroupedStream is resources of scopes of entries, every entry's line naming where it sits so a
// shard can be checked against the source without carrying the source around.
func buildGroupedStream(resources, scopesPer, entriesPer int) logproto.InternalStreamAdapter {
	stream := logproto.InternalStreamAdapter{Labels: `{app="a"}`, Hash: 7}
	for r := 0; r < resources; r++ {
		resource := logproto.ResourceLogs{Attrs: buildNestedAttrs("service", fmt.Sprintf("svc-%d", r))}
		for s := 0; s < scopesPer; s++ {
			scope := logproto.ScopeLogs{Attrs: buildNestedAttrs("scope", fmt.Sprintf("scope-%d-%d", r, s))}
			for e := 0; e < entriesPer; e++ {
				entry := buildEntry(fmt.Sprintf("r%d-s%d-e%d", r, s, e))
				if e == 0 {
					// One entry per scope carries metadata of its own, which has to travel with
					// the entry rather than be confused with the attributes on its group.
					entry.StructuredMetadata = push.LabelsAdapter(buildNestedAttrs("trace_id", "abc123"))
				}
				scope.Entries = append(scope.Entries, entry)
			}
			resource.ScopeLogs = append(resource.ScopeLogs, scope)
		}
		stream.ResourceLogs = append(stream.ResourceLogs, resource)
	}
	return stream
}

// requireShardsCarryTheStream asserts what has to hold however the entries are dealt out: the
// shards together hold every entry exactly once and in order, each under the resource and scope
// attributes it arrived with, and no shard holds an empty group or an unnamed stream.
func requireShardsCarryTheStream(t *testing.T, source logproto.InternalStreamAdapter, shards []logproto.InternalStreamAdapter) {
	t.Helper()

	type placed struct {
		entry         push.Entry
		resourceAttrs []push.LabelAdapter
		scopeAttrs    []push.LabelAdapter
	}
	var want []placed
	for i := range source.ResourceLogs {
		res := &source.ResourceLogs[i]
		for j := range res.ScopeLogs {
			scope := &res.ScopeLogs[j]
			for _, entry := range scope.Entries {
				want = append(want, placed{entry, res.Attrs, scope.Attrs})
			}
		}
	}

	seen := 0
	seenNames := map[string]struct{}{}
	for s := range shards {
		shard := &shards[s]

		// The source's labels and hash, carried through unchanged. Naming a shard is the caller's,
		// so these are placeholders rather than what a named shard ends up with.
		require.NotEqual(t, source.Labels, shard.Labels, "shard %d carries the stream's own name", s)
		require.NotContains(t, seenNames, shard.Labels, "shard %d repeats another shard's name", s)
		seenNames[shard.Labels] = struct{}{}
		require.NotEmpty(t, shard.ResourceLogs, "shard %d holds no resources", s)

		for i := range shard.ResourceLogs {
			res := &shard.ResourceLogs[i]
			require.NotEmpty(t, res.ScopeLogs, "shard %d holds a resource with no scopes", s)

			for j := range res.ScopeLogs {
				scope := &res.ScopeLogs[j]
				require.NotEmpty(t, scope.Entries, "shard %d holds a scope with no entries", s)

				for _, entry := range scope.Entries {
					require.Less(t, seen, len(want), "shard %d holds more entries than the source", s)
					w := want[seen]
					seen++

					require.Equal(t, w.entry, entry, "shard %d: entry %d came back changed", s, seen-1)
					require.Equal(t, w.resourceAttrs, res.Attrs,
						"shard %d: entry %d is under the wrong resource", s, seen-1)
					require.Equal(t, w.scopeAttrs, scope.Attrs,
						"shard %d: entry %d is under the wrong scope", s, seen-1)
				}
			}
		}
	}
	require.Equal(t, len(want), seen, "every entry exactly once, in the order given")
}

// attrCopies is how many times a group's attributes are written across the shards, which is what
// contiguous runs exist to keep down.
func attrCopies(shards []logproto.InternalStreamAdapter) int {
	n := 0
	for i := range shards {
		for j := range shards[i].ResourceLogs {
			n++ // the resource's attributes
			n += len(shards[i].ResourceLogs[j].ScopeLogs)
		}
	}
	return n
}

func TestShardNested(t *testing.T) {
	buildEntries := func(lines ...string) []push.Entry {
		out := make([]push.Entry, 0, len(lines))
		for _, line := range lines {
			out = append(out, buildEntry(line))
		}
		return out
	}

	for _, tc := range []struct {
		name        string
		buildStream func() logproto.InternalStreamAdapter
		shards      int

		wantShards int      // how many come back, when the case is about that
		startShard int      // the number the first shard takes, when the case is about numbering
		wantNames  []string // what each shard is called, stated rather than derived
	}{
		{
			name:        "shards named after their number",
			buildStream: func() logproto.InternalStreamAdapter { return buildGroupedStream(1, 1, 6) },
			shards:      3, wantShards: 3,
			wantNames: []string{
				`{__stream_shard__="0", app="a"}`,
				`{__stream_shard__="1", app="a"}`,
				`{__stream_shard__="2", app="a"}`,
			},
		},
		{
			// Numbering carries on from where the stream's last push left off, wrapping at the
			// number of shards it is divided into.
			name:        "numbered from where the last push left off",
			buildStream: func() logproto.InternalStreamAdapter { return buildGroupedStream(1, 1, 6) },
			shards:      3, wantShards: 3, startShard: 2,
			wantNames: []string{
				`{__stream_shard__="2", app="a"}`,
				`{__stream_shard__="0", app="a"}`,
				`{__stream_shard__="1", app="a"}`,
			},
		},
		{
			// Fewer entries than shards asked for, so only two come back — but they are still
			// numbered against the count asked for, so the next push carries on from 1 rather
			// than landing on these two again.
			name:        "numbered against the count asked for, not the count produced",
			buildStream: func() logproto.InternalStreamAdapter { return buildGroupedStream(1, 1, 2) },
			shards:      8, wantShards: 2, startShard: 7,
			wantNames: []string{
				`{__stream_shard__="7", app="a"}`,
				`{__stream_shard__="0", app="a"}`,
			},
		},
		{
			name:        "one group over several shards",
			buildStream: func() logproto.InternalStreamAdapter { return buildGroupedStream(1, 1, 9) },
			shards:      3, wantShards: 3,
		},
		{
			name:        "a group per shard",
			buildStream: func() logproto.InternalStreamAdapter { return buildGroupedStream(3, 1, 4) },
			shards:      3, wantShards: 3,
		},
		{
			name:        "more groups than shards",
			buildStream: func() logproto.InternalStreamAdapter { return buildGroupedStream(6, 2, 2) },
			shards:      3, wantShards: 3,
		},
		{
			name:        "more shards than entries",
			buildStream: func() logproto.InternalStreamAdapter { return buildGroupedStream(1, 1, 2) },
			shards:      8, wantShards: 2,
		},
		{
			name:        "entries that do not divide evenly",
			buildStream: func() logproto.InternalStreamAdapter { return buildGroupedStream(2, 2, 5) },
			shards:      3, wantShards: 3,
		},
		{
			// One shard is still one shard's worth of work: everything in it, built like any
			// other count rather than handed back as it came.
			name:        "one shard asked for",
			buildStream: func() logproto.InternalStreamAdapter { return buildGroupedStream(3, 2, 2) },
			shards:      1, wantShards: 1,
		},
		{
			name:        "one entry per scope",
			buildStream: func() logproto.InternalStreamAdapter { return buildGroupedStream(4, 3, 1) },
			shards:      4, wantShards: 4,
		},
		{
			name: "empty scopes among full ones, and a resource with none",
			buildStream: func() logproto.InternalStreamAdapter {
				return logproto.InternalStreamAdapter{Labels: `{app="a"}`, Hash: 7, ResourceLogs: []logproto.ResourceLogs{
					{Attrs: buildNestedAttrs("service", "svc-0"), ScopeLogs: []logproto.ScopeLogs{
						{Attrs: buildNestedAttrs("scope", "empty-first")},
						{Attrs: buildNestedAttrs("scope", "full"), Entries: buildEntries("a1", "a2", "a3")},
						{Attrs: buildNestedAttrs("scope", "empty-last")},
					}},
					{Attrs: buildNestedAttrs("service", "no-scopes")},
					{Attrs: buildNestedAttrs("service", "svc-2"), ScopeLogs: []logproto.ScopeLogs{
						{Attrs: buildNestedAttrs("scope", "full"), Entries: buildEntries("c1", "c2")},
					}},
				}}
			},
			shards: 2, wantShards: 2,
		},
		{
			// What FromStream produces: one group, no attributes on either level.
			name: "no attributes anywhere",
			buildStream: func() logproto.InternalStreamAdapter {
				return logproto.InternalStreamAdapter{Labels: `{app="a"}`, Hash: 7, ResourceLogs: []logproto.ResourceLogs{
					{ScopeLogs: []logproto.ScopeLogs{{Entries: buildEntries("a1", "a2", "a3", "a4")}}},
				}}
			},
			shards: 3, wantShards: 3,
		},
		{
			// Two resources a reader cannot tell apart. They are still two.
			name: "two resources carrying identical attributes",
			buildStream: func() logproto.InternalStreamAdapter {
				return logproto.InternalStreamAdapter{Labels: `{app="a"}`, Hash: 7, ResourceLogs: []logproto.ResourceLogs{
					{Attrs: buildNestedAttrs("service", "same"), ScopeLogs: []logproto.ScopeLogs{
						{Attrs: buildNestedAttrs("scope", "same"), Entries: buildEntries("a1", "a2")},
					}},
					{Attrs: buildNestedAttrs("service", "same"), ScopeLogs: []logproto.ScopeLogs{
						{Attrs: buildNestedAttrs("scope", "same"), Entries: buildEntries("b1", "b2")},
					}},
				}}
			},
			shards: 2, wantShards: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := tc.buildStream()
			shards := shardNestedForTest(input, shardLabels, tc.shards, tc.startShard)

			for i, name := range tc.wantNames {
				require.Equal(t, name, shards[i].Labels, "shard %d name", i)
				require.Equal(t, labels.StableHash(mustParseLabels(name)), shards[i].Hash,
					"shard %d hashes to its own name", i)
			}

			// What to expect is built separately rather than read back off the input, which the
			// code under test holds pointers into and could have changed underneath us.
			source := tc.buildStream()
			require.Len(t, shards, tc.wantShards)
			requireShardsCarryTheStream(t, source, shards)

			// The entry counts are the ones round-robin would produce: base, with the remainder
			// spread over the leading shards.
			total := source.EntryCount()
			base, remainder := total/len(shards), total%len(shards)
			for s := range shards {
				want := base
				if s < remainder {
					want++
				}
				require.Equal(t, want, shards[s].EntryCount(), "shard %d entry count", s)
			}
		})
	}
}

func TestShardNestedRepeatsAGroupOnlyWhereItStraddlesAShard(t *testing.T) {
	// A group is written to a shard only if one of its entries went there, so the copies of the
	// group attributes cannot exceed one per group plus one per boundary a group is split across.
	for _, tc := range []struct{ resources, scopesPer, entriesPer, shards int }{
		{1, 1, 100, 8},
		{4, 1, 25, 8},
		{10, 2, 5, 8},
		{20, 1, 5, 4},
		{3, 3, 7, 5},
	} {
		input := buildGroupedStream(tc.resources, tc.scopesPer, tc.entriesPer)
		shards := shardNestedForTest(input, shardLabels, tc.shards, 0)

		source := buildGroupedStream(tc.resources, tc.scopesPer, tc.entriesPer)
		requireShardsCarryTheStream(t, source, shards)

		resources, groups := tc.resources, tc.resources*tc.scopesPer
		// Each resource and each scope is written once, plus once more wherever a shard boundary
		// falls inside it. There are len(shards)-1 boundaries, and one boundary can fall inside
		// at most one resource and one scope.
		bound := (resources + len(shards) - 1) + (groups + len(shards) - 1)
		require.LessOrEqual(t, attrCopies(shards), bound,
			"%d resources x %d scopes x %d entries over %d shards", tc.resources, tc.scopesPer, tc.entriesPer, tc.shards)

	}
}

func TestShardNestedReturnsNothingToShard(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stream logproto.InternalStreamAdapter
		shards int
	}{
		{name: "no shards asked for", stream: buildGroupedStream(2, 2, 2), shards: 0},
		{name: "a negative number of shards", stream: buildGroupedStream(2, 2, 2), shards: -1},
		{name: "a stream of no entries", stream: buildGroupedStream(2, 2, 0), shards: 4},
		{name: "a stream of no groups", stream: logproto.InternalStreamAdapter{Labels: `{app="a"}`}, shards: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Nil(t, shardNestedForTest(tc.stream, shardLabels, tc.shards, 0),
				"nothing to divide, so the caller keeps the stream it has")
		})
	}
}

// A shard's entries are subslices of the source, as the flat path's time shards are, so a caller
// rewriting an entry rewrites it for both. Everything above the entries is the shard's own, and
// appending to a shard cannot reach the entries the next one holds.
func TestShardNestedSharesOnlyItsEntriesWithTheCaller(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprintf("packed=%t", packed), func(t *testing.T) {
			shardStream := func(stream logproto.InternalStreamAdapter, count int) []logproto.InternalStreamAdapter {
				var target uint64
				if packed {
					target = uint64((expandedShardSize(stream) + count - 1) / count)
				}
				return packedShardsForTest(stream, shardLabels, count, 0, target)
			}
			for _, shards := range []int{1, 2, 5} {
				input := buildGroupedStream(2, 2, 3)
				out := shardStream(input, shards)
				require.NotEmpty(t, out)

				out[0].Labels = "rewritten"
				out[0].Hash = 99
				out[0].ResourceLogs[0].Attrs = buildNestedAttrs("service", "MUTATED")

				require.Equal(t, `{app="a"}`, input.Labels, "shards %d: labels", shards)
				require.Equal(t, uint64(7), input.Hash, "shards %d: hash", shards)
				require.Equal(t, "svc-0", input.ResourceLogs[0].Attrs[0].Value, "shards %d: attributes", shards)

				// The entries are the source's, not copies of them, which is what makes a shard free.
				out[0].ResourceLogs[0].ScopeLogs[0].Entries[0].Line = "rewritten"
				require.Equal(t, "rewritten", input.ResourceLogs[0].ScopeLogs[0].Entries[0].Line,
					"shards %d: a shard holds copies of the entries rather than the stream's own", shards)

				// Appending to a shard reallocates rather than reaching into the source. The scope has
				// to be one a boundary falls inside, or the shard holds it whole and an append runs off
				// the end where nothing would notice.
				split := buildGroupedStream(1, 1, 9)
				partial := shardStream(split, 3)
				require.Len(t, partial, 3)
				firstCount := partial[0].EntryCount()
				require.Positive(t, firstCount)
				require.Less(t, firstCount, split.EntryCount(), "a partial run")

				before := make([]push.Entry, len(split.ResourceLogs[0].ScopeLogs[0].Entries))
				copy(before, split.ResourceLogs[0].ScopeLogs[0].Entries)

				first := &partial[0].ResourceLogs[0].ScopeLogs[0]
				first.Entries = append(first.Entries, buildEntry("appended"))

				require.Equal(t, before, split.ResourceLogs[0].ScopeLogs[0].Entries,
					"appending to a shard reached into the source")
				require.Equal(t, before[firstCount].Line, partial[1].ResourceLogs[0].ScopeLogs[0].Entries[0].Line,
					"appending to a shard overwrote what the next one holds")
			}
		})
	}
}

func benchNestedStream(resources, scopes, entriesPerScope, sharedAttrs int) logproto.InternalStreamAdapter {
	attrs := func(prefix string, n int) []push.LabelAdapter {
		out := make([]push.LabelAdapter, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, push.LabelAdapter{
				Name:  fmt.Sprintf("%s.attr.%d", prefix, i),
				Value: strings.Repeat("v", 24),
			})
		}
		return out
	}

	nested := logproto.InternalStreamAdapter{Labels: `{app="checkout", env="prod"}`, Hash: 12345}

	for r := 0; r < resources; r++ {
		resourceAttrs := attrs(fmt.Sprintf("resource%d", r), sharedAttrs)
		resource := logproto.ResourceLogs{Attrs: resourceAttrs}

		for s := 0; s < scopes; s++ {
			scopeAttrs := attrs(fmt.Sprintf("scope%d.%d", r, s), sharedAttrs/4)
			scope := logproto.ScopeLogs{Attrs: scopeAttrs}

			for e := 0; e < entriesPerScope; e++ {
				entry := push.Entry{
					Timestamp: time.Unix(0, 0).UTC().Add(time.Duration(e%360) * time.Minute),
					Line:      strings.Repeat("x", 250),
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: strings.Repeat("a", 32)},
					},
				}
				scope.Entries = append(scope.Entries, entry)

			}
			resource.ScopeLogs = append(resource.ScopeLogs, scope)
		}
		nested.ResourceLogs = append(nested.ResourceLogs, resource)
	}
	return nested
}

var benchShapes = []struct {
	name        string
	sharedAttrs int
}{
	{"attributes shared by their group", 12},
	{"no attributes to share", 0},
}

func BenchmarkRateSharding(b *testing.B) {
	const shards = 8

	for _, shape := range benchShapes {
		nested := benchNestedStream(4, 2, 1000, shape.sharedAttrs)

		keys := make([]uint32, shards)

		b.Run(shape.name+"/nested", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				out := shardNestedForTest(nested, shardLabels, shards, 0)
				if len(out) != shards {
					b.Fatalf("got %d shards", len(out))
				}
				for j := range out {
					keys[j] = lokiring.TokenFor("tenant", out[j].Labels)
				}
			}
		})
	}
}

func expandedShardSize(stream logproto.InternalStreamAdapter) int {
	size := 0
	stream.EachGroup(func(resource, scope []logproto.LabelAdapter, entries []logproto.Entry) {
		size += (util.StructuredMetadataSize(resource)+util.StructuredMetadataSize(scope))*len(entries) + util.EntriesTotalSize(entries)
	})
	return size
}

func TestPackedSharding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sizes  []int
		shards int
		target uint64
		want   []int
	}{
		{"fill before splitting", []int{10, 10, 10, 10, 10, 10, 10}, 10, 30, []int{3, 3, 1}},
		{"front heavy", []int{90, 10, 10, 10, 10, 10, 10, 10, 10, 10}, 10, 100, []int{2, 8}},
		{"back heavy", []int{10, 10, 10, 10, 10, 10, 10, 10, 10, 90}, 10, 100, []int{9, 1}},
		{"oversized entry", []int{900, 1, 1, 1, 1}, 10, 100, []int{1, 4}},
		{"zero byte entries", []int{0, 0, 0, 0}, 10, 100, []int{4}},
		{"zero target distributes entries evenly", []int{90, 10, 10, 10}, 3, 0, []int{2, 1, 1}},
		{"zero shards", []int{10}, 0, 100, nil},
		{"empty", nil, 10, 100, nil},
		{"one shard", []int{100, 1, 1}, 1, 10, []int{3}},
		{"entry boundary needs extra output", []int{60, 60, 60}, 10, 100, []int{1, 1, 1}},
		{"recommended count caps fanout", []int{80, 80, 80, 80}, 3, 100, []int{1, 2, 1}},
		{"shard limit shares excess", []int{20, 20, 20, 20, 20, 20, 20, 20, 20, 20}, 3, 10, []int{3, 4, 3}},
		{"ceiling avoids a large tail", []int{128, 256, 192, 64, 256, 64, 64, 256}, 4, 128, []int{2, 2, 2, 2}},
		{"average equals target", []int{60, 60, 60, 60, 60}, 3, 100, []int{2, 1, 2}},
		{"target above max int", []int{1, 2, 3}, 10, ^uint64(0), []int{3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := make([]logproto.Entry, len(tc.sizes))
			for i, size := range tc.sizes {
				entries[i] = buildEntry(strings.Repeat("x", size))
			}
			input := *logproto.FromStream(logproto.Stream{Labels: shardLabels.String(), Entries: entries})
			out := packedShardsForTest(input, shardLabels, tc.shards, tc.shards-1, tc.target)
			require.Equal(t, tc.want, shardEntryCounts(out))
			if tc.shards <= 0 {
				return
			}
			requireShardsCarryTheStream(t, input, out)
		})
	}
	t.Run("shared and entry metadata contribute to size", func(t *testing.T) {
		input := *logproto.FromStream(logproto.Stream{Labels: shardLabels.String(), Entries: []logproto.Entry{buildEntry("x"), buildEntry("x"), buildEntry("x"), buildEntry("x")}})
		input.ResourceLogs[0].Attrs = buildNestedAttrs("r", strings.Repeat("r", 39))
		input.ResourceLogs[0].ScopeLogs[0].Attrs = buildNestedAttrs("s", strings.Repeat("s", 9))
		input.ResourceLogs[0].ScopeLogs[0].Entries[0].StructuredMetadata = buildNestedAttrs("e", strings.Repeat("e", 101))
		require.Equal(t, 306, expandedShardSize(input))
		require.Equal(t, []int{1, 3}, shardEntryCounts(packedShardsForTest(input, shardLabels, 4, 0, 153)))
	})
	t.Run("group boundaries and empty groups", func(t *testing.T) {
		input := buildGroupedStream(4, 3, 7)
		input.ResourceLogs = append([]logproto.ResourceLogs{{}}, input.ResourceLogs...)
		input.ResourceLogs[1].ScopeLogs = append([]logproto.ScopeLogs{{}}, input.ResourceLogs[1].ScopeLogs...)
		for count := 1; count <= input.EntryCount(); count++ {
			out := packNested(input, count, expandedShardSize(input), 100)
			labelShards(out, shardLabels, count+5, 3)
			requireShardsCarryTheStream(t, input, out)
			require.LessOrEqual(t, attrCopies(out), 4+12+2*(count-1))
		}
	})
}

func BenchmarkPackedRateSharding(b *testing.B) {
	for _, shape := range []struct {
		name                               string
		resources, scopes, entries, shards int
	}{
		{"small push", 1, 1, 100, 100}, {"medium push", 1, 1, 1000, 100}, {"large push", 1, 1, 8000, 8}, {"many groups", 100, 10, 1, 100},
	} {
		for _, attrs := range []int{0, 12} {
			input := benchNestedStream(shape.resources, shape.scopes, shape.entries, attrs)
			size := expandedShardSize(input)
			for _, target := range []int{0, 64 * 1024} {
				b.Run(fmt.Sprintf("%s/attrs=%d/target=%d", shape.name, attrs, target), func(b *testing.B) {
					tracker := NewShardTracker()
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						var out []logproto.InternalStreamAdapter
						if target > 0 {
							out = packNested(input, shape.shards, size, uint64(target))
						} else {
							out = shardNested(input, shape.shards)
						}
						start := tracker.Reserve("tenant", input.Hash, shape.shards, len(out), target > 0)
						labelShards(out, shardLabels, shape.shards, start)
					}
				})
			}
		}
	}
}

func FuzzPackedSharding(f *testing.F) {
	f.Add([]byte{0, 0, 255, 0, 0}, uint8(3))
	f.Add([]byte{255, 1, 1, 1, 1}, uint8(2))
	f.Add([]byte{0, 0, 0, 0, 0}, uint8(3))
	f.Fuzz(func(t *testing.T, data []byte, n uint8) {
		if len(data) == 0 || len(data) > 1024 {
			return
		}
		input := buildGroupedStream(1, 1, len(data))
		for i, size := range data {
			input.ResourceLogs[0].ScopeLogs[0].Entries[i].Line = strings.Repeat("x", int(size)*16)
		}
		count := min(int(n)+1, len(data))
		out := packedShardsForTest(input, shardLabels, count, 0, 1024)
		require.NotEmpty(t, out)
		require.LessOrEqual(t, len(out), count)
		requireShardsCarryTheStream(t, input, out)
	})
}

func TestPackedShardingPreservesNestedEntries(t *testing.T) {
	rng := rand.New(rand.NewPCG(17, 31))
	for trial := range 300 {
		input := logproto.InternalStreamAdapter{Labels: shardLabels.String(), Hash: 7}
		resourceCount := 1 + rng.IntN(5)
		for r := range resourceCount {
			resource := logproto.ResourceLogs{}
			if rng.IntN(2) == 0 {
				resource.Attrs = buildNestedAttrs("resource", strings.Repeat("r", rng.IntN(256)))
			}
			scopeCount := rng.IntN(6)
			for g := range scopeCount {
				scope := logproto.ScopeLogs{}
				if rng.IntN(2) == 0 {
					scope.Attrs = buildNestedAttrs("scope", strings.Repeat("s", rng.IntN(128)))
				}
				entryCount := rng.IntN(20)
				for e := range entryCount {
					size := rng.IntN(2048)
					if trial%10 == 0 {
						size = 0
					}
					entry := buildEntry(strings.Repeat("x", size))
					entry.Timestamp = time.Unix(int64(r), int64(g*20+e))
					if rng.IntN(3) == 0 {
						entry.StructuredMetadata = buildNestedAttrs("entry", strings.Repeat("m", rng.IntN(128)))
					}
					scope.Entries = append(scope.Entries, entry)
				}
				resource.ScopeLogs = append(resource.ScopeLogs, scope)
			}
			input.ResourceLogs = append(input.ResourceLogs, resource)
		}
		before, err := input.Marshal()
		require.NoError(t, err)
		entries, size := input.EntryCount(), expandedShardSize(input)
		totalShards := 1 + rng.IntN(37)
		out := packedShardsForTest(input, shardLabels, totalShards, rng.IntN(totalShards), uint64(1+rng.IntN(64*1024)))
		require.LessOrEqual(t, len(out), min(entries, totalShards))
		if entries > 0 {
			require.NotEmpty(t, out)
		}
		requireShardsCarryTheStream(t, input, out)
		after, err := input.Marshal()
		require.NoError(t, err)
		require.Equal(t, before, after, "sharding mutated input in trial %d", trial)
		gotSize := 0
		for _, shard := range out {
			gotSize += expandedShardSize(shard)
		}
		require.Equal(t, size, gotSize)
	}
}
