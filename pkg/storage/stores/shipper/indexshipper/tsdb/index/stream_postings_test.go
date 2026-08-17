package index

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
)

// drainPostings iterates a Postings to completion and returns the refs.
func drainPostings(t *testing.T, p Postings) []storage.SeriesRef {
	t.Helper()
	var refs []storage.SeriesRef
	for p.Next() {
		refs = append(refs, p.At())
	}
	require.NoError(t, p.Err())
	return refs
}

// mustPostings runs Reader.Postings and asserts that doing so doesn't return an error.
func mustPostings(t *testing.T, r Reader, name string, values ...string) Postings {
	t.Helper()
	p, err := r.Postings(name, nil, values...)
	require.NoError(t, err)
	return p
}

func mustPostingsAndDrain(t *testing.T, r Reader, name string, values ...string) []storage.SeriesRef {
	t.Helper()
	return drainPostings(t, mustPostings(t, r, name, values...))
}

// TestStreamPostings_MatchesMmap cross-checks the streaming Postings
// implementation against the mmap reader on a fixture with many values per
// label name, so the sparse postings-offset table (every symbolFactor-th
// value) has multiple entries and both the single-value and multi-value query
// paths walk forward within and across sparse blocks.
func TestStreamPostings_MatchesMmap(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			fn := writeManySymbolsFixture(t, format)

			mmap, err := NewMmapFileReader(fn)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, mmap.Close()) })

			stream, err := NewStreamFileReader(fn)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, stream.Close()) })

			// LabelValues returns the values in ascending order, which is the
			// order Postings requires. The fixture uses a single "id" label.
			values, err := mmap.LabelValues("id")
			require.NoError(t, err)
			require.Greater(t, len(values), symbolFactor)

			// Single-value queries across every value exercise every sparse
			// block and the forward walk within a block.
			for _, v := range values {
				require.Equal(t,
					mustPostingsAndDrain(t, mmap, "id", v),
					mustPostingsAndDrain(t, stream, "id", v),
				)
			}

			// A multi-value query with every value at once exercises walking
			// across sparse blocks in a single call.
			require.Equal(t,
				mustPostingsAndDrain(t, mmap, "id", values...),
				mustPostingsAndDrain(t, stream, "id", values...),
			)

			// A scattered subset (every 5th value) exercises seeking between
			// non-adjacent sparse entries.
			var subset []string
			for i := 0; i < len(values); i += 5 {
				subset = append(subset, values[i])
			}
			require.Equal(t,
				mustPostingsAndDrain(t, mmap, "id", subset...),
				mustPostingsAndDrain(t, stream, "id", subset...),
			)

			// Unknown label name yields empty postings in both readers.
			require.Empty(t, drainPostings(t, mustPostings(t, stream, "does-not-exist", "x")))
			require.Equal(t,
				mustPostingsAndDrain(t, mmap, "does-not-exist", "x"),
				mustPostingsAndDrain(t, stream, "does-not-exist", "x"),
			)

			// Values outside the range of stored values (before the first and
			// after the last) match mmap — both should be empty.
			for _, v := range []string{"\x00", "zzzzzzzz"} {
				require.Equal(t,
					mustPostingsAndDrain(t, mmap, "id", v),
					mustPostingsAndDrain(t, stream, "id", v),
				)
			}

			// No values requested yields empty postings in both readers.
			require.Equal(t,
				mustPostingsAndDrain(t, mmap, "id"),
				mustPostingsAndDrain(t, stream, "id"),
			)
		})
	}
}

