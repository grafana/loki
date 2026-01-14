package parquet

import (
	"encoding/json"
	"fmt"
	"math/bits"
	"reflect"
	"slices"
	"time"
	"unsafe"

	"github.com/parquet-go/jsonlite"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/internal/memory"
	"github.com/parquet-go/parquet-go/sparse"
	"google.golang.org/protobuf/types/known/structpb"
)

// writeRowsFunc is the type of functions that apply rows to a set of column
// buffers.
//
// - columns is the array of column buffer where the rows are written.
//
// - rows is the array of Go values to write to the column buffers.
//
//   - levels is used to track the column index, repetition and definition levels
//     of values when writing optional or repeated columns.
type writeRowsFunc func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array)

// writeRowsFuncOf generates a writeRowsFunc function for the given Go type and
// parquet schema. The column path indicates the column that the function is
// being generated for in the parquet schema.
func writeRowsFuncOf(t reflect.Type, schema *Schema, path columnPath, tagReplacements []StructTagOption) writeRowsFunc {
	if leaf, exists := schema.Lookup(path...); exists {
		logicalType := leaf.Node.Type().LogicalType()
		if logicalType != nil && logicalType.Json != nil {
			return writeRowsFuncOfJSON(t, schema, path)
		}
	}

	switch t {
	case reflect.TypeFor[deprecated.Int96]():
		return writeRowsFuncOfRequired(t, schema, path)
	case reflect.TypeFor[time.Time]():
		return writeRowsFuncOfTime(t, schema, path, tagReplacements)
	case reflect.TypeFor[json.RawMessage]():
		return writeRowsFuncFor[json.RawMessage](schema, path)
	case reflect.TypeFor[json.Number]():
		return writeRowsFuncFor[json.Number](schema, path)
	case reflect.TypeFor[*structpb.Struct]():
		return writeRowsFuncFor[*structpb.Struct](schema, path)
	case reflect.TypeFor[*jsonlite.Value]():
		return writeRowsFuncFor[*jsonlite.Value](schema, path)
	}

	switch t.Kind() {
	case reflect.Bool,
		reflect.Int,
		reflect.Uint,
		reflect.Int32,
		reflect.Uint32,
		reflect.Int64,
		reflect.Uint64,
		reflect.Float32,
		reflect.Float64,
		reflect.String:
		return writeRowsFuncOfRequired(t, schema, path)
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return writeRowsFuncOfRequired(t, schema, path)
		} else {
			return writeRowsFuncOfSlice(t, schema, path, tagReplacements)
		}
	case reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return writeRowsFuncOfArray(t, schema, path)
		}
	case reflect.Pointer:
		return writeRowsFuncOfPointer(t, schema, path, tagReplacements)
	case reflect.Struct:
		return writeRowsFuncOfStruct(t, schema, path, tagReplacements)
	case reflect.Map:
		return writeRowsFuncOfMap(t, schema, path, tagReplacements)
	case reflect.Interface:
		return writeRowsFuncOfInterface(t, schema, path)
	}
	panic("cannot convert Go values of type " + typeNameOf(t) + " to parquet value")
}

func writeRowsFuncOfRequired(t reflect.Type, schema *Schema, path columnPath) writeRowsFunc {
	column := schema.lazyLoadState().mapping.lookup(path)
	columnIndex := column.columnIndex
	if columnIndex < 0 {
		panic("parquet: column not found: " + path.String())
	}
	return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
		columns[columnIndex].writeValues(levels, rows)
	}
}

