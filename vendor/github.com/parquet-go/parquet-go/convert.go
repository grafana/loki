package parquet

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"time"

	"github.com/parquet-go/parquet-go/internal/memory"
	"golang.org/x/sys/cpu"

	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// ConvertError is an error type returned by calls to Convert when the conversion
// of parquet schemas is impossible or the input row for the conversion is
// malformed.
type ConvertError struct {
	Path []string
	From Node
	To   Node
}

// Error satisfies the error interface.
func (e *ConvertError) Error() string {
	sourceType := e.From.Type()
	targetType := e.To.Type()

	sourceRepetition := fieldRepetitionTypeOf(e.From)
	targetRepetition := fieldRepetitionTypeOf(e.To)

	return fmt.Sprintf("cannot convert parquet column %q from %s %s to %s %s",
		columnPath(e.Path),
		sourceRepetition,
		sourceType,
		targetRepetition,
		targetType,
	)
}

// Conversion is an interface implemented by types that provide conversion of
// parquet rows from one schema to another.
//
// Conversion instances must be safe to use concurrently from multiple goroutines.
type Conversion interface {
	// Applies the conversion logic on the src row, returning the result
	// appended to dst.
	Convert(rows []Row) (int, error)
	// Converts the given column index in the target schema to the original
	// column index in the source schema of the conversion.
	Column(int) int
	// Returns the target schema of the conversion.
	Schema() *Schema
}

type conversion struct {
	columns []conversionColumn
	schema  *Schema
	buffers memory.Pool[conversionBuffer]
	// This field is used to size the column buffers held in the memory.Pool since
	// they are intended to store the source rows being converted from.
	numberOfSourceColumns int
}

type conversionBuffer struct {
	columns [][]Value
}

type conversionColumn struct {
	sourceIndex   int
	convertValues conversionFunc
	targetKind    Kind // Target column kind for creating proper null values
	isOptional    bool // Whether the target column is optional (for null handling)
}

type conversionFunc func([]Value) error

func convertToSelf(column []Value) error { return nil }

func convertToType(targetType, sourceType Type) conversionFunc {
	return func(column []Value) error {
		for i, v := range column {
			v, err := targetType.ConvertValue(v, sourceType)
			if err != nil {
				return err
			}
			column[i].ptr = v.ptr
			column[i].u64 = v.u64
			column[i].kind = v.kind
		}
		return nil
	}
}

func convertToZero(kind Kind) conversionFunc {
	switch kind {
	case Boolean:
		return convertToZeroBoolean
	case Int32:
		return convertToZeroInt32
	case Int64:
		return convertToZeroInt64
	case Int96:
		return convertToZeroInt96
	case Float:
		return convertToZeroFloat
	case Double:
		return convertToZeroDouble
	case ByteArray:
		return convertToZeroByteArray
	case FixedLenByteArray:
		return convertToZeroFixedLenByteArray
	default:
		return convertToSelf
	}
}

func convertToZeroKind(column []Value, kind int8) error {
	for i := range column {
		column[i].ptr = nil
		column[i].u64 = 0
		column[i].kind = kind
	}
	return nil
}

func convertToZeroBoolean(column []Value) error {
	return convertToZeroKind(column, ^int8(Boolean))
}

func convertToZeroInt32(column []Value) error {
	return convertToZeroKind(column, ^int8(Int32))
}

func convertToZeroInt64(column []Value) error {
	return convertToZeroKind(column, ^int8(Int64))
}

func convertToZeroInt96(column []Value) error {
	return convertToZeroKind(column, ^int8(Int96))
}

func convertToZeroFloat(column []Value) error {
	return convertToZeroKind(column, ^int8(Float))
}

func convertToZeroDouble(column []Value) error {
	return convertToZeroKind(column, ^int8(Double))
}

func convertToZeroByteArray(column []Value) error {
	return convertToZeroKind(column, ^int8(ByteArray))
}

func convertToZeroFixedLenByteArray(column []Value) error {
	return convertToZeroKind(column, ^int8(FixedLenByteArray))
}

func convertToNull(column []Value) error {
	return convertToZeroKind(column, 0) // kind = 0 indicates null
}

func convertToNullOptional(maxDefinitionLevel byte) conversionFunc {
	return func(column []Value) error {
		for i := range column {
			column[i].ptr = nil
			column[i].u64 = 0
			column[i].kind = 0 // kind = 0 indicates null
			// For optional fields, if the source value is present (defLevel == max),
			// we need to set defLevel to max-1 to indicate null at the leaf level
			if column[i].definitionLevel == maxDefinitionLevel {
				column[i].definitionLevel--
			}
			// If source is already null (defLevel < max), keep the same defLevel
		}
		return nil
	}
}

func convertToLevels(repetitionLevels, definitionLevels []byte) conversionFunc {
	return func(column []Value) error {
		for i := range column {
			r := column[i].repetitionLevel
			d := column[i].definitionLevel
			column[i].repetitionLevel = repetitionLevels[r]
			column[i].definitionLevel = definitionLevels[d]
		}
		return nil
	}
}

func multiConversionFunc(conversions []conversionFunc) conversionFunc {
	switch len(conversions) {
	case 0:
		return convertToSelf
	case 1:
		return conversions[0]
	default:
		return func(column []Value) error {
			for _, conv := range conversions {
				if err := conv(column); err != nil {
					return err
				}
			}
			return nil
		}
	}
}

func (c *conversion) getBuffer() *conversionBuffer {
	return c.buffers.Get(
		func() *conversionBuffer {
			b := &conversionBuffer{
				columns: make([][]Value, c.numberOfSourceColumns),
			}
			values := make([]Value, c.numberOfSourceColumns)
			for i := range b.columns {
				b.columns[i] = values[i : i : i+1]
			}
			return b
		},
		func(b *conversionBuffer) {},
	)
}

func (c *conversion) putBuffer(b *conversionBuffer) {
	c.buffers.Put(b)
}

