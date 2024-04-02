package index

import (
	"fmt"
	"math"
	"testing"

	tsdb_enc "github.com/prometheus/prometheus/tsdb/encoding"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/encoding"
)

// Test all sort variants
func TestChunkMetasSort(t *testing.T) {
	for _, tc := range []struct {
		desc string
		a, b ChunkMeta
	}{
		{
			desc: "prefer mintime",
			a: ChunkMeta{
				MinTime: 0,
				MaxTime: 5,
			},
			b: ChunkMeta{
				MinTime: 1,
				MaxTime: 4,
			},
		},
		{
			desc: "delegate maxtime",
			a: ChunkMeta{
				MaxTime:  2,
				Checksum: 2,
			},
			b: ChunkMeta{
				MaxTime:  3,
				Checksum: 1,
			},
		},
		{
			desc: "delegate checksum",
			a: ChunkMeta{
				Checksum: 1,
			},
			b: ChunkMeta{
				Checksum: 2,
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			xs := ChunkMetas{tc.a, tc.b}
			require.Equal(t, true, xs.Less(0, 1))
			require.Equal(t, false, xs.Less(1, 0))
		})
	}
}

func TestChunkMetasFinalize(t *testing.T) {
	mkMeta := func(x int) ChunkMeta {
		return ChunkMeta{
			MinTime:  int64(x),
			Checksum: uint32(x),
		}
	}
	for _, tc := range []struct {
		desc          string
		input, output ChunkMetas
	}{
		{
			desc: "reorder",
			input: []ChunkMeta{
				mkMeta(2),
				mkMeta(1),
				mkMeta(3),
			},
			output: []ChunkMeta{
				mkMeta(1),
				mkMeta(2),
				mkMeta(3),
			},
		},
		{
			desc: "remove duplicates",
			input: []ChunkMeta{
				mkMeta(1),
				mkMeta(2),
				mkMeta(2),
				mkMeta(3),
			},
			output: []ChunkMeta{
				mkMeta(1),
				mkMeta(2),
				mkMeta(3),
			},
		},
		{
			desc: "remove trailing duplicates",
			input: []ChunkMeta{
				mkMeta(1),
				mkMeta(2),
				mkMeta(2),
				mkMeta(3),
				mkMeta(4),
				mkMeta(4),
				mkMeta(5),
				mkMeta(5),
			},
			output: []ChunkMeta{
				mkMeta(1),
				mkMeta(2),
				mkMeta(3),
				mkMeta(4),
				mkMeta(5),
			},
		},
		{
			desc: "cleanup after last duplicate",
			input: []ChunkMeta{
				mkMeta(1),
				mkMeta(2),
				mkMeta(2),
				mkMeta(3),
				mkMeta(4),
				mkMeta(5),
				mkMeta(5),
				mkMeta(6),
				mkMeta(7),
			},
			output: []ChunkMeta{
				mkMeta(1),
				mkMeta(2),
				mkMeta(3),
				mkMeta(4),
				mkMeta(5),
				mkMeta(6),
				mkMeta(7),
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.output, tc.input.Finalize())
		})
	}
}