func writeRowsFuncOfOptional(t reflect.Type, schema *Schema, path columnPath, writeRows writeRowsFunc) writeRowsFunc {
	// For interface types, we just increment the definition level for present
	// values without checking for null indexes.
	// Interface: handled by writeRowsFuncOfInterface which manages levels internally
	writeOptional := func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
		if rows.Len() == 0 {
			writeRows(columns, levels, rows)
			return
		}
		levels.definitionLevel++
		writeRows(columns, levels, rows)
	}

	switch t.Kind() {
	case reflect.Interface:
		return writeOptional
	case reflect.Slice:
		// For slices (nested lists), we need to distinguish between nil slices
		// and empty slices. Nil slices should not increment the definition level
		// (they represent null), while non-nil empty slices should increment it
		// (they represent an empty list).
		if t.Elem().Kind() != reflect.Uint8 {
			type sliceHeader struct {
				base unsafe.Pointer
				len  int
				cap  int
			}
			return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
				if rows.Len() == 0 {
					writeRows(columns, levels, rows)
					return
				}
				// Process each slice individually to check for nil
				for i := range rows.Len() {
					p := (*sliceHeader)(rows.Index(i))
					elemLevels := levels
					// A nil slice has base=nil, while an empty slice has base!=nil but len=0
					// We need to increment definition level for non-nil slices (including empty ones)
					if p.base != nil {
						elemLevels.definitionLevel++
					}
					writeRows(columns, elemLevels, rows.Slice(i, i+1))
				}
			}
		}
	}

	nullIndex := nullIndexFuncOf(t)
	return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
		if rows.Len() == 0 {
			writeRows(columns, levels, rows)
			return
		}

		nulls := acquireBitmap(rows.Len())
		defer releaseBitmap(nulls)
		nullIndex(nulls.bits, rows)

		nullLevels := levels
		levels.definitionLevel++
		// In this function, we are dealing with optional values which are
		// neither pointers nor slices; for example, a int32 field marked
		// "optional" in its parent struct.
		//
		// We need to find zero values, which should be represented as nulls
		// in the parquet column. In order to minimize the calls to writeRows
		// and maximize throughput, we use the nullIndex and nonNullIndex
		// functions, which are type-specific implementations of the algorithm.
		//
		// Sections of the input that are contiguous nulls or non-nulls can be
		// sent to a single call to writeRows to be written to the underlying
		// buffer since they share the same definition level.
		//
		// This optimization is defeated by inputs alternating null and non-null
		// sequences of single values, we do not expect this condition to be a
		// common case.
		for i := 0; i < rows.Len(); {
			j := 0
			x := i / 64
			y := i % 64

			if y != 0 {
				if b := nulls.bits[x] >> uint(y); b == 0 {
					x++
					y = 0
				} else {
					y += bits.TrailingZeros64(b)
					goto writeNulls
				}
			}

			for x < len(nulls.bits) && nulls.bits[x] == 0 {
				x++
			}

			if x < len(nulls.bits) {
				y = bits.TrailingZeros64(nulls.bits[x]) % 64
			}

		writeNulls:
			if j = x*64 + y; j > rows.Len() {
				j = rows.Len()
			}

			if i < j {
				writeRows(columns, nullLevels, rows.Slice(i, j))
				i = j
			}

			if y != 0 {
				if b := nulls.bits[x] >> uint(y); b == (1<<uint64(y))-1 {
					x++
					y = 0
				} else {
					y += bits.TrailingZeros64(^b)
					goto writeNonNulls
				}
			}

			for x < len(nulls.bits) && nulls.bits[x] == ^uint64(0) {
				x++
			}

			if x < len(nulls.bits) {
				y = bits.TrailingZeros64(^nulls.bits[x]) % 64
			}

		writeNonNulls:
			if j = x*64 + y; j > rows.Len() {
				j = rows.Len()
			}

			if i < j {
				writeRows(columns, levels, rows.Slice(i, j))
				i = j
			}
		}

	}
}

func writeRowsFuncOfArray(t reflect.Type, schema *Schema, path columnPath) writeRowsFunc {
	column := schema.lazyLoadState().mapping.lookup(path)
	arrayLen := t.Len()
	columnLen := column.node.Type().Length()
	if arrayLen != columnLen {
		panic(fmt.Sprintf("cannot convert Go values of type "+typeNameOf(t)+" to FIXED_LEN_BYTE_ARRAY(%d)", columnLen))
	}
	return writeRowsFuncOfRequired(t, schema, path)
}

