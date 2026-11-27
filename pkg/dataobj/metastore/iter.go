package metastore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/util"
)

func forEachIndexPointer(
	ctx context.Context,
	object *dataobj.Object,
	sStart, sEnd *scalar.Timestamp,
	f func(pointer indexpointers.IndexPointer),
) (err error) {
	targetTenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return fmt.Errorf("extracting org ID: %w", err)
	}
	var reader indexpointers.Reader
	defer util.CloseAndHandleError(&reader, &err)

	const batchSize = 1024
	buf := make([]indexpointers.IndexPointer, batchSize)

	// The arrays of a batch use memory from alloc. The allocator reuses that
	// memory after Reclaim, so a pointer must own its strings.
	alloc := memory.NewAllocator(nil)
	defer alloc.Free()

	// Read each section in batches. decodePointers fills buf column by column, and
	// f receives every row of the batch. A batch can hold fewer rows than len(buf).
	for _, section := range object.Sections().Filter(indexpointers.CheckSection) {
		if section.Tenant != targetTenant {
			continue
		}

		sec, err := indexpointers.Open(ctx, section)
		if err != nil {
			return fmt.Errorf("opening section: %w", err)
		}

		var (
			colPath         *indexpointers.Column
			colMinTimestamp *indexpointers.Column
			colMaxTimestamp *indexpointers.Column
		)

		for _, c := range sec.Columns() {
			if c.Type == indexpointers.ColumnTypePath {
				colPath = c
			}
			if c.Type == indexpointers.ColumnTypeMinTimestamp {
				colMinTimestamp = c
			}
			if c.Type == indexpointers.ColumnTypeMaxTimestamp {
				colMaxTimestamp = c
			}
			if colPath != nil && colMinTimestamp != nil && colMaxTimestamp != nil {
				break
			}
		}

		if colPath == nil || colMinTimestamp == nil || colMaxTimestamp == nil {
			return fmt.Errorf("one of the mandatory columns is missing: (path=%t, minTimestamp=%t, maxTimestamp=%t)", colPath == nil, colMinTimestamp == nil, colMaxTimestamp == nil)
		}

		reader.Reset(indexpointers.ReaderOptions{
			Columns: sec.Columns(),
			Predicates: []indexpointers.Predicate{
				indexpointers.WhereTimeRangeOverlapsWith(colMinTimestamp, colMaxTimestamp, sStart, sEnd),
			},
		})
		if err := reader.Open(ctx); err != nil {
			return fmt.Errorf("opening index pointers reader: %w", err)
		}

		for {
			alloc.Reclaim()
			batch, readErr := reader.ReadRecordBatch(ctx, alloc, batchSize)
			if errors.Is(readErr, io.EOF) {
				break
			} else if readErr != nil {
				return fmt.Errorf("reading recordBatch: %w", readErr)
			}

			if err := decodePointers(batch, buf); err != nil {
				return err
			}
			for rowIdx := range batch.Rows {
				f(buf[rowIdx])
			}
		}
	}

	return nil
}

// decodePointers decodes Path, the timestamps, RunID, RunLength, ShardBuckets
// and PostingKeys of every row of batch into buf. A column that the batch does
// not hold, and a null element, leave the field at its zero value.
//
// It returns an error when a row does not decode. Then the content of
// buf[:batch.Rows] is undefined. It panics when buf holds fewer than batch.Rows
// pointers.
//
// The pointers own their strings, so they stay valid after the caller reclaims
// the allocator of batch.
func decodePointers(batch *indexpointers.RecordBatch, buf []indexpointers.IndexPointer) error {
	numRows := batch.Rows

	// A null value of an optional column leaves its field unset, so
	// clear the rows of the previous batch first.
	clear(buf[:numRows])

	if values := batch.Path; values != nil {
		for rIdx := range numRows {
			if values.IsNull(rIdx) {
				continue
			}
			buf[rIdx].Path = string(values.Get(rIdx))
		}
	}
	if values := batch.MinTimestamp; values != nil {
		for rIdx := range numRows {
			if values.IsNull(rIdx) {
				continue
			}
			buf[rIdx].StartTs = time.Unix(0, values.Get(rIdx))
		}
	}
	if values := batch.MaxTimestamp; values != nil {
		for rIdx := range numRows {
			if values.IsNull(rIdx) {
				continue
			}
			buf[rIdx].EndTs = time.Unix(0, values.Get(rIdx))
		}
	}
	if values := batch.ShardBuckets; values != nil {
		for rIdx := range numRows {
			if values.IsNull(rIdx) {
				continue
			}
			buckets, err := indexpointers.ShardBucketsFromCount(values.Get(rIdx))
			if err != nil {
				return fmt.Errorf("decoding index pointer %d: %w", rIdx, err)
			}
			buf[rIdx].ShardBuckets = buckets
		}
	}
	if values := batch.RunID; values != nil {
		for rIdx := range numRows {
			if values.IsNull(rIdx) {
				continue
			}
			runID, err := indexpointers.RunIDFromValue(values.Get(rIdx))
			if err != nil {
				return fmt.Errorf("decoding index pointer %d: %w", rIdx, err)
			}
			buf[rIdx].RunID = runID
		}
	}
	if values := batch.RunLength; values != nil {
		for rIdx := range numRows {
			if values.IsNull(rIdx) {
				continue
			}
			runLength, err := indexpointers.RunLengthFromValue(values.Get(rIdx))
			if err != nil {
				return fmt.Errorf("decoding index pointer %d: %w", rIdx, err)
			}
			buf[rIdx].RunLength = runLength
		}
	}

	// A posting key range needs both bounds, so decode them together.
	for rIdx := range numRows {
		keys, err := indexpointers.PostingKeysFromBounds(binaryValue(batch.MinPostingKey, rIdx), binaryValue(batch.MaxPostingKey, rIdx))
		if err != nil {
			return fmt.Errorf("decoding index pointer %d: %w", rIdx, err)
		}
		buf[rIdx].PostingKeys = keys
	}
	return nil
}

// binaryValue returns element i of values. It returns nil when values is nil or
// the element is null. The result points into the memory of values, so it is
// valid until the caller reclaims the allocator.
//
// A non-null empty element returns an empty slice that is not nil. Then
// [indexpointers.PostingKeysFromBounds] reports the empty bound as not valid,
// and it does not read it as a null bound.
func binaryValue(values *columnar.UTF8, i int) []byte {
	if values == nil || values.IsNull(i) {
		return nil
	}
	if value := values.Get(i); value != nil {
		return value
	}
	return []byte{}
}
