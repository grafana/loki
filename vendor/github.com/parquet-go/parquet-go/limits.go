package parquet

import (
	"fmt"
	"math"
)

const (
	// MaxColumnDepth is the maximum column depth supported by this package.
	MaxColumnDepth = math.MaxUint8

	// MaxColumnIndex is the maximum column index supported by this package.
	MaxColumnIndex = math.MaxUint16 - 1

	// MaxRepetitionLevel is the maximum repetition level supported by this
	// package.
	MaxRepetitionLevel = math.MaxUint8

	// MaxDefinitionLevel is the maximum definition level supported by this
	// package.
	MaxDefinitionLevel = math.MaxUint8

	// MaxRowGroups is the maximum number of row groups which can be contained
	// in a single parquet file.
	//
	// This limit is enforced by the use of 16 bits signed integers in the file
	// metadata footer of parquet files. It is part of the parquet specification
	// and therefore cannot be changed.
	MaxRowGroups = math.MaxInt16
)

const (
	estimatedSizeOfByteArrayValues = 20
)

// The make* functions below use a single unsigned comparison to bounds-check the
// index (uint(i) > max catches both i < 0 and i > max), and delegate the failure
// to a dedicated out-of-line panic helper. This keeps their inline cost low: the
// previous implementation inlined fmt.Errorf on the failure path, which inflated
// every caller (Value.Level cost 192) and prevented inlining. Keeping the error
// construction out of line lets these helpers inline and lets small callers such
// as Value.WithRepetitionLevel dead-code-eliminate the check when the argument is
// provably in range.

func makeRepetitionLevel(i int) byte {
	if uint(i) > MaxRepetitionLevel {
		panicRepetitionLevelOutOfRange(i)
	}
	return byte(i)
}

func makeDefinitionLevel(i int) byte {
	if uint(i) > MaxDefinitionLevel {
		panicDefinitionLevelOutOfRange(i)
	}
	return byte(i)
}

func makeColumnIndex(i int) uint16 {
	if uint(i) > MaxColumnIndex {
		panicColumnIndexOutOfRange(i)
	}
	return uint16(i)
}

func makeNumValues(i int) int32 {
	if uint(i) > math.MaxInt32 {
		panicNumValuesOutOfRange(i)
	}
	return int32(i)
}

// The panic helpers are kept out of line (along with the error construction) so
// the make* functions above stay cheap to inline.

func panicRepetitionLevelOutOfRange(i int) {
	panic(errIndexOutOfRange("repetition level", i, 0, MaxRepetitionLevel))
}

func panicDefinitionLevelOutOfRange(i int) {
	panic(errIndexOutOfRange("definition level", i, 0, MaxDefinitionLevel))
}

func panicColumnIndexOutOfRange(i int) {
	panic(errIndexOutOfRange("column index", i, 0, MaxColumnIndex))
}

func panicNumValuesOutOfRange(i int) {
	panic(errIndexOutOfRange("number of values", i, 0, math.MaxInt32))
}

// panicLevelOutOfRange performs the fine-grained range check for Value.Level's
// combined fast-path test. It is only called when at least one of the three
// arguments is out of range, so it can afford to recompute which one to produce
// a precise error message. Keeping it out of line lets Value.Level reduce its
// hot path to a single combined comparison and stay cheap enough to inline.
func panicLevelOutOfRange(repetitionLevel, definitionLevel, columnIndex int) {
	if uint(repetitionLevel) > MaxRepetitionLevel {
		panicRepetitionLevelOutOfRange(repetitionLevel)
	}
	if uint(definitionLevel) > MaxDefinitionLevel {
		panicDefinitionLevelOutOfRange(definitionLevel)
	}
	panicColumnIndexOutOfRange(columnIndex)
}

func errIndexOutOfRange(typ string, i, min, max int) error {
	return fmt.Errorf("%s out of range: %d not in [%d:%d]", typ, i, min, max)
}
