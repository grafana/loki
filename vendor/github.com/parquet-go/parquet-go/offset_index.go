package parquet

import (
	"github.com/parquet-go/parquet-go/format"
)

type OffsetIndex interface {
	// NumPages returns the number of pages in the offset index.
	NumPages() int

	// Offset returns the offset starting from the beginning of the file for the
	// page at the given index.
	Offset(int) int64

	// CompressedPageSize returns the size of the page at the given index
	// (in bytes).
	CompressedPageSize(int) int64

	// FirstRowIndex returns the the first row in the page at the given index.
	//
	// The returned row index is based on the row group that the page belongs
	// to, the first row has index zero.
	FirstRowIndex(int) int64
}

type fileOffsetIndex format.OffsetIndex

func (i *fileOffsetIndex) NumPages() int {
	return len(i.PageLocations)
}

func (i *fileOffsetIndex) Offset(j int) int64 {
	return i.PageLocations[j].Offset
}

func (i *fileOffsetIndex) CompressedPageSize(j int) int64 {
	return int64(i.PageLocations[j].CompressedPageSize)
}

func (i *fileOffsetIndex) FirstRowIndex(j int) int64 {
	return i.PageLocations[j].FirstRowIndex
}

type emptyOffsetIndex struct{}

func (emptyOffsetIndex) NumPages() int                { return 0 }
func (emptyOffsetIndex) Offset(int) int64             { return 0 }
func (emptyOffsetIndex) CompressedPageSize(int) int64 { return 0 }
func (emptyOffsetIndex) FirstRowIndex(int) int64      { return 0 }

type booleanOffsetIndex struct{ page *booleanPage }

func (i booleanOffsetIndex) NumPages() int                { return 1 }
func (i booleanOffsetIndex) Offset(int) int64             { return 0 }
func (i booleanOffsetIndex) CompressedPageSize(int) int64 { return i.page.Size() }
func (i booleanOffsetIndex) FirstRowIndex(int) int64      { return 0 }

type int32OffsetIndex struct{ page *int32Page }

func (i int32OffsetIndex) NumPages() int                { return 1 }
func (i int32OffsetIndex) Offset(int) int64             { return 0 }
func (i int32OffsetIndex) CompressedPageSize(int) int64 { return i.page.Size() }
func (i int32OffsetIndex) FirstRowIndex(int) int64      { return 0 }

type int64OffsetIndex struct{ page *int64Page }

func (i int64OffsetIndex) NumPages() int                { return 1 }
func (i int64OffsetIndex) Offset(int) int64             { return 0 }
func (i int64OffsetIndex) CompressedPageSize(int) int64 { return i.page.Size() }
func (i int64OffsetIndex) FirstRowIndex(int) int64      { return 0 }

type int96OffsetIndex struct{ page *int96Page }

func (i int96OffsetIndex) NumPages() int                { return 1 }
func (i int96OffsetIndex) Offset(int) int64             { return 0 }
func (i int96OffsetIndex) CompressedPageSize(int) int64 { return i.page.Size() }
func (i int96OffsetIndex) FirstRowIndex(int) int64      { return 0 }

type floatOffsetIndex struct{ page *floatPage }

func (i floatOffsetIndex) NumPages() int                { return 1 }
func (i floatOffsetIndex) Offset(int) int64             { return 0 }
func (i floatOffsetIndex) CompressedPageSize(int) int64 { return i.page.Size() }
func (i floatOffsetIndex) FirstRowIndex(int) int64      { return 0 }

type doubleOffsetIndex struct{ page *doublePage }

func (i doubleOffsetIndex) NumPages() int                { return 1 }
func (i doubleOffsetIndex) Offset(int) int64             { return 0 }
func (i doubleOffsetIndex) CompressedPageSize(int) int64 { return i.page.Size() }
func (i doubleOffsetIndex) FirstRowIndex(int) int64      { return 0 }

type byteArrayOffsetIndex struct{ page *byteArrayPage }

func (i byteArrayOffsetIndex) NumPages() int                { return 1 }
func (i byteArrayOffsetIndex) Offset(int) int64             { return 0 }
func (i byteArrayOffsetIndex) CompressedPageSize(int) int64 { return i.page.Size() }
func (i byteArrayOffsetIndex) FirstRowIndex(int) int64      { return 0 }

type fixedLenByteArrayOffsetIndex struct{ page *fixedLenByteArrayPage }

func (i fixedLenByteArrayOffsetIndex) NumPages() int                { return 1 }
func (i fixedLenByteArrayOffsetIndex) Offset(int) int64             { return 0 }
func (i fixedLenByteArrayOffsetIndex) CompressedPageSize(int) int64 { return i.page.Size() }
func (i fixedLenByteArrayOffsetIndex) FirstRowIndex(int) int64      { return 0 }

type uint32OffsetIndex struct{ page *uint32Page }

func (i uint32OffsetIndex) NumPages() int                { return 1 }
func (i uint32OffsetIndex) Offset(int) int64             { return 0 }
func (i uint32OffsetIndex) CompressedPageSize(int) int64 { return i.page.Size() }
func (i uint32OffsetIndex) FirstRowIndex(int) int64      { return 0 }

type uint64OffsetIndex struct{ page *uint64Page }

func (i uint64OffsetIndex) NumPages() int                { return 1 }
func (i uint64OffsetIndex) Offset(int) int64             { return 0 }
func (i uint64OffsetIndex) CompressedPageSize(int) int64 { return i.page.Size() }
func (i uint64OffsetIndex) FirstRowIndex(int) int64      { return 0 }

type be128OffsetIndex struct{ page *be128Page }

func (i be128OffsetIndex) NumPages() int                { return 1 }
func (i be128OffsetIndex) Offset(int) int64             { return 0 }
func (i be128OffsetIndex) CompressedPageSize(int) int64 { return i.page.Size() }
func (i be128OffsetIndex) FirstRowIndex(int) int64      { return 0 }
