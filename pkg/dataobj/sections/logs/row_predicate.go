package logs

import (
	"time"
)

// RowPredicate is an expression used to filter rows in a data object.
type RowPredicate interface {
	isRowPredicate()
}

// Supported predicates.
type (
	// An AndRowPredicate is a RowPredicate which requires both its Left and
	// Right predicate to be true.
	AndRowPredicate struct{ Left, Right RowPredicate }

	// An OrRowPredicate is a RowPredicate which requires either its Left or
	// Right predicate to be true.
	OrRowPredicate struct{ Left, Right RowPredicate }

	// A NotRowPredicate is a RowPredicate which requires its Inner predicate to
	// be false.
	NotRowPredicate struct{ Inner RowPredicate }

	// A TimeRangeRowPredicate is a RowPredicate which requires the timestamp of
	// the entry to be within the range of StartTime and EndTime.
	TimeRangeRowPredicate struct {
		StartTime, EndTime time.Time
		IncludeStart       bool // Whether StartTime is inclusive.
		IncludeEnd         bool // Whether EndTime is inclusive.
	}

	// A LogMessageFilterRowPredicate is a RowPredicate that requires the log
	// message of the entry to pass a Keep function.
	LogMessageFilterRowPredicate struct {
		// Keep receives a nil line when the entry has no message value. The slice
		// belongs to the reader and is valid only during the call, so Keep must not
		// retain, modify, or append to it. Reads of one query can call the same Keep
		// at the same time, so it must be safe for concurrent use.
		Keep func(line []byte) bool

		// KeepBatch is optional. It tests a batch of lines at once, which saves a Keep call and a
		// line copy for each row. The reader calls Keep when it cannot use KeepBatch, so Keep must
		// always be set.
		//
		// Line i is data[offsets[i]:offsets[i+1]]. An entry with no message is an empty line, and
		// Keep gets a nil line for it, so Keep must treat a nil and an empty line alike. Lines have
		// no separator, offsets can start after 0, and data can hold bytes that belong to no line.
		//
		// KeepBatch appends to dstIndexes the index of every line that Keep keeps, in ascending
		// order with no repeat, and returns the result. It must give the same result as Keep on
		// every line.
		//
		// The arguments belong to the reader and are valid only during the call. KeepBatch must
		// not retain data, offsets or dstIndexes, and must not modify data or offsets. Like Keep,
		// it must be safe for concurrent use.
		KeepBatch func(data []byte, offsets []int32, dstIndexes []int32) []int32
	}

	// A MetadataMatcherRowPredicate is a RowPredicate that requires a metadata
	// key named Key to exist with a value of Value.
	MetadataMatcherRowPredicate struct{ Key, Value string }

	// A MetadataFilterRowPredicate is a RowPredicate that requires that metadata
	// with the provided key pass a Keep function.
	//
	// The key is provided to the keep function to allow the same function to be
	// used for multiple filter predicates.
	//
	// Uses of MetadataFilterRowPredicate are not eligible for page filtering and
	// should only be used when a condition cannot be expressed by other basic
	// predicates.
	MetadataFilterRowPredicate struct {
		Key  string
		Keep func(key, value string) bool
	}
)

func (AndRowPredicate) isRowPredicate()              {}
func (OrRowPredicate) isRowPredicate()               {}
func (NotRowPredicate) isRowPredicate()              {}
func (TimeRangeRowPredicate) isRowPredicate()        {}
func (MetadataMatcherRowPredicate) isRowPredicate()  {}
func (MetadataFilterRowPredicate) isRowPredicate()   {}
func (LogMessageFilterRowPredicate) isRowPredicate() {}
