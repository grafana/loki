package index

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc"
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
func mustPostings(t *testing.T, r Reader, fpFilter FingerprintFilter, name string, values ...string) Postings {
	t.Helper()
	p, err := r.Postings(name, fpFilter, values...)
	require.NoError(t, err)
	return p
}

func mustPostingsAndDrain(t *testing.T, r Reader, fpFilter FingerprintFilter, name string, values ...string) []storage.SeriesRef {
	t.Helper()
	return drainPostings(t, mustPostings(t, r, fpFilter, name, values...))
}

// TestStreamPostings_MatchesMmap cross-checks the streaming Postings
// implementation against the mmap reader on a fixture with many values per
// label name, so the sparse postings-offset table (every symbolFactor-th
// value) has multiple entries and both the single-value and multi-value query
// paths walk forward within and across sparse blocks.
func TestStreamPostings_MatchesMmap(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			mmap, stream := openBothReaders(t, writeManySymbolsFixture(t, format))

			// LabelValues returns the values in ascending order, which is the
			// order Postings requires. The fixture uses a single "id" label.
			values, err := mmap.LabelValues("id")
			require.NoError(t, err)
			require.Greater(t, len(values), symbolFactor)

			// Single-value queries across every value exercise every sparse
			// block and the forward walk within a block.
			for _, v := range values {
				require.Equal(t,
					mustPostingsAndDrain(t, mmap, nil, "id", v),
					mustPostingsAndDrain(t, stream, nil, "id", v),
				)
			}

			// A multi-value query with every value at once exercises walking
			// across sparse blocks in a single call.
			require.Equal(t,
				mustPostingsAndDrain(t, mmap, nil, "id", values...),
				mustPostingsAndDrain(t, stream, nil, "id", values...),
			)

			// A scattered subset (every 5th value) exercises seeking between
			// non-adjacent sparse entries.
			var subset []string
			for i := 0; i < len(values); i += 5 {
				subset = append(subset, values[i])
			}
			require.Equal(t,
				mustPostingsAndDrain(t, mmap, nil, "id", subset...),
				mustPostingsAndDrain(t, stream, nil, "id", subset...),
			)

			// Unknown label name yields empty postings in both readers.
			require.Empty(t, mustPostingsAndDrain(t, stream, nil, "does-not-exist", "x"))
			require.Equal(t,
				mustPostingsAndDrain(t, mmap, nil, "does-not-exist", "x"),
				mustPostingsAndDrain(t, stream, nil, "does-not-exist", "x"),
			)

			// Values outside the range of stored values (before the first and
			// after the last) match mmap — both should be empty.
			for _, v := range []string{"\x00", "zzzzzzzz"} {
				require.Equal(t,
					mustPostingsAndDrain(t, mmap, nil, "id", v),
					mustPostingsAndDrain(t, stream, nil, "id", v),
				)
			}

			// No values requested yields empty postings in both readers.
			require.Equal(t,
				mustPostingsAndDrain(t, mmap, nil, "id"),
				mustPostingsAndDrain(t, stream, nil, "id"),
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

	const numSeries = 4000

	series := make([]seriesFixture, 0, numSeries)
	for i := range numSeries {
		id := fmt.Sprintf("v%05d", i)
		ls := labels.FromStrings("id", id)
		// Attach "shared"="all" to ~2/3 of the series so its postings list is
		// large but leaves gaps in the ref sequence.
		if i%3 != 0 {
			ls = labels.FromStrings("id", id, "shared", "all")
		}
		series = append(series, seriesFixture{
			ls:     ls,
			chunks: []ChunkMeta{{Checksum: 1, MinTime: 0, MaxTime: 10, KB: 1, Entries: 1}},
		})
	}

	return writeIndexFixture(t, format, series)
}

// TestStreamingPostings_SeekMatchesMmap cross-checks the streaming postings
// iterator's Seek (a binary search over the on-disk refs) against the mmap
// BigEndianPostings over a large, gapped postings list.
func TestStreamingPostings_SeekMatchesMmap(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			mmap, stream := openBothReaders(t, writeSharedLabelFixture(t, format))

			// Ground-truth ordered ref list for the shared postings.
			allRefs := mustPostingsAndDrain(t, mmap, nil, "shared", "all")
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
				mmapPostings := mustPostings(t, mmap, nil, "shared", "all")
				streamPostings := mustPostings(t, stream, nil, "shared", "all")
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
			mmapPostings := mustPostings(t, mmap, nil, "shared", "all")
			streamPostings := mustPostings(t, stream, nil, "shared", "all")
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

// TestStreamPostings_ShardedMatchesMmap cross-checks shard-aware Postings
// (fpFilter != nil) against the mmap reader over the large shared-label
// postings list, across several shard configurations.
func TestStreamPostings_ShardedMatchesMmap(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			mmap, stream := openBothReaders(t, writeSharedLabelFixture(t, format))

			// Should both have the same fingerprintOffsets
			require.NotEmpty(t, mmap.fingerprintOffsets)
			require.Equal(t, mmap.fingerprintOffsets, stream.fingerprintOffsets)

			// The unsharded ground truth.
			allSeriesRefs := mustPostingsAndDrain(t, mmap, nil, "shared", "all")
			require.Greater(t, len(allSeriesRefs), 1000, "need a large postings list to exercise sharding")

			// Every shard, in every configuration, must return exactly what the
			// mmap reader returns.
			shards := []ShardAnnotation{
				NewShard(0, 2), NewShard(1, 2), NewShard(2, 16), NewShard(13, 32),
				NewShard(0, 4), NewShard(1, 4), NewShard(2, 4), NewShard(3, 4),
			}
			for _, shard := range shards {
				require.Equal(t,
					mustPostingsAndDrain(t, mmap, shard, "shared", "all"),
					mustPostingsAndDrain(t, stream, shard, "shared", "all"),
					"shard %d/%d", shard.Shard, shard.Of,
				)
			}

			// A single shard covering the whole space is an identity filter:
			// the fpFilter != nil path must still return the full list.
			require.Equal(t, allSeriesRefs, mustPostingsAndDrain(t, stream, NewShard(0, 1), "shared", "all"))

			// Series are written in fingerprint order, so a 2-way partition
			// must cover every series (sharding may return a slight superset at
			// the boundaries, never a subset of the union).
			union := map[storage.SeriesRef]struct{}{}
			for _, shard := range []ShardAnnotation{NewShard(0, 2), NewShard(1, 2)} {
				for _, ref := range mustPostingsAndDrain(t, stream, shard, "shared", "all") {
					union[ref] = struct{}{}
				}
			}
			for _, ref := range allSeriesRefs {
				require.Contains(t, union, ref, "series %d missing from the 2-way shard partition", ref)
			}
		})
	}
}

