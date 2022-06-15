package index

import (
	"sort"

	"github.com/prometheus/common/model"
)

// Meta holds information about a chunk of data.
type ChunkMeta struct {
	Checksum uint32

	MinTime, MaxTime int64

	// Bytes stored, rounded to nearest KB
	KB uint32

	Entries uint32
}

func (c ChunkMeta) From() model.Time                 { return model.Time(c.MinTime) }
func (c ChunkMeta) Through() model.Time              { return model.Time(c.MaxTime) }
func (c ChunkMeta) Bounds() (model.Time, model.Time) { return c.From(), c.Through() }

type ChunkMetas []ChunkMeta

func (c ChunkMetas) Len() int      { return len(c) }
func (c ChunkMetas) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c ChunkMetas) Bounds() (mint, maxt model.Time) {
	ln := len(c)
	if ln == 0 {
		return
	}

	mint, maxt = model.Time(c[0].MinTime), model.Time(c[ln-1].MaxTime)
	// even when sorted, we need to check all chunks for maxt
	// since we sort by (min, max, checksum). Therefore
	// check mint here as well to ensure this works on unordered ChunkMetas too
	for _, chk := range c {
		from, through := chk.Bounds()
		if mint > from {
			mint = from
		}

		if maxt < through {
			maxt = through
		}
	}
	return
}

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

// Finalize sorts and dedupes
// TODO(owen-d): can we remove the need for this by ensuring we only push
// in order and without duplicates?
func (c ChunkMetas) Finalize() ChunkMetas {
	sort.Sort(c)

	if len(c) == 0 {
		return c
	}

	res := ChunkMetasPool.Get()
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
		ChunkMetasPool.Put(res) // release unused slice to pool
		return c
	}

	// otherwise, append any remaining values
	res = append(res, c[lastDuplicate+1:]...)
	// release self to pool; res will be returned instead
	ChunkMetasPool.Put(c)
	return res

}
