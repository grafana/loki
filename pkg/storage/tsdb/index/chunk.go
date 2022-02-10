package index

// Meta holds information about a chunk of data.
type ChunkMeta struct {
	Checksum uint32

	// Time range the data covers.
	// When MaxTime == math.MaxInt64 the chunk is still open and being appended to.
	MinTime, MaxTime int64
}
