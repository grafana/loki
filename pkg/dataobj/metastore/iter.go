package metastore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
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

// forEachIndexPointerAllTenants invokes f for every index-pointer row in the object, across all tenants
// and unfiltered by time. It differs from forEachIndexPointer, which serves a query (one tenant, a time
// predicate); this reads the whole ToC so a warmer can cache it once and time-filter per query.
func forEachIndexPointerAllTenants(
	ctx context.Context,
	object *dataobj.Object,
	f func(tenant string, pointer indexpointers.IndexPointer),
) error {
	var reader indexpointers.Reader
	defer reader.Close()

	const batchSize = 1024
	buf := make([]indexpointers.IndexPointer, batchSize)

	for _, section := range object.Sections().Filter(indexpointers.CheckSection) {
		sec, err := indexpointers.Open(ctx, section)
		if err != nil {
			return fmt.Errorf("opening section: %w", err)
		}

		var hasPath, hasMin, hasMax bool
		for _, c := range sec.Columns() {
			switch c.Type {
			case indexpointers.ColumnTypePath:
				hasPath = true
			case indexpointers.ColumnTypeMinTimestamp:
				hasMin = true
			case indexpointers.ColumnTypeMaxTimestamp:
				hasMax = true
			}
		}
		if !hasPath || !hasMin || !hasMax {
			return fmt.Errorf("index pointers section for tenant %q is missing a mandatory column (path=%t, minTimestamp=%t, maxTimestamp=%t)", section.Tenant, hasPath, hasMin, hasMax)
		}

		reader.Reset(indexpointers.ReaderOptions{Columns: sec.Columns()})
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

			// Zero the rows before decoding: a null column value leaves buf[rIdx] untouched, so without
			// this a null could carry a value over from a previous batch or a different tenant's section.
			for i := range numRows {
				buf[i] = indexpointers.IndexPointer{}
			}

			for colIdx := range int(rec.NumCols()) {
				col := rec.Column(colIdx)
				switch sec.Columns()[colIdx].Type {
				case indexpointers.ColumnTypePath:
					values := col.(*array.String)
					for rIdx := range numRows {
						if !col.IsNull(rIdx) {
							buf[rIdx].Path = values.Value(rIdx)
						}
					}
				case indexpointers.ColumnTypeMinTimestamp:
					values := col.(*array.Timestamp)
					for rIdx := range numRows {
						if !col.IsNull(rIdx) {
							buf[rIdx].StartTs = time.Unix(0, int64(values.Value(rIdx)))
						}
					}
				case indexpointers.ColumnTypeMaxTimestamp:
					values := col.(*array.Timestamp)
					for rIdx := range numRows {
						if !col.IsNull(rIdx) {
							buf[rIdx].EndTs = time.Unix(0, int64(values.Value(rIdx)))
						}
					}
				default:
					continue
				}
			}

			for rowIdx := range numRows {
				f(section.Tenant, buf[rowIdx])
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

// buildStreamReaderPredicate builds predicates for the stream reader
// using the provided time range and label matchers.
func buildStreamReaderPredicate(sec *streams.Section, sStart, sEnd *scalar.Timestamp, matchers []*labels.Matcher) ([]streams.Predicate, error) {
	var (
		minTsColumn  *streams.Column
		maxTsColumn  *streams.Column
		labelColumns = make(map[string]*streams.Column)
	)

	for _, col := range sec.Columns() {
		switch col.Type {
		case streams.ColumnTypeMinTimestamp:
			minTsColumn = col
		case streams.ColumnTypeMaxTimestamp:
			maxTsColumn = col
		case streams.ColumnTypeLabel:
			labelColumns[col.Name] = col
		}
	}

	if minTsColumn == nil || maxTsColumn == nil {
		return nil, errors.New("buildStreamReaderPredicate: section is missing required columns")
	}

	var predicates []streams.Predicate
	predicates = append(predicates, buildTimeRangePredicate(minTsColumn, maxTsColumn, sStart, sEnd))
	for _, matcher := range matchers {
		predicates = append(predicates, buildLabelPredicate(matcher, labelColumns))
	}

	return predicates, nil
}

// buildTimeRangePredicate builds a predicate for time range overlap.
// A stream's [minTs, maxTs] overlaps with query [start, end] if:
// maxTs >= start AND minTs <= end
func buildTimeRangePredicate(minTsColumn, maxTsColumn *streams.Column, start, end *scalar.Timestamp) streams.Predicate {
	// maxTs >= start
	maxCheck := streams.NotPredicate{
		Inner: streams.LessThanPredicate{
			Column: maxTsColumn,
			Value:  start,
		},
	}

	// minTs <= end
	minCheck := streams.NotPredicate{
		Inner: streams.GreaterThanPredicate{
			Column: minTsColumn,
			Value:  end,
		},
	}

	return streams.AndPredicate{
		Left:  maxCheck,
		Right: minCheck,
	}
}

// buildLabelPredicate builds a predicate for a label matcher.
func buildLabelPredicate(matcher *labels.Matcher, columns map[string]*streams.Column) streams.Predicate {
	col := columns[matcher.Name]

	switch matcher.Type {
	case labels.MatchEqual:
		if col == nil && matcher.Value != "" {
			// Column(NULL) == "value" is always false
			return streams.FalsePredicate{}
		} else if col == nil && matcher.Value == "" {
			// Column(NULL) == "" is always true
			return streams.TruePredicate{}
		}

		buf := memory.NewBufferBytes([]byte(matcher.Value))
		return streams.EqualPredicate{
			Column: col,
			Value:  scalar.NewBinaryScalar(buf, arrow.BinaryTypes.Binary),
		}

	case labels.MatchNotEqual:
		if col == nil && matcher.Value != "" {
			// Column(NULL) != "value" is always true
			return streams.TruePredicate{}
		} else if col == nil && matcher.Value == "" {
			// Column(NULL) != "" is always false
			return streams.FalsePredicate{}
		}

		buf := memory.NewBufferBytes([]byte(matcher.Value))
		return streams.NotPredicate{
			Inner: streams.EqualPredicate{
				Column: col,
				Value:  scalar.NewBinaryScalar(buf, arrow.BinaryTypes.Binary),
			},
		}

	case labels.MatchRegexp:
		if col == nil {
			// Column(NULL) is treated as empty string.
			// Return true if regex matches "", false otherwise.
			if matcher.Matches("") {
				return streams.TruePredicate{}
			}
			return streams.FalsePredicate{}
		}

		return streams.FuncPredicate{
			Column: col,
			Keep: func(_ *streams.Column, value scalar.Scalar) bool {
				return matcher.Matches(string(getBytes(value)))
			},
		}

	case labels.MatchNotRegexp:
		if col == nil {
			// Column(NULL) is treated as empty string.
			// Return true if regex does NOT match "", false otherwise.
			if matcher.Matches("") {
				return streams.TruePredicate{}
			}
			return streams.FalsePredicate{}
		}

		return streams.FuncPredicate{
			Column: col,
			Keep: func(_ *streams.Column, value scalar.Scalar) bool {
				// we don't need the negation here because matcher.Matches() already handles the negation correctly.
				return matcher.Matches(string(getBytes(value)))
			},
		}

	default:
		panic(fmt.Sprintf("buildLabelPredicate: unsupported label matcher type %s", matcher.Type))
	}
}

func getBytes(value scalar.Scalar) []byte {
	if !value.IsValid() {
		return nil
	}

	switch value := value.(type) {
	case *scalar.Binary:
		return value.Data()
	case *scalar.String:
		return value.Data()
	}

	return nil
}
