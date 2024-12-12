package parquet

import (
	"errors"
	"fmt"
	"io"
	"reflect"
)

const (
	defaultRowBufferSize = 42
)

// Row represents a parquet row as a slice of values.
//
// Each value should embed a column index, repetition level, and definition
// level allowing the program to determine how to reconstruct the original
// object from the row.
type Row []Value

// MakeRow constructs a Row from a list of column values.
//
// The function panics if the column indexes of values in each column do not
// match their position in the argument list.
func MakeRow(columns ...[]Value) Row { return AppendRow(nil, columns...) }

// AppendRow appends to row the given list of column values.
//
// AppendRow can be used to construct a Row value from columns, while retaining
// the underlying memory buffer to avoid reallocation; for example:
//
// The function panics if the column indexes of values in each column do not
// match their position in the argument list.
func AppendRow(row Row, columns ...[]Value) Row {
	numValues := 0

	for expectedColumnIndex, column := range columns {
		numValues += len(column)

		for _, value := range column {
			if value.columnIndex != ^int16(expectedColumnIndex) {
				panic(fmt.Sprintf("value of column %d has column index %d", expectedColumnIndex, value.Column()))
			}
		}
	}

	if capacity := cap(row) - len(row); capacity < numValues {
		row = append(make(Row, 0, len(row)+numValues), row...)
	}

	return appendRow(row, columns)
}

func appendRow(row Row, columns [][]Value) Row {
	for _, column := range columns {
		row = append(row, column...)
	}
	return row
}

// Clone creates a copy of the row which shares no pointers.
//
// This method is useful to capture rows after a call to RowReader.ReadRows when
// values need to be retained before the next call to ReadRows or after the lifespan
// of the reader.
func (row Row) Clone() Row {
	clone := make(Row, len(row))
	for i := range row {
		clone[i] = row[i].Clone()
	}
	return clone
}

// Equal returns true if row and other contain the same sequence of values.
func (row Row) Equal(other Row) bool {
	if len(row) != len(other) {
		return false
	}
	for i := range row {
		if !Equal(row[i], other[i]) {
			return false
		}
		if row[i].repetitionLevel != other[i].repetitionLevel {
			return false
		}
		if row[i].definitionLevel != other[i].definitionLevel {
			return false
		}
		if row[i].columnIndex != other[i].columnIndex {
			return false
		}
	}
	return true
}

// Range calls f for each column of row.
func (row Row) Range(f func(columnIndex int, columnValues []Value) bool) {
	columnIndex := 0

	for i := 0; i < len(row); {
		j := i + 1

		for j < len(row) && row[j].columnIndex == ^int16(columnIndex) {
			j++
		}

		if !f(columnIndex, row[i:j:j]) {
			break
		}

		columnIndex++
		i = j
	}
}

// RowSeeker is an interface implemented by readers of parquet rows which can be
// positioned at a specific row index.
type RowSeeker interface {
	// Positions the stream on the given row index.
	//
	// Some implementations of the interface may only allow seeking forward.
	//
	// The method returns io.ErrClosedPipe if the stream had already been closed.
	SeekToRow(int64) error
}

// RowReader reads a sequence of parquet rows.
type RowReader interface {
	// ReadRows reads rows from the reader, returning the number of rows read
	// into the buffer, and any error that occurred. Note that the rows read
	// into the buffer are not safe for reuse after a subsequent call to
	// ReadRows. Callers that want to reuse rows must copy the rows using Clone.
	//
	// When all rows have been read, the reader returns io.EOF to indicate the
	// end of the sequence. It is valid for the reader to return both a non-zero
	// number of rows and a non-nil error (including io.EOF).
	//
	// The buffer of rows passed as argument will be used to store values of
	// each row read from the reader. If the rows are not nil, the backing array
	// of the slices will be used as an optimization to avoid re-allocating new
	// arrays.
	//
	// The application is expected to handle the case where ReadRows returns
	// less rows than requested and no error, by looking at the first returned
	// value from ReadRows, which is the number of rows that were read.
	ReadRows([]Row) (int, error)
}

