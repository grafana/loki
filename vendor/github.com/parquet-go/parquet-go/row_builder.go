package parquet

// RowBuilder is a type which helps build parquet rows incrementally by adding
// values to columns.
type RowBuilder struct {
	columns [][]Value
	models  []Value
	levels  []columnLevel
	groups  []*columnGroup
}

type columnLevel struct {
	repetitionDepth byte
	repetitionLevel byte
	definitionLevel byte
}

type columnGroup struct {
	baseColumn      []Value
	members         []int16
	startIndex      int16
	endIndex        int16
	repetitionLevel byte
	definitionLevel byte
}

// NewRowBuilder constructs a RowBuilder which builds rows for the parquet
// schema passed as argument.
func NewRowBuilder(schema Node) *RowBuilder {
	if schema.Leaf() {
		panic("schema of row builder must be a group")
	}
	n := numLeafColumnsOf(schema)
	b := &RowBuilder{
		columns: make([][]Value, n),
		models:  make([]Value, n),
		levels:  make([]columnLevel, n),
	}
	buffers := make([]Value, len(b.columns))
	for i := range b.columns {
		b.columns[i] = buffers[i : i : i+1]
	}
	topGroup := &columnGroup{baseColumn: []Value{{}}}
	endIndex := b.configure(schema, 0, columnLevel{}, topGroup)
	topGroup.endIndex = endIndex
	b.groups = append(b.groups, topGroup)
	return b
}

func (b *RowBuilder) configure(node Node, columnIndex int16, level columnLevel, group *columnGroup) (endIndex int16) {
	switch {
	case node.Optional():
		level.definitionLevel++
		endIndex = b.configure(Required(node), columnIndex, level, group)

		for i := columnIndex; i < endIndex; i++ {
			b.models[i].kind = 0 // null if not set
			b.models[i].ptr = nil
			b.models[i].u64 = 0
		}

	case node.Repeated():
		level.definitionLevel++

		group = &columnGroup{
			startIndex:      columnIndex,
			repetitionLevel: level.repetitionDepth,
			definitionLevel: level.definitionLevel,
		}

		level.repetitionDepth++
		endIndex = b.configure(Required(node), columnIndex, level, group)

		for i := columnIndex; i < endIndex; i++ {
			b.models[i].kind = 0 // null if not set
			b.models[i].ptr = nil
			b.models[i].u64 = 0
		}

		group.endIndex = endIndex
		b.groups = append(b.groups, group)

	case node.Leaf():
		typ := node.Type()
		kind := typ.Kind()
		model := makeValueKind(kind)
		model.repetitionLevel = level.repetitionLevel
		model.definitionLevel = level.definitionLevel
		// FIXED_LEN_BYTE_ARRAY is the only type which needs to be given a
		// non-nil zero-value if the field is required.
		if kind == FixedLenByteArray {
			zero := make([]byte, typ.Length())
			model.ptr = &zero[0]
			model.u64 = uint64(len(zero))
		}
		group.members = append(group.members, columnIndex)
		b.models[columnIndex] = model
		b.levels[columnIndex] = level
		endIndex = columnIndex + 1

	default:
		endIndex = columnIndex

		for _, field := range node.Fields() {
			endIndex = b.configure(field, endIndex, level, group)
		}
	}
	return endIndex
}

// Add adds columnValue to the column at columnIndex.
func (b *RowBuilder) Add(columnIndex int, columnValue Value) {
	level := &b.levels[columnIndex]
	columnValue.repetitionLevel = level.repetitionLevel
	columnValue.definitionLevel = level.definitionLevel
	columnValue.columnIndex = ^int16(columnIndex)
	level.repetitionLevel = level.repetitionDepth
	b.columns[columnIndex] = append(b.columns[columnIndex], columnValue)
}

// Next must be called to indicate the start of a new repeated record for the
// column at the given index.
//
// If the column index is part of a repeated group, the builder automatically
// starts a new record for all adjacent columns, the application does not need
// to call this method for each column of the repeated group.
//
// Next must be called after adding a sequence of records.
func (b *RowBuilder) Next(columnIndex int) {
	for _, group := range b.groups {
		if group.startIndex <= int16(columnIndex) && int16(columnIndex) < group.endIndex {
			for i := group.startIndex; i < group.endIndex; i++ {
				if level := &b.levels[i]; level.repetitionLevel != 0 {
					level.repetitionLevel = group.repetitionLevel
				}
			}
			break
		}
	}
}

// Reset clears the internal state of b, making it possible to reuse while
// retaining the internal buffers.
func (b *RowBuilder) Reset() {
	for i, column := range b.columns {
		clearValues(column)
		b.columns[i] = column[:0]
	}
	for i := range b.levels {
		b.levels[i].repetitionLevel = 0
	}
}

// Row materializes the current state of b into a parquet row.
func (b *RowBuilder) Row() Row {
	numValues := 0
	for _, column := range b.columns {
		numValues += len(column)
	}
	return b.AppendRow(make(Row, 0, numValues))
}

// AppendRow appends the current state of b to row and returns it.
func (b *RowBuilder) AppendRow(row Row) Row {
	for _, group := range b.groups {
		maxColumn := group.baseColumn

		for _, columnIndex := range group.members {
			if column := b.columns[columnIndex]; len(column) > len(maxColumn) {
				maxColumn = column
			}
		}

		if len(maxColumn) != 0 {
			columns := b.columns[group.startIndex:group.endIndex]

			for i, column := range columns {
				if len(column) < len(maxColumn) {
					n := len(column)
					column = append(column, maxColumn[n:]...)

					columnIndex := group.startIndex + int16(i)
					model := b.models[columnIndex]

					for n < len(column) {
						v := &column[n]
						v.kind = model.kind
						v.ptr = model.ptr
						v.u64 = model.u64
						v.definitionLevel = group.definitionLevel
						v.columnIndex = ^columnIndex
						n++
					}

					columns[i] = column
				}
			}
		}
	}

	return appendRow(row, b.columns)
}
