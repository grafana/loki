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