// postingsListOffset returns the absolute file offset of the postings list for
// name=value, from r's postings offset table.
func postingsListOffset(t *testing.T, r *StreamReader, name, value string) int {
	t.Helper()
	offset := -1
	require.NoError(t, streamPostingsOffsetTable(context.Background(), r.factory, int(r.toc.PostingsTable), func(n, v []byte, postingsOffset uint64, _ int) error {
		if string(n) == name && string(v) == value {
			offset = int(postingsOffset)
		}
		return nil
	}))
	require.NotEqual(t, -1, offset, "no postings list for %s=%q", name, value)
	return offset
}

// writeMixedPostingsFixture builds an index whose "grp" label has many values
// with one series each, so their postings lists are tiny and adjacent and a
// lookup over a run of them straddles read-ahead window boundaries, plus one
// value in the middle of the run, bigValue, whose postings list is larger than
// the window.
func writeMixedPostingsFixture(t *testing.T, format int) (path, bigValue string) {
	t.Helper()

	const (
		numSmall    = 1200
		numBig      = 2000
		bigAfterIdx = numSmall / 2
	)
	bigValue = fmt.Sprintf("s%04dx", bigAfterIdx) // sorts between s0600 and s0601

	series := make([]seriesFixture, 0, numSmall+numBig)
	add := func(grp string) {
		series = append(series, seriesFixture{
			ls:     labels.FromStrings("grp", grp, "id", fmt.Sprintf("%05d", len(series))),
			chunks: []ChunkMeta{{Checksum: 1, MinTime: 0, MaxTime: 10, KB: 1, Entries: 1}},
		})
	}
	for i := range numSmall {
		add(fmt.Sprintf("s%04d", i))
	}
	for range numBig {
		add(bigValue)
	}

	return writeIndexFixture(t, format, series), bigValue
}

