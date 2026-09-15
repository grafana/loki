package distributor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

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
	for s := range shards {
		shard := &shards[s]
		// The source's labels and hash, carried through unchanged. Naming a shard is the caller's,
		// so these are placeholders rather than what a named shard ends up with.
		require.Equal(t, source.Labels, shard.Labels, "shard %d labels", s)
		require.Equal(t, source.Hash, shard.Hash, "shard %d hash", s)
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

		wantShards int // how many come back, when the case is about that
	}{
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
			shards := shardNested(&input, tc.shards)

			// What to expect is built separately rather than read back off the input, which the
			// code under test holds pointers into and could have changed underneath us.
			source := tc.buildStream()
			require.Len(t, shards, tc.wantShards)
			requireShardsCarryTheStream(t, source, shards)

			// The entry counts are the ones round-robin would produce: base, with the remainder
			// spread over the leading shards.
			total := nestedEntryCount(&source)
			base, remainder := total/len(shards), total%len(shards)
			for s := range shards {
				want := base
				if s < remainder {
					want++
				}
				require.Equal(t, want, nestedEntryCount(&shards[s]), "shard %d entry count", s)
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
		shards := shardNested(&input, tc.shards)

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
			require.Nil(t, shardNested(&tc.stream, tc.shards),
				"nothing to divide, so the caller keeps the stream it has")
		})
	}
}

// A shard's entries are subslices of the source, as the flat path's time shards are, so a caller
// rewriting an entry rewrites it for both. Everything above the entries is the shard's own, and
// appending to a shard cannot reach the entries the next one holds.
func TestShardNestedSharesOnlyItsEntriesWithTheCaller(t *testing.T) {
	for _, shards := range []int{1, 2, 5} {
		input := buildGroupedStream(2, 2, 3)
		out := shardNested(&input, shards)
		require.NotEmpty(t, out)

		out[0].Labels = "rewritten"
		out[0].Hash = 99
		out[0].ResourceLogs[0].Attrs = buildNestedAttrs("service", "MUTATED")

		require.Equal(t, `{app="a"}`, input.Labels, "shards %d: labels", shards)
		require.Equal(t, uint64(7), input.Hash, "shards %d: hash", shards)
		require.Equal(t, "svc-0", input.ResourceLogs[0].Attrs[0].Value, "shards %d: attributes", shards)

		// Appending to a shard reallocates rather than reaching into the source. The scope has
		// to be one a boundary falls inside, or the shard holds it whole and an append runs off
		// the end where nothing would notice.
		split := buildGroupedStream(1, 1, 9)
		partial := shardNested(&split, 3)
		require.Len(t, partial, 3)
		require.Len(t, partial[0].ResourceLogs[0].ScopeLogs[0].Entries, 3, "a partial run")

		before := make([]push.Entry, len(split.ResourceLogs[0].ScopeLogs[0].Entries))
		copy(before, split.ResourceLogs[0].ScopeLogs[0].Entries)

		first := &partial[0].ResourceLogs[0].ScopeLogs[0]
		first.Entries = append(first.Entries, buildEntry("appended"))

		require.Equal(t, before, split.ResourceLogs[0].ScopeLogs[0].Entries,
			"appending to a shard reached into the source")
		require.Equal(t, "r0-s0-e3", partial[1].ResourceLogs[0].ScopeLogs[0].Entries[0].Line,
			"appending to a shard overwrote what the next one holds")
	}
}

// benchStreams builds one logical push in both shapes. The nested one keeps each resource's and
// scope's attributes on the group that owns them; the flat one carries them on every entry, which
// is what the OTLP parser produces today. With sharedAttrs of zero the two hold identical entries,
// which isolates what the sharding itself costs from what carrying the attributes costs.
func benchStreams(resources, scopes, entriesPerScope, sharedAttrs int) (logproto.InternalStreamAdapter, logproto.Stream) {
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
	flat := logproto.Stream{Labels: nested.Labels, Hash: nested.Hash}

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

				// The same entry as the flat path holds it: its own metadata, then the resource's
				// attributes, then the scope's, in the order otlp.go appends them.
				expanded := entry
				expanded.StructuredMetadata = make(push.LabelsAdapter, 0,
					len(entry.StructuredMetadata)+len(resourceAttrs)+len(scopeAttrs))
				expanded.StructuredMetadata = append(expanded.StructuredMetadata, entry.StructuredMetadata...)
				expanded.StructuredMetadata = append(expanded.StructuredMetadata, resourceAttrs...)
				expanded.StructuredMetadata = append(expanded.StructuredMetadata, scopeAttrs...)
				flat.Entries = append(flat.Entries, expanded)
			}
			resource.ScopeLogs = append(resource.ScopeLogs, scope)
		}
		nested.ResourceLogs = append(nested.ResourceLogs, resource)
	}
	return nested, flat
}

var benchShapes = []struct {
	name        string
	sharedAttrs int
}{
	// What an OTLP push looks like: attributes on the groups, copied onto every entry by the flat
	// path.
	{"attributes shared by their group", 12},
	// The same entries either way, which leaves only the difference between the two algorithms.
	{"no attributes to share", 0},
}

func BenchmarkRateSharding(b *testing.B) {
	const shards = 8

	for _, shape := range benchShapes {
		nested, flat := benchStreams(4, 2, 1000, shape.sharedAttrs)

		b.Run(shape.name+"/nested", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if out := shardNested(&nested, shards); len(out) != shards {
					b.Fatalf("got %d shards", len(out))
				}
			}
		})

		b.Run(shape.name+"/flat", func(b *testing.B) {
			d := &Distributor{logger: log.NewNopLogger(), shardTracker: NewShardTracker()}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if out := d.divideEntriesBetweenShards("tenant", shards, shardstreams.Config{}, flat, "policy"); len(out) != shards {
					b.Fatalf("got %d shards", len(out))
				}
			}
		})
	}
}
