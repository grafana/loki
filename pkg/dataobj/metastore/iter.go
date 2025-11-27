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
	buf := make([]streamSectionPointerBuilder, batchSize)

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
				buildPointersTimeRangePredicate(colMinTimestamp, colMaxTimestamp, sStart, sEnd),
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
						buf[rIdx].path = col.(*array.String).Value(rIdx)
					}
				case pointers.ColumnTypeSection:
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].section = col.(*array.Int64).Value(rIdx)
					}
				case pointers.ColumnTypeStreamID:
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].streamID = col.(*array.Int64).Value(rIdx)
					}
				case pointers.ColumnTypeStreamIDRef:
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].streamIDRef = col.(*array.Int64).Value(rIdx)
					}
				case pointers.ColumnTypeMinTimestamp:
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].start = time.Unix(0, int64(col.(*array.Timestamp).Value(rIdx)))
					}
				case pointers.ColumnTypeMaxTimestamp:
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].end = time.Unix(0, int64(col.(*array.Timestamp).Value(rIdx)))
					}
				case pointers.ColumnTypeRowCount:
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].lineCount = col.(*array.Int64).Value(rIdx)
					}
				case pointers.ColumnTypeUncompressedSize:
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].uncompressedSize = col.(*array.Int64).Value(rIdx)
					}
				default:
					continue
				}
			}

			for rowIdx := range numRows {
				b := buf[rowIdx]
				f(pointers.SectionPointer{
					Path:             b.path,
					Section:          b.section,
					PointerKind:      pointers.PointerKindStreamIndex,
					StreamID:         b.streamID,
					StreamIDRef:      b.streamIDRef,
					StartTs:          b.start,
					EndTs:            b.end,
					LineCount:        b.lineCount,
					UncompressedSize: b.uncompressedSize,
				})
			}

			if errors.Is(readErr, io.EOF) {
				break
			}
		}
	}

	return nil
}

type streamSectionPointerBuilder struct {
	path             string
	section          int64
	streamID         int64
	streamIDRef      int64
	start            time.Time
	end              time.Time
	lineCount        int64
	uncompressedSize int64
}

func (b streamSectionPointerBuilder) build() streamSectionPointerBuilder {
	return streamSectionPointerBuilder{
		path:             b.path,
		section:          b.section,
		streamID:         b.streamID,
		streamIDRef:      b.streamIDRef,
		start:            b.start,
		end:              b.end,
		lineCount:        b.lineCount,
		uncompressedSize: b.uncompressedSize,
	}
}

func buildPointersTimeRangePredicate(
	colMinTimestamp, colMaxTimestamp *pointers.Column,
	sStart scalar.Scalar, sEnd scalar.Scalar,
) pointers.Predicate {
	return pointers.AndPredicate{
		Left: pointers.OrPredicate{
			Left: pointers.EqualPredicate{
				Column: colMaxTimestamp,
				Value:  sStart,
			},
			Right: pointers.GreaterThanPredicate{
				Column: colMaxTimestamp,
				Value:  sStart,
			},
		},
		Right: pointers.OrPredicate{
			Left: pointers.EqualPredicate{
				Column: colMinTimestamp,
				Value:  sEnd,
			},
			Right: pointers.LessThanPredicate{
				Column: colMinTimestamp,
				Value:  sEnd,
			},
		},
	}
}
