package chunkenc

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// sharedHashOf is a stand in for the hash the distributor will put on the wire. Any stable
// hash of the shared set will do, MemChunk only uses it as a cache key.
func sharedHashOf(shared push.LabelsAdapter) uint64 {
	h := xxhash.New()
	for _, l := range shared {
		_, _ = h.WriteString(l.Name)
		_, _ = h.WriteString(l.Value)
	}
	return h.Sum64()
}

// effectiveSM is the pre-merged structured metadata a producer expanding the shared sets would
// have put on an entry: the shared list, itself the stream's resource attributes followed by its
// scope ones, and then the entry's own metadata. Own comes last because the read path keeps the
// last pair for a repeated name, which is what gives own > scope > resource. It is the baseline
// the deferred path is compared against, both for the bytes stored and for what is read back.
func effectiveSM(shared, own push.LabelsAdapter) push.LabelsAdapter {
	expanded := make(push.LabelsAdapter, 0, len(shared)+len(own))
	expanded = append(expanded, shared...)
	expanded = append(expanded, own...)
	return expanded
}

// materializedSM is what the read path gives back for a set of structured metadata: sorted by
// label name, with pairs repeating a label name collapsed to the last one by the pipeline's
// labels builder.
func materializedSM(sm push.LabelsAdapter) push.LabelsAdapter {
	sorted := slices.Clone(sm)
	slices.SortStableFunc(sorted, func(a, b logproto.LabelAdapter) int { return strings.Compare(a.Name, b.Name) })

	deduped := sorted[:0]
	for i, l := range sorted {
		if i+1 < len(sorted) && sorted[i+1].Name == l.Name {
			continue
		}
		deduped = append(deduped, l)
	}

	return deduped
}

type sharedEntry struct {
	ts     int64
	line   string
	own    push.LabelsAdapter
	shared push.LabelsAdapter
}

func readEntries(t *testing.T, c *MemChunk) []push.Entry {
	t.Helper()

	it, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, noopStreamPipeline)
	require.NoError(t, err)
	defer it.Close()

	var out []push.Entry
	for it.Next() {
		e := it.At()
		out = append(out, push.Entry{
			Timestamp:          e.Timestamp,
			Line:               e.Line,
			StructuredMetadata: slices.Clone(e.StructuredMetadata),
		})
	}
	require.NoError(t, it.Err())

	return out
}

