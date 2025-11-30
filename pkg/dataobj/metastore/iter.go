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
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
)

// forEachStreamSectionPointer iterates over all the section pointers that point to one of the
// [streamIDs] in a given [indexObj] that overlap [sStart, sEnd] (inclusive) time range and
// calls [f] on every found SectionPointer.
func forEachStreamSectionPointer(
	ctx context.Context,
	indexObj *dataobj.Object,
	sStart, sEnd *scalar.Timestamp,
	streamIDs []int64,
	f func(pointers.SectionPointer),
) error {
	targetTenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return fmt.Errorf("extracting org ID: %w", err)
	}
	var reader pointers.Reader
	defer reader.Close()

	// prepare streamIDs scalars only once, they are used in the predicate later
	var sStreamIDs []scalar.Scalar
	for _, streamID := range streamIDs {
		sStreamIDs = append(sStreamIDs, scalar.NewInt64Scalar(streamID))
	}

	const batchSize = 128
	buf := make([]pointers.SectionPointer, batchSize)

	// iterate over the sections and fill buf column by column
	// once the read operation is over invoke client's [f] on every read row (numRows not always the same as len(buf))
	for _, section := range indexObj.Sections().Filter(pointers.CheckSection) {
		if section.Tenant != targetTenant {
			continue
		}

		sec, err := pointers.Open(ctx, section)
		if err != nil {
			return fmt.Errorf("opening section: %w", err)
		}

		pointerCols, err := findPointersColumnsByTypes(
			sec.Columns(),
			pointers.ColumnTypePath,
			pointers.ColumnTypeSection,
			pointers.ColumnTypeStreamID,
			pointers.ColumnTypeStreamIDRef,
			pointers.ColumnTypeMinTimestamp,
			pointers.ColumnTypeMaxTimestamp,
			pointers.ColumnTypeRowCount,
			pointers.ColumnTypeUncompressedSize,
		)
		if err != nil {
			return fmt.Errorf("finding pointers columns: %w", err)
		}

		var (
			colStreamID     *pointers.Column
			colMinTimestamp *pointers.Column
			colMaxTimestamp *pointers.Column
		)

		for _, c := range pointerCols {
			if c.Type == pointers.ColumnTypeStreamID {
				colStreamID = c
			}
			if c.Type == pointers.ColumnTypeMinTimestamp {
				colMinTimestamp = c
			}
			if c.Type == pointers.ColumnTypeMaxTimestamp {
				colMaxTimestamp = c
			}
			if colStreamID != nil && colMinTimestamp != nil && colMaxTimestamp != nil {
				break
			}
		}

		if colStreamID == nil || colMinTimestamp == nil || colMaxTimestamp == nil {
			return fmt.Errorf(
				"one of mandatory columns is missing: (streamID=%t, minTimestamp=%t, maxTimestamp=%t)",
				colStreamID == nil, colMinTimestamp == nil, colMaxTimestamp == nil,
			)
		}

		reader.Reset(pointers.ReaderOptions{
			Columns: pointerCols,
			Predicates: []pointers.Predicate{
				pointers.WhereTimeRangeOverlapsWith(colMinTimestamp, colMaxTimestamp, sStart, sEnd),
				pointers.InPredicate{
					Column: colStreamID,
					Values: sStreamIDs,
				},
			},
		})

		for {
			rec, readErr := reader.Read(ctx, batchSize)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return fmt.Errorf("reading recordBatch: %w", readErr)
			}
			numRows := int(rec.NumRows())
			if numRows == 0 && errors.Is(readErr, io.EOF) {
				break
			}

			for colIdx := range int(rec.NumCols()) {
				col := rec.Column(colIdx)
				pointerCol := pointerCols[colIdx]

				switch pointerCol.Type {
				case pointers.ColumnTypePath:
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].Path = col.(*array.String).Value(rIdx)
					}
				case pointers.ColumnTypeSection:
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].Section = col.(*array.Int64).Value(rIdx)
					}
				case pointers.ColumnTypeStreamID:
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].StreamID = col.(*array.Int64).Value(rIdx)
					}
				case pointers.ColumnTypeStreamIDRef:
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].StreamIDRef = col.(*array.Int64).Value(rIdx)
					}
				case pointers.ColumnTypeMinTimestamp:
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].StartTs = time.Unix(0, int64(col.(*array.Timestamp).Value(rIdx)))
					}
				case pointers.ColumnTypeMaxTimestamp:
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].EndTs = time.Unix(0, int64(col.(*array.Timestamp).Value(rIdx)))
					}
				case pointers.ColumnTypeRowCount:
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].LineCount = col.(*array.Int64).Value(rIdx)
					}
				case pointers.ColumnTypeUncompressedSize:
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].UncompressedSize = col.(*array.Int64).Value(rIdx)
					}
				default:
					continue
				}
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
