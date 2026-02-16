package columnartest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
)

// Scalar returns a scalar value representing the given kind.
//
// value must either be nil (meaning a null scalar value) or a type which can
// directly be converted to kind:
//
//   - nil for [columnar.KindNull]
//   - bool for [columnar.KindBool]
//   - int64 for [columnar.KindInt64]
//   - uint64 for [columnar.KindUint64]
//   - string or a byte slice for [columnar.KindUTF8]
//
// If value cannot be represented as the kind provided, Scalar fails t.
func Scalar(t testing.TB, kind columnar.Kind, value any) columnar.Scalar {
	t.Helper()

	switch kind {
	case columnar.KindNull:
		return scalarNull(t, value)
	case columnar.KindBool:
		return scalarBool(t, value)
	case columnar.KindInt64:
		return scalarNumber[int64](t, value)
	case columnar.KindUint64:
		return scalarNumber[uint64](t, value)
	case columnar.KindUTF8:
		return scalarUTF8(t, value)
	default:
		require.FailNow(t, "unsupported kind", "kind %s is currently not supported", kind)
		panic("unreachable")
	}
}

func scalarNull(t testing.TB, value any) *columnar.NullScalar {
	t.Helper()

	require.Nil(t, value, "value must be nil for null scalar")
	return &columnar.NullScalar{}
}

func scalarBool(t testing.TB, value any) *columnar.BoolScalar {
	t.Helper()

	if value == nil {
		return &columnar.BoolScalar{Null: true}
	}

	require.IsType(t, false, value, "value must be nil or bool for bool scalar")
	return &columnar.BoolScalar{Value: value.(bool)}
}

func scalarNumber[T columnar.Numeric](t testing.TB, value any) *columnar.NumberScalar[T] {
	t.Helper()

	if value == nil {
		return &columnar.NumberScalar[T]{Null: true}
	}

	switch value := value.(type) {
	case int:
		return &columnar.NumberScalar[T]{Value: T(value)}
	case T:
		return &columnar.NumberScalar[T]{Value: value}
	}

	var zero T
	require.FailNow(t, "unexpected value type", "value must be nil, %T, or int for %T scalar", zero, zero)
	panic("unreachable")
}

func scalarUTF8(t testing.TB, value any) *columnar.UTF8Scalar {
	t.Helper()

	if value == nil {
		return &columnar.UTF8Scalar{Null: true}
	}

	switch value := value.(type) {
	case []byte:
		return &columnar.UTF8Scalar{Value: value}
	case string:
		return &columnar.UTF8Scalar{Value: []byte(value)}
	}

	require.FailNow(t, "unexpected value type", "value must be nil, []byte, or string for UTF8 scalar, got %T", value)
	panic("unreachable")
}