var sharedMetadataCases = []struct {
	name    string
	entries []sharedEntry
}{
	{
		name: "single shared set, entries with and without own metadata",
		entries: []sharedEntry{
			{1, "lineA", nil, push.LabelsAdapter{{Name: "service_name", Value: "svc"}, {Name: "zone", Value: "eu"}}},
			{2, "lineB", push.LabelsAdapter{{Name: "traceID", Value: "abc"}}, push.LabelsAdapter{{Name: "service_name", Value: "svc"}, {Name: "zone", Value: "eu"}}},
			{3, "lineC", push.LabelsAdapter{{Name: "traceID", Value: "def"}}, push.LabelsAdapter{{Name: "service_name", Value: "svc"}, {Name: "zone", Value: "eu"}}},
		},
	},
	{
		name: "two shared sets interleaved in one chunk",
		entries: []sharedEntry{
			{1, "lineA", push.LabelsAdapter{{Name: "traceID", Value: "abc"}}, push.LabelsAdapter{{Name: "service_name", Value: "svc1"}, {Name: "zone", Value: "eu"}}},
			{2, "lineB", push.LabelsAdapter{{Name: "traceID", Value: "def"}}, push.LabelsAdapter{{Name: "cluster", Value: "c2"}, {Name: "service_name", Value: "svc2"}}},
			{3, "lineC", nil, push.LabelsAdapter{{Name: "service_name", Value: "svc1"}, {Name: "zone", Value: "eu"}}},
			{4, "lineD", push.LabelsAdapter{{Name: "user", Value: "u1"}}, push.LabelsAdapter{{Name: "cluster", Value: "c2"}, {Name: "service_name", Value: "svc2"}}},
		},
	},
	{
		name: "own metadata sorting around, before and after the shared set",
		entries: []sharedEntry{
			{1, "lineA", push.LabelsAdapter{{Name: "zzz", Value: "1"}, {Name: "aaa", Value: "2"}}, push.LabelsAdapter{{Name: "mmm", Value: "3"}}},
			{2, "lineB", push.LabelsAdapter{{Name: "nnn", Value: "4"}}, push.LabelsAdapter{{Name: "mmm", Value: "3"}}},
		},
	},
	{
		name: "unsorted shared set",
		entries: []sharedEntry{
			{1, "lineA", push.LabelsAdapter{{Name: "traceID", Value: "abc"}}, push.LabelsAdapter{{Name: "zone", Value: "eu"}, {Name: "cluster", Value: "c1"}, {Name: "service_name", Value: "svc"}}},
			{2, "lineB", nil, push.LabelsAdapter{{Name: "zone", Value: "eu"}, {Name: "cluster", Value: "c1"}, {Name: "service_name", Value: "svc"}}},
		},
	},
	{
		name: "empty shared set",
		entries: []sharedEntry{
			{1, "lineA", push.LabelsAdapter{{Name: "traceID", Value: "abc"}}, nil},
			{2, "lineB", nil, nil},
		},
	},
	{
		name: "same line and metadata at the same timestamp is still deduplicated",
		entries: []sharedEntry{
			{1, "lineA", push.LabelsAdapter{{Name: "traceID", Value: "abc"}}, push.LabelsAdapter{{Name: "zone", Value: "eu"}}},
			{1, "lineA", push.LabelsAdapter{{Name: "traceID", Value: "abc"}}, push.LabelsAdapter{{Name: "zone", Value: "eu"}}},
		},
	},
	{
		name: "shared set larger than the insertion sort threshold",
		entries: func() []sharedEntry {
			shared := make(push.LabelsAdapter, 0, 20)
			for i := 0; i < 20; i++ {
				shared = append(shared, logproto.LabelAdapter{
					Name:  fmt.Sprintf("resource_attribute_%02d", i),
					Value: fmt.Sprintf("value-%02d", i),
				})
			}
			entries := make([]sharedEntry, 0, 5)
			for i := 0; i < 5; i++ {
				entries = append(entries, sharedEntry{
					ts:     int64(i + 1),
					line:   fmt.Sprintf("line%d", i),
					own:    push.LabelsAdapter{{Name: "traceID", Value: fmt.Sprintf("trace-%d", i)}},
					shared: shared,
				})
			}
			return entries
		}(),
	},
	{
		name: "shared values only, no own metadata at all",
		entries: []sharedEntry{
			{1, "lineA", nil, push.LabelsAdapter{{Name: "zone", Value: "eu"}}},
			{2, "lineB", nil, push.LabelsAdapter{{Name: "zone", Value: "eu"}}},
			{3, "lineC", nil, push.LabelsAdapter{{Name: "zone", Value: "us"}}},
		},
	},
}

// TestAppendWithSharedStructuredMetadata is the guardrail of the deferred OTLP attribute
// expansion: a chunk built from entries carrying only their own structured metadata plus a
// shared set must be byte for byte identical to a chunk built from entries carrying the
// expanded union, and must read back the same entries.
//
// That guarantee only holds when an entry's own structured metadata and the shared set share
// no label name, which is what every case below is built on. Collisions are covered by
// TestAppendWithSharedStructuredMetadataNameCollision instead: the two paths deliberately
// disagree there, and only the deferred one is deterministic.
func TestAppendWithSharedStructuredMetadata(t *testing.T) {
	for _, enc := range []compression.Codec{compression.None, compression.Snappy, compression.Zstd} {
		for _, tc := range sharedMetadataCases {
			t.Run(fmt.Sprintf("%s/%s", enc, tc.name), func(t *testing.T) {
				expanded := newMemChunkWithFormat(ChunkFormatV4, enc, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, testTargetSize)
				deferred := newMemChunkWithFormat(ChunkFormatV4, enc, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, testTargetSize)

				for _, e := range tc.entries {
					dupExpanded, err := expanded.Append(logprotoEntryWithStructuredMetadata(e.ts, e.line, effectiveSM(e.shared, e.own)))
					require.NoError(t, err)

					dupDeferred, err := deferred.AppendWithSharedStructuredMetadata(
						logprotoEntryWithStructuredMetadata(e.ts, e.line, slices.Clone(e.own)),
						sharedHashOf(e.shared),
						e.shared,
					)
					require.NoError(t, err)
					require.Equal(t, dupExpanded, dupDeferred)
				}

				// Head block only reads, before anything is cut.
				require.Equal(t, readEntries(t, expanded), readEntries(t, deferred))

				for i, e := range readEntries(t, deferred) {
					expected := materializedSM(effectiveSM(tc.entries[i].shared, tc.entries[i].own))
					if len(expected) == 0 {
						require.Empty(t, e.StructuredMetadata)
						continue
					}
					require.Equal(t, expected, e.StructuredMetadata)
				}

				require.NoError(t, expanded.Close())
				require.NoError(t, deferred.Close())

				expandedBytes, err := expanded.Bytes()
				require.NoError(t, err)
				deferredBytes, err := deferred.Bytes()
				require.NoError(t, err)
				require.Equal(t, expandedBytes, deferredBytes)

				// Post serialization reads.
				fromBytes, err := NewByteChunk(deferredBytes, testBlockSize, testTargetSize)
				require.NoError(t, err)
				require.Equal(t, readEntries(t, expanded), readEntries(t, fromBytes))
			})
		}
	}
}

