package index

// Meta holds information about a chunk of data.
type ChunkMeta struct {
	Checksum uint32

	// Nanosecond precision
	MinTime, MaxTime int64

	// Bytes use an uint64 as an uint32 can only hold [0,4GB)
	// While this is well within current chunk guidelines (1.5MB being "standard"),
	// I (owen-d) prefer to overallocate here
	// Since TSDB accesses are seeked rather than scanned, this choice
	// should have little effect as long as there is enough memory available
	Bytes uint64

	Entries uint32
}
