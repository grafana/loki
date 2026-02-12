package executor

import (
	"fmt"
	"math"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// RecordBatchSizeBytes returns the approximate size in bytes of the record batch,
// by summing the size of all column data (including nested buffers).
// It is used to decide when to flush batched records in drainPipeline.
func RecordBatchSizeBytes(rec arrow.RecordBatch) int {
	if rec == nil {
		return 0
	}
	var n uint64
	for i := 0; i < int(rec.NumCols()); i++ {
		n += rec.Column(i).Data().SizeInBytes()
	}
	if n > math.MaxInt {
		return math.MaxInt
	}
	return int(n)
}

// ConcatenateRecordBatches concatenates the given records into a single RecordBatch.
// The schema of the first record is used. Caller must release the input records after use.
// The returned record batch must be released by the caller.
func ConcatenateRecordBatches(records []arrow.RecordBatch) (arrow.RecordBatch, error) {
	if len(records) == 0 {
		return nil, fmt.Errorf("need at least one record")
	}
	if len(records) == 1 {
		// Return a retained reference so the caller can release both the input and the result (which are actually the same).
		records[0].Retain()
		return records[0], nil
	}

	schema := records[0].Schema()
	numCols := schema.NumFields()
	concatenatedCols := make([]arrow.Array, numCols)
	for col := 0; col < numCols; col++ {
		colArrays := make([]arrow.Array, len(records))
		for i, r := range records {
			colArrays[i] = r.Column(col)
		}
		concat, err := array.Concatenate(colArrays, memory.DefaultAllocator)
		if err != nil {
			for j := 0; j < col; j++ {
				concatenatedCols[j].Release()
			}
			return nil, err
		}
		concatenatedCols[col] = concat
	}
	var totalRows int64
	for _, r := range records {
		totalRows += r.NumRows()
	}
	return array.NewRecordBatch(schema, concatenatedCols, totalRows), nil
}