// headBlockSymbolPairs returns the structured metadata symbols the head block holds for every
// entry, resolved back to name/value pairs and in the exact order they were stored in.
func headBlockSymbolPairs(t *testing.T, c *MemChunk) [][2]string {
	t.Helper()

	hb, ok := c.head.(*unorderedHeadBlock)
	require.True(t, ok, "test needs an unordered head block")

	var out [][2]string
	for _, e := range hb.rt.Query(interval{0, math.MaxInt64}) {
		for _, entry := range e.(*nsEntries).entries {
			for _, sym := range entry.structuredMetadataSymbols {
				out = append(out, [2]string{c.symbolizer.lookup(sym.Name), c.symbolizer.lookup(sym.Value)})
			}
		}
	}

	return out
}

// TestAppendWithSharedStructuredMetadataNameCollision pins down the tie break applied when an
// entry's own structured metadata repeats a label name of the shared set: both pairs are kept,
// the shared one is stored first and the entry's own one last, so the read path, which
// collapses pairs repeating a label name down to the last one, resolves the collision in
// favour of the entry.
//
// The pre-merged path is intentionally not compared against here: it sorts the union with an
// unstable sort, so past the insertion sort threshold it is not even self consistent.
func TestAppendWithSharedStructuredMetadataNameCollision(t *testing.T) {
	// A shared set well past the 12 element threshold below which the pre-merged path's sort
	// happens to be stable, so that this exercises the pdqsort branch the old path was
	// nondeterministic on.
	largeShared := make(push.LabelsAdapter, 0, 20)
	for i := 0; i < 20; i++ {
		largeShared = append(largeShared, logproto.LabelAdapter{
			Name:  fmt.Sprintf("attr_%02d", i),
			Value: fmt.Sprintf("shared-%02d", i),
		})
	}

	for _, tc := range []struct {
		name string
		own  push.LabelsAdapter
		// Ordered name/value pairs the chunk is expected to store for the entry.
		expectedStored [][2]string
		// What the read path is expected to hand back once duplicate names are collapsed.
		expectedRead push.LabelsAdapter
	}{
		{
			name: "single collision",
			own:  push.LabelsAdapter{{Name: "attr_05", Value: "own"}},
			expectedStored: func() [][2]string {
				out := make([][2]string, 0, 21)
				for i, l := range largeShared {
					out = append(out, [2]string{l.Name, l.Value})
					if i == 5 {
						// Own comes right after the shared pair it collides with.
						out = append(out, [2]string{"attr_05", "own"})
					}
				}
				return out
			}(),
		},
		{
			name: "several collisions plus a non colliding pair",
			own: push.LabelsAdapter{
				{Name: "attr_00", Value: "own"},
				{Name: "attr_19", Value: "own"},
				{Name: "traceID", Value: "abc"},
			},
			expectedStored: func() [][2]string {
				out := make([][2]string, 0, 23)
				for i, l := range largeShared {
					out = append(out, [2]string{l.Name, l.Value})
					if i == 0 || i == 19 {
						out = append(out, [2]string{l.Name, "own"})
					}
				}
				// traceID sorts after every attr_NN.
				return append(out, [2]string{"traceID", "abc"})
			}(),
		},
		{
			name: "exact name and value duplicate is kept twice",
			own:  push.LabelsAdapter{{Name: "attr_07", Value: "shared-07"}},
			expectedStored: func() [][2]string {
				out := make([][2]string, 0, 21)
				for i, l := range largeShared {
					out = append(out, [2]string{l.Name, l.Value})
					if i == 7 {
						out = append(out, [2]string{"attr_07", "shared-07"})
					}
				}
				return out
			}(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Two entries so that both the interning path (first entry, cached == nil) and the
			// cached path (second entry) go through the same tie break.
			for _, entryIdx := range []int{0, 1} {
				c := newMemChunkWithFormat(ChunkFormatV4, compression.None, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, testTargetSize)

				for i := 0; i <= entryIdx; i++ {
					_, err := c.AppendWithSharedStructuredMetadata(
						logprotoEntryWithStructuredMetadata(int64(i+1), fmt.Sprintf("line%d", i), slices.Clone(tc.own)),
						sharedHashOf(largeShared),
						largeShared,
					)
					require.NoError(t, err)
				}

				stored := headBlockSymbolPairs(t, c)
				// Only look at the entry under test, every entry stores the same pairs.
				require.Equal(t, tc.expectedStored, stored[entryIdx*len(tc.expectedStored):])

				// And what the read path resolves them to: last pair wins for a repeated name,
				// which is always the entry's own one.
				expectedRead := materializedSM(storedToLabelsAdapter(tc.expectedStored))
				read := readEntries(t, c)
				require.Equal(t, expectedRead, read[entryIdx].StructuredMetadata)

				for _, l := range tc.own {
					require.Equal(t, l.Value, valueOf(read[entryIdx].StructuredMetadata, l.Name), "the entry's own %q must win the collision", l.Name)
				}
			}
		})
	}
}

