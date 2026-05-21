package columnartest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/types"
)

// Scalar returns a scalar value representing the given kind.
//
// value must either be nil (meaning a null scalar value) or a type which can
// directly be converted to kind:
//
//   - nil for [types.KindNull]
//   - bool for [types.KindBool]
//   - int32 for [types.KindInt32]
//   - int64 for [types.KindInt64]
//   - uint32 for [types.KindUint32]
//   - uint64 for [types.KindUint64]
//   - string or a byte slice for [types.KindUTF8]
//
// If value cannot be represented as the kind provided, Scalar fails t.
func Scalar(t testing.TB, kind types.Kind, value any) columnar.Scalar {
	t.Helper()

	switch kind {
	case types.KindNull:
		return scalarNull(t, value)
	case types.KindBool:
		return scalarBool(t, value)
	case types.KindInt32:
		return scalarNumber[int32](t, value)
	case types.KindInt64:
		return scalarNumber[int64](t, value)
	case types.KindUint32:
		return scalarNumber[uint32](t, value)
	case types.KindUint64:
		return scalarNumber[uint64](t, value)
	case types.KindUTF8:
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