func TestChunkMetas_Add(t *testing.T) {
	chunkMetas := ChunkMetas{
		{
			MinTime:  1,
			MaxTime:  1,
			Checksum: 1,
		},
		{
			MinTime:  2,
			MaxTime:  3,
			Checksum: 1,
		},
		{
			MinTime:  4,
			MaxTime:  5,
			Checksum: 1,
		},
		{
			MinTime:  4,
			MaxTime:  6,
			Checksum: 1,
		},
		{
			MinTime:  4,
			MaxTime:  6,
			Checksum: 2,
		},
	}.Finalize()

	for i, tc := range []struct {
		chunkMetas  ChunkMetas
		toAdd       ChunkMeta
		expectedPos int
	}{
		// no existing chunks
		{
			chunkMetas: ChunkMetas{},
			toAdd:      ChunkMeta{MinTime: 0},
		},
		// add to the beginning
		{
			chunkMetas: chunkMetas,
			toAdd:      ChunkMeta{MinTime: 0},
		},
		{
			chunkMetas: chunkMetas,
			toAdd: ChunkMeta{
				MinTime:  1,
				MaxTime:  1,
				Checksum: 0,
			},
		},
		// add in between
		{
			chunkMetas: chunkMetas,
			toAdd: ChunkMeta{
				MinTime: 2,
				MaxTime: 2,
			},
			expectedPos: 1,
		},
		{
			chunkMetas: chunkMetas,
			toAdd: ChunkMeta{
				MinTime:  3,
				MaxTime:  5,
				Checksum: 1,
			},
			expectedPos: 2,
		},
		{
			chunkMetas: chunkMetas,
			toAdd: ChunkMeta{
				MinTime:  4,
				MaxTime:  4,
				Checksum: 1,
			},
			expectedPos: 2,
		},
		{
			chunkMetas: chunkMetas,
			toAdd: ChunkMeta{
				MinTime:  4,
				MaxTime:  5,
				Checksum: 0,
			},
			expectedPos: 2,
		},
		// add to the end
		{
			chunkMetas: chunkMetas,
			toAdd: ChunkMeta{
				MinTime:  5,
				MaxTime:  6,
				Checksum: 2,
			},
			expectedPos: 5,
		},
		{
			chunkMetas: chunkMetas,
			toAdd: ChunkMeta{
				MinTime:  4,
				MaxTime:  7,
				Checksum: 2,
			},
			expectedPos: 5,
		},
		{
			chunkMetas: chunkMetas,
			toAdd: ChunkMeta{
				MinTime:  4,
				MaxTime:  6,
				Checksum: 3,
			},
			expectedPos: 5,
		},
		// chunk already exists
		{
			chunkMetas: chunkMetas,
			toAdd: ChunkMeta{
				MinTime:  4,
				MaxTime:  6,
				Checksum: 2,
			},
			expectedPos: 4,
		},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			chunkMetasCopy := make(ChunkMetas, len(tc.chunkMetas))
			copy(chunkMetasCopy, tc.chunkMetas)
			chunkMetasCopy = append(chunkMetasCopy, tc.toAdd)

			chunkMetas := tc.chunkMetas.Add(tc.toAdd)
			require.Equal(t, chunkMetasCopy.Finalize(), chunkMetas)
			require.Equal(t, tc.toAdd, chunkMetas[tc.expectedPos])
		})
	}
}

func TestChunkMetas_Drop(t *testing.T) {
	chunkMetas := ChunkMetas{
		{
			MinTime:  1,
			MaxTime:  1,
			Checksum: 1,
		},
		{
			MinTime:  2,
			MaxTime:  3,
			Checksum: 1,
		},
		{
			MinTime:  4,
			MaxTime:  5,
			Checksum: 1,
		},
		{
			MinTime:  4,
			MaxTime:  6,
			Checksum: 1,
		},
		{
			MinTime:  4,
			MaxTime:  6,
			Checksum: 2,
		},
	}.Finalize()

	// dropChunkMeta makes a copy of ChunkMetas and drops the element at index i
	dropChunkMeta := func(i int) ChunkMetas {
		chunkMetasCopy := make(ChunkMetas, len(chunkMetas))
		copy(chunkMetasCopy, chunkMetas)

		return append(chunkMetasCopy[:i], chunkMetasCopy[i+1:]...)
	}

	for i, tc := range []struct {
		chunkMetas         ChunkMetas
		toDrop             ChunkMeta
		expectedChunkMetas ChunkMetas
		expectedChunkFound bool
	}{
		// no existing chunk
		{
			chunkMetas:         ChunkMetas{},
			toDrop:             ChunkMeta{MinTime: 0},
			expectedChunkMetas: ChunkMetas{},
		},
		// drop the first chunk
		{
			chunkMetas:         chunkMetas,
			toDrop:             chunkMetas[0],
			expectedChunkMetas: dropChunkMeta(0),
			expectedChunkFound: true,
		},
		// drop in between
		{
			chunkMetas:         chunkMetas,
			toDrop:             chunkMetas[1],
			expectedChunkMetas: dropChunkMeta(1),
			expectedChunkFound: true,
		},
		{
			chunkMetas:         chunkMetas,
			toDrop:             chunkMetas[2],
			expectedChunkMetas: dropChunkMeta(2),
			expectedChunkFound: true,
		},
		// drop from the end
		{
			chunkMetas:         chunkMetas,
			toDrop:             chunkMetas[len(chunkMetas)-1],
			expectedChunkMetas: dropChunkMeta(len(chunkMetas) - 1),
			expectedChunkFound: true,
		},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			chunkMetasCopy := make(ChunkMetas, len(tc.chunkMetas))
			copy(chunkMetasCopy, tc.chunkMetas)

			chunkMetasCopy, chunkFound := chunkMetasCopy.Drop(tc.toDrop)
			require.Equal(t, tc.expectedChunkFound, chunkFound)
			require.Equal(t, tc.expectedChunkMetas, chunkMetasCopy)
		})
	}
}