// Convert here satisfies the Conversion interface, and does the actual work
// to convert between the source and target Rows.
func (c *conversion) Convert(rows []Row) (int, error) {
	source := c.getBuffer()
	defer c.putBuffer(source)

	for n, row := range rows {
		for i, values := range source.columns {
			source.columns[i] = values[:0]
		}
		row.Range(func(columnIndex int, columnValues []Value) bool {
			source.columns[columnIndex] = append(source.columns[columnIndex], columnValues...)
			return true
		})
		row = row[:0]

		for columnIndex, conv := range c.columns {
			columnOffset := len(row)
			if conv.sourceIndex < 0 {
				// When there is no source column, we put a single value as
				// placeholder in the column. This is a condition where the
				// target contained a column which did not exist at had not
				// other columns existing at that same level.
				var value Value
				if conv.isOptional {
					// Optional field: create null value (kind = 0)
					value = Value{}
				} else {
					// Required field: create typed zero value
					value = ZeroValue(conv.targetKind)
				}
				row = append(row, value)
			} else {
				sourceValues := source.columns[conv.sourceIndex]
				// We must copy to the output row first and not mutate the
				// source columns because multiple target columns may map to
				// the same source column.
				row = append(row, sourceValues...)
			}
			columnValues := row[columnOffset:]

			if err := conv.convertValues(columnValues); err != nil {
				return n, err
			}

			// Since the column index may have changed between the source and
			// taget columns we ensure that the right value is always written
			// to the output row.
			for i := range columnValues {
				// Fix: If we have a zero Value{}, convert it to a properly typed value
				// For optional fields, keep as null (kind = 0)
				// For required fields, convert to typed zero value
				if columnValues[i].Kind() == Kind(0) && !conv.isOptional {
					columnValues[i] = ZeroValue(conv.targetKind)
				}

				columnValues[i].columnIndex = ^int16(columnIndex)
			}
		}

		rows[n] = row
	}

	return len(rows), nil
}

func (c *conversion) Column(i int) int {
	return c.columns[i].sourceIndex
}

func (c *conversion) Schema() *Schema {
	return c.schema
}

type identity struct{ schema *Schema }

func (id identity) Convert(rows []Row) (int, error) { return len(rows), nil }
func (id identity) Column(i int) int                { return i }
func (id identity) Schema() *Schema                 { return id.schema }

// Convert constructs a conversion function from one parquet schema to another.
//
// The function supports converting between schemas where the source or target
// have extra columns; if there are more columns in the source, they will be
// stripped out of the rows. Extra columns in the target schema will be set to
// null or zero values.
//
// The returned function is intended to be used to append the converted source
// row to the destination buffer.
func Convert(to, from Node) (conv Conversion, err error) {
	schema, _ := to.(*Schema)
	if schema == nil {
		schema = NewSchema("", to)
	}

	if EqualNodes(to, from) {
		return identity{schema}, nil
	}

	targetMapping, targetColumns := columnMappingOf(to)
	sourceMapping, sourceColumns := columnMappingOf(from)
	columns := make([]conversionColumn, len(targetColumns))

	for i, path := range targetColumns {
		targetColumn := targetMapping.lookup(path)
		sourceColumn := sourceMapping.lookup(path)

		conversions := []conversionFunc{}
		if sourceColumn.node != nil {
			targetType := targetColumn.node.Type()
			sourceType := sourceColumn.node.Type()
			if !EqualTypes(targetType, sourceType) {
				conversions = append(conversions, convertToType(targetType, sourceType))
			}

			repetitionLevels := make([]byte, len(path)+1)
			definitionLevels := make([]byte, len(path)+1)
			targetRepetitionLevel := byte(0)
			targetDefinitionLevel := byte(0)
			sourceRepetitionLevel := byte(0)
			sourceDefinitionLevel := byte(0)
			targetNode := to
			sourceNode := from

			for j := range path {
				targetNode = fieldByName(targetNode, path[j])
				sourceNode = fieldByName(sourceNode, path[j])

				targetRepetitionLevel, targetDefinitionLevel = applyFieldRepetitionType(
					fieldRepetitionTypeOf(targetNode),
					targetRepetitionLevel,
					targetDefinitionLevel,
				)
				sourceRepetitionLevel, sourceDefinitionLevel = applyFieldRepetitionType(
					fieldRepetitionTypeOf(sourceNode),
					sourceRepetitionLevel,
					sourceDefinitionLevel,
				)

				repetitionLevels[sourceRepetitionLevel] = targetRepetitionLevel
				definitionLevels[sourceDefinitionLevel] = targetDefinitionLevel
			}

			repetitionLevels = repetitionLevels[:sourceRepetitionLevel+1]
			definitionLevels = definitionLevels[:sourceDefinitionLevel+1]

			if !isDirectLevelMapping(repetitionLevels) || !isDirectLevelMapping(definitionLevels) {
				conversions = append(conversions, convertToLevels(repetitionLevels, definitionLevels))
			}

		} else {
			// Column doesn't exist in source - this is a missing column
			targetType := targetColumn.node.Type()
			targetKind := targetType.Kind()

			// Check if the leaf field itself is optional (not just nested in optional/repeated structure)
			isOptionalField := targetColumn.node.Optional()

			closestColumn := sourceMapping.lookupClosest(path)
			if closestColumn.node != nil {
				// There's a sibling column we can use as a template for structure
				if isOptionalField {
					// Optional field: convert to null values while mirroring structure
					conversions = append(conversions, convertToNullOptional(targetColumn.maxDefinitionLevel))
				} else {
					// Required field: convert to typed zero values
					conversions = append(conversions, convertToZero(targetKind))
				}
				// Use the closest column as source for structure/levels
				sourceColumn = closestColumn
			} else {
				// No sibling columns exist
				if !isOptionalField {
					// Required field: create typed zero value
					conversions = append(conversions, convertToZero(targetKind))
				}
				// Keep sourceColumn with columnIndex -1
				// For optional fields without siblings, we'll create a single null value per row
			}
		}

		// Store target column type for creating proper null values
		targetType := targetColumn.node.Type()

		// Determine sourceIndex: -1 if column doesn't exist in source
		sourceIndex := int(sourceColumn.columnIndex)
		if sourceColumn.node == nil {
			sourceIndex = -1
		}

		// Determine if target column is optional
		isOptional := targetColumn.maxDefinitionLevel > 0

		columns[i] = conversionColumn{
			sourceIndex:   sourceIndex,
			convertValues: multiConversionFunc(conversions),
			targetKind:    targetType.Kind(), // Store target kind for null value creation
			isOptional:    isOptional,
		}
	}

	c := &conversion{
		columns:               columns,
		schema:                schema,
		numberOfSourceColumns: len(sourceColumns),
	}
	return c, nil
}