// indexOfPair returns the position of the first name/value pair in stored, or -1.
func indexOfPair(stored [][2]string, name, value string) int {
	for i, p := range stored {
		if p[0] == name && p[1] == value {
			return i
		}
	}
	return -1
}

// TestAppendWithSharedStructuredMetadataSharedSetTieOrder pins the contract that
// push.CombinedShared depends on: within the single shared list the chunk is handed, two pairs
// carrying the same label name must come out of the sorted merge in the order they went in.
//
// That list is a stream's resource attributes followed by its scope attributes, and the read
// path keeps the last pair for a repeated name. Preserving the given order is therefore the
// only thing making a resource/scope collision resolve to the scope value, and the entry's own
// value win over both, i.e. the own > scope > resource precedence OpenTelemetry prescribes. A
// merge that reordered equal names within the shared list would silently invert it to
// resource > scope, with nothing else in the stack noticing.
func TestAppendWithSharedStructuredMetadataSharedSetTieOrder(t *testing.T) {
	// Built descending and long enough that sorting it goes past the insertion sort threshold,
	// into the pdqsort branch where an unstable sort would be free to swap the equal names.
	largeResource := make(push.LabelsAdapter, 0, 20)
	largeScope := make(push.LabelsAdapter, 0, 20)
	for i := 19; i >= 0; i-- {
		largeResource = append(largeResource, logproto.LabelAdapter{Name: fmt.Sprintf("attr_%02d", i), Value: "resource"})
		largeScope = append(largeScope, logproto.LabelAdapter{Name: fmt.Sprintf("attr_%02d", i), Value: "scope"})
	}

	for _, tc := range []struct {
		name            string
		resource, scope push.LabelsAdapter
		own             push.LabelsAdapter
		// Names present in more than one of the three, whose stored order is the contract.
		colliding []string
		// What the read path must resolve each name to.
		expectedRead map[string]string
	}{
		{
			name:         "resource and scope collide, already sorted",
			resource:     push.LabelsAdapter{{Name: "attr", Value: "resource"}},
			scope:        push.LabelsAdapter{{Name: "attr", Value: "scope"}},
			colliding:    []string{"attr"},
			expectedRead: map[string]string{"attr": "scope"},
		},
		{
			name:         "resource and scope collide, own wins over both",
			resource:     push.LabelsAdapter{{Name: "attr", Value: "resource"}},
			scope:        push.LabelsAdapter{{Name: "attr", Value: "scope"}},
			own:          push.LabelsAdapter{{Name: "attr", Value: "own"}},
			colliding:    []string{"attr"},
			expectedRead: map[string]string{"attr": "own"},
		},
		{
			name: "combined list is unsorted, forcing the stable sort",
			resource: push.LabelsAdapter{
				{Name: "zone", Value: "resource"},
				{Name: "attr", Value: "resource"},
			},
			scope: push.LabelsAdapter{
				{Name: "attr", Value: "scope"},
				{Name: "app", Value: "scope"},
			},
			colliding: []string{"attr"},
			expectedRead: map[string]string{
				"attr": "scope",
				"zone": "resource",
				"app":  "scope",
			},
		},
		{
			name:      "every name collides, past the insertion sort threshold",
			resource:  largeResource,
			scope:     largeScope,
			colliding: []string{"attr_00", "attr_07", "attr_19"},
			expectedRead: func() map[string]string {
				want := make(map[string]string, 20)
				for i := 0; i < 20; i++ {
					want[fmt.Sprintf("attr_%02d", i)] = "scope"
				}
				return want
			}(),
		},
		{
			name:      "every name collides and own collides too",
			resource:  largeResource,
			scope:     largeScope,
			own:       push.LabelsAdapter{{Name: "attr_07", Value: "own"}},
			colliding: []string{"attr_00", "attr_07", "attr_19"},
			expectedRead: func() map[string]string {
				want := make(map[string]string, 20)
				for i := 0; i < 20; i++ {
					want[fmt.Sprintf("attr_%02d", i)] = "scope"
				}
				want["attr_07"] = "own"
				return want
			}(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			combined := push.CombinedShared(tc.resource, tc.scope)

			// Two entries so that both the interning path (first entry, cached == nil) and the
			// cached path (second entry) go through the same tie break.
			for _, entryIdx := range []int{0, 1} {
				c := newMemChunkWithFormat(ChunkFormatV4, compression.None, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, testTargetSize)

				for i := 0; i <= entryIdx; i++ {
					_, err := c.AppendWithSharedStructuredMetadata(
						logprotoEntryWithStructuredMetadata(int64(i+1), fmt.Sprintf("line%d", i), slices.Clone(tc.own)),
						sharedHashOf(combined),
						combined,
					)
					require.NoError(t, err)
				}

				stored := headBlockSymbolPairs(t, c)
				perEntry := len(stored) / (entryIdx + 1)
				entryStored := stored[entryIdx*perEntry:]

				for _, name := range tc.colliding {
					resourceAt := indexOfPair(entryStored, name, "resource")
					scopeAt := indexOfPair(entryStored, name, "scope")
					require.NotEqual(t, -1, resourceAt, "resource pair for %q must be stored", name)
					require.NotEqual(t, -1, scopeAt, "scope pair for %q must be stored", name)
					require.Less(t, resourceAt, scopeAt,
						"the resource pair for %q must be stored before the scope pair, or scope stops winning at read time", name)

					if ownAt := indexOfPair(entryStored, name, "own"); ownAt != -1 {
						require.Less(t, scopeAt, ownAt,
							"the entry's own pair for %q must be stored last, or it stops winning at read time", name)
					}
				}

				read := readEntries(t, c)
				for name, want := range tc.expectedRead {
					require.Equal(t, want, valueOf(read[entryIdx].StructuredMetadata, name), "read value of %q", name)
				}
			}
		})
	}
}