func writeRowsFuncOfPointer(t reflect.Type, schema *Schema, path columnPath, tagReplacements []StructTagOption) writeRowsFunc {
	elemType := t.Elem()
	elemSize := uintptr(elemType.Size())
	writeRows := writeRowsFuncOf(elemType, schema, path, tagReplacements)

	if len(path) == 0 {
		// This code path is taken when generating a writeRowsFunc for a pointer
		// type. In this case, we do not need to increase the definition level
		// since we are not deailng with an optional field but a pointer to the
		// row type.
		return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
			if rows.Len() == 0 {
				writeRows(columns, levels, rows)
				return
			}

			for i := range rows.Len() {
				p := *(*unsafe.Pointer)(rows.Index(i))
				a := sparse.Array{}
				if p != nil {
					a = makeArray(p, 1, elemSize)
				}
				writeRows(columns, levels, a)
			}
		}
	}

	// Check if the schema node at this path is optional. If not, the pointer
	// is just a Go implementation detail (like proto message types) and we
	// should not increment the definition level.
	node := findByPath(schema, path)
	isOptional := node != nil && node.Optional()

	return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
		if rows.Len() == 0 {
			writeRows(columns, levels, rows)
			return
		}

		for i := range rows.Len() {
			p := *(*unsafe.Pointer)(rows.Index(i))
			a := sparse.Array{}
			elemLevels := levels
			if p != nil {
				a = makeArray(p, 1, elemSize)
				if isOptional {
					elemLevels.definitionLevel++
				}
			}
			writeRows(columns, elemLevels, a)
		}

	}
}

func writeRowsFuncOfSlice(t reflect.Type, schema *Schema, path columnPath, tagReplacements []StructTagOption) writeRowsFunc {
	elemType := t.Elem()
	elemSize := uintptr(elemType.Size())
	writeRows := writeRowsFuncOf(elemType, schema, path, tagReplacements)

	return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
		type sliceHeader struct {
			base unsafe.Pointer
			len  int
			cap  int
		}

		if rows.Len() == 0 {
			writeRows(columns, levels, rows)
			return
		}

		levels.repetitionDepth++

		for i := range rows.Len() {
			p := (*sliceHeader)(rows.Index(i))
			a := makeArray(p.base, p.len, elemSize)
			b := sparse.Array{}

			elemLevels := levels
			if a.Len() > 0 {
				b = a.Slice(0, 1)
				elemLevels.definitionLevel++
			}

			writeRows(columns, elemLevels, b)

			if a.Len() > 1 {
				elemLevels.repetitionLevel = elemLevels.repetitionDepth

				writeRows(columns, elemLevels, a.Slice(1, a.Len()))
			}
		}

	}
}

func writeRowsFuncOfStruct(t reflect.Type, schema *Schema, path columnPath, tagReplacements []StructTagOption) writeRowsFunc {
	type column struct {
		offset    uintptr
		writeRows writeRowsFunc
	}

	fields := structFieldsOf(path, t, tagReplacements)
	columns := make([]column, len(fields))

	for i, f := range fields {
		columnPath := path.append(f.Name)
		// Check if the schema node at the field path is optional and/or a list
		// Use the schema structure, not the Go struct tags
		node := findByPath(schema, columnPath)
		list := false
		forEachStructTagOption(f, func(_ reflect.Type, option, _ string) {
			switch option {
			case "list":
				list = true
				columnPath = columnPath.append("list", "element")
			}
		})
		writeRows := writeRowsFuncOf(f.Type, schema, columnPath, tagReplacements)
		// Check if the schema node is optional (from the schema, not the Go struct tag)
		if node.Optional() {
			kind := f.Type.Kind()
			switch {
			case kind == reflect.Pointer:
			case kind == reflect.Interface:
				// Interface types handle their own definition levels through
				// writeRowsFuncOfInterface -> writeValueFuncOf -> writeValueFuncOfOptional
			case kind == reflect.Slice && !list && f.Type.Elem().Kind() != reflect.Uint8:
				// For slices other than []byte, optional applies
				// to the element, not the list.
			case f.Type == reflect.TypeFor[json.RawMessage]():
				// json.RawMessage handles its own definition levels through
				// writeRowsFuncOfJSONRawMessage -> writeValueFuncOf -> writeValueFuncOfOptional
			case f.Type == reflect.TypeFor[time.Time]():
				// time.Time is a struct but has IsZero() method,
				// so it needs special handling.
				// Don't use writeRowsFuncOfOptional which relies
				// on bitmap batching.
			default:
				writeRows = writeRowsFuncOfOptional(f.Type, schema, columnPath, writeRows)
			}
		}

		columns[i] = column{
			offset:    f.Offset,
			writeRows: writeRows,
		}
	}

	return func(buffers []ColumnBuffer, levels columnLevels, rows sparse.Array) {
		if rows.Len() == 0 {
			for _, column := range columns {
				column.writeRows(buffers, levels, rows)
			}
		} else {
			for _, column := range columns {
				column.writeRows(buffers, levels, rows.Offset(column.offset))
			}
		}
	}
}

