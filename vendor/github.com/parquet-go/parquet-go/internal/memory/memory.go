package memory

// Datum is a constraint for types that can be stored in chunk and slice buffers.
// It includes the common numeric types used in parquet files.
type Datum interface {
	~byte | ~int32 | ~int64 | ~uint32 | ~uint64 | ~float32 | ~float64
}
