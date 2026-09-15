package distributor

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

const timeShardLen = time.Hour

// shardBase is where the offsets below are measured from. The epoch, because it falls on a bucket
// boundary for any whole number of hours, so an offset of 30m is in the first bucket and 90m the
// second, rather than wherever an arbitrary instant would put them.
func shardBase() time.Time { return time.Unix(0, 0).UTC() }

// timeSpreadStream gives every group an entry at each offset, with the line naming where and when
// the entry sits so a shard can be checked without carrying the source around.
func timeSpreadStream(resources, scopesPer int, offsets []time.Duration) logproto.InternalStreamAdapter {
	stream := logproto.InternalStreamAdapter{Labels: `{app="a"}`, Hash: 7}
	for r := 0; r < resources; r++ {
		resource := logproto.ResourceLogs{Attrs: buildNestedAttrs("service", fmt.Sprintf("svc-%d", r))}
		for s := 0; s < scopesPer; s++ {
			scope := logproto.ScopeLogs{Attrs: buildNestedAttrs("scope", fmt.Sprintf("scope-%d-%d", r, s))}
			for _, off := range offsets {
				scope.Entries = append(scope.Entries, push.Entry{
					Timestamp: shardBase().Add(off),
					Line:      fmt.Sprintf("r%d-s%d-%s", r, s, off),
				})
			}
			resource.ScopeLogs = append(resource.ScopeLogs, scope)
		}
		stream.ResourceLogs = append(stream.ResourceLogs, resource)
	}
	return stream
}

func flatten(stream logproto.InternalStreamAdapter) logproto.Stream {
	flat := logproto.Stream{Labels: stream.Labels, Hash: stream.Hash}
	for i := range stream.ResourceLogs {
		for j := range stream.ResourceLogs[i].ScopeLogs {
			flat.Entries = append(flat.Entries, stream.ResourceLogs[i].ScopeLogs[j].Entries...)
		}
	}
	return flat
}

func linesOf(stream logproto.InternalStreamAdapter) []string {
	var out []string
	for i := range stream.ResourceLogs {
		for j := range stream.ResourceLogs[i].ScopeLogs {
			for _, e := range stream.ResourceLogs[i].ScopeLogs[j].Entries {
				out = append(out, e.Line)
			}
		}
	}
	sort.Strings(out)
	return out
}

func flatLinesOf(stream logproto.Stream) []string {
	out := make([]string, 0, len(stream.Entries))
	for _, e := range stream.Entries {
		out = append(out, e.Line)
	}
	sort.Strings(out)
	return out
}

