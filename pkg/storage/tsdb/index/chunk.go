package index

import (
	"sort"
)

// Meta holds information about a chunk of data.
type ChunkMeta struct {
	Checksum uint32

	MinTime, MaxTime int64

	// Bytes stored, rounded to nearest KB
	KB uint32

	Entries uint32
}

type ChunkMetas []ChunkMeta

func (c ChunkMetas) Len() int      { return len(c) }
func (c ChunkMetas) Swap(i, j int) { c[i], c[j] = c[j], c[i] }

// Sort by (MinTime, MaxTime, Checksum)
func (c ChunkMetas) Less(i, j int) bool {
	a, b := c[i], c[j]
	if a.MinTime != b.MinTime {
		return a.MinTime < b.MinTime
	}

	if a.MaxTime != b.MaxTime {
		return a.MaxTime < b.MaxTime
	}

	return a.Checksum < b.Checksum
}

// finalize sorts and dedupes
// TODO(owen-d): can we remove the need for this by ensuring we only push
// in order and without duplicates?
func (c ChunkMetas) finalize() ChunkMetas {
	sort.Sort(c)

	if len(c) == 0 {
		return c
	}

	var res ChunkMetas
	lastDuplicate := -1
	prior := c[0]

	// minimize reslicing costs due to duplicates
	for i := 1; i < len(c); i++ {
		x := c[i]
		if x.MinTime == prior.MinTime && x.MaxTime == prior.MaxTime && x.Checksum == prior.Checksum {
			res = append(res, c[lastDuplicate+1:i]...)
			lastDuplicate = i
		}
		prior = x
	}

	// no duplicates were found, short circuit
	// by returning unmodified underlying slice
	if len(res) == 0 {
		return c
	}

	// otherwise, append any remaining values
	res = append(res, c[lastDuplicate+1:]...)
	return res

}