// TestChunkPageMarkerEncodeDecode tests that the chunk page marker can be encoded and decoded.
func TestChunkPageMarkerEncodeDecode(t *testing.T) {
	// Create a chunk page marker.
	marker := chunkPageMarker{
		ChunksInPage: 1,
		KB:           2,
		Entries:      3,
		Offset:       4,
		MinTime:      5,
		MaxTime:      6,
	}

	// Encode the chunk page marker.
	encbuf := encoding.EncWrap(tsdb_enc.Encbuf{B: make([]byte, 0)})
	marker.encode(&encbuf, 4, 1)
	bs := RealByteSlice(encbuf.Get())

	// decode
	d := encoding.DecWrap(tsdb_enc.NewDecbufRaw(bs, bs.Len()))
	var decMarker chunkPageMarker
	decMarker.decode(&d)

	// Verify the decoded chunk page marker.
	require.Equal(t, marker, decMarker)
}

func mkChks(n int) (chks []ChunkMeta) {
	for i := 0; i < n; i++ {
		chks = append(chks, chkFrom(i))
	}
	return chks
}

func chkFrom(i int) ChunkMeta {
	return ChunkMeta{
		Checksum: uint32(i),
		MinTime:  int64(i),
		MaxTime:  int64(i + 1),
		KB:       uint32(i),
		Entries:  uint32(i),
	}
}

func TestChunkEncodingRoundTrip(t *testing.T) {
	for _, version := range []int{
		FormatV2,
		FormatV3,
	} {
		for _, nChks := range []int{
			0,
			8,
		} {
			for _, pageSize := range []int{
				4,
				5,
				8,
				10,
				ChunkPageSize,
			} {
				t.Run(fmt.Sprintf("version %d nChks %d pageSize %d", version, nChks, pageSize), func(t *testing.T) {
					chks := mkChks(nChks)
					var w Writer
					w.Version = version
					primary := encoding.EncWrap(tsdb_enc.Encbuf{B: make([]byte, 0)})
					scratch := encoding.EncWrap(tsdb_enc.Encbuf{B: make([]byte, 0)})
					w.addChunks(chks, &primary, &scratch, pageSize)

					decbuf := encoding.DecWrap(tsdb_enc.Decbuf{B: primary.Get()})
					dec := newDecoder(nil, 0)

					dst := []ChunkMeta{}
					require.Nil(t, dec.readChunks(version, &decbuf, 0, 0, math.MaxInt64, &dst))

					if len(chks) == 0 {
						require.Equal(t, 0, len(dst))
					} else {
						require.Equal(t, chks, dst)
					}
				})
			}
		}

	}
}

