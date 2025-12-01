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
			// the section has no rows with stream-based indices and can be ignored completely
			continue
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
					values := col.(*array.String)
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].Path = values.Value(rIdx)
					}
				case pointers.ColumnTypeSection:
					values := col.(*array.Int64)
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].Section = values.Value(rIdx)
					}
				case pointers.ColumnTypeStreamID:
					values := col.(*array.Int64)
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].StreamID = values.Value(rIdx)
					}
				case pointers.ColumnTypeStreamIDRef:
					values := col.(*array.Int64)
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].StreamIDRef = values.Value(rIdx)
					}
				case pointers.ColumnTypeMinTimestamp:
					values := col.(*array.Timestamp)
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].StartTs = time.Unix(0, int64(values.Value(rIdx)))
					}
				case pointers.ColumnTypeMaxTimestamp:
					values := col.(*array.Timestamp)
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].EndTs = time.Unix(0, int64(values.Value(rIdx)))
					}
				case pointers.ColumnTypeRowCount:
					values := col.(*array.Int64)
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].LineCount = values.Value(rIdx)
					}
				case pointers.ColumnTypeUncompressedSize:
					values := col.(*array.Int64)
					for rIdx := range numRows {
						if col.IsNull(rIdx) {
							continue
						}
						buf[rIdx].UncompressedSize = values.Value(rIdx)
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

func forEachMatchedPointerSectionKey(
	ctx context.Context,
	object *dataobj.Object,
	columnName scalar.Scalar,
	matchColumnValue string,
	f func(key SectionKey),
) error {
	targetTenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return fmt.Errorf("extracting org ID: %w", err)
	}

	var reader pointers.Reader
	defer reader.Close()

	const batchSize = 128
	buf := make([]SectionKey, batchSize)

	for _, section := range object.Sections().Filter(pointers.CheckSection) {
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
			pointers.ColumnTypeColumnName,
			pointers.ColumnTypeValuesBloomFilter,
		)
		if err != nil {
			return fmt.Errorf("finding pointers columns: %w", err)
		}

		var (
			colColumnName *pointers.Column
			colBloom      *pointers.Column
		)

		for _, c := range pointerCols {
			if c.Type == pointers.ColumnTypeColumnName {
				colColumnName = c
			}
			if c.Type == pointers.ColumnTypeValuesBloomFilter {
				colBloom = c
			}
			if colColumnName != nil && colBloom != nil {
				break
			}
		}

		if colColumnName == nil || colBloom == nil {
			// the section has no rows for blooms and can be ignored completely
			continue
		}

		reader.Reset(
			pointers.ReaderOptions{
				Columns: pointerCols,
				Predicates: []pointers.Predicate{
					pointers.WhereBloomFilterMatches(colColumnName, colBloom, columnName, matchColumnValue),
				},
			},
		)

		for {
			rec, readErr := reader.Read(ctx, batchSize)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return fmt.Errorf("reading record batch: %w", readErr)
			}
			if rec == nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				continue
			}

			numRows := int(rec.NumRows())
			if numRows == 0 {
				rec.Release()
				if errors.Is(readErr, io.EOF) {
					break
				}
				continue
			}

			for colIdx := range int(rec.NumCols()) {
				col := rec.Column(colIdx)
				pointerCol := pointerCols[colIdx]

				switch pointerCol.Type {
				case pointers.ColumnTypePath:
					values := col.(*array.String)
					for rowIdx := range numRows {
						if col.IsNull(rowIdx) {
							continue
						}
						buf[rowIdx].ObjectPath = values.Value(rowIdx)
					}
				case pointers.ColumnTypeSection:
					values := col.(*array.Int64)
					for rowIdx := range numRows {
						if col.IsNull(rowIdx) {
							continue
						}
						buf[rowIdx].SectionIdx = values.Value(rowIdx)
					}
				default:
					continue
				}
			}

			for _, sectionKey := range buf {
				f(sectionKey)
			}

			if errors.Is(readErr, io.EOF) {
				break
			}
		}
	}

	return nil
}

func findPointersColumnsByTypes(allColumns []*pointers.Column, columnTypes ...pointers.ColumnType) ([]*pointers.Column, error) {
	result := make([]*pointers.Column, 0, len(columnTypes))

	for _, c := range allColumns {
		for _, neededType := range columnTypes {
			if neededType != c.Type {
				continue
			}

			result = append(result, c)
		}
	}

	return result, nil
}
