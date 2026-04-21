package columnartest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/memory"
)

// RequireDatumsEqual asserts that the provided datum matches the expected
// datum, otherwise t fails.
//
// When mask is non-empty, only positions where the mask bit is set are
// compared. This is useful when some positions have undefined values (e.g.,
// unselected rows in a selection vector).
func RequireDatumsEqual(t testing.TB, expect, actual columnar.Datum, mask memory.Bitmap) {
	t.Helper()

	if expectScalar, ok := expect.(columnar.Scalar); ok {
		require.Implements(t, (*columnar.Scalar)(nil), actual)
		RequireScalarsEqual(t, expectScalar, actual.(columnar.Scalar))
	} else {
		require.Implements(t, (*columnar.Array)(nil), actual)
		RequireArraysEqual(t, expect.(columnar.Array), actual.(columnar.Array), mask)
	}
}

// RequireScalarsEqual asserts that the provided scalar matches the expected
// scalar, otherwise t fails.
func RequireScalarsEqual(t testing.TB, expect, actual columnar.Scalar) {
	t.Helper()

	require.Equal(t, expect.Kind(), actual.Kind(), "kind mismatch")
	require.Equal(t, expect.IsNull(), actual.IsNull(), "null mismatch")

	if expect.IsNull() {
		// We don't want to compare values when expect is null, as the Value
		// field is undefined when the scalar is null.
		return
	}

	require.Equal(t, expect, actual)
}

// RequireArraysEqual asserts that the provided array matches the expected
// array, otherwise t fails.
//
// When mask is non-empty, only positions where the mask bit is set are
// compared.
func RequireArraysEqual(t testing.TB, expect, actual columnar.Array, mask memory.Bitmap) {
	t.Helper()

	require.Equal(t, expect.Kind(), actual.Kind(), "kind mismatch")
	require.Equal(t, expect.Len(), actual.Len(), "length mismatch")

	switch expect.Kind() {
	case types.KindNull:
		requireNullArraysEqual(t, expect.(*columnar.Null), actual.(*columnar.Null), mask)
	case types.KindBool:
		requireArraysEqual(t, expect.(*columnar.Bool), actual.(*columnar.Bool), mask)
	case types.KindInt32:
		requireArraysEqual(t, expect.(*columnar.Number[int32]), actual.(*columnar.Number[int32]), mask)
	case types.KindInt64:
		requireArraysEqual(t, expect.(*columnar.Number[int64]), actual.(*columnar.Number[int64]), mask)
	case types.KindUint32:
		requireArraysEqual(t, expect.(*columnar.Number[uint32]), actual.(*columnar.Number[uint32]), mask)
	case types.KindUint64:
		requireArraysEqual(t, expect.(*columnar.Number[uint64]), actual.(*columnar.Number[uint64]), mask)
	case types.KindUTF8:
		requireArraysEqual(t, expect.(*columnar.UTF8), actual.(*columnar.UTF8), mask)
	case types.KindStruct:
		requireStructArraysEqual(t, expect.(*columnar.Struct), actual.(*columnar.Struct), mask)
	}
}

func requireStructArraysEqual(t testing.TB, expect, actual *columnar.Struct, mask memory.Bitmap) {
	t.Helper()

	require.Equal(t, expect.NumFields(), actual.NumFields(), "field count mismatch")

	for i := range expect.NumFields() {
		RequireArraysEqual(t, expect.Field(i), actual.Field(i), mask)
	}

	for i := range expect.Len() {
		if mask.Len() > 0 && !mask.Get(i) {
			continue
		}
		require.Equal(t, expect.IsNull(i), actual.IsNull(i), "struct null mismatch at index %d", i)
	}
}

func requireNullArraysEqual(t testing.TB, left, right *columnar.Null, mask memory.Bitmap) {
	// Nothing to do here; the base checks in RequireArraysEqual covers
	// everything that could differ between two null arrays.
	t.Helper()
}

// valueArray is a generic representation of an Array that supports a Get(int)
// method to get a specific value at the given index.
type valueArray[T any] interface {
	Len() int
	IsNull(i int) bool
	Get(i int) T
}

func requireArraysEqual[T any](t testing.TB, left, right valueArray[T], mask memory.Bitmap) {
	t.Helper()

	for i := range left.Len() {
		if mask.Len() > 0 && !mask.Get(i) {
			continue // Skip positions not in the mask.
		}
		require.Equal(t, left.IsNull(i), right.IsNull(i), "null mismatch at index %d", i)
		if left.IsNull(i) || right.IsNull(i) {
			continue
		}
		require.Equal(t, left.Get(i), right.Get(i), "value mismatch at index %d", i)
	}
}