func TestSearchWithPageMarkers(t *testing.T) {
	for _, pageSize := range []int{
		2,
		10,
	} {
		for _, tc := range []struct {
			desc       string
			chks, exp  []ChunkMeta
			mint, maxt int64
		}{
			{
				desc: "half time range",
				chks: []ChunkMeta{
					{MinTime: 0, MaxTime: 1},
					{MinTime: 1, MaxTime: 2},
					{MinTime: 2, MaxTime: 3},
					{MinTime: 3, MaxTime: 4},
					{MinTime: 4, MaxTime: 5},
				},
				mint: 2,
				maxt: 4,
				exp: []ChunkMeta{
					{MinTime: 1, MaxTime: 2},
					{MinTime: 2, MaxTime: 3},
					{MinTime: 3, MaxTime: 4},
				},
			},
			{
				desc: "no chunks in time range",
				chks: []ChunkMeta{
					{MinTime: 0, MaxTime: 1},
					{MinTime: 1, MaxTime: 2},
					{MinTime: 2, MaxTime: 3},
					{MinTime: 3, MaxTime: 4},
					{MinTime: 4, MaxTime: 5},
				},
				mint: 6,
				maxt: 7,
				exp:  []ChunkMeta{},
			},
			{
				desc: "all chunks within time range",
				chks: []ChunkMeta{
					{MinTime: 1, MaxTime: 2},
					{MinTime: 2, MaxTime: 3},
					{MinTime: 3, MaxTime: 4},
				},
				mint: 1,
				maxt: 4,
				exp: []ChunkMeta{
					{MinTime: 1, MaxTime: 2},
					{MinTime: 2, MaxTime: 3},
					{MinTime: 3, MaxTime: 4},
				},
			},
			{
				desc: "semi ordered chunks",
				chks: []ChunkMeta{
					{MinTime: 5, MaxTime: 50},
					{MinTime: 10, MaxTime: 20},
					{MinTime: 11, MaxTime: 20},
					{MinTime: 12, MaxTime: 20},
					{MinTime: 13, MaxTime: 20},
					{MinTime: 15, MaxTime: 30},
				},
				mint: 25,
				maxt: 30,
				exp: []ChunkMeta{
					{MinTime: 5, MaxTime: 50},
					{MinTime: 15, MaxTime: 30},
				},
			},
			{
				desc: "second half of chunks",
				chks: []ChunkMeta{
					{MinTime: 0, MaxTime: 1},
					{MinTime: 1, MaxTime: 2},
					{MinTime: 2, MaxTime: 3},
					{MinTime: 3, MaxTime: 4},
					{MinTime: 4, MaxTime: 5},
					{MinTime: 5, MaxTime: 6},
					{MinTime: 6, MaxTime: 7},
					{MinTime: 7, MaxTime: 8},
					{MinTime: 8, MaxTime: 9},
				},
				mint: 6,
				maxt: 100,
				exp: []ChunkMeta{
					{MinTime: 5, MaxTime: 6},
					{MinTime: 6, MaxTime: 7},
					{MinTime: 7, MaxTime: 8},
					{MinTime: 8, MaxTime: 9},
				},
			},
			{
				desc: "second half of chunks, out of order support",
				chks: []ChunkMeta{
					{MinTime: 0, MaxTime: 1},
					{MinTime: 1, MaxTime: 2},

					// when batchsize=2, the first chunk in this page
					// has the highest maxt, so it will be the page marker's maxt.
					// this test will error returning a different chunk than expected
					// unless we account for this (chunks are delta encoded against the previous)
					{MinTime: 2, MaxTime: 4},
					{MinTime: 3, MaxTime: 3},

					{MinTime: 4, MaxTime: 5},
					{MinTime: 5, MaxTime: 6},

					{MinTime: 6, MaxTime: 7},
					{MinTime: 7, MaxTime: 8},

					{MinTime: 8, MaxTime: 9},
				},
				mint: 6,
				maxt: 100,
				exp: []ChunkMeta{
					{MinTime: 5, MaxTime: 6},
					{MinTime: 6, MaxTime: 7},
					{MinTime: 7, MaxTime: 8},
					{MinTime: 8, MaxTime: 9},
				},
			},
		} {
			t.Run(fmt.Sprintf("%s-pagesize-%d", tc.desc, pageSize), func(t *testing.T) {
				var w Writer
				w.Version = FormatV3
				primary := encoding.EncWrap(tsdb_enc.Encbuf{B: make([]byte, 0)})
				scratch := encoding.EncWrap(tsdb_enc.Encbuf{B: make([]byte, 0)})
				w.addChunks(tc.chks, &primary, &scratch, pageSize)

				decbuf := encoding.DecWrap(tsdb_enc.Decbuf{B: primary.Get()})
				dec := newDecoder(nil, 0)
				dst := []ChunkMeta{}
				require.Nil(t, dec.readChunksV3(&decbuf, tc.mint, tc.maxt, &dst))
				require.Equal(t, tc.exp, dst)
			})
		}
	}

}

