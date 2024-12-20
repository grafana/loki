package parquet

import (
	"errors"
	"fmt"
)

var (
	// ErrCorrupted is an error returned by the Err method of ColumnPages
	// instances when they encountered a mismatch between the CRC checksum
	// recorded in a page header and the one computed while reading the page
	// data.
	ErrCorrupted = errors.New("corrupted parquet page")

	// ErrMissingRootColumn is an error returned when opening an invalid parquet
	// file which does not have a root column.
	ErrMissingRootColumn = errors.New("parquet file is missing a root column")

	// ErrRowGroupSchemaMissing is an error returned when attempting to write a
	// row group but the source has no schema.
	ErrRowGroupSchemaMissing = errors.New("cannot write rows to a row group which has no schema")

	// ErrRowGroupSchemaMismatch is an error returned when attempting to write a
	// row group but the source and destination schemas differ.
	ErrRowGroupSchemaMismatch = errors.New("cannot write row groups with mismatching schemas")

	// ErrRowGroupSortingColumnsMismatch is an error returned when attempting to
	// write a row group but the sorting columns differ in the source and
	// destination.
	ErrRowGroupSortingColumnsMismatch = errors.New("cannot write row groups with mismatching sorting columns")

	// ErrSeekOutOfRange is an error returned when seeking to a row index which
	// is less than the first row of a page.
	ErrSeekOutOfRange = errors.New("seek to row index out of page range")

	// ErrUnexpectedDictionaryPage is an error returned when a page reader
	// encounters a dictionary page after the first page, or in a column
	// which does not use a dictionary encoding.
	ErrUnexpectedDictionaryPage = errors.New("unexpected dictionary page")

	// ErrMissingPageHeader is an error returned when a page reader encounters
	// a malformed page header which is missing page-type-specific information.
	ErrMissingPageHeader = errors.New("missing page header")

	// ErrUnexpectedRepetitionLevels is an error returned when attempting to
	// decode repetition levels into a page which is not part of a repeated
	// column.
	ErrUnexpectedRepetitionLevels = errors.New("unexpected repetition levels")

	// ErrUnexpectedDefinitionLevels is an error returned when attempting to
	// decode definition levels into a page which is part of a required column.
	ErrUnexpectedDefinitionLevels = errors.New("unexpected definition levels")

	// ErrTooManyRowGroups is returned when attempting to generate a parquet
	// file with more than MaxRowGroups row groups.
	ErrTooManyRowGroups = errors.New("the limit of 32767 row groups has been reached")

	// ErrConversion is used to indicate that a conversion betwen two values
	// cannot be done because there are no rules to translate between their
	// physical types.
	ErrInvalidConversion = errors.New("invalid conversion between parquet values")
)

type errno int

const (
	ok errno = iota
	indexOutOfBounds
)

func (e errno) check() {
	switch e {
	case ok:
	case indexOutOfBounds:
		panic("index out of bounds")
	default:
		panic("BUG: unknown error code")
	}
}

func errRowIndexOutOfBounds(rowIndex, rowCount int64) error {
	return fmt.Errorf("row index out of bounds: %d/%d", rowIndex, rowCount)
}