// requireTimeShardsAreWellFormed asserts what has to hold whatever the entries look like: a shard
// names the stream, holds no empty group, opens no more resources or scopes than the source has
// with those attributes, holds only entries the matching source group holds, keeps a bucketed
// shard's entries inside its window and the trailing shard's at or after the cutoff with that
// shard last, and between them holds every entry as often as the source does.
//
// Groups are matched by their attributes rather than by position, because bucketing reorders
// entries, and entries are counted by value rather than identified by line, because a stream may
// carry the same line more than once. Two source groups carrying identical attributes are
// indistinguishable in the output, so the checks below bound how many of each a shard may open
// rather than pretending to tell them apart.
func requireTimeShardsAreWellFormed(t *testing.T, source logproto.InternalStreamAdapter, shards []nestedTimeShard, ignoreLogsFrom time.Time) {
	t.Helper()

	entryKey := func(e push.Entry) string {
		return fmt.Sprint(e.Timestamp.UnixNano(), "\x00", e.Line, "\x00", e.StructuredMetadata)
	}
	type groupKey struct{ resource, scope string }

	sourceResources := map[string]int{} // how many resources carry these attributes
	sourceScopes := map[groupKey]int{}  // how many groups carry this pair of attributes
	sourceEntries := map[groupKey]map[string]int{}
	want := map[string]int{}
	for i := range source.ResourceLogs {
		res := &source.ResourceLogs[i]
		sourceResources[fmt.Sprint(res.Attrs)]++
		for j := range res.ScopeLogs {
			scope := &res.ScopeLogs[j]
			gk := groupKey{fmt.Sprint(res.Attrs), fmt.Sprint(scope.Attrs)}
			sourceScopes[gk]++
			if sourceEntries[gk] == nil {
				sourceEntries[gk] = map[string]int{}
			}
			for _, entry := range scope.Entries {
				sourceEntries[gk][entryKey(entry)]++
				want[entryKey(entry)]++
			}
		}
	}

	seen := map[string]int{}
	for s := range shards {
		shard := &shards[s]
		require.Equal(t, source.Labels, shard.stream.Labels, "shard %d labels", s)
		require.Equal(t, source.Hash, shard.stream.Hash, "shard %d hash", s)
		require.NotEmpty(t, shard.stream.ResourceLogs, "shard %d holds no resources", s)
		if shard.recent() {
			require.Equal(t, len(shards)-1, s, "the trailing shard comes last")
		}

		// A resource holding several scopes is written once per shard, and a group once per
		// shard, so a shard cannot hold more of either than the source has to give it.
		shardResources, shardScopes := map[string]int{}, map[groupKey]int{}

		for i := range shard.stream.ResourceLogs {
			res := &shard.stream.ResourceLogs[i]
			require.NotEmpty(t, res.ScopeLogs, "shard %d holds a resource with no scopes", s)

			resourceAttrs := fmt.Sprint(res.Attrs)
			shardResources[resourceAttrs]++
			require.LessOrEqual(t, shardResources[resourceAttrs], sourceResources[resourceAttrs],
				"shard %d opens more resources carrying %s than the source has", s, resourceAttrs)

			for j := range res.ScopeLogs {
				scope := &res.ScopeLogs[j]
				require.NotEmpty(t, scope.Entries, "shard %d holds a scope with no entries", s)

				gk := groupKey{resourceAttrs, fmt.Sprint(scope.Attrs)}
				shardScopes[gk]++
				require.LessOrEqual(t, shardScopes[gk], sourceScopes[gk],
					"shard %d opens more scopes carrying %v than the source has", s, gk)

				for _, entry := range scope.Entries {
					require.NotZero(t, sourceEntries[gk][entryKey(entry)],
						"shard %d holds %q under attributes the source never had it under", s, entry.Line)
					seen[entryKey(entry)]++

					if shard.recent() {
						require.False(t, entry.Timestamp.Before(ignoreLogsFrom),
							"shard %d is the trailing one, so %s is not too recent to bucket", s, entry.Line)
						continue
					}
					require.True(t,
						!entry.Timestamp.Before(shard.start) && entry.Timestamp.Before(shard.end),
						"shard %d [%s, %s) holds an entry at %s", s, shard.start, shard.end, entry.Timestamp)
				}
			}
		}
	}

	require.Equal(t, want, seen, "every entry appears as often as the source holds it")
}