// RowReaderFrom reads parquet rows from reader.
type RowReaderFrom interface {
	ReadRowsFrom(RowReader) (int64, error)
}

// RowReaderWithSchema is an extension of the RowReader interface which
// advertises the schema of rows returned by ReadRow calls.
type RowReaderWithSchema interface {
	RowReader
	Schema() *Schema
}

// RowReadSeeker is an interface implemented by row readers which support
// seeking to arbitrary row positions.
type RowReadSeeker interface {
	RowReader
	RowSeeker
}

// RowWriter writes parquet rows to an underlying medium.
type RowWriter interface {
	// Writes rows to the writer, returning the number of rows written and any
	// error that occurred.
	//
	// Because columnar operations operate on independent columns of values,
	// writes of rows may not be atomic operations, and could result in some
	// rows being partially written. The method returns the number of rows that
	// were successfully written, but if an error occurs, values of the row(s)
	// that failed to be written may have been partially committed to their
	// columns. For that reason, applications should consider a write error as
	// fatal and assume that they need to discard the state, they cannot retry
	// the write nor recover the underlying file.
	WriteRows([]Row) (int, error)
}

// RowWriterTo writes parquet rows to a writer.
type RowWriterTo interface {
	WriteRowsTo(RowWriter) (int64, error)
}

// RowWriterWithSchema is an extension of the RowWriter interface which
// advertises the schema of rows expected to be passed to WriteRow calls.
type RowWriterWithSchema interface {
	RowWriter
	Schema() *Schema
}

// RowReaderFunc is a function type implementing the RowReader interface.
type RowReaderFunc func([]Row) (int, error)

func (f RowReaderFunc) ReadRows(rows []Row) (int, error) { return f(rows) }

// RowWriterFunc is a function type implementing the RowWriter interface.
type RowWriterFunc func([]Row) (int, error)

func (f RowWriterFunc) WriteRows(rows []Row) (int, error) { return f(rows) }

// MultiRowWriter constructs a RowWriter which dispatches writes to all the
// writers passed as arguments.
//
// When writing rows, if any of the writers returns an error, the operation is
// aborted and the error returned. If one of the writers did not error, but did
// not write all the rows, the operation is aborted and io.ErrShortWrite is
// returned.
//
// Rows are written sequentially to each writer in the order they are given to
// this function.
func MultiRowWriter(writers ...RowWriter) RowWriter {
	m := &multiRowWriter{writers: make([]RowWriter, len(writers))}
	copy(m.writers, writers)
	return m
}

type multiRowWriter struct{ writers []RowWriter }

func (m *multiRowWriter) WriteRows(rows []Row) (int, error) {
	for _, w := range m.writers {
		n, err := w.WriteRows(rows)
		if err != nil {
			return n, err
		}
		if n != len(rows) {
			return n, io.ErrShortWrite
		}
	}
	return len(rows), nil
}

type forwardRowSeeker struct {
	rows  RowReader
	seek  int64
	index int64
}

func (r *forwardRowSeeker) ReadRows(rows []Row) (int, error) {
	for {
		n, err := r.rows.ReadRows(rows)

		if n > 0 && r.index < r.seek {
			skip := r.seek - r.index
			r.index += int64(n)
			if skip >= int64(n) {
				continue
			}

			for i, j := 0, int(skip); j < n; i++ {
				rows[i] = append(rows[i][:0], rows[j]...)
			}

			n -= int(skip)
		}

		return n, err
	}
}

func (r *forwardRowSeeker) SeekToRow(rowIndex int64) error {
	if rowIndex >= r.index {
		r.seek = rowIndex
		return nil
	}
	return fmt.Errorf(
		"SeekToRow: %T does not implement parquet.RowSeeker: cannot seek backward from row %d to %d",
		r.rows,
		r.index,
		rowIndex,
	)
}

