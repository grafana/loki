package metastore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

func forEachIndexPointer(
	ctx context.Context,
	object *dataobj.Object,
	sStart, sEnd *scalar.Timestamp,
	f func(pointer indexpointers.IndexPointer),
) error {
	targetTenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return fmt.Errorf("extracting org ID: %w", err)
	}
	var reader indexpointers.Reader
	defer reader.Close()

	const batchSize = 1024
	buf := make([]indexpointers.IndexPointer, batchSize)

	// iterate over the sections and fill buf column by column
	// once the read operation is over invoke client's [f] on every read row (numRows not always the same as len(buf))
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
			rec, readErr := reader.Read(ctx, batchSize)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return fmt.Errorf("reading recordBatch: %w", readErr)
			}
			numRows := int(rec.NumRows())
			if numRows == 0 && errors.Is(readErr, io.EOF) {
				break
			}

			// A null value of an optional column leaves its field unset, so
			// clear the rows of the previous batch first.
			clear(buf[:numRows])

			// The posting key bounds decode together, after both columns.
			var minPostingKeys, maxPostingKeys *array.Binary

			for colIdx := range int(rec.NumCols()) {
				col := rec.Column(colIdx)
				pointerCol := sec.Columns()[colIdx]

				switch pointerCol.Type {
				case indexpointers.ColumnTypePath:
					values := col.(*array.String)
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].Path = values.Value(rIdx)
					}
				case indexpointers.ColumnTypeMinTimestamp:
					values := col.(*array.Timestamp)
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].StartTs = time.Unix(0, int64(values.Value(rIdx)))
					}
				case indexpointers.ColumnTypeMaxTimestamp:
					values := col.(*array.Timestamp)
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].EndTs = time.Unix(0, int64(values.Value(rIdx)))
					}
				case indexpointers.ColumnTypeShardBuckets:
					values := col.(*array.Int64)
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buckets, err := indexpointers.ShardBucketsFromCount(values.Value(rIdx))
						if err != nil {
							return fmt.Errorf("decoding index pointer %d: %w", rIdx, err)
						}
						buf[rIdx].ShardBuckets = buckets
					}
				case indexpointers.ColumnTypeRunID:
					values := col.(*array.Int64)
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						runID, err := indexpointers.RunIDFromValue(values.Value(rIdx))
						if err != nil {
							return fmt.Errorf("decoding index pointer %d: %w", rIdx, err)
						}
						buf[rIdx].RunID = runID
					}
				case indexpointers.ColumnTypeMinPostingKey:
					if minPostingKeys != nil {
						return fmt.Errorf("section has more than one %s column", pointerCol.Type)
					}
					minPostingKeys = col.(*array.Binary)
				case indexpointers.ColumnTypeMaxPostingKey:
					if maxPostingKeys != nil {
						return fmt.Errorf("section has more than one %s column", pointerCol.Type)
					}
					maxPostingKeys = col.(*array.Binary)
				default:
					continue
				}
			}

			for rIdx := range numRows {
				keys, err := indexpointers.PostingKeysFromBounds(binaryValue(minPostingKeys, rIdx), binaryValue(maxPostingKeys, rIdx))
				if err != nil {
					return fmt.Errorf("decoding index pointer %d: %w", rIdx, err)
				}
				buf[rIdx].PostingKeys = keys
			}

			for rowIdx := range numRows {
				f(buf[rowIdx])
			}

			if errors.Is(readErr, io.EOF) {
				break
			}
		}
	}

	return nil
}

// binaryValue returns the value of values at row i, or nil if values is nil or
// the value is null. The value borrows memory from values.
func binaryValue(values *array.Binary, i int) []byte {
	if values == nil || values.IsNull(i) {
		return nil
	}
	return values.Value(i)
}
