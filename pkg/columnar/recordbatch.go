package columnar

type RecordBatch struct {
	nrows int64
	arrs  []Array
}

func NewRecordBatch(nrows int64, arrs []Array) RecordBatch {
	return RecordBatch{
		nrows: nrows,
		arrs:  arrs,
	}
}

func (rb RecordBatch) NumRows() int64 {
	return rb.nrows
}

func (rb RecordBatch) NumCols() int64 {
	return int64(len(rb.arrs))
}

func (rb RecordBatch) Column(i int64) Array {
	return rb.arrs[i]
}
