package compute

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow/bitutil"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Not negates the input boolean datum. Not returns an error if the input kind
// is not a boolean.
//
// Special cases:
//
//   - The negation of null is null.
func Not(alloc *memory.Allocator, input columnar.Datum) (columnar.Datum, error) {
	if got, want := input.Kind(), columnar.KindBool; got != want {
		return nil, fmt.Errorf("invalid input kind %s, expected %s", got, want)
	}

	switch input := input.(type) {
	case *columnar.BoolScalar:
		return notScalar(input), nil
	case *columnar.Bool:
		return notArray(alloc, input), nil
	default:
		panic(fmt.Sprintf("unexpected input type %T", input))
	}
}

func notScalar(input *columnar.BoolScalar) *columnar.BoolScalar {
	return &columnar.BoolScalar{
		Value: !input.Value, // garbage data if null
		Null:  input.Null,
	}
}

func notArray(alloc *memory.Allocator, input *columnar.Bool) *columnar.Bool {
	count := input.Len()

	var validity memory.Bitmap
	if input.Nulls() > 0 {
		// Only copy the validity bitmap from input if it has any nulls.
		validity = memory.NewBitmap(alloc, count)
		validity.AppendBitmap(input.Validity())
	}

	valuesBitmap := memory.NewBitmap(alloc, count)
	valuesBitmap.Resize(count)

	inputBitmap := input.Values()

	var (
		inputBytes, inputOffset   = inputBitmap.Bytes()
		valuesBytes, valuesOffset = valuesBitmap.Bytes()
	)

	bitutil.InvertBitmap(inputBytes, inputOffset, count, valuesBytes, valuesOffset)
	return columnar.NewBool(valuesBitmap, validity)
}

// And computes the logical AND of two input boolean datums. And returns an
// error if the input kind of either datum is not a boolean. If both input
// datums are arrays, they must be of the same length.
//
// Special cases:
//
//   - If either side of the AND is null, the result is null.
func And(alloc *memory.Allocator, left, right columnar.Datum) (columnar.Datum, error) {
	return dispatchLogical(alloc, logicalAndKernel, left, right)
}

// Or computes the logical OR of two input boolean datums. Or returns an error
// if the input kind of either datum is not a boolean. If both input datums are
// arrays, they must be of the same length.
//
// Special cases:
//
//   - If either side of the OR is null, the result is null.
func Or(alloc *memory.Allocator, left, right columnar.Datum) (columnar.Datum, error) {
	return dispatchLogical(alloc, logicalOrKernel, left, right)
}

func dispatchLogical(alloc *memory.Allocator, kernel logicalKernel, left, right columnar.Datum) (columnar.Datum, error) {
	if got, want := left.Kind(), columnar.KindBool; got != want {
		return nil, fmt.Errorf("invalid input kind %s, expected %s", got, want)
	} else if left.Kind() != right.Kind() {
		return nil, fmt.Errorf("both inputs must be %s, got %s and %s", columnar.KindBool, left.Kind(), right.Kind())
	}

	_, leftScalar := left.(columnar.Scalar)
	_, rightScalar := right.(columnar.Scalar)

	switch {
	case leftScalar && rightScalar:
		return logicalSS(kernel, left.(*columnar.BoolScalar), right.(*columnar.BoolScalar)), nil
	case leftScalar && !rightScalar:
		return logicalSA(alloc, kernel, left.(*columnar.BoolScalar), right.(*columnar.Bool)), nil
	case !leftScalar && rightScalar:
		return logicalAS(alloc, kernel, left.(*columnar.Bool), right.(*columnar.BoolScalar)), nil
	case !leftScalar && !rightScalar:
		return logicalAA(alloc, kernel, left.(*columnar.Bool), right.(*columnar.Bool))
	}

	panic("unreachable")
}

func logicalSS(kernel logicalKernel, left, right *columnar.BoolScalar) *columnar.BoolScalar {
	return &columnar.BoolScalar{
		Value: kernel.DoSS(left.Value, right.Value),
		Null:  !computeValiditySS(left.Null, right.Null),
	}
}

func logicalSA(alloc *memory.Allocator, kernel logicalKernel, left *columnar.BoolScalar, right *columnar.Bool) *columnar.Bool {
	validity := computeValiditySA(alloc, left.Null, right.Validity())

	if left.Null {
		// When left is null, the result is all nulls (set to the length of
		// right).
		values := memory.NewBitmap(alloc, right.Len())
		values.AppendCount(false, right.Len()) // Append all false to avoid garbage data in results.

		return columnar.NewBool(values, validity)
	}

	values := memory.NewBitmap(alloc, right.Len())
	kernel.DoSA(&values, left.Value, right.Values())

	return columnar.NewBool(values, validity)
}

func logicalAS(alloc *memory.Allocator, kernel logicalKernel, left *columnar.Bool, right *columnar.BoolScalar) *columnar.Bool {
	validity := computeValidityAS(alloc, left.Validity(), right.Null)

	if right.Null {
		// When right is null, the result is all nulls (set to the length of
		// left).
		values := memory.NewBitmap(alloc, left.Len())
		values.AppendCount(false, left.Len()) // Append all false to avoid garbage data in results.

		return columnar.NewBool(values, validity)
	}

	values := memory.NewBitmap(alloc, left.Len())
	kernel.DoAS(&values, left.Values(), right.Value)

	return columnar.NewBool(values, validity)
}

func logicalAA(alloc *memory.Allocator, kernel logicalKernel, left, right *columnar.Bool) (*columnar.Bool, error) {
	if left.Len() != right.Len() {
		return nil, fmt.Errorf("array length mismatch: %d != %d", left.Len(), right.Len())
	}

	validity, err := computeValidityAA(alloc, left.Validity(), right.Validity())
	if err != nil {
		return nil, err
	}

	values := memory.NewBitmap(alloc, left.Len())
	kernel.DoAA(&values, left.Values(), right.Values())

	return columnar.NewBool(values, validity), nil
}