// The buckets, and what lands in them, have to be what the flat path produces. Only the shape of
// each shard differs.
//
// The stream is built by the case rather than described by it, so a case can hold timestamps no
// offset from a base can express. It is a func because the assertions need a copy of the source the
// code under test never had a pointer into.
func TestTimeShardNestedMatchesTheFlatPath(t *testing.T) {
	spread := func(resources, scopes int, offsets ...time.Duration) func() logproto.InternalStreamAdapter {
		return func() logproto.InternalStreamAdapter { return timeSpreadStream(resources, scopes, offsets) }
	}
	oneGroup := func(entries ...push.Entry) func() logproto.InternalStreamAdapter {
		return func() logproto.InternalStreamAdapter {
			return logproto.InternalStreamAdapter{Labels: `{app="a"}`, Hash: 7,
				ResourceLogs: []logproto.ResourceLogs{{
					Attrs: buildNestedAttrs("service", "svc-0"),
					ScopeLogs: []logproto.ScopeLogs{{
						Attrs:   buildNestedAttrs("scope", "scope-0-0"),
						Entries: entries,
					}},
				}}}
		}
	}
	at := func(d time.Duration, line string) push.Entry {
		return push.Entry{Timestamp: shardBase().Add(d), Line: line}
	}

	for _, tc := range []struct {
		name           string
		stream         func() logproto.InternalStreamAdapter
		shardLen       time.Duration
		ignoreLogsFrom time.Time
	}{
		{
			name:           "entries spread over several buckets",
			stream:         spread(3, 2, 0, 30*time.Minute, 90*time.Minute, 3*time.Hour),
			shardLen:       timeShardLen,
			ignoreLogsFrom: shardBase().Add(5 * time.Hour),
		},
		{
			name:           "some entries too recent to bucket",
			stream:         spread(2, 2, 0, 90*time.Minute, 4*time.Hour+30*time.Minute, 5*time.Hour),
			shardLen:       timeShardLen,
			ignoreLogsFrom: shardBase().Add(4 * time.Hour),
		},
		{
			name:           "one bucket only",
			stream:         spread(4, 1, 0, 10*time.Minute, 20*time.Minute),
			shardLen:       timeShardLen,
			ignoreLogsFrom: shardBase().Add(3 * time.Hour),
		},
		{
			name:           "a bucket straddling the recent cutoff",
			stream:         spread(2, 1, 0, 2*time.Hour+15*time.Minute, 2*time.Hour+45*time.Minute),
			shardLen:       timeShardLen,
			ignoreLogsFrom: shardBase().Add(2*time.Hour + 30*time.Minute),
		},
		{
			name:           "several entries in one bucket, none recent",
			stream:         spread(1, 3, 5*time.Minute, 15*time.Minute, 25*time.Minute),
			shardLen:       timeShardLen,
			ignoreLogsFrom: shardBase().Add(6 * time.Hour),
		},
		{
			// An entry exactly on the cutoff. The flat path trails it rather than bucketing it.
			name:           "an entry exactly on the recent cutoff",
			stream:         spread(2, 1, 0, 90*time.Minute, 2*time.Hour),
			shardLen:       timeShardLen,
			ignoreLogsFrom: shardBase().Add(2 * time.Hour),
		},
		{
			// The same boundary one level up: the whole-stream shortcut turns on the oldest entry.
			name:           "every entry at or after the cutoff, the oldest exactly on it",
			stream:         spread(2, 1, 2*time.Hour, 3*time.Hour),
			shardLen:       timeShardLen,
			ignoreLogsFrom: shardBase().Add(2 * time.Hour),
		},
		{
			// Not in time order. A bucket takes a group's entries as one run because groups are
			// walked one at a time, not because anything is sorted.
			name:           "entries given out of time order",
			stream:         spread(3, 2, 70*time.Minute, 0, 2*time.Hour, 45*time.Minute),
			shardLen:       timeShardLen,
			ignoreLogsFrom: shardBase().Add(6 * time.Hour),
		},
		{
			// The same entry under two different resources, which nothing about the format
			// forbids and which the checks must not mistake for one entry duplicated.
			name: "an identical entry in two groups",
			stream: func() logproto.InternalStreamAdapter {
				group := func(service string) logproto.ResourceLogs {
					return logproto.ResourceLogs{
						Attrs: buildNestedAttrs("service", service),
						ScopeLogs: []logproto.ScopeLogs{{
							Attrs: buildNestedAttrs("scope", "only"),
							Entries: []push.Entry{
								at(10*time.Minute, "shared-line"),
								at(90*time.Minute, "shared-line"),
							},
						}},
					}
				}
				return logproto.InternalStreamAdapter{Labels: `{app="a"}`, Hash: 7,
					ResourceLogs: []logproto.ResourceLogs{group("svc-0"), group("svc-1")}}
			},
			shardLen:       timeShardLen,
			ignoreLogsFrom: shardBase().Add(5 * time.Hour),
		},
		{
			// The same line at the same instant, twice. Real traffic repeats lines, and matching
			// entries back to their group by value rather than by line is what lets it.
			name: "identical entries in one group",
			stream: oneGroup(
				at(10*time.Minute, "repeated"),
				at(10*time.Minute, "repeated"),
				at(90*time.Minute, "distinct"),
			),
			shardLen:       timeShardLen,
			ignoreLogsFrom: shardBase().Add(5 * time.Hour),
		},
		{
			// Finer than a second, which the bucket key has to keep whole: truncated to seconds,
			// two windows would merge and a shard would advertise one excluding entries it holds.
			name: "a shard length finer than a second",
			stream: oneGroup(
				at(100*time.Second, "at-000ms"),
				at(100*time.Second+600*time.Millisecond, "at-600ms"),
				at(100*time.Second+1200*time.Millisecond, "at-1200ms"),
			),
			shardLen:       500 * time.Millisecond,
			ignoreLogsFrom: shardBase().Add(time.Hour),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nested := tc.stream()
			source := tc.stream()
			got, ok := timeShardNested(&nested, tc.shardLen, tc.ignoreLogsFrom)
			requireTimeShardsAreWellFormed(t, source, got, tc.ignoreLogsFrom)

			want, wantOK := shardStreamByTime(flatten(tc.stream()), labels.FromStrings("app", "a"),
				tc.shardLen, tc.ignoreLogsFrom)

			require.Equal(t, wantOK, ok, "whether there was anything to shard")
			require.Len(t, got, len(want), "number of time shards")

			// Oldest bucket first with the trailing shard last, as the flat path returns them.
			// Asserted here rather than left to the comparison below, which only notices a
			// misordering when the map iteration happens to produce one.
			for i := range got {
				if got[i].recent() {
					require.Equal(t, len(got)-1, i, "the trailing shard comes last")
					continue
				}
				if i > 0 {
					require.True(t, got[i-1].start.Before(got[i].start),
						"shard %d starts before shard %d", i-1, i)
				}
			}

			for i := range want {
				require.Equal(t, flatLinesOf(want[i].Stream), linesOf(got[i].stream),
					"shard %d carries the same entries", i)
				require.Equal(t, want[i].linesTotalLen, got[i].linesTotalLen,
					"shard %d line length", i)

				// The flat path names each bucket in the label; this one reports the window.
				if got[i].recent() {
					require.NotContains(t, want[i].Stream.Labels, "__time_shard__",
						"shard %d is the trailing one", i)
					continue
				}
				require.Contains(t, want[i].Stream.Labels,
					fmt.Sprintf(`__time_shard__="%d_%d"`, got[i].start.Unix(), got[i].end.Unix()),
					"shard %d window", i)
			}
		})
	}
}

