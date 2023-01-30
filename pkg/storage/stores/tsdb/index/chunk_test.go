package index

import (
	"fmt"
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