// CopyRows copies rows from src to dst.
//
// The underlying types of src and dst are tested to determine if they expose
// information about the schema of rows that are read and expected to be
// written. If the schema information are available but do not match, the
// function will attempt to automatically convert the rows from the source
// schema to the destination.
//
// As an optimization, the src argument may implement RowWriterTo to bypass
// the default row copy logic and provide its own. The dst argument may also
// implement RowReaderFrom for the same purpose.
//
// The function returns the number of rows written, or any error encountered
// other than io.EOF.
func CopyRows(dst RowWriter, src RowReader) (int64, error) {
	return copyRows(dst, src, nil)
}

func copyRows(dst RowWriter, src RowReader, buf []Row) (written int64, err error) {
	targetSchema := targetSchemaOf(dst)
	sourceSchema := sourceSchemaOf(src)

	if targetSchema != nil && sourceSchema != nil {
		if !nodesAreEqual(targetSchema, sourceSchema) {
			conv, err := Convert(targetSchema, sourceSchema)
			if err != nil {
				return 0, err
			}
			// The conversion effectively disables a potential optimization
			// if the source reader implemented RowWriterTo. It is a trade off
			// we are making to optimize for safety rather than performance.
			//
			// Entering this code path should not be the common case tho, it is
			// most often used when parquet schemas are evolving, but we expect
			// that the majority of files of an application to be sharing a
			// common schema.
			src = ConvertRowReader(src, conv)
		}
	}

	if wt, ok := src.(RowWriterTo); ok {
		return wt.WriteRowsTo(dst)
	}

	if rf, ok := dst.(RowReaderFrom); ok {
		return rf.ReadRowsFrom(src)
	}

	if len(buf) == 0 {
		buf = make([]Row, defaultRowBufferSize)
	}

	defer clearRows(buf)

	for {
		rn, err := src.ReadRows(buf)

		if rn > 0 {
			wn, err := dst.WriteRows(buf[:rn])
			if err != nil {
				return written, err
			}

			written += int64(wn)
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			}
			return written, err
		}

		if rn == 0 {
			return written, io.ErrNoProgress
		}
	}
}

func makeRows(n int) []Row {
	buf := make([]Value, n)
	row := make([]Row, n)
	for i := range row {
		row[i] = buf[i : i : i+1]
	}
	return row
}

func clearRows(rows []Row) {
	for i, values := range rows {
		clearValues(values)
		rows[i] = values[:0]
	}
}

func sourceSchemaOf(r RowReader) *Schema {
	if rrs, ok := r.(RowReaderWithSchema); ok {
		return rrs.Schema()
	}
	return nil
}

func targetSchemaOf(w RowWriter) *Schema {
	if rws, ok := w.(RowWriterWithSchema); ok {
		return rws.Schema()
	}
	return nil
}

// =============================================================================
// Functions returning closures are marked with "go:noinline" below to prevent
// losing naming information of the closure in stack traces.
//
// Because some of the functions are very short (simply return a closure), the
// compiler inlines when at their call site, which result in the closure being
// named something like parquet.deconstructFuncOf.func2 instead of the original
// parquet.deconstructFuncOfLeaf.func1; the latter being much more meaningful
// when reading CPU or memory profiles.
// =============================================================================

type levels struct {
	repetitionDepth byte
	repetitionLevel byte
	definitionLevel byte
}

// deconstructFunc accepts a row, the current levels, the value to deserialize
// the current column onto, and returns the row minus the deserialied value(s)
// It recurses until it hits a leaf node, then deserializes that value
// individually as the base case.
type deconstructFunc func([][]Value, levels, reflect.Value)

func deconstructFuncOf(columnIndex int16, node Node) (int16, deconstructFunc) {
	switch {
	case node.Optional():
		return deconstructFuncOfOptional(columnIndex, node)
	case node.Repeated():
		return deconstructFuncOfRepeated(columnIndex, node)
	case isList(node):
		return deconstructFuncOfList(columnIndex, node)
	case isMap(node):
		return deconstructFuncOfMap(columnIndex, node)
	default:
		return deconstructFuncOfRequired(columnIndex, node)
	}
}