func isDirectLevelMapping(levels []byte) bool {
	for i, level := range levels {
		if level != byte(i) {
			return false
		}
	}
	return true
}

// findAdjacentColumnChunk finds a sibling column at the same repetition depth
// Returns nil if no suitable adjacent column exists
func findAdjacentColumnChunk(schema *Schema, targetColumnIndex int16, columns []ColumnChunk, sourceMapping columnMapping) ColumnChunk {
	var targetLeaf leafColumn
	targetFound := false

	forEachLeafColumnOf(schema, func(leaf leafColumn) {
		if leaf.columnIndex == targetColumnIndex {
			targetLeaf = leaf
			targetFound = true
		}
	})

	if !targetFound {
		return nil
	}

	// Find a sibling: same parent path and same max repetition level
	targetParentPath := targetLeaf.path
	if len(targetParentPath) > 0 {
		targetParentPath = targetParentPath[:len(targetParentPath)-1]
	}

	var adjacentChunk ColumnChunk
	forEachLeafColumnOf(schema, func(leaf leafColumn) {
		if leaf.columnIndex == targetColumnIndex {
			return // Skip self
		}

		// Check if this is a sibling
		if len(leaf.path) > 0 {
			leafParentPath := leaf.path[:len(leaf.path)-1]
			if targetParentPath.equal(leafParentPath) &&
				leaf.maxRepetitionLevel == targetLeaf.maxRepetitionLevel {
				// Check if this column exists in the converted row group
				if int(leaf.columnIndex) < len(columns) && columns[leaf.columnIndex] != nil {
					// Make sure it's not another missing column
					if _, ok := columns[leaf.columnIndex].(*missingColumnChunk); !ok {
						adjacentChunk = columns[leaf.columnIndex]
						return // Found a suitable adjacent column
					}
				}
			}
		}
	})

	return adjacentChunk
}

// ConvertRowGroup constructs a wrapper of the given row group which applies
// the given schema conversion to its rows.
func ConvertRowGroup(rowGroup RowGroup, conv Conversion) RowGroup {
	if EqualNodes(rowGroup.Schema(), conv.Schema()) {
		return rowGroup
	}
	schema := conv.Schema()
	numRows := rowGroup.NumRows()
	rowGroupColumns := rowGroup.ColumnChunks()
	sourceSchema := rowGroup.Schema()

	// Build a mapping to detect missing columns
	sourceMapping, _ := columnMappingOf(sourceSchema)

	columns := make([]ColumnChunk, numLeafColumnsOf(schema))

	// First pass: create all non-missing columns
	forEachLeafColumnOf(schema, func(leaf leafColumn) {
		i := leaf.columnIndex
		j := conv.Column(int(leaf.columnIndex))

		// Check if this column actually exists in the source schema
		sourceColumn := sourceMapping.lookup(leaf.path)
		isMissing := sourceColumn.node == nil

		if !isMissing {
			if i == int16(j) {
				columns[i] = rowGroupColumns[j]
			} else {
				columns[i] = &convertedColumnChunk{
					chunk:             rowGroupColumns[j],
					targetColumnIndex: ^int16(i),
				}
			}
		}
	})

	// Second pass: create missing columns with references to adjacent columns
	forEachLeafColumnOf(schema, func(leaf leafColumn) {
		i := leaf.columnIndex

		// Check if this column actually exists in the source schema
		sourceColumn := sourceMapping.lookup(leaf.path)
		isMissing := sourceColumn.node == nil

		if isMissing {
			// Find adjacent column for mirroring levels
			adjacentChunk := findAdjacentColumnChunk(schema, i, columns, sourceMapping)

			columns[i] = &missingColumnChunk{
				typ:                leaf.node.Type(),
				column:             i,
				numRows:            numRows,
				numValues:          numRows, // May be adjusted when reading
				numNulls:           numRows, // Depends on required vs optional
				maxRepetitionLevel: leaf.maxRepetitionLevel,
				maxDefinitionLevel: leaf.maxDefinitionLevel,
				adjacentChunk:      adjacentChunk,
			}
		}
	})

	// Sorting columns must exist on the conversion schema in order to be
	// advertised on the converted row group otherwise the resulting rows
	// would not be in the right order.
	sorting := []SortingColumn{}
	for _, col := range rowGroup.SortingColumns() {
		if !hasColumnPath(schema, col.Path()) {
			break
		}
		sorting = append(sorting, col)
	}

	return &convertedRowGroup{
		// The pair of rowGroup+conv is retained to construct a converted row
		// reader by wrapping the underlying row reader of the row group because
		// it allows proper reconstruction of the repetition and definition
		// levels.
		//
		// Columns of the source row group which do not exist in the target are
		// masked to prevent loading unneeded pages when reading rows from the
		// converted row group.
		rowGroup: maskMissingRowGroupColumns(rowGroup, len(columns), conv),
		columns:  columns,
		sorting:  sorting,
		conv:     conv,
	}
}

func maskMissingRowGroupColumns(r RowGroup, numColumns int, conv Conversion) RowGroup {
	rowGroupColumns := r.ColumnChunks()
	columns := make([]ColumnChunk, len(rowGroupColumns))
	missing := make([]missingColumnChunk, len(columns))
	numRows := r.NumRows()

	// Compute max levels for each column in the source schema
	sourceSchema := r.Schema()
	forEachLeafColumnOf(sourceSchema, func(leaf leafColumn) {
		i := leaf.columnIndex
		missing[i] = missingColumnChunk{
			typ:                rowGroupColumns[i].Type(),
			column:             int16(i),
			numRows:            numRows,
			numValues:          numRows,
			numNulls:           numRows,
			maxRepetitionLevel: leaf.maxRepetitionLevel,
			maxDefinitionLevel: leaf.maxDefinitionLevel,
		}
	})

	for i := range columns {
		columns[i] = &missing[i]
	}

	for i := range numColumns {
		j := conv.Column(i)
		if j >= 0 && j < len(columns) {
			columns[j] = rowGroupColumns[j]
		}
	}

	return &rowGroup{
		schema:  r.Schema(),
		numRows: numRows,
		columns: columns,
	}
}

