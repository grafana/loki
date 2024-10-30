package index

import (
	"sort"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/encoding"
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

// Add adds ChunkMeta at the right place in order. It assumes existing ChunkMetas have already been sorted by using Finalize.
// There is no chance of a data loss even if the chunks are not sorted because the chunk would anyways be added so the assumption is relatively safe.
func (c ChunkMetas) Add(chk ChunkMeta) ChunkMetas {
	j := sort.Search(len(c), func(i int) bool {
		ichk := c[i]
		if ichk.MinTime != chk.MinTime {
			return ichk.MinTime > chk.MinTime
		}

		if ichk.MaxTime != chk.MaxTime {
			return ichk.MaxTime > chk.MaxTime
		}

		return ichk.Checksum >= chk.Checksum
	})

	if j < len(c) && c[j].MinTime == chk.MinTime && c[j].MaxTime == chk.MaxTime && c[j].Checksum == chk.Checksum {
		return c
	}

	res := append(c, chk)
	copy(res[j+1:], res[j:])
	res[j] = chk

	return res
}

// Drop drops ChunkMeta. It assumes existing ChunkMetas have already been sorted by using Finalize.
// Calling Drop on non-sorted result can result in not dropping the chunk even if it exists.
// It returns a boolean indicating if the chunk was found and dropped or not so the caller can take appropriate actions if not.
// ToDo(Sandeep): See if we can do something about the assumption on sorted chunks.
// Maybe always build ChunkMetas using Add method which should always keep the ChunkMetas deduped and sorted.
func (c ChunkMetas) Drop(chk ChunkMeta) (ChunkMetas, bool) {
	j := sort.Search(len(c), func(i int) bool {
		ichk := c[i]
		if ichk.MinTime != chk.MinTime {
			return ichk.MinTime >= chk.MinTime
		}

		if ichk.MaxTime != chk.MaxTime {
			return ichk.MaxTime >= chk.MaxTime
		}

		return ichk.Checksum >= chk.Checksum
	})

	if j >= len(c) || c[j] != chk {
		return c, false
	}

	return c[:j+copy(c[j:], c[j+1:])], true
}

// Some of these fields can realistically be 32bit, but
// this gives us a lot of wiggle room and they're already
// encoded only for every n-th chunk based on `ChunkPageSize`
type chunkPageMarker struct {
	// ChunksInPage denotes the number of chunks
	// in the page
	ChunksInPage int

	// KB, Entries denote the KB and number of entries
	// in each page
	KB, Entries uint32

	// byte offset where this chunk starts relative
	// to the chunks in this series
	Offset int

	// bounds associated with this page
	MinTime, MaxTime int64

	// internal field to test if MinTime has been set since the zero
	// value is valid and I don't want to use a pointer. This is only
	// used during encoding.
	minTimeSet bool
}

func (m *chunkPageMarker) subsetOf(from, through int64) bool {
	return from <= m.MinTime && through >= m.MaxTime
}

func (m *chunkPageMarker) combine(c ChunkMeta) {
	m.KB += c.KB
	m.Entries += c.Entries
	if !m.minTimeSet || c.MinTime < m.MinTime {
		m.MinTime = c.MinTime
		m.minTimeSet = true
	}
	if c.MaxTime > m.MaxTime {
		m.MaxTime = c.MaxTime
	}
}

func (m *chunkPageMarker) encode(e *encoding.Encbuf, offset int, chunksRemaining int) {
	// put chunks, kb, entries, offset, mintime, maxtime
	e.PutUvarint(chunksRemaining)
	e.PutBE32(m.KB)
	e.PutBE32(m.Entries)
	e.PutUvarint(offset)
	e.PutVarint64(m.MinTime)
	e.PutVarint64(m.MaxTime - m.MinTime) // delta-encoded
}

func (m *chunkPageMarker) clear() {
	*m = chunkPageMarker{}
}

func (m *chunkPageMarker) decode(d *encoding.Decbuf) {
	m.ChunksInPage = d.Uvarint()
	m.KB = d.Be32()
	m.Entries = d.Be32()
	m.Offset = d.Uvarint()
	m.MinTime = d.Varint64()
	m.MaxTime = m.MinTime + d.Varint64()
}

// Chunks per page. This can be inferred in the data format
// via the ChunksRemaining field
// and can thus be changed without needing a
// new tsdb version
const ChunkPageSize = 16

// Minimum number of chunks present to use page based lookup
// instead of linear scan which performs better at lower n-values.
const DefaultMaxChunksToBypassMarkerLookup = 64

type chunkPageMarkers []chunkPageMarker

type ChunkStats struct {
	Chunks, KB, Entries uint64
}

func (cs *ChunkStats) addRaw(chunks int, kb, entries uint32) {
	cs.Chunks += uint64(chunks)
	cs.KB += uint64(kb)
	cs.Entries += uint64(entries)
}

func (cs *ChunkStats) AddChunk(chk *ChunkMeta, from, through int64) {
	factor := util.GetFactorOfTime(from, through, chk.MinTime, chk.MaxTime)
	kb := uint32(float64(chk.KB) * factor)
	entries := uint32(float64(chk.Entries) * factor)
	cs.addRaw(1, kb, entries)
}
