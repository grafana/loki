package chunks

type ChunkRef uint64

type Meta struct {
	// Start offset of the chunk
	Ref ChunkRef
	// Min and Max time nanoseconds precise.
	MinTime, MaxTime int64
}