type missingColumnChunk struct {
	typ                Type
	column             int16
	numRows            int64
	numValues          int64
	numNulls           int64
	maxRepetitionLevel byte        // Maximum repetition level for this column
	maxDefinitionLevel byte        // Maximum definition level for this column
	adjacentChunk      ColumnChunk // Adjacent column chunk to mirror levels from (nil if none)
}

func (c *missingColumnChunk) Type() Type  { return c.typ }
func (c *missingColumnChunk) Column() int { return int(c.column) }
func (c *missingColumnChunk) Pages() Pages {
	var adjacentPages Pages
	if c.adjacentChunk != nil {
		adjacentPages = c.adjacentChunk.Pages()
	}
	return onePage(missingPage{
		missingColumnChunk: c,
		adjacentPages:      adjacentPages,
	})
}
func (c *missingColumnChunk) ColumnIndex() (ColumnIndex, error) { return missingColumnIndex{c}, nil }
func (c *missingColumnChunk) OffsetIndex() (OffsetIndex, error) { return missingOffsetIndex{}, nil }
func (c *missingColumnChunk) BloomFilter() BloomFilter          { return missingBloomFilter{} }
func (c *missingColumnChunk) NumValues() int64                  { return c.numValues }

type missingColumnIndex struct{ *missingColumnChunk }

func (i missingColumnIndex) NumPages() int       { return 1 }
func (i missingColumnIndex) NullCount(int) int64 { return i.numNulls }
func (i missingColumnIndex) NullPage(int) bool   { return true }
func (i missingColumnIndex) MinValue(int) Value  { return Value{} }
func (i missingColumnIndex) MaxValue(int) Value  { return Value{} }
func (i missingColumnIndex) IsAscending() bool   { return true }
func (i missingColumnIndex) IsDescending() bool  { return false }

type missingOffsetIndex struct{}

func (missingOffsetIndex) NumPages() int                { return 1 }
func (missingOffsetIndex) Offset(int) int64             { return 0 }
func (missingOffsetIndex) CompressedPageSize(int) int64 { return 0 }
func (missingOffsetIndex) FirstRowIndex(int) int64      { return 0 }

type missingBloomFilter struct{}

func (missingBloomFilter) ReadAt([]byte, int64) (int, error) { return 0, io.EOF }
func (missingBloomFilter) Size() int64                       { return 0 }
func (missingBloomFilter) Check(Value) (bool, error)         { return false, nil }

type missingPage struct {
	*missingColumnChunk
	adjacentPages Pages // Pages from adjacent column for level mirroring
}

func (p missingPage) Column() int                       { return int(p.column) }
func (p missingPage) Dictionary() Dictionary            { return nil }
func (p missingPage) NumRows() int64                    { return p.numRows }
func (p missingPage) NumValues() int64                  { return p.numValues }
func (p missingPage) NumNulls() int64                   { return p.numNulls }
func (p missingPage) Bounds() (min, max Value, ok bool) { return }
func (p missingPage) Slice(i, j int64) Page {
	return missingPage{
		missingColumnChunk: &missingColumnChunk{
			typ:                p.typ,
			column:             p.column,
			numRows:            j - i,
			numValues:          j - i,
			numNulls:           j - i,
			maxRepetitionLevel: p.maxRepetitionLevel,
			maxDefinitionLevel: p.maxDefinitionLevel,
			adjacentChunk:      p.adjacentChunk,
		},
		adjacentPages: p.adjacentPages,
	}
}
func (p missingPage) Size() int64              { return 0 }
func (p missingPage) RepetitionLevels() []byte { return nil }
func (p missingPage) DefinitionLevels() []byte { return nil }
func (p missingPage) Data() encoding.Values    { return p.typ.NewValues(nil, nil) }
func (p missingPage) Values() ValueReader {
	var adjacentReader ValueReader
	if p.adjacentPages != nil {
		// Open the adjacent page to read levels from
		if adjacentPage, err := p.adjacentPages.ReadPage(); err == nil {
			adjacentReader = adjacentPage.Values()
		}
	}

	return &missingPageValues{
		page:           p,
		adjacentReader: adjacentReader,
		adjacentBuffer: make([]Value, 1024), // Reasonable buffer size
	}
}

type missingPageValues struct {
	page           missingPage
	read           int64
	adjacentReader ValueReader // Reader for adjacent column to mirror levels
	adjacentBuffer []Value     // Buffer for reading adjacent values
}

func (r *missingPageValues) ReadValues(values []Value) (int, error) {
	remain := r.page.numValues - r.read
	if int64(len(values)) > remain {
		values = values[:remain]
	}

	typ := r.page.typ
	columnIndex := ^r.page.column

	// Case 1: No adjacent column (root-level field, no siblings)
	if r.adjacentReader == nil {
		return r.readWithoutAdjacent(values, typ, columnIndex)
	}

	// Case 2: Has adjacent column - mirror its repetition/definition levels
	return r.readWithAdjacent(values, typ, columnIndex)
}

func (r *missingPageValues) readWithoutAdjacent(values []Value, typ Type, columnIndex int16) (int, error) {
	// For fields without siblings, assume one value per row
	// Definition level depends on whether field is required or optional
	isRequired := r.page.maxDefinitionLevel == 0

	if isRequired {
		// Required field: produce zero/default values
		for i := range values {
			values[i] = ZeroValue(typ.Kind())
			values[i].repetitionLevel = 0
			values[i].definitionLevel = r.page.maxDefinitionLevel // Present value
			values[i].columnIndex = columnIndex
		}
	} else {
		// Optional field: produce nulls
		definitionLevel := byte(0)
		if r.page.maxDefinitionLevel > 0 {
			definitionLevel = r.page.maxDefinitionLevel - 1
		}

		for i := range values {
			values[i] = Value{
				repetitionLevel: 0,
				definitionLevel: definitionLevel,
				columnIndex:     columnIndex,
			}
		}
	}

	r.read += int64(len(values))
	if r.read == r.page.numValues {
		return len(values), io.EOF
	}
	return len(values), nil
}