func writeRowsFuncOfInterface(t reflect.Type, schema *Schema, path columnPath) writeRowsFunc {
	node := findByPath(schema, path)
	if node == nil {
		panic("column not found: " + path.String())
	}

	columnIndex := findColumnIndex(schema, node, path)
	if columnIndex < 0 {
		// Empty group node (e.g., from interface{} in map[string]any).
		// Return a no-op function since there are no columns to write.
		return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
			// No-op: nothing to write for empty groups
		}
	}

	// Get the schema-based write function for this node
	_, writeValue := writeValueFuncOf(columnIndex, node)

	return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
		for i := range rows.Len() {
			writeValue(columns, levels, reflect.NewAt(t, rows.Index(i)).Elem())
		}
	}
}

// writeRowsFuncOfMapToGroup handles writing a Go map to a Parquet GROUP schema
// (as opposed to a MAP logical type). This allows map[string]T to be written
// to schemas with named optional fields.
func writeRowsFuncOfMapToGroup(t reflect.Type, schema *Schema, path columnPath, groupNode Node, tagReplacements []StructTagOption) writeRowsFunc {
	if t.Key().Kind() != reflect.String {
		panic("map keys must be strings when writing to GROUP schema")
	}

	type fieldWriter struct {
		fieldName  string
		writeRows  writeRowsFunc // Writes null/empty value
		writeValue writeValueFunc
	}

	fields := groupNode.Fields()
	if len(fields) == 0 {
		// Empty group - return no-op function
		return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
			// No-op: empty group has no fields to write
		}
	}

	writers := make([]fieldWriter, len(fields))
	valueType := t.Elem()
	columnIndex := findColumnIndex(schema, findByPath(schema, path), path)

	for i, field := range fields {
		fieldPath := path.append(field.Name())
		writeRows := writeRowsFuncOf(valueType, schema, fieldPath, tagReplacements)
		if field.Optional() {
			writeRows = writeRowsFuncOfOptional(valueType, schema, fieldPath, writeRows)
		}

		var writeValue writeValueFunc
		columnIndex, writeValue = writeValueFuncOf(columnIndex, field)

		writers[i] = fieldWriter{
			fieldName:  field.Name(),
			writeRows:  writeRows,
			writeValue: writeValue,
		}
	}

	// We make sepcial cases for the common types to avoid paying the cost of
	// reflection in calls like MapIndex which force the returned value to be
	// allocated on the heap.
	var writeMaps writeRowsFunc
	switch {
	case t.ConvertibleTo(reflect.TypeFor[map[string]string]()):
		writeMaps = func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
			numRows := rows.Len()
			numValues := len(writers) * numRows
			buffer := stringArrayPool.Get(
				func() *stringArray { return new(stringArray) },
				func(b *stringArray) { b.values = b.values[:0] },
			)
			buffer.values = slices.Grow(buffer.values, numValues)[:numValues]
			defer stringArrayPool.Put(buffer)
			defer clear(buffer.values)

			for i := range numRows {
				m := *(*map[string]string)(reflect.NewAt(t, rows.Index(i)).UnsafePointer())
				for j := range writers {
					buffer.values[j*numRows+i] = m[writers[j].fieldName]
				}
			}

			for j := range writers {
				a := sparse.MakeStringArray(buffer.values[j*numRows : (j+1)*numRows])
				writers[j].writeRows(columns, levels, a.UnsafeArray())
			}
		}

	case t.ConvertibleTo(reflect.TypeFor[map[string]any]()):
		writeMaps = func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
			for i := range rows.Len() {
				m := *(*map[string]any)(reflect.NewAt(t, rows.Index(i)).UnsafePointer())

				for j := range writers {
					w := &writers[j]
					v := m[w.fieldName]
					w.writeValue(columns, levels, reflect.ValueOf(v))
				}
			}
		}

	default:
		writeMaps = func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
			for i := range rows.Len() {
				m := reflect.NewAt(t, rows.Index(i)).Elem()

				for j := range writers {
					w := &writers[j]
					keyValue := reflect.ValueOf(&w.fieldName).Elem()
					mapValue := m.MapIndex(keyValue)
					w.writeValue(columns, levels, mapValue)
				}
			}
		}
	}

	return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
		if rows.Len() == 0 {
			for _, w := range writers {
				w.writeRows(columns, levels, sparse.Array{})
			}
		} else {
			writeMaps(columns, levels, rows)
		}
	}
}

