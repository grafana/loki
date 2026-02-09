package columnar

// RecordBatch is a collection of equal-length arrays.
// This corresponds to the RecordBatch concept in the Arrow specification.
type RecordBatch struct {
	schema *Schema
	nrows  int64
	arrs   []Array
}

// NewRecordBatch returns a new RecordBatch created from the provided arrays.
// nrows specifies the total number of rows in the batch.
func NewRecordBatch(schema *Schema, nrows int64, arrs []Array) *RecordBatch {
	return &RecordBatch{
		schema: schema,
		nrows:  nrows,
		arrs:   arrs,
	}
}

// Schema returns the schema of the record batch.
func (rb *RecordBatch) Schema() *Schema {
	return rb.schema
}

// NumRows returns the number of rows in the batch.
func (rb *RecordBatch) NumRows() int64 {
	return rb.nrows
}

// NumCols returns the number of columns in the batch.
func (rb *RecordBatch) NumCols() int64 {
	return int64(len(rb.arrs))
}

// Column returns the column array at index i.
func (rb *RecordBatch) Column(i int64) Array {
	return rb.arrs[i]
}

// Slice returns a slice of rb from index i to j. The returned slice has a
// length of j-i and shares memory with rb.
//
// Slice panics if the following invariant is not met: 0 <= i <= j <= rb.NumRows()
func (rb *RecordBatch) Slice(i, j int) *RecordBatch {
	if i < 0 || j < i || j > int(rb.NumRows()) {
		panic(errorSliceBounds{i, j, int(rb.NumRows())})
	}

	var (
		nrows = j - i
		arrs  = make([]Array, len(rb.arrs))
	)
	for idx := range len(arrs) {
		arrs[idx] = rb.arrs[idx].Slice(i, j)
	}
	return NewRecordBatch(rb.schema, int64(nrows), arrs)
}