func (r *missingPageValues) readWithAdjacent(values []Value, typ Type, columnIndex int16) (int, error) {
	// Read values from adjacent column to get its levels
	n, err := r.adjacentReader.ReadValues(r.adjacentBuffer[:len(values)])
	if err != nil && err != io.EOF {
		return 0, err
	}

	// Determine if this missing column is required or optional
	isRequired := r.page.maxDefinitionLevel == 0

	for i := range n {
		repLevel := r.adjacentBuffer[i].repetitionLevel
		adjacentDefLevel := r.adjacentBuffer[i].definitionLevel

		var defLevel byte
		var value Value

		if isRequired {
			// Required field: produce zero/default values
			value = ZeroValue(typ.Kind())
			// Mirror adjacent definition level structure
			defLevel = adjacentDefLevel
		} else {
			// Optional field: produce nulls
			// Definition level indicates null at appropriate nesting
			if adjacentDefLevel < r.page.maxDefinitionLevel {
				// Adjacent is null at some level, follow its definition
				defLevel = adjacentDefLevel
			} else {
				// Adjacent is present, but we are null at leaf level
				defLevel = r.page.maxDefinitionLevel - 1
			}
		}

		value.repetitionLevel = repLevel
		value.definitionLevel = defLevel
		value.columnIndex = columnIndex
		values[i] = value
	}

	r.read += int64(n)
	if err == io.EOF {
		return n, io.EOF
	}
	return n, nil
}

func (r *missingPageValues) Close() error {
	r.read = r.page.numValues
	return nil
}

type convertedRowGroup struct {
	rowGroup RowGroup
	columns  []ColumnChunk
	sorting  []SortingColumn
	conv     Conversion
}

func (c *convertedRowGroup) NumRows() int64                  { return c.rowGroup.NumRows() }
func (c *convertedRowGroup) ColumnChunks() []ColumnChunk     { return c.columns }
func (c *convertedRowGroup) Schema() *Schema                 { return c.conv.Schema() }
func (c *convertedRowGroup) SortingColumns() []SortingColumn { return c.sorting }
func (c *convertedRowGroup) Rows() Rows {
	rows := c.rowGroup.Rows()
	return &convertedRows{
		Closer: rows,
		rows:   rows,
		conv:   c.conv,
	}
}

// ConvertRowReader constructs a wrapper of the given row reader which applies
// the given schema conversion to the rows.
func ConvertRowReader(rows RowReader, conv Conversion) RowReaderWithSchema {
	return &convertedRows{rows: &forwardRowSeeker{rows: rows}, conv: conv}
}

type convertedRows struct {
	io.Closer
	rows RowReadSeeker
	conv Conversion
}

func (c *convertedRows) ReadRows(rows []Row) (int, error) {
	n, err := c.rows.ReadRows(rows)
	if n > 0 {
		var convErr error
		n, convErr = c.conv.Convert(rows[:n])
		if convErr != nil {
			err = convErr
		}
	}
	return n, err
}

func (c *convertedRows) Schema() *Schema {
	return c.conv.Schema()
}

func (c *convertedRows) SeekToRow(rowIndex int64) error {
	return c.rows.SeekToRow(rowIndex)
}

// convertedColumnChunk wraps a ColumnChunk to fix the column index after reordering.
// When ConvertRowGroup reorders columns, the underlying chunk's Column() method
// returns the original position. This wrapper fixes both Column() and the
// columnIndex in values read from the chunk.
type convertedColumnChunk struct {
	chunk             ColumnChunk
	targetColumnIndex int16 // XOR-encoded column index (^int16(columnIndex))
}

func (c *convertedColumnChunk) Type() Type {
	return c.chunk.Type()
}

func (c *convertedColumnChunk) Column() int {
	return int(^c.targetColumnIndex)
}

func (c *convertedColumnChunk) NumValues() int64 {
	return c.chunk.NumValues()
}

func (c *convertedColumnChunk) Pages() Pages {
	return &convertedPages{
		pages:             c.chunk.Pages(),
		targetColumnIndex: c.targetColumnIndex,
	}
}

func (c *convertedColumnChunk) ColumnIndex() (ColumnIndex, error) {
	return c.chunk.ColumnIndex()
}

func (c *convertedColumnChunk) OffsetIndex() (OffsetIndex, error) {
	return c.chunk.OffsetIndex()
}

func (c *convertedColumnChunk) BloomFilter() BloomFilter {
	return c.chunk.BloomFilter()
}

// convertedPages wraps Pages to return convertedPage instances.
type convertedPages struct {
	pages             Pages
	targetColumnIndex int16
}

func (p *convertedPages) ReadPage() (Page, error) {
	page, err := p.pages.ReadPage()
	if err != nil {
		return nil, err
	}
	return &convertedPage{
		page:              page,
		targetColumnIndex: p.targetColumnIndex,
	}, nil
}

func (p *convertedPages) SeekToRow(rowIndex int64) error {
	return p.pages.SeekToRow(rowIndex)
}

func (p *convertedPages) Close() error {
	return p.pages.Close()
}

// convertedPage wraps a Page to return a convertedValueReader.
type convertedPage struct {
	page              Page
	targetColumnIndex int16
}

func (p *convertedPage) Type() Type {
	return p.page.Type()
}

func (p *convertedPage) Column() int {
	return int(^p.targetColumnIndex)
}

func (p *convertedPage) Dictionary() Dictionary {
	return p.page.Dictionary()
}

func (p *convertedPage) NumRows() int64 {
	return p.page.NumRows()
}

func (p *convertedPage) NumValues() int64 {
	return p.page.NumValues()
}

