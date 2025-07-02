// Package arrowtest provides utilities for testing Arrow records.
package arrowtest

import (
	"encoding/base64"
	"time"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

type (
	// Rows is a slice of [Row].
	Rows []Row

	// Row represents a single record row as a map of column name to value.
	Row map[string]any
)

// RecordRows converts an [arrow.Record] into [Rows] for comparison in tests.
// RecordRows requires all columns in the record to have a unique name.
//
// Most values are converted to their Go equivalents, with the exception of
// timestamps, which are converted to strings using [Time].
//
// Callers building expected [Rows] must use the same functions.
func RecordRows(rec arrow.Record) (Rows, error) {
	rows := make(Rows, rec.NumRows())

	for i := range int(rec.NumRows()) {
		row := make(Row, rec.NumCols())
		for j := range int(rec.NumCols()) {
			row[rec.Schema().Field(j).Name] = rec.Column(j).GetOneForMarshal(i)
		}

		rows[i] = row
	}

	return rows, nil
}

// TableRows concatenates all chunks of the [arrow.Table] into a single
// [arrow.Record], and then returns it as [Rows]. TableRows requires all
// columns in the table to have a unique name.
//
// See [RecordRows] for specifies on how values are converted into Go values
// for a [Row].
func TableRows(alloc memory.Allocator, table arrow.Table) (Rows, error) {
	rec, err := mergeTable(alloc, table)
	if err != nil {
		return nil, err
	}
	defer rec.Release()

	return RecordRows(rec)
}

// mergeTable merges all chunks in an [arrow.Table] into a single
// [arrow.Record].
func mergeTable(alloc memory.Allocator, table arrow.Table) (arrow.Record, error) {
	recordColumns := make([]arrow.Array, table.NumCols())

	for i := range int(table.NumCols()) {
		column, err := array.Concatenate(table.Column(i).Data().Chunks(), alloc)
		if err != nil {
			return nil, err
		}
		defer column.Release()
		recordColumns[i] = column
	}

	return array.NewRecord(table.Schema(), recordColumns, table.NumRows()), nil
}

// Base64 encodes the given string as base64.
func Base64(s string) string {
	rawString := unsafe.Slice(unsafe.StringData(s), len(s))
	return base64.StdEncoding.EncodeToString(rawString)
}

// Time returns a string representation of t in the format emitted by
// [TableRows].
//
// Callers must configure t with the same timezone and precision used by the
// Arrow column.
func Time(t time.Time) string {
	// This is the format used by [array.Timestamp.ValueStr]. Arrow will
	// automatically truncate the timestamp before formatting it, but we bypass
	// that here to make it the caller's responsibility instead.
	return t.Format("2006-01-02 15:04:05.999999999Z0700")
}
