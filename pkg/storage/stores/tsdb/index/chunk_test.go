package index

import (
	"testing"

	"github.com/stretchr/testify/require"
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
