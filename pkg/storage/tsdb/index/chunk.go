package index

// Meta holds information about a chunk of data.
type ChunkMeta struct {
	Checksum uint32

	MinTime, MaxTime int64

	// Bytes use an uint64 as an uint32 can only hold [0,4GB)
	// While this is well within current chunk guidelines (1.5MB being "standard"),
	// I (owen-d) prefer to overallocate here
	// Since TSDB accesses are seeked rather than scanned, this choice
	// should have little effect as long as there is enough memory available
	Bytes uint64

	Entries uint32
}

type ChunkMetas []ChunkMeta

func (c ChunkMetas) Len() int      { return len(c) }
func (c ChunkMetas) Swap(i, j int) { c[i], c[j] = c[j], c[i] }

// Sort by (MinTime, MaxTime, Checksum)
func (c ChunkMetas) Less(i, j int) bool {
	a, b := c[i], c[j]
	if a.MinTime < b.MinTime {
		return true
	} else if a.MinTime > b.MinTime {
		return false
	}

	if a.MaxTime < b.MaxTime {
		return true
	} else if a.MaxTime > b.MaxTime {
		return false
	}

	return a.Checksum < b.Checksum
}