func TestDecoderChunkStats(t *testing.T) {
	for _, pageSize := range []int{2, 10} {
		for _, version := range []int{
			FormatV2,
			FormatV3,
		} {
			for _, tc := range []struct {
				desc          string
				chks          []ChunkMeta
				from, through int64
				exp           ChunkStats
			}{
				{
					desc: "full range",
					chks: []ChunkMeta{
						{MinTime: 0, MaxTime: 1, KB: 1, Entries: 1},
						{MinTime: 1, MaxTime: 2, KB: 1, Entries: 1},
						{MinTime: 2, MaxTime: 3, KB: 1, Entries: 1},
						{MinTime: 3, MaxTime: 4, KB: 1, Entries: 1},
						{MinTime: 4, MaxTime: 5, KB: 1, Entries: 1},
					},
					from:    0,
					through: 5,
					exp: ChunkStats{
						Chunks:  5,
						KB:      5,
						Entries: 5,
					},
				},
				{
					desc: "overlapping",
					chks: []ChunkMeta{
						{MinTime: 0, MaxTime: 1, KB: 1, Entries: 1},
						{MinTime: 1, MaxTime: 4, KB: 1, Entries: 1},
						{MinTime: 2, MaxTime: 3, KB: 1, Entries: 1},
						{MinTime: 3, MaxTime: 5, KB: 1, Entries: 1},
						{MinTime: 4, MaxTime: 5, KB: 1, Entries: 1},
					},
					from:    0,
					through: 5,
					exp: ChunkStats{
						Chunks:  5,
						KB:      5,
						Entries: 5,
					},
				},
				{
					desc: "middle",
					chks: []ChunkMeta{
						{MinTime: 0, MaxTime: 1, KB: 1, Entries: 1},
						{MinTime: 1, MaxTime: 2, KB: 1, Entries: 1},
						{MinTime: 2, MaxTime: 3, KB: 1, Entries: 1},
						{MinTime: 3, MaxTime: 4, KB: 1, Entries: 1},
						{MinTime: 5, MaxTime: 6, KB: 1, Entries: 1},
					},
					from:    2,
					through: 4,
					exp: ChunkStats{
						// technically the 2nd chunk overlaps, but we don't add
						// any of it's stats as its only 1 nanosecond in the range
						// and thus gets integer-divisioned to 0
						Chunks:  3,
						KB:      2,
						Entries: 2,
					},
				},
				{
					desc: "middle with complete overlaps",
					chks: []ChunkMeta{
						// for example with pageSize=2
						{MinTime: 0, MaxTime: 1, KB: 1, Entries: 1},
						{MinTime: 1, MaxTime: 2, KB: 1, Entries: 1},

						{MinTime: 2, MaxTime: 3, KB: 1, Entries: 1},
						{MinTime: 3, MaxTime: 4, KB: 1, Entries: 1}, // partial overlap

						{MinTime: 4, MaxTime: 5, KB: 1, Entries: 1}, // full overlap
						{MinTime: 5, MaxTime: 6, KB: 1, Entries: 1},

						{MinTime: 6, MaxTime: 7, KB: 1, Entries: 1}, // full overlap
						{MinTime: 7, MaxTime: 8, KB: 1, Entries: 1},

						{MinTime: 8, MaxTime: 9, KB: 1, Entries: 1}, // partial overlap
						{MinTime: 9, MaxTime: 10, KB: 1, Entries: 1},
					},
					from:    4,
					through: 9,
					exp: ChunkStats{
						// same deal with previous case, 1ns overlap isn't enough
						// to include its data
						Chunks:  6,
						KB:      5,
						Entries: 5,
					},
				},
			} {
				t.Run(fmt.Sprintf("%s_version=%d_pageSize=%d", tc.desc, version, pageSize), func(t *testing.T) {
					var w Writer
					w.Version = version
					primary := encoding.EncWrap(tsdb_enc.Encbuf{B: make([]byte, 0)})
					scratch := encoding.EncWrap(tsdb_enc.Encbuf{B: make([]byte, 0)})
					w.addChunks(tc.chks, &primary, &scratch, pageSize)

					decbuf := encoding.DecWrap(tsdb_enc.Decbuf{B: primary.Get()})
					dec := newDecoder(nil, 0)

					stats, err := dec.readChunkStats(version, &decbuf, 1, tc.from, tc.through)
					require.Nil(t, err)
					require.Equal(t, tc.exp, stats)
				})
			}
		}
	}
}

