package parquet

import (
	"fmt"
	"math"
)

const (
	// MaxColumnDepth is the maximum column depth supported by this package.
	MaxColumnDepth = math.MaxInt8

	// MaxColumnIndex is the maximum column index supported by this package.
	MaxColumnIndex = math.MaxInt16

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

func makeRepetitionLevel(i int) byte {
	checkIndexRange("repetition level", i, 0, MaxRepetitionLevel)
	return byte(i)
}

func makeDefinitionLevel(i int) byte {
	checkIndexRange("definition level", i, 0, MaxDefinitionLevel)
	return byte(i)
}

func makeColumnIndex(i int) int16 {
	checkIndexRange("column index", i, 0, MaxColumnIndex)
	return int16(i)
}

func makeNumValues(i int) int32 {
	checkIndexRange("number of values", i, 0, math.MaxInt32)
	return int32(i)
}

func checkIndexRange(typ string, i, min, max int) {
	if i < min || i > max {
		panic(errIndexOutOfRange(typ, i, min, max))
	}
}

func errIndexOutOfRange(typ string, i, min, max int) error {
	return fmt.Errorf("%s out of range: %d not in [%d:%d]", typ, i, min, max)
}
