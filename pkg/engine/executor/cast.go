package executor

import (
	"strconv"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

type builderInterface interface {
	AppendNull()
	NewArray() arrow.Array
	Release()
}

// castStringToNumeric is a generic function that casts string arrays to numeric types
func castStringToNumeric[T any, B builderInterface](
	stringCol arrow.Array,
	mem memory.Allocator,
	createBuilder func(memory.Allocator) B,
	parseValue func(string) (T, error),
	appendValue func(B, T),
) (arrow.Array, error) {
	// Type assert to string array
	stringArray, ok := stringCol.(*array.String)
	if !ok {
		// If it's not a string array, return an array of NULLs
		builder := createBuilder(mem)
		defer builder.Release()

		for i := 0; i < stringCol.Len(); i++ {
			builder.AppendNull()
		}
		return builder.NewArray(), nil
	}

	// Create the builder
	builder := createBuilder(mem)
	defer builder.Release()

	// Process each string value
	for i := 0; i < stringArray.Len(); i++ {
		if !stringArray.IsValid(i) {
			// Input is NULL, output is NULL
			builder.AppendNull()
			continue
		}

		// Get the string value
		strVal := stringArray.Value(i)

		// Try to parse the value
		val, err := parseValue(strVal)
		if err != nil {
			// Cannot parse, append NULL
			builder.AppendNull()
		} else {
			// Successfully parsed, append the value
			appendValue(builder, val)
		}
	}

	return builder.NewArray(), nil
}

// CastStringToInt64 casts a string arrow array to int64, converting numeric strings
// to their integer values and non-numeric strings to NULL.
func CastStringToInt64(stringCol arrow.Array, mem memory.Allocator) (arrow.Array, error) {
	return castStringToNumeric(
		stringCol,
		mem,
		array.NewInt64Builder,
		func(s string) (int64, error) {
			return strconv.ParseInt(s, 10, 64)
		},
		func(b *array.Int64Builder, v int64) {
			b.Append(v)
		},
	)
}

// CastStringToFloat64 casts a string arrow array to float64, converting numeric strings
// to their float values and non-numeric strings to NULL.
// This handles special values like NaN, Inf, -Inf automatically.
func CastStringToFloat64(stringCol arrow.Array, mem memory.Allocator) (arrow.Array, error) {
	return castStringToNumeric(
		stringCol,
		mem,
		array.NewFloat64Builder,
		func(s string) (float64, error) {
			return strconv.ParseFloat(s, 64)
		},
		func(b *array.Float64Builder, v float64) {
			b.Append(v)
		},
	)
}