// TestStreamPostings_ManyValuesMatchMmap cross-checks lookups which span multiple
// read-ahead windows.
func TestStreamPostings_ManyValuesMatchMmap(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			path, bigValue := writeMixedPostingsFixture(t, format)
			mmap, stream := openBothReaders(t, path)

			values, err := mmap.LabelValues("grp")
			require.NoError(t, err)
			bigIdx := slices.Index(values, bigValue)
			require.Positive(t, bigIdx)

			// Check the fixture has the shape the test relies on.
			require.Greater(t, 4*len(mustPostingsAndDrain(t, mmap, nil, "grp", bigValue)), streamenc.ReaderBufferSize,
				"the big list must be larger than the read-ahead window")
			require.Greater(t,
				postingsListOffset(t, stream, "grp", values[bigIdx-1])-postingsListOffset(t, stream, "grp", values[0]),
				2*streamenc.ReaderBufferSize,
				"the small lists before the big one must span several windows")

			var everyThird []string
			for i := 0; i < len(values); i += 3 {
				everyThird = append(everyThird, values[i])
			}

			for _, tc := range []struct {
				name   string
				values []string
			}{
				{name: "every value", values: values},
				{name: "run around the big list", values: values[bigIdx-5 : bigIdx+6]},
				{name: "every third value", values: everyThird},
				{name: "big list then absent values", values: []string{bigValue, bigValue + "-absent", "zzz"}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					require.Equal(t,
						mustPostingsAndDrain(t, mmap, nil, "grp", tc.values...),
						mustPostingsAndDrain(t, stream, nil, "grp", tc.values...),
					)
				})
			}
		})
	}
}

// A corrupt postings list must be rejected, and a corrupt count must be
// rejected before the buffer for its refs is allocated.
func TestStreamPostings_RejectsCorruptPostingsList(t *testing.T) {
	// The corrupted list sits in the middle of a run of lists the lookup reads.
	const corruptValue = "v0064"

	for _, tc := range []struct {
		name string
		// corrupt mutates the index file b, given the offset of the list for
		// corruptValue.
		corrupt func(b []byte, list int)
		wantErr error
	}{
		{
			name:    "ref",
			corrupt: func(b []byte, list int) { b[list+8] ^= 0x01 },
			wantErr: streamenc.ErrInvalidChecksum,
		},
		{
			name:    "count disagrees with length",
			corrupt: func(b []byte, list int) { b[list+7] ^= 0x01 },
			wantErr: streamenc.ErrInvalidSize,
		},
		{
			// A consistent length and count whose refs would need a 2GiB
			// buffer.
			name: "huge count",
			corrupt: func(b []byte, list int) {
				const n = 1 << 29
				binary.BigEndian.PutUint32(b[list:], uint32(4+4*n))
				binary.BigEndian.PutUint32(b[list+4:], n)
			},
			wantErr: streamenc.ErrInvalidSize,
		},
		{
			// Where int is 32 bits, both values read as negative and
			// 4+4*count equals the length, so a check in int arithmetic
			// passes and the buffer allocation panics on a negative size.
			name: "count that overflows a 32-bit int",
			corrupt: func(b []byte, list int) {
				binary.BigEndian.PutUint32(b[list:], 0xFFFFFFFC)   // -4 as an int32
				binary.BigEndian.PutUint32(b[list+4:], 0xFFFFFFFE) // -2 as an int32
			},
			wantErr: streamenc.ErrInvalidSize,
		},
		{
			// Where int is 32 bits, 4*count wraps around to 4, which agrees
			// with the length.
			name: "count whose refs wrap around a 32-bit int",
			corrupt: func(b []byte, list int) {
				binary.BigEndian.PutUint32(b[list:], 8)
				binary.BigEndian.PutUint32(b[list+4:], 0x80000001)
			},
			wantErr: streamenc.ErrInvalidSize,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeManySymbolsFixture(t, FormatV4)
			values := func() []string {
				mmap, stream := openBothReaders(t, path)
				list := postingsListOffset(t, stream, "id", corruptValue)
				corruptFileBytes(t, path, func(b []byte) { tc.corrupt(b, list) })

				values, err := mmap.LabelValues("id")
				require.NoError(t, err)
				return values
			}()
			require.Contains(t, values, corruptValue)

			stream, err := NewStreamFileReader(path, DefaultStreamOptions())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, stream.Close()) })

			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, err = stream.Postings("id", nil, values...)
			runtime.ReadMemStats(&after)

			require.ErrorIs(t, err, tc.wantErr)
			require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20), "the lookup allocated for the corrupt count")
		})
	}
}
