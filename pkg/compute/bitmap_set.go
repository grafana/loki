package compute

import (
	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// UnionBitmaps returns a new bitmap holding the bits set in a or b. A nil
// operand is treated as empty. The shorter operand is zero-extended to the
// longer before the union, since the underlying boolean kernels require
// equal-length operands.
//
// The result uses the nil (GC-managed) allocator: callers accumulate unions
// that escape into long-lived maps, so the result must not be backed by a
// reclaimable allocator region.
func UnionBitmaps(a, b *memory.Bitmap) *memory.Bitmap {
	return combineBitmaps(Or, a, b)
}

// IntersectBitmaps returns a new bitmap holding the bits set in both a and b.
// Nil and length handling match [UnionBitmaps].
func IntersectBitmaps(a, b *memory.Bitmap) *memory.Bitmap {
	return combineBitmaps(And, a, b)
}

// DifferenceBitmaps returns a new bitmap holding the bits set in a but not in b
// (a AND NOT b). Nil and length handling match [UnionBitmaps].
func DifferenceBitmaps(a, b *memory.Bitmap) *memory.Bitmap {
	left, right := alignBitmaps(a, b)
	negated, err := Not(nil, columnar.NewBool(right, memory.Bitmap{}), memory.Bitmap{})
	if err != nil {
		panic("compute.DifferenceBitmaps: " + err.Error())
	}
	result, err := And(nil, columnar.NewBool(left, memory.Bitmap{}), negated, memory.Bitmap{})
	if err != nil {
		panic("compute.DifferenceBitmaps: " + err.Error())
	}
	out := result.(*columnar.Bool).Values()
	return &out
}

func combineBitmaps(op func(*memory.Allocator, columnar.Datum, columnar.Datum, memory.Bitmap) (columnar.Datum, error), a, b *memory.Bitmap) *memory.Bitmap {
	left, right := alignBitmaps(a, b)
	result, err := op(nil, columnar.NewBool(left, memory.Bitmap{}), columnar.NewBool(right, memory.Bitmap{}), memory.Bitmap{})
	if err != nil {
		panic("compute.combineBitmaps: " + err.Error())
	}
	out := result.(*columnar.Bool).Values()
	return &out
}

// alignBitmaps returns a and b as values, zero-extending the shorter to match
// the longer. A nil bitmap becomes an empty one.
func alignBitmaps(a, b *memory.Bitmap) (memory.Bitmap, memory.Bitmap) {
	var left, right memory.Bitmap
	if a != nil {
		left = *a
	}
	if b != nil {
		right = *b
	}
	if left.Len() == right.Len() {
		return left, right
	}
	n := max(left.Len(), right.Len())
	return extendBitmap(left, n), extendBitmap(right, n)
}

// extendBitmap returns a length-n copy of b with the trailing bits cleared,
// leaving b unmodified.
func extendBitmap(b memory.Bitmap, n int) memory.Bitmap {
	out := memory.NewBitmap(nil, n)
	out.AppendBitmap(b)
	out.AppendCount(false, n-b.Len())
	return out
}
