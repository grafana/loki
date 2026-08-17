package streams

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
)

// ReadShardBuckets returns the shard bucket of each requested stream ID, reading only the stream_id and
// __shard_bucket__ columns — the stream labels are never decoded. It pushes the IDs down as a stream-ID
// predicate, so only the requested rows are scanned.
//
// Check err before ok: on any read error it returns (nil, false, err). When err is nil, ok reports
// whether the section has the __shard_bucket__ column; ok is false for a data object written before the
// column existed, and the caller must then fall back to computing shard membership from stream labels.
//
// A requested ID absent from the returned map does not exist in the section — except that a stream whose
// stored bucket value is null is also skipped. The writer always populates the column, so null values do
// not occur in practice.
func ReadShardBuckets(ctx context.Context, sec *Section, ids []int64) (buckets map[int64]uint64, ok bool, err error) {
	var idCol, bucketCol *Column
	for _, col := range sec.Columns() {
		switch col.Type {
		case ColumnTypeStreamID:
			idCol = col
		case ColumnTypeShardBucket:
			bucketCol = col
		}
	}
	if idCol == nil || bucketCol == nil {
		return nil, false, nil
	}

	buckets = make(map[int64]uint64, len(ids))
	if len(ids) == 0 {
		return buckets, true, nil
	}

	values := make([]scalar.Scalar, len(ids))
	for i, id := range ids {
		values[i] = scalar.NewInt64Scalar(id)
	}

	reader := NewReader(ReaderOptions{
		Columns:    []*Column{idCol, bucketCol},
		Predicates: []Predicate{InPredicate{Column: idCol, Values: values}},
		Allocator:  memory.DefaultAllocator,
	})
	defer reader.Close()
	if err := reader.Open(ctx); err != nil {
		return nil, false, fmt.Errorf("opening shard-bucket reader: %w", err)
	}

	for {
		batch, readErr := reader.Read(ctx, 1024)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			if batch != nil {
				batch.Release()
			}
			return nil, false, fmt.Errorf("reading shard buckets: %w", readErr)
		}
		// Read may return a non-nil batch (possibly with zero rows) alongside io.EOF; release it either
		// way per the Reader.Read contract.
		if batch != nil {
			if batch.NumRows() > 0 {
				idArr, ok := batch.Column(0).(*array.Int64)
				if !ok {
					batch.Release()
					return nil, false, fmt.Errorf("stream_id column has unexpected type %T", batch.Column(0))
				}
				bucketArr, ok := batch.Column(1).(*array.Int64)
				if !ok {
					batch.Release()
					return nil, false, fmt.Errorf("__shard_bucket__ column has unexpected type %T", batch.Column(1))
				}
				for i := 0; i < int(batch.NumRows()); i++ {
					if idArr.IsNull(i) || bucketArr.IsNull(i) {
						continue
					}
					buckets[idArr.Value(i)] = uint64(bucketArr.Value(i))
				}
			}
			batch.Release()
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	return buckets, true, nil
}