func TestTimeShardNestedLeavesRecentStreamsAlone(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stream logproto.InternalStreamAdapter
		ignore time.Duration
	}{
		{
			name:   "every entry newer than the cutoff",
			stream: timeSpreadStream(2, 2, []time.Duration{3 * time.Hour, 4 * time.Hour}),
			ignore: 2 * time.Hour,
		},
		{
			name:   "no entries at all",
			stream: timeSpreadStream(2, 2, nil),
			ignore: 2 * time.Hour,
		},
		{
			name:   "no groups at all",
			stream: logproto.InternalStreamAdapter{Labels: `{app="a"}`, Hash: 7},
			ignore: 2 * time.Hour,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.stream
			shards, ok := timeShardNested(&source, timeShardLen, shardBase().Add(tc.ignore))
			require.False(t, ok, "nothing old enough to bucket")
			require.Nil(t, shards)
		})
	}
}

// Time shards are fed to rate sharding in production, so the invariants have to survive the pair.
func TestTimeShardsCanThemselvesBeRateSharded(t *testing.T) {
	ignoreLogsFrom := shardBase().Add(6 * time.Hour)
	offsets := []time.Duration{0, 30 * time.Minute, 90 * time.Minute, 2 * time.Hour}
	nested := timeSpreadStream(3, 2, offsets)

	timeShards, ok := timeShardNested(&nested, timeShardLen, ignoreLogsFrom)
	require.True(t, ok)
	require.NotEmpty(t, timeShards)

	lines := map[string]int{}
	for i := range timeShards {
		for _, shards := range [][]logproto.InternalStreamAdapter{
			shardNested(&timeShards[i].stream, 1),
			shardNested(&timeShards[i].stream, 3),
		} {
			require.NotEmpty(t, shards)
			requireShardsCarryTheStream(t, timeShards[i].stream, shards)
		}
		for _, shard := range shardNested(&timeShards[i].stream, 3) {
			for j := range shard.ResourceLogs {
				for k := range shard.ResourceLogs[j].ScopeLogs {
					for _, entry := range shard.ResourceLogs[j].ScopeLogs[k].Entries {
						lines[entry.Line]++
					}
				}
			}
		}
	}

	require.Len(t, lines, 3*2*len(offsets), "every entry survives both stages")
	for line, n := range lines {
		require.Equal(t, 1, n, "%s appears more than once across the time shards", line)
	}
}

func BenchmarkTimeSharding(b *testing.B) {
	const shardLen = time.Hour
	ignoreLogsFrom := time.Unix(0, 0).UTC().Add(1000 * time.Hour)

	for _, shape := range benchShapes {
		nested, flat := benchStreams(4, 2, 1000, shape.sharedAttrs)
		lbls := labels.FromStrings("app", "checkout", "env", "prod")

		b.Run(shape.name+"/nested", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, ok := timeShardNested(&nested, shardLen, ignoreLogsFrom); !ok {
					b.Fatal("nothing sharded")
				}
			}
		})

		b.Run(shape.name+"/flat", func(b *testing.B) {
			// shardStreamByTime sorts the caller's entries in place, so each run is handed an
			// unsorted copy. Restoring it is not what is being measured.
			entries := make([]push.Entry, len(flat.Entries))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				copy(entries, flat.Entries)
				subject := logproto.Stream{Labels: flat.Labels, Hash: flat.Hash, Entries: entries}
				b.StartTimer()

				if _, ok := shardStreamByTime(subject, lbls, shardLen, ignoreLogsFrom); !ok {
					b.Fatal("nothing sharded")
				}
			}
		})
	}
}
