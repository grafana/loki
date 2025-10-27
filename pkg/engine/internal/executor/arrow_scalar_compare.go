package executor

import (
	"bytes"
	"cmp"
	"errors"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/scalar"
)

// compareScalars compares two rows from the given arrays and indices, returning:
//
// - -1 if left < right
// - 0 if left == right
// - 1 if left > right
//
// If nullsFirst is true, then null values are considered to sort before
// non-null values.
//
// compareScalars returns an error if the two scalars are of different types,
// or if the scalar type is not supported for comparison.
func compareScalars(left, right scalar.Scalar, nullsFirst bool) (int, error) {
	leftNull := left == nil || !left.IsValid()
	rightNull := right == nil || !right.IsValid()

	// First, handle one or both of the scalars being null.
	switch {
	case leftNull && rightNull:
		return 0, nil

	case leftNull && !rightNull: // left < right if b.NullsFirst is true
		if nullsFirst {
			return -1, nil
		}
		return 1, nil

	case !leftNull && rightNull: // left > right if b.NullsFirst is true
		if nullsFirst {
			return 1, nil
		}
		return -1, nil
	}

	if !arrow.TypeEqual(left.DataType(), right.DataType()) {
		// We should never hit this, since compareRow is only called for two arrays
		// coming from the same [arrow.Field].
		return 0, errors.New("received scalars of different types")
	}

	// Fast-path: check the builtin support for scalar equality.
	if scalar.Equals(left, right) {
		return 0, nil
	}

	// Switch on the scalar type to compare the values. This is only composed of
	// types we know the query engine uses, and types that we know have clear
	// sorting semantics.
	//
	// Unsupported scalar types are treated as equal for consistent sorting, but
	// otherwise it's up to the caller to detect unexpected sort types and reject
	// the query.
	switch left.(type) {
	case *scalar.Binary:
		left, right := left.(*scalar.Binary), right.(*scalar.Binary)
		return bytes.Compare(left.Data(), right.Data()), nil

	case *scalar.Dictionary:
		left, right := left.(*scalar.Dictionary), right.(*scalar.Dictionary)

		leftValue, _ := left.GetEncodedValue()
		rightValue, _ := right.GetEncodedValue()

		return compareScalars(leftValue, rightValue, nullsFirst)

	case *scalar.Duration:
		left, right := left.(*scalar.Duration), right.(*scalar.Duration)
		return cmp.Compare(left.Value, right.Value), nil

	case *scalar.Float16:
		left, right := left.(*scalar.Float16), right.(*scalar.Float16)
		return left.Value.Cmp(right.Value), nil

	case *scalar.Float32:
		left, right := left.(*scalar.Float32), right.(*scalar.Float32)
		return cmp.Compare(left.Value, right.Value), nil

	case *scalar.Float64:
		left, right := left.(*scalar.Float64), right.(*scalar.Float64)
		return cmp.Compare(left.Value, right.Value), nil

	case *scalar.Int8:
		left, right := left.(*scalar.Int8), right.(*scalar.Int8)
		return cmp.Compare(left.Value, right.Value), nil

	case *scalar.Int16:
		left, right := left.(*scalar.Int16), right.(*scalar.Int16)
		return cmp.Compare(left.Value, right.Value), nil

	case *scalar.Int32:
		left, right := left.(*scalar.Int32), right.(*scalar.Int32)
		return cmp.Compare(left.Value, right.Value), nil

	case *scalar.Int64:
		left, right := left.(*scalar.Int64), right.(*scalar.Int64)
		return cmp.Compare(left.Value, right.Value), nil

	case *scalar.RunEndEncoded:
		left, right := left.(*scalar.RunEndEncoded), right.(*scalar.RunEndEncoded)
		return compareScalars(left.Value, right.Value, nullsFirst)

	case *scalar.String:
		left, right := left.(*scalar.String), right.(*scalar.String)
		return bytes.Compare(left.Data(), right.Data()), nil

	case *scalar.Timestamp:
		left, right := left.(*scalar.Timestamp), right.(*scalar.Timestamp)
		return cmp.Compare(left.Value, right.Value), nil

	case *scalar.Uint8:
		left, right := left.(*scalar.Uint8), right.(*scalar.Uint8)
		return cmp.Compare(left.Value, right.Value), nil

	case *scalar.Uint16:
		left, right := left.(*scalar.Uint16), right.(*scalar.Uint16)
		return cmp.Compare(left.Value, right.Value), nil

	case *scalar.Uint32:
		left, right := left.(*scalar.Uint32), right.(*scalar.Uint32)
		return cmp.Compare(left.Value, right.Value), nil

	case *scalar.Uint64:
		left, right := left.(*scalar.Uint64), right.(*scalar.Uint64)
		return cmp.Compare(left.Value, right.Value), nil
	}

	return 0, nil
}
