package chunks

type ChunkRef uint64

type Meta struct {
	// Start offset of the chunk
	Ref ChunkRef
	// Min and Max time nanoseconds precise.
	MinTime, MaxTime int64
}

func NewChunkRef(offset, size uint64) ChunkRef {
	return ChunkRef(offset<<32 | size)
}

func (b ChunkRef) Unpack() (int, int) {
	offset := int(b >> 32)
	size := int((b << 32) >> 32)
	return offset, size
}