func (p *convertedPage) NumNulls() int64 {
	return p.page.NumNulls()
}

func (p *convertedPage) Bounds() (min, max Value, ok bool) {
	return p.page.Bounds()
}

func (p *convertedPage) Size() int64 {
	return p.page.Size()
}

func (p *convertedPage) RepetitionLevels() []byte {
	return p.page.RepetitionLevels()
}

func (p *convertedPage) DefinitionLevels() []byte {
	return p.page.DefinitionLevels()
}

func (p *convertedPage) Data() encoding.Values {
	return p.page.Data()
}

func (p *convertedPage) Values() ValueReader {
	return &convertedValueReader{
		reader:            p.page.Values(),
		targetColumnIndex: p.targetColumnIndex,
	}
}

func (p *convertedPage) Slice(i, j int64) Page {
	return &convertedPage{
		page:              p.page.Slice(i, j),
		targetColumnIndex: p.targetColumnIndex,
	}
}

func (p *convertedPage) Retain() {
	Retain(p.page)
}

func (p *convertedPage) Release() {
	Release(p.page)
}

func (p *convertedPage) ReleaseAndDetachValues() {
	releaseAndDetachValues(p.page)
}

var (
	_ retainable = (*convertedPage)(nil)
	_ releasable = (*convertedPage)(nil)
	_ detachable = (*convertedPage)(nil)
)

// convertedValueReader wraps a ValueReader to rewrite columnIndex in values.
type convertedValueReader struct {
	reader            ValueReader
	targetColumnIndex int16
}

func (r *convertedValueReader) ReadValues(values []Value) (int, error) {
	n, err := r.reader.ReadValues(values)
	// Rewrite columnIndex for all values to match the target column position
	for i := range n {
		values[i].columnIndex = r.targetColumnIndex
	}
	return n, err
}

var (
	trueBytes  = []byte(`true`)
	falseBytes = []byte(`false`)
	unixEpoch  = time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC)
)

func convertBooleanToInt32(v Value) (Value, error) {
	return v.convertToInt32(int32(v.byte())), nil
}

func convertBooleanToInt64(v Value) (Value, error) {
	return v.convertToInt64(int64(v.byte())), nil
}

func convertBooleanToInt96(v Value) (Value, error) {
	return v.convertToInt96(deprecated.Int96{0: uint32(v.byte())}), nil
}

func convertBooleanToFloat(v Value) (Value, error) {
	return v.convertToFloat(float32(v.byte())), nil
}

func convertBooleanToDouble(v Value) (Value, error) {
	return v.convertToDouble(float64(v.byte())), nil
}

func convertBooleanToByteArray(v Value) (Value, error) {
	return v.convertToByteArray([]byte{v.byte()}), nil
}

func convertBooleanToFixedLenByteArray(v Value, size int) (Value, error) {
	b := []byte{v.byte()}
	c := make([]byte, size)
	copy(c, b)
	return v.convertToFixedLenByteArray(c), nil
}

func convertBooleanToString(v Value) (Value, error) {
	b := ([]byte)(nil)
	if v.boolean() {
		b = trueBytes
	} else {
		b = falseBytes
	}
	return v.convertToByteArray(b), nil
}

func convertInt32ToBoolean(v Value) (Value, error) {
	return v.convertToBoolean(v.int32() != 0), nil
}

func convertInt32ToInt64(v Value) (Value, error) {
	return v.convertToInt64(int64(v.int32())), nil
}

func convertInt32ToInt96(v Value) (Value, error) {
	return v.convertToInt96(deprecated.Int32ToInt96(v.int32())), nil
}

func convertInt32ToFloat(v Value) (Value, error) {
	return v.convertToFloat(float32(v.int32())), nil
}

func convertInt32ToDouble(v Value) (Value, error) {
	return v.convertToDouble(float64(v.int32())), nil
}

func convertInt32ToByteArray(v Value) (Value, error) {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v.uint32())
	return v.convertToByteArray(b), nil
}

func convertInt32ToFixedLenByteArray(v Value, size int) (Value, error) {
	b := make([]byte, 4)
	c := make([]byte, size)
	binary.LittleEndian.PutUint32(b, v.uint32())
	copy(c, b)
	return v.convertToFixedLenByteArray(c), nil
}

func convertInt32ToString(v Value) (Value, error) {
	return v.convertToByteArray(strconv.AppendInt(nil, int64(v.int32()), 10)), nil
}

func convertInt64ToBoolean(v Value) (Value, error) {
	return v.convertToBoolean(v.int64() != 0), nil
}

func convertInt64ToInt32(v Value) (Value, error) {
	return v.convertToInt32(int32(v.int64())), nil
}

func convertInt64ToInt96(v Value) (Value, error) {
	return v.convertToInt96(deprecated.Int64ToInt96(v.int64())), nil
}

func convertInt64ToFloat(v Value) (Value, error) {
	return v.convertToFloat(float32(v.int64())), nil
}

func convertInt64ToDouble(v Value) (Value, error) {
	return v.convertToDouble(float64(v.int64())), nil
}

func convertInt64ToByteArray(v Value) (Value, error) {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v.uint64())
	return v.convertToByteArray(b), nil
}

func convertInt64ToFixedLenByteArray(v Value, size int) (Value, error) {
	b := make([]byte, 8)
	c := make([]byte, size)
	binary.LittleEndian.PutUint64(b, v.uint64())
	copy(c, b)
	return v.convertToFixedLenByteArray(c), nil
}

func convertInt64ToString(v Value) (Value, error) {
	return v.convertToByteArray(strconv.AppendInt(nil, v.int64(), 10)), nil
}

func convertInt96ToBoolean(v Value) (Value, error) {
	return v.convertToBoolean(!v.int96().IsZero()), nil
}

func convertInt96ToInt32(v Value) (Value, error) {
	return v.convertToInt32(v.int96().Int32()), nil
}

func convertInt96ToInt64(v Value) (Value, error) {
	return v.convertToInt64(v.int96().Int64()), nil
}