//go:noinline
func deconstructFuncOfOptional(columnIndex int16, node Node) (int16, deconstructFunc) {
	columnIndex, deconstruct := deconstructFuncOf(columnIndex, Required(node))
	return columnIndex, func(columns [][]Value, levels levels, value reflect.Value) {
		if value.IsValid() {
			if value.IsZero() {
				value = reflect.Value{}
			} else {
				if value.Kind() == reflect.Ptr {
					value = value.Elem()
				}
				levels.definitionLevel++
			}
		}
		deconstruct(columns, levels, value)
	}
}

//go:noinline
func deconstructFuncOfRepeated(columnIndex int16, node Node) (int16, deconstructFunc) {
	columnIndex, deconstruct := deconstructFuncOf(columnIndex, Required(node))
	return columnIndex, func(columns [][]Value, levels levels, value reflect.Value) {
		if value.Kind() == reflect.Interface {
			value = value.Elem()
		}

		if !value.IsValid() || value.Len() == 0 {
			deconstruct(columns, levels, reflect.Value{})
			return
		}

		levels.repetitionDepth++
		levels.definitionLevel++

		for i, n := 0, value.Len(); i < n; i++ {
			deconstruct(columns, levels, value.Index(i))
			levels.repetitionLevel = levels.repetitionDepth
		}
	}
}

func deconstructFuncOfRequired(columnIndex int16, node Node) (int16, deconstructFunc) {
	switch {
	case node.Leaf():
		return deconstructFuncOfLeaf(columnIndex, node)
	default:
		return deconstructFuncOfGroup(columnIndex, node)
	}
}

func deconstructFuncOfList(columnIndex int16, node Node) (int16, deconstructFunc) {
	return deconstructFuncOf(columnIndex, Repeated(listElementOf(node)))
}

//go:noinline
func deconstructFuncOfMap(columnIndex int16, node Node) (int16, deconstructFunc) {
	keyValue := mapKeyValueOf(node)
	keyValueType := keyValue.GoType()
	keyValueElem := keyValueType.Elem()
	keyType := keyValueElem.Field(0).Type
	valueType := keyValueElem.Field(1).Type
	nextColumnIndex, deconstruct := deconstructFuncOf(columnIndex, schemaOf(keyValueElem))
	return nextColumnIndex, func(columns [][]Value, levels levels, mapValue reflect.Value) {
		if !mapValue.IsValid() || mapValue.Len() == 0 {
			deconstruct(columns, levels, reflect.Value{})
			return
		}

		levels.repetitionDepth++
		levels.definitionLevel++

		elem := reflect.New(keyValueElem).Elem()
		k := elem.Field(0)
		v := elem.Field(1)

		for _, key := range mapValue.MapKeys() {
			k.Set(key.Convert(keyType))
			v.Set(mapValue.MapIndex(key).Convert(valueType))
			deconstruct(columns, levels, elem)
			levels.repetitionLevel = levels.repetitionDepth
		}
	}
}

//go:noinline
func deconstructFuncOfGroup(columnIndex int16, node Node) (int16, deconstructFunc) {
	fields := node.Fields()
	funcs := make([]deconstructFunc, len(fields))
	for i, field := range fields {
		columnIndex, funcs[i] = deconstructFuncOf(columnIndex, field)
	}
	return columnIndex, func(columns [][]Value, levels levels, value reflect.Value) {
		if value.IsValid() {
			for i, f := range funcs {
				f(columns, levels, fields[i].Value(value))
			}
		} else {
			for _, f := range funcs {
				f(columns, levels, value)
			}
		}
	}
}

//go:noinline
func deconstructFuncOfLeaf(columnIndex int16, node Node) (int16, deconstructFunc) {
	if columnIndex > MaxColumnIndex {
		panic("row cannot be deconstructed because it has more than 127 columns")
	}
	typ := node.Type()
	kind := typ.Kind()
	lt := typ.LogicalType()
	valueColumnIndex := ^columnIndex
	return columnIndex + 1, func(columns [][]Value, levels levels, value reflect.Value) {
		v := Value{}

		if value.IsValid() {
			v = makeValue(kind, lt, value)
		}

		v.repetitionLevel = levels.repetitionLevel
		v.definitionLevel = levels.definitionLevel
		v.columnIndex = valueColumnIndex

		columns[columnIndex] = append(columns[columnIndex], v)
	}
}