func BenchmarkChunkStats(b *testing.B) {
	for _, nChks := range []int{2, 4, 10, 100, 1000, 10000, 100000} {
		chks := mkChks(nChks)
		// Only request the middle 20% of chunks.
		from, through := int64(nChks*40/100), int64(nChks*60/100)
		for _, version := range []int{FormatV2, FormatV3} {
			b.Run(fmt.Sprintf("version %d/%d chunks", version, nChks), func(b *testing.B) {
				var w Writer
				w.Version = version
				primary := encoding.EncWrap(tsdb_enc.Encbuf{B: make([]byte, 0)})
				scratch := encoding.EncWrap(tsdb_enc.Encbuf{B: make([]byte, 0)})
				w.addChunks(chks, &primary, &scratch, ChunkPageSize)

				dec := newDecoder(nil, DefaultMaxChunksToBypassMarkerLookup)
				for i := 0; i < b.N; i++ {
					decbuf := encoding.DecWrap(tsdb_enc.Decbuf{B: primary.Get()})

					_, _ = dec.readChunkStats(version, &decbuf, 1, from, through)
				}
			})
		}
	}
}

func BenchmarkReadChunks(b *testing.B) {
	for _, nChks := range []int{2, 4, 10, 50, 100, 150, 1000, 10000, 100000} {
		chks := mkChks(nChks)
		res := ChunkMetasPool.Get()
		// Only request the middle 20% of chunks.
		from, through := int64(nChks*40/100), int64(nChks*60/100)
		for _, version := range []int{FormatV2, FormatV3} {
			b.Run(fmt.Sprintf("version %d/%d chunks", version, nChks), func(b *testing.B) {
				var w Writer
				w.Version = version
				primary := encoding.EncWrap(tsdb_enc.Encbuf{B: make([]byte, 0)})
				scratch := encoding.EncWrap(tsdb_enc.Encbuf{B: make([]byte, 0)})
				w.addChunks(chks, &primary, &scratch, ChunkPageSize)

				dec := newDecoder(nil, DefaultMaxChunksToBypassMarkerLookup)
				for i := 0; i < b.N; i++ {
					decbuf := encoding.DecWrap(tsdb_enc.Decbuf{B: primary.Get()})

					_ = dec.readChunks(version, &decbuf, 1, from, through, &res)
				}
			})
		}
	}

}
