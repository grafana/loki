package parquet

import (
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/sparse"
)

// ColumnBuffer is an interface representing columns of a row group.
//
// ColumnBuffer implements sort.Interface as a way to support reordering the
// rows that have been written to it.
//
// The current implementation has a limitation which prevents applications from
// providing custom versions of this interface because it contains unexported
// methods. The only way to create ColumnBuffer values is to call the
// NewColumnBuffer of Type instances. This limitation may be lifted in future
// releases.
type ColumnBuffer interface {
	// Exposes a read-only view of the column buffer.
	ColumnChunk

	// The column implements ValueReaderAt as a mechanism to read values at
	// specific locations within the buffer.
	ValueReaderAt

	// The column implements ValueWriter as a mechanism to optimize the copy
	// of values into the buffer in contexts where the row information is
	// provided by the values because the repetition and definition levels
	// are set.
	ValueWriter

	// For indexed columns, returns the underlying dictionary holding the column
	// values. If the column is not indexed, nil is returned.
	Dictionary() Dictionary

	// Returns a copy of the column. The returned copy shares no memory with
	// the original, mutations of either column will not modify the other.
	Clone() ColumnBuffer

	// Returns the column as a Page.
	Page() Page

	// Clears all rows written to the column.
	Reset()

	// Returns the current capacity of the column (rows).
	Cap() int

	// Returns the number of rows currently written to the column.
	Len() int

	// Compares rows at index i and j and reports whether i < j.
	Less(i, j int) bool

	// Swaps rows at index i and j.
	Swap(i, j int)

	// Returns the size of the column buffer in bytes.
	Size() int64

	// This method is employed to write rows from arrays of Go values into the
	// column buffer. The method is currently unexported because it uses unsafe
	// APIs which would be difficult for applications to leverage, increasing
	// the risk of introducing bugs in the code. As a consequence, applications
	// cannot use custom implementations of the ColumnBuffer interface since
	// they cannot declare an unexported method that would match this signature.
	// It means that in order to create a ColumnBuffer value, programs need to
	// go through a call to NewColumnBuffer on a Type instance. We make this
	// trade off for now as it is preferrable to optimize for safety over
	// extensibility in the public APIs, we might revisit in the future if we
	// learn about valid use cases for custom column buffer types.
	writeValues(levels columnLevels, rows sparse.Array)

	// Parquet primitive type write methods. Each column buffer implementation
	// supports only the Parquet types it can handle and panics for others.
	// These methods are unexported for the same reasons as writeValues above.
	writeBoolean(levels columnLevels, value bool)
	writeInt32(levels columnLevels, value int32)
	writeInt64(levels columnLevels, value int64)
	writeInt96(levels columnLevels, value deprecated.Int96)
	writeFloat(levels columnLevels, value float32)
	writeDouble(levels columnLevels, value float64)
	writeByteArray(levels columnLevels, value []byte)
	writeNull(levels columnLevels)
}

func columnIndexOfNullable(base ColumnBuffer, maxDefinitionLevel byte, definitionLevels []byte) (ColumnIndex, error) {
	index, err := base.ColumnIndex()
	if err != nil {
		return nil, err
	}
	return &nullableColumnIndex{
		ColumnIndex:        index,
		maxDefinitionLevel: maxDefinitionLevel,
		definitionLevels:   definitionLevels,
	}, nil
}

type nullableColumnIndex struct {
	ColumnIndex
	maxDefinitionLevel byte
	definitionLevels   []byte
}

func (index *nullableColumnIndex) NullPage(i int) bool {
	return index.NullCount(i) == int64(len(index.definitionLevels))
}

func (index *nullableColumnIndex) NullCount(i int) int64 {
	return int64(countLevelsNotEqual(index.definitionLevels, index.maxDefinitionLevel))
}

type nullOrdering func(column ColumnBuffer, i, j int, maxDefinitionLevel, definitionLevel1, definitionLevel2 byte) bool

func nullsGoFirst(column ColumnBuffer, i, j int, maxDefinitionLevel, definitionLevel1, definitionLevel2 byte) bool {
	if definitionLevel1 != maxDefinitionLevel {
		return definitionLevel2 == maxDefinitionLevel
	} else {
		return definitionLevel2 == maxDefinitionLevel && column.Less(i, j)
	}
}

func nullsGoLast(column ColumnBuffer, i, j int, maxDefinitionLevel, definitionLevel1, definitionLevel2 byte) bool {
	return definitionLevel1 == maxDefinitionLevel && (definitionLevel2 != maxDefinitionLevel || column.Less(i, j))
}

// reversedColumnBuffer is an adapter of ColumnBuffer which inverses the order
// in which rows are ordered when the column gets sorted.
//
// This type is used when buffers are constructed with sorting columns ordering
// values in descending order.
type reversedColumnBuffer struct{ ColumnBuffer }

func (col *reversedColumnBuffer) Less(i, j int) bool { return col.ColumnBuffer.Less(j, i) }

var (
	_ ColumnBuffer = (*optionalColumnBuffer)(nil)
	_ ColumnBuffer = (*repeatedColumnBuffer)(nil)
	_ ColumnBuffer = (*booleanColumnBuffer)(nil)
	_ ColumnBuffer = (*int32ColumnBuffer)(nil)
	_ ColumnBuffer = (*int64ColumnBuffer)(nil)
	_ ColumnBuffer = (*int96ColumnBuffer)(nil)
	_ ColumnBuffer = (*floatColumnBuffer)(nil)
	_ ColumnBuffer = (*doubleColumnBuffer)(nil)
	_ ColumnBuffer = (*byteArrayColumnBuffer)(nil)
	_ ColumnBuffer = (*fixedLenByteArrayColumnBuffer)(nil)
	_ ColumnBuffer = (*uint32ColumnBuffer)(nil)
	_ ColumnBuffer = (*uint64ColumnBuffer)(nil)
	_ ColumnBuffer = (*be128ColumnBuffer)(nil)
)