type stringArray struct{ values []string }

var stringArrayPool memory.Pool[stringArray]

// findColumnIndex finds the column index for a given node and path.
// For leaf nodes, returns the column index directly.
// For group nodes, recursively finds the first leaf column.
// Returns -1 for empty group nodes (groups with no fields), which can occur
// when using interface{} types in maps (e.g., map[string]any).
func findColumnIndex(schema *Schema, node Node, path columnPath) int16 {
	col := schema.lazyLoadState().mapping.lookup(path)
	if col.columnIndex >= 0 {
		return col.columnIndex
	}
	if node.Leaf() {
		panic("node is a leaf but has no column index: " + path.String())
	}
	fields := node.Fields()
	if len(fields) == 0 {
		// Empty group nodes can occur with interface{} types (e.g., map[string]any).
		// Return -1 to indicate there are no columns to write.
		return -1
	}
	firstFieldPath := path.append(fields[0].Name())
	return findColumnIndex(schema, fields[0], firstFieldPath)
}

func writeRowsFuncOfMap(t reflect.Type, schema *Schema, path columnPath, tagReplacements []StructTagOption) writeRowsFunc {
	// Check if the schema at this path is a MAP or a GROUP.
	node := findByPath(schema, path)
	if node != nil && !isMap(node) {
		// The schema is a GROUP (not a MAP), so we need to handle it differently.
		// Instead of using key_value structure, we iterate through the GROUP's fields
		// and look up corresponding map keys.
		return writeRowsFuncOfMapToGroup(t, schema, path, node, tagReplacements)
	}

	// Standard MAP logical type handling
	keyPath := path.append("key_value", "key")
	keyType := t.Key()
	writeKeys := writeRowsFuncOf(keyType, schema, keyPath, tagReplacements)

	valuePath := path.append("key_value", "value")
	valueNode := findByPath(schema, valuePath)
	// If the value is a LIST type, adjust the path to include list/element
	if valueNode != nil && isList(valueNode) {
		valuePath = valuePath.append("list", "element")
	}
	valueType := t.Elem()
	writeValues := writeRowsFuncOf(valueType, schema, valuePath, tagReplacements)

	return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
		if rows.Len() == 0 {
			writeKeys(columns, levels, rows)
			writeValues(columns, levels, rows)
			return
		}

		levels.repetitionDepth++
		makeMap := makeMapFuncOf(t)

		for i := range rows.Len() {
			m := reflect.NewAt(t, rows.Index(i)).Elem()
			n := m.Len()

			if n == 0 {
				empty := sparse.Array{}
				writeKeys(columns, levels, empty)
				writeValues(columns, levels, empty)
				continue
			}

			elemLevels := levels
			elemLevels.definitionLevel++

			keys, values := makeMap(m).entries()
			writeKeys(columns, elemLevels, keys.Slice(0, 1))
			writeValues(columns, elemLevels, values.Slice(0, 1))
			if n > 1 {
				elemLevels.repetitionLevel = elemLevels.repetitionDepth
				writeKeys(columns, elemLevels, keys.Slice(1, n))
				writeValues(columns, elemLevels, values.Slice(1, n))
			}
		}

	}
}