// writeSharedLabelFixture builds an index where a single label pair
// ("shared"="all") is attached to a large, gapped subset of the series, so its
// postings list holds many refs with gaps between them — enough to span
// several read buffers and exercise the streaming Seek's binary search.
func writeSharedLabelFixture(t *testing.T, format int) string {
	t.Helper()
	dir := t.TempDir()
	fileName := filepath.Join(dir, IndexFilename)

	creator, err := NewWriter(context.Background(), format, fileName)
	require.NoError(t, err)

	const numSeries = 4000

	type entry struct {
		ls labels.Labels
	}
	symbolSet := map[string]struct{}{"id": {}, "shared": {}, "all": {}}
	entries := make([]entry, 0, numSeries)
	for j := range numSeries {
		id := fmt.Sprintf("v%05d", j)
		symbolSet[id] = struct{}{}
		// Attach "shared"="all" to ~2/3 of the series so its postings list is
		// large but leaves gaps in the ref sequence.
		if j%3 == 0 {
			entries = append(entries, entry{ls: labels.FromStrings("id", id)})
		} else {
			entries = append(entries, entry{ls: labels.FromStrings("id", id, "shared", "all")})
		}
	}

	symbols := make([]string, 0, len(symbolSet))
	for s := range symbolSet {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)
	for _, s := range symbols {
		require.NoError(t, creator.AddSymbol(s))
	}

	// AddSeries requires ascending fingerprint order.
	sort.Slice(entries, func(i, j int) bool {
		return labels.StableHash(entries[i].ls) < labels.StableHash(entries[j].ls)
	})
	for i, e := range entries {
		require.NoError(t, creator.AddSeries(
			storage.SeriesRef(i+1),
			e.ls,
			model.Fingerprint(labels.StableHash(e.ls)),
			ChunkMeta{Checksum: 1, MinTime: 0, MaxTime: 10, KB: 1, Entries: 1},
		))
	}

	_, err = creator.Close(false)
	require.NoError(t, err)
	return fileName
}

// TestStreamingPostings_SeekMatchesMmap cross-checks the streaming postings
// iterator's Seek (a binary search over the on-disk refs) against the mmap
// BigEndianPostings over a large, gapped postings list.
func TestStreamingPostings_SeekMatchesMmap(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			fn := writeSharedLabelFixture(t, format)

			mmap, err := NewMmapFileReader(fn)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, mmap.Close()) })

			stream, err := NewStreamFileReader(fn)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, stream.Close()) })

			// Ground-truth ordered ref list for the shared postings.
			allRefs := mustPostingsAndDrain(t, mmap, "shared", "all")
			require.Greater(t, len(allRefs), 1000, "need a large postings list to exercise the binary search")

			// A target that falls in a gap (absent, between two refs) so we
			// exercise landing on the next-greater ref.
			var gap storage.SeriesRef
			for k := 0; k+1 < len(allRefs); k++ {
				if allRefs[k+1] > allRefs[k]+1 {
					gap = allRefs[k] + 1
					break
				}
			}
			require.NotZero(t, gap)

			// Part 1: fresh iterators, full-range search for a spread of targets:
			// before-first, exact, gap, midpoint, last, past-end.
			targets := []storage.SeriesRef{
				0,
				allRefs[0],
				gap,
				allRefs[len(allRefs)/2],
				allRefs[len(allRefs)-1],
				allRefs[len(allRefs)-1] + 1,
			}
			for _, target := range targets {
				mmapPostings := mustPostings(t, mmap, "shared", "all")
				streamPostings := mustPostings(t, stream, "shared", "all")
				mmapOk, streamOk := mmapPostings.Seek(target), streamPostings.Seek(target)
				require.Equal(t, mmapOk, streamOk)
				if mmapOk {
					require.Equal(t, mmapPostings.At(), streamPostings.At())
					// The remainder after the sought ref must match too.
					require.Equal(t, drainPostings(t, mmapPostings), drainPostings(t, streamPostings))
				}
				require.NoError(t, mmapPostings.Err())
				require.NoError(t, streamPostings.Err())
			}

			// Part 2: one iterator pair driven in lockstep with interleaved Next
			// and ascending Seek, exercising a binary search from a non-zero
			// starting position.
			mmapPostings := mustPostings(t, mmap, "shared", "all")
			streamPostings := mustPostings(t, stream, "shared", "all")
			n := len(allRefs)
			type op struct {
				seek   bool
				target storage.SeriesRef
			}
			for i, o := range []op{
				{seek: false},
				{seek: false},
				{seek: true, target: allRefs[n/4]},
				{seek: false},
				{seek: true, target: allRefs[n/2] + 1},
				{seek: true, target: allRefs[3*n/4]},
				{seek: false},
				{seek: true, target: allRefs[n-1]},
			} {
				var mmapOk, streamOk bool
				if o.seek {
					mmapOk, streamOk = mmapPostings.Seek(o.target), streamPostings.Seek(o.target)
				} else {
					mmapOk, streamOk = mmapPostings.Next(), streamPostings.Next()
				}
				require.Equal(t, mmapOk, streamOk, "op %d ok", i)
				if !mmapOk {
					break
				}
				require.Equal(t, mmapPostings.At(), streamPostings.At())
			}
			require.NoError(t, mmapPostings.Err())
			require.NoError(t, streamPostings.Err())
		})
	}
}