func storedToLabelsAdapter(pairs [][2]string) push.LabelsAdapter {
	out := make(push.LabelsAdapter, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, logproto.LabelAdapter{Name: p[0], Value: p[1]})
	}
	return out
}

func valueOf(lbls push.LabelsAdapter, name string) string {
	for _, l := range lbls {
		if l.Name == name {
			return l.Value
		}
	}
	return ""
}

// TestAppendWithSharedStructuredMetadataDoesNotMutateInput makes sure neither the entry's own
// nor the shared structured metadata, which is owned by the caller and reused across entries,
// gets reordered while being interned.
func TestAppendWithSharedStructuredMetadataDoesNotMutateInput(t *testing.T) {
	c := newMemChunkWithFormat(ChunkFormatV4, compression.None, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, testTargetSize)

	own := push.LabelsAdapter{{Name: "zzz", Value: "1"}, {Name: "aaa", Value: "2"}}
	shared := push.LabelsAdapter{{Name: "zone", Value: "eu"}, {Name: "cluster", Value: "c1"}}
	entry := logprotoEntryWithStructuredMetadata(1, "line", own)

	_, err := c.AppendWithSharedStructuredMetadata(entry, sharedHashOf(shared), shared)
	require.NoError(t, err)

	require.Equal(t, push.LabelsAdapter{{Name: "zzz", Value: "1"}, {Name: "aaa", Value: "2"}}, own)
	require.Equal(t, push.LabelsAdapter{{Name: "zone", Value: "eu"}, {Name: "cluster", Value: "c1"}}, shared)
}