// "reconstructX" turns a Go value into a Go representation of a Parquet series
// of values

type reconstructFunc func(reflect.Value, levels, [][]Value) error

func reconstructFuncOf(columnIndex int16, node Node) (int16, reconstructFunc) {
	switch {
	case node.Optional():
		return reconstructFuncOfOptional(columnIndex, node)
	case node.Repeated():
		return reconstructFuncOfRepeated(columnIndex, node)
	case isList(node):
		return reconstructFuncOfList(columnIndex, node)
	case isMap(node):
		return reconstructFuncOfMap(columnIndex, node)
	default:
		return reconstructFuncOfRequired(columnIndex, node)
	}
}

//go:noinline
func reconstructFuncOfOptional(columnIndex int16, node Node) (int16, reconstructFunc) {
	// We convert the optional func to required so that we eventually reach the
	// leaf base-case.  We're still using the heuristics of optional in the
	// returned closure (see levels.definitionLevel++), but we don't actually do
	// deserialization here, that happens in the leaf function, hence this line.
	nextColumnIndex, reconstruct := reconstructFuncOf(columnIndex, Required(node))

	return nextColumnIndex, func(value reflect.Value, levels levels, columns [][]Value) error {
		levels.definitionLevel++

		if columns[0][0].definitionLevel < levels.definitionLevel {
			value.Set(reflect.Zero(value.Type()))
			return nil
		}

		if value.Kind() == reflect.Ptr {
			if value.IsNil() {
				value.Set(reflect.New(value.Type().Elem()))
			}
			value = value.Elem()
		}

		return reconstruct(value, levels, columns)
	}
}

func setMakeSlice(v reflect.Value, n int) reflect.Value {
	t := v.Type()
	if t.Kind() == reflect.Interface {
		t = reflect.TypeOf(([]interface{})(nil))
	}
	s := reflect.MakeSlice(t, n, n)
	v.Set(s)
	return s
}

//go:noinline
func reconstructFuncOfRepeated(columnIndex int16, node Node) (int16, reconstructFunc) {
	nextColumnIndex, reconstruct := reconstructFuncOf(columnIndex, Required(node))
	return nextColumnIndex, func(value reflect.Value, levels levels, columns [][]Value) error {
		levels.repetitionDepth++
		levels.definitionLevel++

		if columns[0][0].definitionLevel < levels.definitionLevel {
			setMakeSlice(value, 0)
			return nil
		}

		values := make([][]Value, len(columns))
		column := columns[0]
		n := 0

		for i, column := range columns {
			values[i] = column[0:0:len(column)]
		}

		for i := 0; i < len(column); {
			i++
			n++

			for i < len(column) && column[i].repetitionLevel > levels.repetitionDepth {
				i++
			}
		}

		value = setMakeSlice(value, n)

		for i := 0; i < n; i++ {
			for j, column := range values {
				column = column[:cap(column)]
				if len(column) == 0 {
					continue
				}

				k := 1
				for k < len(column) && column[k].repetitionLevel > levels.repetitionDepth {
					k++
				}

				values[j] = column[:k]
			}

			if err := reconstruct(value.Index(i), levels, values); err != nil {
				return err
			}

			for j, column := range values {
				values[j] = column[len(column):len(column):cap(column)]
			}

			levels.repetitionLevel = levels.repetitionDepth
		}

		return nil
	}
}

func reconstructFuncOfRequired(columnIndex int16, node Node) (int16, reconstructFunc) {
	switch {
	case node.Leaf():
		return reconstructFuncOfLeaf(columnIndex, node)
	default:
		return reconstructFuncOfGroup(columnIndex, node)
	}
}

func reconstructFuncOfList(columnIndex int16, node Node) (int16, reconstructFunc) {
	return reconstructFuncOf(columnIndex, Repeated(listElementOf(node)))
}