func convertInt96ToFloat(v Value) (Value, error) {
	return v, invalidConversion(v, "INT96", "FLOAT")
}

func convertInt96ToDouble(v Value) (Value, error) {
	return v, invalidConversion(v, "INT96", "DOUBLE")
}

func convertInt96ToByteArray(v Value) (Value, error) {
	return v.convertToByteArray(v.byteArray()), nil
}

func convertInt96ToFixedLenByteArray(v Value, size int) (Value, error) {
	b := v.byteArray()
	if len(b) < size {
		c := make([]byte, size)
		copy(c, b)
		b = c
	} else {
		b = b[:size]
	}
	return v.convertToFixedLenByteArray(b), nil
}

func convertInt96ToString(v Value) (Value, error) {
	return v.convertToByteArray([]byte(v.String())), nil
}

func convertFloatToBoolean(v Value) (Value, error) {
	return v.convertToBoolean(v.float() != 0), nil
}

func convertFloatToInt32(v Value) (Value, error) {
	return v.convertToInt32(int32(v.float())), nil
}

func convertFloatToInt64(v Value) (Value, error) {
	return v.convertToInt64(int64(v.float())), nil
}

func convertFloatToInt96(v Value) (Value, error) {
	return v, invalidConversion(v, "FLOAT", "INT96")
}

func convertFloatToDouble(v Value) (Value, error) {
	return v.convertToDouble(float64(v.float())), nil
}

func convertFloatToByteArray(v Value) (Value, error) {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v.uint32())
	return v.convertToByteArray(b), nil
}

func convertFloatToFixedLenByteArray(v Value, size int) (Value, error) {
	b := make([]byte, 4)
	c := make([]byte, size)
	binary.LittleEndian.PutUint32(b, v.uint32())
	copy(c, b)
	return v.convertToFixedLenByteArray(c), nil
}

func convertFloatToString(v Value) (Value, error) {
	return v.convertToByteArray(strconv.AppendFloat(nil, float64(v.float()), 'g', -1, 32)), nil
}

func convertDoubleToBoolean(v Value) (Value, error) {
	return v.convertToBoolean(v.double() != 0), nil
}

func convertDoubleToInt32(v Value) (Value, error) {
	return v.convertToInt32(int32(v.double())), nil
}

func convertDoubleToInt64(v Value) (Value, error) {
	return v.convertToInt64(int64(v.double())), nil
}

func convertDoubleToInt96(v Value) (Value, error) {
	return v, invalidConversion(v, "FLOAT", "INT96")
}

func convertDoubleToFloat(v Value) (Value, error) {
	return v.convertToFloat(float32(v.double())), nil
}

func convertDoubleToByteArray(v Value) (Value, error) {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v.uint64())
	return v.convertToByteArray(b), nil
}

func convertDoubleToFixedLenByteArray(v Value, size int) (Value, error) {
	b := make([]byte, 8)
	c := make([]byte, size)
	binary.LittleEndian.PutUint64(b, v.uint64())
	copy(c, b)
	return v.convertToFixedLenByteArray(c), nil
}

func convertDoubleToString(v Value) (Value, error) {
	return v.convertToByteArray(strconv.AppendFloat(nil, v.double(), 'g', -1, 64)), nil
}

func convertByteArrayToBoolean(v Value) (Value, error) {
	return v.convertToBoolean(!isZero(v.byteArray())), nil
}

func convertByteArrayToInt32(v Value) (Value, error) {
	b := make([]byte, 4)
	copy(b, v.byteArray())
	return v.convertToInt32(int32(binary.LittleEndian.Uint32(b))), nil
}

func convertByteArrayToInt64(v Value) (Value, error) {
	b := make([]byte, 8)
	copy(b, v.byteArray())
	return v.convertToInt64(int64(binary.LittleEndian.Uint64(b))), nil
}

func convertByteArrayToInt96(v Value) (Value, error) {
	b := make([]byte, 12)
	copy(b, v.byteArray())
	return v.convertToInt96(deprecated.Int96{
		0: binary.LittleEndian.Uint32(b[0:4]),
		1: binary.LittleEndian.Uint32(b[4:8]),
		2: binary.LittleEndian.Uint32(b[8:12]),
	}), nil
}

func convertByteArrayToFloat(v Value) (Value, error) {
	b := make([]byte, 4)
	copy(b, v.byteArray())
	return v.convertToFloat(math.Float32frombits(binary.LittleEndian.Uint32(b))), nil
}

func convertByteArrayToDouble(v Value) (Value, error) {
	b := make([]byte, 8)
	copy(b, v.byteArray())
	return v.convertToDouble(math.Float64frombits(binary.LittleEndian.Uint64(b))), nil
}

func convertByteArrayToFixedLenByteArray(v Value, size int) (Value, error) {
	b := v.byteArray()
	if len(b) < size {
		c := make([]byte, size)
		copy(c, b)
		b = c
	} else {
		b = b[:size]
	}
	return v.convertToFixedLenByteArray(b), nil
}

func convertFixedLenByteArrayToString(v Value) (Value, error) {
	b := v.byteArray()
	c := make([]byte, hex.EncodedLen(len(b)))
	hex.Encode(c, b)
	return v.convertToByteArray(c), nil
}

func convertStringToBoolean(v Value) (Value, error) {
	b, err := strconv.ParseBool(v.string())
	if err != nil {
		return v, conversionError(v, "STRING", "BOOLEAN", err)
	}
	return v.convertToBoolean(b), nil
}

func convertStringToInt32(v Value) (Value, error) {
	i, err := strconv.ParseInt(v.string(), 10, 32)
	if err != nil {
		return v, conversionError(v, "STRING", "INT32", err)
	}
	return v.convertToInt32(int32(i)), nil
}

func convertStringToInt64(v Value) (Value, error) {
	i, err := strconv.ParseInt(v.string(), 10, 64)
	if err != nil {
		return v, conversionError(v, "STRING", "INT64", err)
	}
	return v.convertToInt64(i), nil
}