// TestAppendWithSharedStructuredMetadataAcrossBlockCuts covers the lifetime of the shared
// metadata cache: the symbol table is per chunk, so a shared set stays interned across block
// cuts and is never interned twice.
func TestAppendWithSharedStructuredMetadataAcrossBlockCuts(t *testing.T) {
	const (
		blockSize = 1024
		entries   = 500
	)

	shared := []push.LabelsAdapter{
		{{Name: "cluster", Value: "c1"}, {Name: "service_name", Value: "svc1"}},
		{{Name: "cluster", Value: "c2"}, {Name: "service_name", Value: "svc2"}},
	}

	expanded := newMemChunkWithFormat(ChunkFormatV4, compression.None, UnorderedWithStructuredMetadataHeadBlockFmt, blockSize, testTargetSize)
	deferred := newMemChunkWithFormat(ChunkFormatV4, compression.None, UnorderedWithStructuredMetadataHeadBlockFmt, blockSize, testTargetSize)

	for i := 0; i < entries; i++ {
		own := push.LabelsAdapter{{Name: "traceID", Value: fmt.Sprintf("trace-%d", i%7)}}
		s := shared[i%len(shared)]
		line := fmt.Sprintf("this is a reasonably long log line number %d", i)

		_, err := expanded.Append(logprotoEntryWithStructuredMetadata(int64(i+1), line, effectiveSM(s, own)))
		require.NoError(t, err)

		_, err = deferred.AppendWithSharedStructuredMetadata(logprotoEntryWithStructuredMetadata(int64(i+1), line, own), sharedHashOf(s), s)
		require.NoError(t, err)
	}

	require.Greater(t, deferred.BlockCount(), 1, "test should have cut several blocks")
	require.Len(t, deferred.sharedSM.cache, len(shared), "each shared set must be interned exactly once per chunk")

	require.NoError(t, expanded.Close())
	require.NoError(t, deferred.Close())

	expandedBytes, err := expanded.Bytes()
	require.NoError(t, err)
	deferredBytes, err := deferred.Bytes()
	require.NoError(t, err)
	require.Equal(t, expandedBytes, deferredBytes)
}

// TestAppendWithSharedStructuredMetadataOlderFormats checks that the shared metadata is
// dropped by the formats that drop the entry's own structured metadata.
func TestAppendWithSharedStructuredMetadataOlderFormats(t *testing.T) {
	for _, tc := range []struct {
		chunkFormat byte
		headFormat  HeadBlockFmt
	}{
		{ChunkFormatV2, OrderedHeadBlockFmt},
		{ChunkFormatV3, OrderedHeadBlockFmt},
		{ChunkFormatV3, UnorderedHeadBlockFmt},
	} {
		t.Run(fmt.Sprintf("chunkFormat=%d/headFormat=%v", tc.chunkFormat, tc.headFormat), func(t *testing.T) {
			own := push.LabelsAdapter{{Name: "traceID", Value: "abc"}}
			shared := push.LabelsAdapter{{Name: "cluster", Value: "c1"}}

			expanded := newMemChunkWithFormat(tc.chunkFormat, compression.None, tc.headFormat, testBlockSize, testTargetSize)
			deferred := newMemChunkWithFormat(tc.chunkFormat, compression.None, tc.headFormat, testBlockSize, testTargetSize)

			for i := 1; i <= 3; i++ {
				_, err := expanded.Append(logprotoEntryWithStructuredMetadata(int64(i), "line", effectiveSM(shared, own)))
				require.NoError(t, err)

				_, err = deferred.AppendWithSharedStructuredMetadata(logprotoEntryWithStructuredMetadata(int64(i), "line", slices.Clone(own)), sharedHashOf(shared), shared)
				require.NoError(t, err)
			}

			for _, e := range readEntries(t, deferred) {
				require.Empty(t, e.StructuredMetadata)
			}

			require.NoError(t, expanded.Close())
			require.NoError(t, deferred.Close())

			expandedBytes, err := expanded.Bytes()
			require.NoError(t, err)
			deferredBytes, err := deferred.Bytes()
			require.NoError(t, err)
			require.Equal(t, expandedBytes, deferredBytes)
		})
	}
}