//go:noinline
func reconstructFuncOfMap(columnIndex int16, node Node) (int16, reconstructFunc) {
	keyValue := mapKeyValueOf(node)
	keyValueType := keyValue.GoType()
	keyValueElem := keyValueType.Elem()
	keyValueZero := reflect.Zero(keyValueElem)
	nextColumnIndex, reconstruct := reconstructFuncOf(columnIndex, schemaOf(keyValueElem))
	return nextColumnIndex, func(value reflect.Value, levels levels, columns [][]Value) error {
		levels.repetitionDepth++
		levels.definitionLevel++

		if columns[0][0].definitionLevel < levels.definitionLevel {
			value.Set(reflect.MakeMap(value.Type()))
			return nil
		}

		values := make([][]Value, len(columns))
		column := columns[0]
		t := value.Type()
		if t.Kind() == reflect.Interface {
			t = reflect.TypeOf((map[string]any)(nil))
		}
		k := t.Key()
		v := t.Elem()
		n := 0

		for i, column := range columns {
			values[i] = column[0:0:len(column)]
		}

		for i := 0; i < len(column); {
			i++
			n++

			for i < len(column) && column[i].repetitionLevel > levels.repetitionDepth {
				i++
			}
		}

		if value.IsNil() {
			m := reflect.MakeMapWithSize(t, n)
			value.Set(m)
			value = m // track map instead of interface{} for read[any]()
		}

		elem := reflect.New(keyValueElem).Elem()
		for i := 0; i < n; i++ {
			for j, column := range values {
				column = column[:cap(column)]
				k := 1

				for k < len(column) && column[k].repetitionLevel > levels.repetitionDepth {
					k++
				}

				values[j] = column[:k]
			}

			if err := reconstruct(elem, levels, values); err != nil {
				return err
			}

			for j, column := range values {
				values[j] = column[len(column):len(column):cap(column)]
			}

			value.SetMapIndex(elem.Field(0).Convert(k), elem.Field(1).Convert(v))
			elem.Set(keyValueZero)
			levels.repetitionLevel = levels.repetitionDepth
		}

		return nil
	}
}

//go:noinline
func reconstructFuncOfGroup(columnIndex int16, node Node) (int16, reconstructFunc) {
	fields := node.Fields()
	funcs := make([]reconstructFunc, len(fields))
	columnOffsets := make([]int16, len(fields))
	firstColumnIndex := columnIndex

	for i, field := range fields {
		columnIndex, funcs[i] = reconstructFuncOf(columnIndex, field)
		columnOffsets[i] = columnIndex - firstColumnIndex
	}

	return columnIndex, func(value reflect.Value, levels levels, columns [][]Value) error {
		if value.Kind() == reflect.Interface {
			value.Set(reflect.MakeMap(reflect.TypeOf((map[string]interface{})(nil))))
			value = value.Elem()
		}

		if value.Kind() == reflect.Map {
			elemType := value.Type().Elem()
			name := reflect.New(reflect.TypeOf("")).Elem()
			elem := reflect.New(elemType).Elem()
			zero := reflect.Zero(elemType)

			if value.Len() > 0 {
				value.Set(reflect.MakeMap(value.Type()))
			}

			off := int16(0)

			for i, f := range funcs {
				name.SetString(fields[i].Name())
				end := columnOffsets[i]
				err := f(elem, levels, columns[off:end:end])
				if err != nil {
					return fmt.Errorf("%s → %w", name, err)
				}
				off = end
				value.SetMapIndex(name, elem)
				elem.Set(zero)
			}
		} else {
			off := int16(0)

			for i, f := range funcs {
				end := columnOffsets[i]
				err := f(fields[i].Value(value), levels, columns[off:end:end])
				if err != nil {
					return fmt.Errorf("%s → %w", fields[i].Name(), err)
				}
				off = end
			}
		}

		return nil
	}
}

//go:noinline
func reconstructFuncOfLeaf(columnIndex int16, node Node) (int16, reconstructFunc) {
	typ := node.Type()
	return columnIndex + 1, func(value reflect.Value, _ levels, columns [][]Value) error {
		column := columns[0]
		if len(column) == 0 {
			return fmt.Errorf("no values found in parquet row for column %d", columnIndex)
		}
		return typ.AssignValue(value, column[0])
	}
}