func convertStringToInt96(v Value) (Value, error) {
	i, ok := new(big.Int).SetString(v.string(), 10)
	if !ok {
		return v, conversionError(v, "STRING", "INT96", strconv.ErrSyntax)
	}
	b := i.Bytes()
	c := make([]byte, 12)
	copy(c, b)
	if cpu.IsBigEndian {
		bufLen := len(c)
		for idx := 0; idx < bufLen; idx = idx + 4 {
			for m, n := (idx + 0), (idx + 3); m < n; m, n = m+1, n-1 {
				c[m], c[n] = c[n], c[m]
			}
		}
	}
	i96 := unsafecast.Slice[deprecated.Int96](c)
	return v.convertToInt96(i96[0]), nil
}

func convertStringToFloat(v Value) (Value, error) {
	f, err := strconv.ParseFloat(v.string(), 32)
	if err != nil {
		return v, conversionError(v, "STRING", "FLOAT", err)
	}
	return v.convertToFloat(float32(f)), nil
}

func convertStringToDouble(v Value) (Value, error) {
	f, err := strconv.ParseFloat(v.string(), 64)
	if err != nil {
		return v, conversionError(v, "STRING", "DOUBLE", err)
	}
	return v.convertToDouble(f), nil
}

func convertStringToFixedLenByteArray(v Value, size int) (Value, error) {
	b := v.byteArray()
	c := make([]byte, size)
	_, err := hex.Decode(c, b)
	if err != nil {
		return v, conversionError(v, "STRING", "BYTE_ARRAY", err)
	}
	return v.convertToFixedLenByteArray(c), nil
}

func convertStringToDate(v Value, tz *time.Location) (Value, error) {
	t, err := time.ParseInLocation("2006-01-02", v.string(), tz)
	if err != nil {
		return v, conversionError(v, "STRING", "DATE", err)
	}
	d := daysSinceUnixEpoch(t)
	return v.convertToInt32(int32(d)), nil
}

func convertStringToTimeMillis(v Value, tz *time.Location) (Value, error) {
	t, err := time.ParseInLocation("15:04:05.999", v.string(), tz)
	if err != nil {
		return v, conversionError(v, "STRING", "TIME", err)
	}
	m := nearestMidnightLessThan(t)
	milliseconds := t.Sub(m).Milliseconds()
	return v.convertToInt32(int32(milliseconds)), nil
}

func convertStringToTimeMicros(v Value, tz *time.Location) (Value, error) {
	t, err := time.ParseInLocation("15:04:05.999999", v.string(), tz)
	if err != nil {
		return v, conversionError(v, "STRING", "TIME", err)
	}
	m := nearestMidnightLessThan(t)
	microseconds := t.Sub(m).Microseconds()
	return v.convertToInt64(microseconds), nil
}

func convertDateToTimestamp(v Value, u format.TimeUnit, tz *time.Location) (Value, error) {
	t := unixEpoch.AddDate(0, 0, int(v.int32()))
	d := timeUnitDuration(u)
	return v.convertToInt64(int64(t.In(tz).Sub(unixEpoch) / d)), nil
}

func convertDateToString(v Value) (Value, error) {
	t := unixEpoch.AddDate(0, 0, int(v.int32()))
	b := t.AppendFormat(make([]byte, 0, 10), "2006-01-02")
	return v.convertToByteArray(b), nil
}

func convertTimeMillisToString(v Value, tz *time.Location) (Value, error) {
	t := time.UnixMilli(int64(v.int32())).In(tz)
	b := t.AppendFormat(make([]byte, 0, 12), "15:04:05.999")
	return v.convertToByteArray(b), nil
}

func convertTimeMicrosToString(v Value, tz *time.Location) (Value, error) {
	t := time.UnixMicro(v.int64()).In(tz)
	b := t.AppendFormat(make([]byte, 0, 15), "15:04:05.999999")
	return v.convertToByteArray(b), nil
}

func convertTimestampToDate(v Value, u format.TimeUnit, tz *time.Location) (Value, error) {
	t := timestamp(v, u, tz)
	d := daysSinceUnixEpoch(t)
	return v.convertToInt32(int32(d)), nil
}

func convertTimestampToTimeMillis(v Value, u format.TimeUnit, sourceZone, targetZone *time.Location) (Value, error) {
	t := timestamp(v, u, sourceZone)
	m := nearestMidnightLessThan(t)
	milliseconds := t.In(targetZone).Sub(m).Milliseconds()
	return v.convertToInt32(int32(milliseconds)), nil
}

func convertTimestampToTimeMicros(v Value, u format.TimeUnit, sourceZone, targetZone *time.Location) (Value, error) {
	t := timestamp(v, u, sourceZone)
	m := nearestMidnightLessThan(t)
	microseconds := t.In(targetZone).Sub(m).Microseconds()
	return v.convertToInt64(int64(microseconds)), nil
}

func convertTimestampToTimestamp(v Value, sourceUnit, targetUnit format.TimeUnit) (Value, error) {
	sourceScale := timeUnitDuration(sourceUnit).Nanoseconds()
	targetScale := timeUnitDuration(targetUnit).Nanoseconds()
	targetValue := (v.int64() * sourceScale) / targetScale
	return v.convertToInt64(targetValue), nil
}

const nanosecondsPerDay = 24 * 60 * 60 * 1e9

func daysSinceUnixEpoch(t time.Time) int {
	return int(t.Sub(unixEpoch).Hours()) / 24
}

func nearestMidnightLessThan(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func timestamp(v Value, u format.TimeUnit, tz *time.Location) time.Time {
	return unixEpoch.In(tz).Add(time.Duration(v.int64()) * timeUnitDuration(u))
}

func timeUnitDuration(unit format.TimeUnit) time.Duration {
	switch {
	case unit.Millis != nil:
		return time.Millisecond
	case unit.Micros != nil:
		return time.Microsecond
	default:
		return time.Nanosecond
	}
}

func invalidConversion(value Value, from, to string) error {
	return fmt.Errorf("%s to %s: %s: %w", from, to, value, ErrInvalidConversion)
}

func conversionError(value Value, from, to string, err error) error {
	return fmt.Errorf("%s to %s: %q: %s: %w", from, to, value.string(), err, ErrInvalidConversion)
}