func writeRowsFuncOfJSON(t reflect.Type, schema *Schema, path columnPath) writeRowsFunc {
	// If this is a string or a byte array write directly.
	switch t.Kind() {
	case reflect.String:
		return writeRowsFuncOfRequired(t, schema, path)
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return writeRowsFuncOfRequired(t, schema, path)
		}
	}

	columnIndex := findColumnIndex(schema, schema, path)
	if columnIndex < 0 {
		// Empty group - return no-op function
		return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
			// No-op: empty group has no columns to write
		}
	}

	return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
		if rows.Len() == 0 {
			columns[columnIndex].writeNull(levels)
			return
		}

		b := memory.SliceBuffer[byte]{}
		w := memory.SliceWriter{Buffer: &b}
		defer b.Reset()

		for i := range rows.Len() {
			v := reflect.NewAt(t, rows.Index(i))
			b.Resize(0)

			enc := json.NewEncoder(w)
			enc.SetEscapeHTML(false)

			if err := enc.Encode(v.Interface()); err != nil {
				panic(err)
			}

			data := b.Slice()
			columns[columnIndex].writeByteArray(levels, data[:len(data)-1])
		}
	}
}

func writeRowsFuncOfTime(_ reflect.Type, schema *Schema, path columnPath, tagReplacements []StructTagOption) writeRowsFunc {
	t := reflect.TypeFor[int64]()
	elemSize := uintptr(t.Size())
	writeRows := writeRowsFuncOf(t, schema, path, tagReplacements)

	col, _ := schema.Lookup(path...)
	unit := Nanosecond.TimeUnit()
	lt := col.Node.Type().LogicalType()
	if lt != nil && lt.Timestamp != nil {
		unit = lt.Timestamp.Unit
	}

	// Check if the column is optional
	isOptional := col.Node.Optional()

	return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
		if rows.Len() == 0 {
			writeRows(columns, levels, rows)
			return
		}

		// If we're optional and the current definition level is already > 0,
		// then we're in a pointer/nested context where writeRowsFuncOfPointer
		// already handles optionality.
		//
		// Don't double-handle it here. For simple optional fields,
		// definitionLevel starts at 0.
		alreadyHandled := isOptional && levels.definitionLevel > 0

		times := rows.TimeArray()
		for i := range times.Len() {
			t := times.Index(i)

			// For optional fields, check if the value is zero
			// (unless already handled by pointer wrapper).
			elemLevels := levels
			if isOptional && !alreadyHandled && t.IsZero() {
				// Write as NULL (don't increment definition level).
				empty := sparse.Array{}
				writeRows(columns, elemLevels, empty)
				continue
			}

			// For optional non-zero values, increment definition level
			// (unless already handled).
			if isOptional && !alreadyHandled {
				elemLevels.definitionLevel++
			}

			var val int64
			switch {
			case unit.Millis != nil:
				val = t.UnixMilli()
			case unit.Micros != nil:
				val = t.UnixMicro()
			default:
				val = t.UnixNano()
			}

			a := makeArray(reflectValueData(reflect.ValueOf(val)), 1, elemSize)
			writeRows(columns, elemLevels, a)
		}
	}
}

// countLeafColumns returns the number of leaf columns in a node
func countLeafColumns(node Node) int16 {
	if node.Leaf() {
		return 1
	}
	count := int16(0)
	for _, field := range node.Fields() {
		count += countLeafColumns(field)
	}
	return count
}

func writeRowsFuncFor[T any](schema *Schema, path columnPath) writeRowsFunc {
	node := findByPath(schema, path)
	if node == nil {
		panic("parquet: column not found: " + path.String())
	}

	columnIndex := findColumnIndex(schema, node, path)
	if columnIndex < 0 {
		// Empty group - return no-op function
		return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
			// No-op: empty group has no columns to write
		}
	}

	_, writeValue := writeValueFuncOf(columnIndex, node)

	return func(columns []ColumnBuffer, levels columnLevels, rows sparse.Array) {
		for i := range rows.Len() {
			p := rows.Index(i)
			v := reflect.ValueOf((*T)(p)).Elem()
			writeValue(columns, levels, v)
		}
	}
}