func TestSpaceForWithSharedStructuredMetadata(t *testing.T) {
	own := push.LabelsAdapter{{Name: "traceID", Value: "abc"}}
	shared := push.LabelsAdapter{{Name: "cluster", Value: "c1"}, {Name: "service_name", Value: "svc"}}
	entry := logprotoEntryWithStructuredMetadata(1, "line", own)

	t.Run("charges the shared pairs at the symbolized rate", func(t *testing.T) {
		// The entry needs len(line) + len(own strings) + 2 pairs * 8 bytes = 4 + 10 + 16 = 30
		// bytes of head block room.
		c := newMemChunkWithFormat(ChunkFormatV4, compression.None, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, 30)
		require.False(t, c.SpaceForWithSharedStructuredMetadata(entry, shared))

		c = newMemChunkWithFormat(ChunkFormatV4, compression.None, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, 31)
		require.True(t, c.SpaceForWithSharedStructuredMetadata(entry, shared))
	})

	t.Run("shared strings are only charged once, through the symbol table", func(t *testing.T) {
		c := newMemChunkWithFormat(ChunkFormatV4, compression.None, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, testTargetSize)
		require.True(t, c.SpaceForWithSharedStructuredMetadata(entry, shared))

		before := c.symbolizer.UncompressedSize()
		_, err := c.AppendWithSharedStructuredMetadata(logprotoEntryWithStructuredMetadata(1, "line", own), sharedHashOf(shared), shared)
		require.NoError(t, err)
		grownBy := c.symbolizer.UncompressedSize() - before
		require.Equal(t, metaLabelsLen(logproto.FromLabelAdaptersToLabels(effectiveSM(shared, own))), grownBy)

		// Appending the same shared set again only grows the table by the entry's own strings.
		before = c.symbolizer.UncompressedSize()
		_, err = c.AppendWithSharedStructuredMetadata(logprotoEntryWithStructuredMetadata(2, "line", push.LabelsAdapter{{Name: "traceID", Value: "def"}}), sharedHashOf(shared), shared)
		require.NoError(t, err)
		require.Equal(t, len("def"), c.symbolizer.UncompressedSize()-before)
	})

	t.Run("ignores structured metadata for formats below v4", func(t *testing.T) {
		c := newMemChunkWithFormat(ChunkFormatV3, compression.None, UnorderedHeadBlockFmt, testBlockSize, 5)
		require.True(t, c.SpaceForWithSharedStructuredMetadata(entry, shared))
	})

	// A v4 chunk can end up with a head block on a format that drops structured metadata:
	// MemchunkFromCheckpoint takes the head format from the ingester's config, not from the
	// chunk (see pkg/ingester/checkpoint.go). AppendWithSharedStructuredMetadata then drops
	// the shared set, so charging for it would cut the chunk earlier than the bytes it
	// actually stores warrant.
	v4WithOldHead := func(t *testing.T, targetSize int) *MemChunk {
		t.Helper()
		c := newMemChunkWithFormat(ChunkFormatV4, compression.None, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, targetSize)
		require.NoError(t, c.ConvertHead(UnorderedHeadBlockFmt))
		require.Equal(t, UnorderedHeadBlockFmt, c.head.Format())
		return c
	}

	t.Run("does not charge the shared pairs when the head block drops structured metadata", func(t *testing.T) {
		// len(line) + len(own strings) = 4 + 10 = 14 bytes of head block room, the two shared
		// pairs must add nothing on top.
		require.False(t, v4WithOldHead(t, 14).SpaceForWithSharedStructuredMetadata(entry, shared))
		require.True(t, v4WithOldHead(t, 15).SpaceForWithSharedStructuredMetadata(entry, shared))

		// The same entry does not fit a chunk whose head block does store structured
		// metadata, which is where the shared pairs are charged.
		c := newMemChunkWithFormat(ChunkFormatV4, compression.None, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, 15)
		require.False(t, c.SpaceForWithSharedStructuredMetadata(entry, shared))
	})

	// Sizing and storage must agree: what SpaceFor charges for the shared pairs must be what
	// the head block ends up growing by for them.
	t.Run("the charge matches what the head block ends up storing", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			chunk func(t *testing.T) *MemChunk
			// Whether the head block stores structured metadata at all.
			storesSM bool
		}{
			{
				name: "head block storing structured metadata",
				chunk: func(*testing.T) *MemChunk {
					return newMemChunkWithFormat(ChunkFormatV4, compression.None, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, testTargetSize)
				},
				storesSM: true,
			},
			{
				name:     "head block dropping structured metadata",
				chunk:    func(t *testing.T) *MemChunk { return v4WithOldHead(t, testTargetSize) },
				storesSM: false,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				c := tc.chunk(t)

				// What SpaceForWithSharedStructuredMetadata charges for the shared pairs, i.e.
				// the difference between sizing the entry with and without them.
				charged := 0
				if c.head.Format() >= UnorderedWithStructuredMetadataHeadBlockFmt {
					charged = len(shared) * 2 * 4
				}

				before := c.head.UncompressedSize()
				_, err := c.AppendWithSharedStructuredMetadata(logprotoEntryWithStructuredMetadata(1, "line", own), sharedHashOf(shared), shared)
				require.NoError(t, err)
				grownBy := c.head.UncompressedSize() - before

				expected := len("line")
				if tc.storesSM {
					// The head block charges every stored pair, own and shared alike, at the
					// symbolized rate.
					expected += (len(own) + len(shared)) * 2 * 4
				}
				require.Equal(t, expected, grownBy)

				// The shared part of the charge is exactly the shared part of the growth.
				storedShared := grownBy - len("line")
				if tc.storesSM {
					storedShared -= len(own) * 2 * 4
				}
				require.Equal(t, charged, storedShared)
			})
		}
	})
}

