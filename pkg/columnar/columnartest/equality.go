package columnartest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
)

// RequireDatumsEqual asserts that the provided datum matches the expected
// datum, otherwise t fails.
func RequireDatumsEqual(t testing.TB, expect, actual columnar.Datum) {
	t.Helper()

	if expectScalar, ok := expect.(columnar.Scalar); ok {
		require.Implements(t, (*columnar.Scalar)(nil), actual)
		RequireScalarsEqual(t, expectScalar, actual.(columnar.Scalar))
	} else {
		require.Implements(t, (*columnar.Array)(nil), actual)
		RequireArraysEqual(t, expect.(columnar.Array), actual.(columnar.Array))
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
func RequireArraysEqual(t testing.TB, expect, actual columnar.Array) {
	t.Helper()

	require.Equal(t, expect.Kind(), actual.Kind(), "kind mismatch")
	require.Equal(t, expect.Len(), actual.Len(), "length mismatch")
	require.Equal(t, expect.Nulls(), actual.Nulls(), "null count mismatch")

	switch expect.Kind() {
	case columnar.KindNull:
		requireNullArraysEqual(t, expect.(*columnar.Null), actual.(*columnar.Null))
	case columnar.KindBool:
		requireArraysEqual(t, expect.(*columnar.Bool), actual.(*columnar.Bool))
	case columnar.KindInt64:
		requireArraysEqual(t, expect.(*columnar.Number[int64]), actual.(*columnar.Number[int64]))
	case columnar.KindUint64:
		requireArraysEqual(t, expect.(*columnar.Number[uint64]), actual.(*columnar.Number[uint64]))
	case columnar.KindUTF8:
		requireArraysEqual(t, expect.(*columnar.UTF8), actual.(*columnar.UTF8))
	}
}

func requireNullArraysEqual(t testing.TB, left, right *columnar.Null) {
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

func requireArraysEqual[T any](t testing.TB, left, right valueArray[T]) {
	t.Helper()

	for i := range left.Len() {
		require.Equal(t, left.IsNull(i), right.IsNull(i), "null mismatch at index %d", i)
		if left.IsNull(i) || right.IsNull(i) {
			continue
		}
		require.Equal(t, left.Get(i), right.Get(i), "value mismatch at index %d", i)
	}
}