func BenchmarkAppendWithSharedStructuredMetadata(b *testing.B) {
	const (
		numEntries = 1000
		numShared  = 20
	)

	shared := make(push.LabelsAdapter, 0, numShared)
	for i := 0; i < numShared; i++ {
		shared = append(shared, logproto.LabelAdapter{
			Name:  fmt.Sprintf("resource_attribute_%02d", i),
			Value: fmt.Sprintf("resource-attribute-value-%02d", i),
		})
	}
	hash := sharedHashOf(shared)

	// Both variants start from entries that are already built, so the benchmark measures the
	// interning done by the chunk, not the expansion the producer no longer has to do.
	entries := make([]*logproto.Entry, 0, numEntries)
	expandedEntries := make([]*logproto.Entry, 0, numEntries)
	for i := 0; i < numEntries; i++ {
		e := logprotoEntryWithStructuredMetadata(
			int64(i+1),
			fmt.Sprintf("this is a reasonably long log line number %d", i),
			push.LabelsAdapter{{Name: "traceID", Value: fmt.Sprintf("trace-%d", i)}},
		)
		entries = append(entries, e)

		expanded := *e
		expanded.StructuredMetadata = effectiveSM(shared, e.StructuredMetadata)
		expandedEntries = append(expandedEntries, &expanded)
	}

	b.Run("expanded", func(b *testing.B) {
		b.ReportAllocs()
		for n := 0; n < b.N; n++ {
			c := newMemChunkWithFormat(ChunkFormatV4, compression.None, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, testTargetSize)
			for _, e := range expandedEntries {
				if _, err := c.Append(e); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("shared", func(b *testing.B) {
		b.ReportAllocs()
		for n := 0; n < b.N; n++ {
			c := newMemChunkWithFormat(ChunkFormatV4, compression.None, UnorderedWithStructuredMetadataHeadBlockFmt, testBlockSize, testTargetSize)
			for _, e := range entries {
				if _, err := c.AppendWithSharedStructuredMetadata(e, hash, shared); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}
