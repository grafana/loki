package compute_test

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestEquals(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
		expectError bool
	}{
		{
			name:        "fails on mismatched types",
			left:        columnartest.Scalar(t, columnar.KindInt64, int64(0)),
			right:       columnartest.Scalar(t, columnar.KindUint64, uint64(0)),
			expectError: true,
		},
		{
			name:        "fails on mismatch length arrays",
			left:        columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false),
			right:       columnartest.Array(t, columnar.KindBool, &alloc, true, false),
			expectError: true,
		},

		// Bool (scalar, scalar) tests
		{
			name:   "type=bool/true-scalar == false-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, true),
			right:  columnartest.Scalar(t, columnar.KindBool, false),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=bool/true-scalar == true-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, true),
			right:  columnartest.Scalar(t, columnar.KindBool, true),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=bool/valid-scalar == null-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, true),
			right:  columnartest.Scalar(t, columnar.KindBool, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=bool/null-scalar == valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, false),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=bool/null-scalar == null-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// Bool (scalar, array) tests
		{
			name:   "type=bool/true-scalar == array",
			left:   columnartest.Scalar(t, columnar.KindBool, true),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
		},
		{
			name:   "type=bool/false-scalar == array",
			left:   columnartest.Scalar(t, columnar.KindBool, false),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
		},
		{
			name:   "type=bool/null-scalar == array",
			left:   columnartest.Scalar(t, columnar.KindBool, nil),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Bool (array, scalar) tests
		{
			name:   "type=bool/array == true-scalar",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, true),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
		},
		{
			name:   "type=bool/array == false-scalar",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, false),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
		},
		{
			name:   "type=bool/array == null-scalar",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Bool (array, array) tests
		{
			name:   "type=bool/array == array",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false, nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, false, true, false, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, true, nil, nil, nil),
		},

		// Int64 (scalar, scalar) tests
		{
			name:   "type=int64/equal-scalar == equal-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=int64/scalar == different-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=int64/valid-scalar == null-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=int64/null-scalar == valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=int64/null-scalar == null-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// Int64 (scalar, array) tests
		{
			name:   "type=int64/valid-scalar == array",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
		},
		{
			name:   "type=int64/null-scalar == array",
			left:   columnartest.Scalar(t, columnar.KindInt64, nil),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Int64 (array, scalar) tests
		{
			name:   "type=int64/array == valid-scalar",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
		},
		{
			name:   "type=int64/array == null-scalar",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Int64 (array, array) tests
		{
			name:   "type=int64/array == array",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2), int64(3), int64(4), nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(3), int64(3), int64(5), int64(1), int64(2), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false, nil, nil, nil),
		},

		// Uint64 (scalar, scalar) tests
		{
			name:   "type=uint64/equal-scalar == equal-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=uint64/scalar == different-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=uint64/valid-scalar == null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=uint64/null-scalar == valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=uint64/null-scalar == null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// Uint64 (scalar, array) tests
		{
			name:   "type=uint64/valid-scalar == array",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
		},
		{
			name:   "type=uint64/null-scalar == array",
			left:   columnartest.Scalar(t, columnar.KindUint64, nil),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Uint64 (array, scalar) tests
		{
			name:   "type=uint64/array == valid-scalar",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
		},
		{
			name:   "type=uint64/array == null-scalar",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Uint64 (array, array) tests
		{
			name:   "type=uint64/array == array",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(1), uint64(2), uint64(3), uint64(4), nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(1), uint64(3), uint64(3), uint64(5), uint64(1), uint64(2), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false, nil, nil, nil),
		},

		// UTF8 (scalar, scalar) tests
		{
			name:   "type=utf8/equal-scalar == equal-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "hello"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "hello"),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=utf8/scalar == different-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "hello"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "world"),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=utf8/valid-scalar == null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "hello"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=utf8/null-scalar == valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "world"),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=utf8/null-scalar == null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// UTF8 (scalar, array) tests
		{
			name:   "type=utf8/valid-scalar == array",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "foo"),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
		},
		{
			name:   "type=utf8/null-scalar == array",
			left:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// UTF8 (array, scalar) tests
		{
			name:   "type=utf8/array == valid-scalar",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "bar"),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
		},
		{
			name:   "type=utf8/array == null-scalar",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// UTF8 (array, array) tests
		{
			name:   "type=utf8/array == array",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "a", "b", "c", "d", nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "a", "x", "c", "y", "foo", "bar", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false, nil, nil, nil),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := compute.Equals(&alloc, tc.left, tc.right)
			if tc.expectError {
				require.Error(t, err, "invalid function call should result in an error")
				return
			}

			require.NoError(t, err, "valid function call should not result in an error")
			columnartest.RequireDatumsEqual(t, tc.expect, actual)
		})
	}
}

func TestNotEquals(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
		expectError bool
	}{
		{
			name:        "fails on mismatched types",
			left:        columnartest.Scalar(t, columnar.KindInt64, int64(0)),
			right:       columnartest.Scalar(t, columnar.KindUint64, uint64(0)),
			expectError: true,
		},
		{
			name:        "fails on mismatch length arrays",
			left:        columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false),
			right:       columnartest.Array(t, columnar.KindBool, &alloc, true, false),
			expectError: true,
		},

		// Bool (scalar, scalar) tests
		{
			name:   "type=bool/true-scalar != false-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, true),
			right:  columnartest.Scalar(t, columnar.KindBool, false),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=bool/true-scalar != true-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, true),
			right:  columnartest.Scalar(t, columnar.KindBool, true),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=bool/valid-scalar != null-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, true),
			right:  columnartest.Scalar(t, columnar.KindBool, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=bool/null-scalar != valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, false),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=bool/null-scalar != null-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// Bool (scalar, array) tests
		{
			name:   "type=bool/true-scalar != array",
			left:   columnartest.Scalar(t, columnar.KindBool, true),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
		},
		{
			name:   "type=bool/false-scalar != array",
			left:   columnartest.Scalar(t, columnar.KindBool, false),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
		},
		{
			name:   "type=bool/null-scalar != array",
			left:   columnartest.Scalar(t, columnar.KindBool, nil),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Bool (array, scalar) tests
		{
			name:   "type=bool/array != true-scalar",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, true),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
		},
		{
			name:   "type=bool/array != false-scalar",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, false),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
		},
		{
			name:   "type=bool/array != null-scalar",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Bool (array, array) tests
		{
			name:   "type=bool/array != array",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false, nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, false, true, false, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, true, false, nil, nil, nil),
		},

		// Int64 (scalar, scalar) tests
		{
			name:   "type=int64/equal-scalar != equal-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=int64/scalar != different-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=int64/valid-scalar != null-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=int64/null-scalar != valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=int64/null-scalar != null-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// Int64 (scalar, array) tests
		{
			name:   "type=int64/valid-scalar != array",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
		},
		{
			name:   "type=int64/null-scalar != array",
			left:   columnartest.Scalar(t, columnar.KindInt64, nil),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Int64 (array, scalar) tests
		{
			name:   "type=int64/array != valid-scalar",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
		},
		{
			name:   "type=int64/array != null-scalar",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Int64 (array, array) tests
		{
			name:   "type=int64/array != array",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2), int64(3), int64(4), nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(3), int64(3), int64(5), int64(1), int64(2), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, false, true, nil, nil, nil),
		},

		// Uint64 (scalar, scalar) tests
		{
			name:   "type=uint64/equal-scalar != equal-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=uint64/scalar != different-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=uint64/valid-scalar != null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=uint64/null-scalar != valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=uint64/null-scalar != null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// Uint64 (scalar, array) tests
		{
			name:   "type=uint64/valid-scalar != array",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
		},
		{
			name:   "type=uint64/null-scalar != array",
			left:   columnartest.Scalar(t, columnar.KindUint64, nil),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Uint64 (array, scalar) tests
		{
			name:   "type=uint64/array != valid-scalar",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
		},
		{
			name:   "type=uint64/array != null-scalar",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Uint64 (array, array) tests
		{
			name:   "type=uint64/array != array",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(1), uint64(2), uint64(3), uint64(4), nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(1), uint64(3), uint64(3), uint64(5), uint64(1), uint64(2), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, false, true, nil, nil, nil),
		},

		// UTF8 (scalar, scalar) tests
		{
			name:   "type=utf8/equal-scalar != equal-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "hello"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "hello"),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=utf8/scalar != different-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "hello"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "world"),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=utf8/valid-scalar != null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "hello"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=utf8/null-scalar != valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "world"),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=utf8/null-scalar != null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// UTF8 (scalar, array) tests
		{
			name:   "type=utf8/valid-scalar != array",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "foo"),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
		},
		{
			name:   "type=utf8/null-scalar != array",
			left:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// UTF8 (array, scalar) tests
		{
			name:   "type=utf8/array != valid-scalar",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "bar"),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
		},
		{
			name:   "type=utf8/array != null-scalar",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// UTF8 (array, array) tests
		{
			name:   "type=utf8/array != array",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "a", "b", "c", "d", nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "a", "x", "c", "y", "foo", "bar", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, false, true, nil, nil, nil),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := compute.NotEquals(&alloc, tc.left, tc.right)
			if tc.expectError {
				require.Error(t, err, "invalid function call should result in an error")
				return
			}

			require.NoError(t, err, "valid function call should not result in an error")
			columnartest.RequireDatumsEqual(t, tc.expect, actual)
		})
	}
}

func TestLessThan(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
		expectError bool
	}{
		{
			name:        "fails on mismatched types",
			left:        columnartest.Scalar(t, columnar.KindInt64, int64(0)),
			right:       columnartest.Scalar(t, columnar.KindUint64, uint64(0)),
			expectError: true,
		},
		{
			name:        "fails on mismatch length arrays",
			left:        columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2), int64(3)),
			right:       columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2)),
			expectError: true,
		},
		{
			name:        "fails on bool type",
			left:        columnartest.Scalar(t, columnar.KindBool, true),
			right:       columnartest.Scalar(t, columnar.KindBool, false),
			expectError: true,
		},

		// Int64 (scalar, scalar) tests
		{
			name:   "type=int64/5 < 10",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=int64/10 < 5",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=int64/5 < 5",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=int64/valid-scalar < null-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=int64/null-scalar < valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// Int64 (scalar, array) tests
		{
			name:   "type=int64/valid-scalar < array",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(3), int64(5), int64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, nil),
		},
		{
			name:   "type=int64/null-scalar < array",
			left:   columnartest.Scalar(t, columnar.KindInt64, nil),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Int64 (array, scalar) tests
		{
			name:   "type=int64/array < valid-scalar",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(3), int64(5), int64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, nil),
		},
		{
			name:   "type=int64/array < null-scalar",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Int64 (array, array) tests
		{
			name:   "type=int64/array < array",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(5), int64(10), int64(10), nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(5), int64(5), int64(15), int64(1), int64(2), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, true, nil, nil, nil),
		},

		// Uint64 (scalar, scalar) tests
		{
			name:   "type=uint64/5 < 10",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=uint64/10 < 5",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=uint64/5 < 5",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=uint64/valid-scalar < null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=uint64/null-scalar < valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// Uint64 (scalar, array) tests
		{
			name:   "type=uint64/valid-scalar < array",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(3), uint64(5), uint64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, nil),
		},
		{
			name:   "type=uint64/null-scalar < array",
			left:   columnartest.Scalar(t, columnar.KindUint64, nil),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Uint64 (array, scalar) tests
		{
			name:   "type=uint64/array < valid-scalar",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(3), uint64(5), uint64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, nil),
		},
		{
			name:   "type=uint64/array < null-scalar",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Uint64 (array, array) tests
		{
			name:   "type=uint64/array < array",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(1), uint64(5), uint64(10), uint64(10), nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(5), uint64(5), uint64(15), uint64(1), uint64(2), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, true, nil, nil, nil),
		},

		// UTF8 (scalar, scalar) tests
		{
			name:   "type=utf8/a < b",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "a"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "b"),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=utf8/b < a",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "b"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "a"),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=utf8/a < a",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "a"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "a"),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=utf8/valid-scalar < null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "hello"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=utf8/null-scalar < valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "world"),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// UTF8 (scalar, array) tests
		{
			name:   "type=utf8/valid-scalar < array",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "foo"),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "bar", "foo", "zoo", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, nil),
		},
		{
			name:   "type=utf8/null-scalar < array",
			left:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// UTF8 (array, scalar) tests
		{
			name:   "type=utf8/array < valid-scalar",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "apple", "banana", "cherry", nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "banana"),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, nil),
		},
		{
			name:   "type=utf8/array < null-scalar",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// UTF8 (array, array) tests
		{
			name:   "type=utf8/array < array",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "a", "b", "c", "z", nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "b", "b", "a", "z", "foo", "bar", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, false, nil, nil, nil),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := compute.LessThan(&alloc, tc.left, tc.right)
			if tc.expectError {
				require.Error(t, err, "invalid function call should result in an error")
				return
			}

			require.NoError(t, err, "valid function call should not result in an error")
			columnartest.RequireDatumsEqual(t, tc.expect, actual)
		})
	}
}

func TestLessOrEqual(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
		expectError bool
	}{
		{
			name:        "fails on mismatched types",
			left:        columnartest.Scalar(t, columnar.KindInt64, int64(0)),
			right:       columnartest.Scalar(t, columnar.KindUint64, uint64(0)),
			expectError: true,
		},
		{
			name:        "fails on mismatch length arrays",
			left:        columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2), int64(3)),
			right:       columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2)),
			expectError: true,
		},
		{
			name:        "fails on bool type",
			left:        columnartest.Scalar(t, columnar.KindBool, true),
			right:       columnartest.Scalar(t, columnar.KindBool, false),
			expectError: true,
		},

		// Int64 (scalar, scalar) tests
		{
			name:   "type=int64/5 <= 10",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=int64/10 <= 5",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=int64/5 <= 5",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=int64/valid-scalar <= null-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=int64/null-scalar <= valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// Int64 (scalar, array) tests
		{
			name:   "type=int64/valid-scalar <= array",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(3), int64(5), int64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, true, nil),
		},
		{
			name:   "type=int64/null-scalar <= array",
			left:   columnartest.Scalar(t, columnar.KindInt64, nil),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Int64 (array, scalar) tests
		{
			name:   "type=int64/array <= valid-scalar",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(3), int64(5), int64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, nil),
		},
		{
			name:   "type=int64/array <= null-scalar",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Int64 (array, array) tests
		{
			name:   "type=int64/array <= array",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(5), int64(10), int64(10), nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(5), int64(5), int64(15), int64(1), int64(2), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, true, nil, nil, nil),
		},

		// Uint64 (scalar, scalar) tests
		{
			name:   "type=uint64/5 <= 10",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=uint64/10 <= 5",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=uint64/5 <= 5",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=uint64/valid-scalar <= null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=uint64/null-scalar <= valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// Uint64 (scalar, array) tests
		{
			name:   "type=uint64/valid-scalar <= array",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(3), uint64(5), uint64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, true, nil),
		},
		{
			name:   "type=uint64/null-scalar <= array",
			left:   columnartest.Scalar(t, columnar.KindUint64, nil),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Uint64 (array, scalar) tests
		{
			name:   "type=uint64/array <= valid-scalar",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(3), uint64(5), uint64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, nil),
		},
		{
			name:   "type=uint64/array <= null-scalar",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Uint64 (array, array) tests
		{
			name:   "type=uint64/array <= array",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(1), uint64(5), uint64(10), uint64(10), nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(5), uint64(5), uint64(15), uint64(1), uint64(2), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, true, nil, nil, nil),
		},

		// UTF8 (scalar, scalar) tests
		{
			name:   "type=utf8/a <= b",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "a"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "b"),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=utf8/b <= a",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "b"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "a"),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=utf8/a <= a",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "a"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "a"),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=utf8/valid-scalar <= null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "hello"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=utf8/null-scalar <= valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "world"),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// UTF8 (scalar, array) tests
		{
			name:   "type=utf8/valid-scalar <= array",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "foo"),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "bar", "foo", "zoo", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, true, nil),
		},
		{
			name:   "type=utf8/null-scalar <= array",
			left:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// UTF8 (array, scalar) tests
		{
			name:   "type=utf8/array <= valid-scalar",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "apple", "banana", "cherry", nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "banana"),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, nil),
		},
		{
			name:   "type=utf8/array <= null-scalar",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// UTF8 (array, array) tests
		{
			name:   "type=utf8/array <= array",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "a", "b", "c", "z", nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "b", "b", "a", "z", "foo", "bar", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, true, nil, nil, nil),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := compute.LessOrEqual(&alloc, tc.left, tc.right)
			if tc.expectError {
				require.Error(t, err, "invalid function call should result in an error")
				return
			}

			require.NoError(t, err, "valid function call should not result in an error")
			columnartest.RequireDatumsEqual(t, tc.expect, actual)
		})
	}
}

func TestGreaterThan(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
		expectError bool
	}{
		{
			name:        "fails on mismatched types",
			left:        columnartest.Scalar(t, columnar.KindInt64, int64(0)),
			right:       columnartest.Scalar(t, columnar.KindUint64, uint64(0)),
			expectError: true,
		},
		{
			name:        "fails on mismatch length arrays",
			left:        columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2), int64(3)),
			right:       columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2)),
			expectError: true,
		},
		{
			name:        "fails on bool type",
			left:        columnartest.Scalar(t, columnar.KindBool, true),
			right:       columnartest.Scalar(t, columnar.KindBool, false),
			expectError: true,
		},

		// Int64 (scalar, scalar) tests
		{
			name:   "type=int64/10 > 5",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=int64/5 > 10",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=int64/5 > 5",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=int64/valid-scalar > null-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=int64/null-scalar > valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// Int64 (scalar, array) tests
		{
			name:   "type=int64/valid-scalar > array",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(3), int64(5), int64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, nil),
		},
		{
			name:   "type=int64/null-scalar > array",
			left:   columnartest.Scalar(t, columnar.KindInt64, nil),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Int64 (array, scalar) tests
		{
			name:   "type=int64/array > valid-scalar",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(3), int64(5), int64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, nil),
		},
		{
			name:   "type=int64/array > null-scalar",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Int64 (array, array) tests
		{
			name:   "type=int64/array > array",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(5), int64(10), int64(10), nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(5), int64(5), int64(15), int64(1), int64(2), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false, nil, nil, nil),
		},

		// Uint64 (scalar, scalar) tests
		{
			name:   "type=uint64/10 > 5",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=uint64/5 > 10",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=uint64/5 > 5",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=uint64/valid-scalar > null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=uint64/null-scalar > valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// Uint64 (scalar, array) tests
		{
			name:   "type=uint64/valid-scalar > array",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(3), uint64(5), uint64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, nil),
		},
		{
			name:   "type=uint64/null-scalar > array",
			left:   columnartest.Scalar(t, columnar.KindUint64, nil),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Uint64 (array, scalar) tests
		{
			name:   "type=uint64/array > valid-scalar",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(3), uint64(5), uint64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, nil),
		},
		{
			name:   "type=uint64/array > null-scalar",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Uint64 (array, array) tests
		{
			name:   "type=uint64/array > array",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(5), uint64(10), uint64(10), nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(1), uint64(5), uint64(5), uint64(15), uint64(1), uint64(2), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false, nil, nil, nil),
		},

		// UTF8 (scalar, scalar) tests
		{
			name:   "type=utf8/b > a",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "b"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "a"),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=utf8/a > b",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "a"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "b"),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=utf8/a > a",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "a"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "a"),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=utf8/valid-scalar > null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "hello"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=utf8/null-scalar > valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "world"),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// UTF8 (scalar, array) tests
		{
			name:   "type=utf8/valid-scalar > array",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "foo"),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "bar", "foo", "zoo", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, nil),
		},
		{
			name:   "type=utf8/null-scalar > array",
			left:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// UTF8 (array, scalar) tests
		{
			name:   "type=utf8/array > valid-scalar",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "apple", "banana", "cherry", nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "banana"),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, nil),
		},
		{
			name:   "type=utf8/array > null-scalar",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// UTF8 (array, array) tests
		{
			name:   "type=utf8/array > array",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "b", "b", "c", "z", nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "a", "b", "z", "z", "foo", "bar", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, false, nil, nil, nil),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := compute.GreaterThan(&alloc, tc.left, tc.right)
			if tc.expectError {
				require.Error(t, err, "invalid function call should result in an error")
				return
			}

			require.NoError(t, err, "valid function call should not result in an error")
			columnartest.RequireDatumsEqual(t, tc.expect, actual)
		})
	}
}

func TestGreaterOrEqual(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
		expectError bool
	}{
		{
			name:        "fails on mismatched types",
			left:        columnartest.Scalar(t, columnar.KindInt64, int64(0)),
			right:       columnartest.Scalar(t, columnar.KindUint64, uint64(0)),
			expectError: true,
		},
		{
			name:        "fails on mismatch length arrays",
			left:        columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2), int64(3)),
			right:       columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2)),
			expectError: true,
		},
		{
			name:        "fails on bool type",
			left:        columnartest.Scalar(t, columnar.KindBool, true),
			right:       columnartest.Scalar(t, columnar.KindBool, false),
			expectError: true,
		},

		// Int64 (scalar, scalar) tests
		{
			name:   "type=int64/10 >= 5",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=int64/5 >= 10",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=int64/5 >= 5",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=int64/valid-scalar >= null-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Scalar(t, columnar.KindInt64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=int64/null-scalar >= valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindInt64, nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// Int64 (scalar, array) tests
		{
			name:   "type=int64/valid-scalar >= array",
			left:   columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(3), int64(5), int64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, nil),
		},
		{
			name:   "type=int64/null-scalar >= array",
			left:   columnartest.Scalar(t, columnar.KindInt64, nil),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Int64 (array, scalar) tests
		{
			name:   "type=int64/array >= valid-scalar",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(3), int64(5), int64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, int64(5)),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, true, nil),
		},
		{
			name:   "type=int64/array >= null-scalar",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindInt64, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Int64 (array, array) tests
		{
			name:   "type=int64/array >= array",
			left:   columnartest.Array(t, columnar.KindInt64, &alloc, int64(5), int64(5), int64(10), int64(10), nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(5), int64(5), int64(15), int64(1), int64(2), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, true, false, nil, nil, nil),
		},

		// Uint64 (scalar, scalar) tests
		{
			name:   "type=uint64/10 >= 5",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=uint64/5 >= 10",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=uint64/5 >= 5",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=uint64/valid-scalar >= null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Scalar(t, columnar.KindUint64, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=uint64/null-scalar >= valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindUint64, nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(10)),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// Uint64 (scalar, array) tests
		{
			name:   "type=uint64/valid-scalar >= array",
			left:   columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(3), uint64(5), uint64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, nil),
		},
		{
			name:   "type=uint64/null-scalar >= array",
			left:   columnartest.Scalar(t, columnar.KindUint64, nil),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Uint64 (array, scalar) tests
		{
			name:   "type=uint64/array >= valid-scalar",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(3), uint64(5), uint64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, uint64(5)),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, true, nil),
		},
		{
			name:   "type=uint64/array >= null-scalar",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(10), nil),
			right:  columnartest.Scalar(t, columnar.KindUint64, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// Uint64 (array, array) tests
		{
			name:   "type=uint64/array >= array",
			left:   columnartest.Array(t, columnar.KindUint64, &alloc, uint64(5), uint64(5), uint64(10), uint64(10), nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindUint64, &alloc, uint64(1), uint64(5), uint64(5), uint64(15), uint64(1), uint64(2), nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, true, false, nil, nil, nil),
		},

		// UTF8 (scalar, scalar) tests
		{
			name:   "type=utf8/b >= a",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "b"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "a"),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=utf8/a >= b",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "a"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "b"),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "type=utf8/a >= a",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "a"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "a"),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "type=utf8/valid-scalar >= null-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "hello"),
			right:  columnartest.Scalar(t, columnar.KindUTF8, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "type=utf8/null-scalar >= valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "world"),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// UTF8 (scalar, array) tests
		{
			name:   "type=utf8/valid-scalar >= array",
			left:   columnartest.Scalar(t, columnar.KindUTF8, "foo"),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "bar", "foo", "zoo", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, nil),
		},
		{
			name:   "type=utf8/null-scalar >= array",
			left:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// UTF8 (array, scalar) tests
		{
			name:   "type=utf8/array >= valid-scalar",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "apple", "banana", "cherry", nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, "banana"),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, true, nil),
		},
		{
			name:   "type=utf8/array >= null-scalar",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "foo", "bar", nil),
			right:  columnartest.Scalar(t, columnar.KindUTF8, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// UTF8 (array, array) tests
		{
			name:   "type=utf8/array >= array",
			left:   columnartest.Array(t, columnar.KindUTF8, &alloc, "b", "b", "c", "z", nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindUTF8, &alloc, "a", "b", "z", "z", "foo", "bar", nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, true, nil, nil, nil),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := compute.GreaterOrEqual(&alloc, tc.left, tc.right)
			if tc.expectError {
				require.Error(t, err, "invalid function call should result in an error")
				return
			}

			require.NoError(t, err, "valid function call should not result in an error")
			columnartest.RequireDatumsEqual(t, tc.expect, actual)
		})
	}
}

type equalityFunction func(alloc *memory.Allocator, left, right columnar.Datum) (columnar.Datum, error)

func BenchmarkEqualityFunctions(b *testing.B) {
	var alloc memory.Allocator

	type scenario struct {
		name  string
		fn    equalityFunction
		left  columnar.Array
		right columnar.Array
	}

	var (
		boolLeft, boolRight     = makeBenchBoolArrays(&alloc)
		int64Left, int64Right   = makeBenchInt64Arrays(&alloc)
		uint64Left, uint64Right = makeBenchUint64Arrays(&alloc)
		utf8Left, utf8Right     = makeBenchUTF8Arrays(&alloc)
	)

	scenarios := []scenario{
		{
			name:  "function=Equals/type=bool",
			fn:    compute.Equals,
			left:  boolLeft,
			right: boolRight,
		},
		{
			name:  "function=Equals/type=int64",
			fn:    compute.Equals,
			left:  int64Left,
			right: int64Right,
		},
		{
			name:  "function=Equals/type=uint64",
			fn:    compute.Equals,
			left:  uint64Left,
			right: uint64Right,
		},
		{
			name:  "function=Equals/type=utf8",
			fn:    compute.Equals,
			left:  utf8Left,
			right: utf8Right,
		},
		{
			name:  "function=NotEquals/type=bool",
			fn:    compute.NotEquals,
			left:  boolLeft,
			right: boolRight,
		},
		{
			name:  "function=NotEquals/type=int64",
			fn:    compute.NotEquals,
			left:  int64Left,
			right: int64Right,
		},
		{
			name:  "function=NotEquals/type=uint64",
			fn:    compute.NotEquals,
			left:  uint64Left,
			right: uint64Right,
		},
		{
			name:  "function=NotEquals/type=utf8",
			fn:    compute.NotEquals,
			left:  utf8Left,
			right: utf8Right,
		},
		{
			name:  "function=LessThan/type=int64",
			fn:    compute.LessThan,
			left:  int64Left,
			right: int64Right,
		},
		{
			name:  "function=LessThan/type=uint64",
			fn:    compute.LessThan,
			left:  uint64Left,
			right: uint64Right,
		},
		{
			name:  "function=LessThan/type=utf8",
			fn:    compute.LessThan,
			left:  utf8Left,
			right: utf8Right,
		},
		{
			name:  "function=LessOrEqual/type=int64",
			fn:    compute.LessOrEqual,
			left:  int64Left,
			right: int64Right,
		},
		{
			name:  "function=LessOrEqual/type=uint64",
			fn:    compute.LessOrEqual,
			left:  uint64Left,
			right: uint64Right,
		},
		{
			name:  "function=LessOrEqual/type=utf8",
			fn:    compute.LessOrEqual,
			left:  utf8Left,
			right: utf8Right,
		},
		{
			name:  "function=GreaterThan/type=int64",
			fn:    compute.GreaterThan,
			left:  int64Left,
			right: int64Right,
		},
		{
			name:  "function=GreaterThan/type=uint64",
			fn:    compute.GreaterThan,
			left:  uint64Left,
			right: uint64Right,
		},
		{
			name:  "function=GreaterThan/type=utf8",
			fn:    compute.GreaterThan,
			left:  utf8Left,
			right: utf8Right,
		},
		{
			name:  "function=GreaterOrEqual/type=int64",
			fn:    compute.GreaterOrEqual,
			left:  int64Left,
			right: int64Right,
		},
		{
			name:  "function=GreaterOrEqual/type=uint64",
			fn:    compute.GreaterOrEqual,
			left:  uint64Left,
			right: uint64Right,
		},
		{
			name:  "function=GreaterOrEqual/type=utf8",
			fn:    compute.GreaterOrEqual,
			left:  utf8Left,
			right: utf8Right,
		},
	}

	for _, s := range scenarios {
		b.Run(s.name, func(b *testing.B) {
			tempAlloc := memory.NewAllocator(&alloc)
			for b.Loop() {
				tempAlloc.Reclaim()

				_, _ = s.fn(tempAlloc, s.left, s.right)
			}

			totalValues := s.left.Len() + s.right.Len()
			b.SetBytes(int64(s.left.Size() + s.right.Size()))
			b.ReportMetric(float64(totalValues*b.N)/b.Elapsed().Seconds(), "values/s")
		})
	}
}

func makeBenchBoolArrays(alloc *memory.Allocator) (left, right *columnar.Bool) {
	leftBuilder := columnar.NewBoolBuilder(alloc)
	leftBuilder.Grow(8192)

	rightBuilder := columnar.NewBoolBuilder(alloc)
	rightBuilder.Grow(8192)

	rnd := rand.New(rand.NewSource(0))
	for range 8192 {
		for _, builder := range []*columnar.BoolBuilder{leftBuilder, rightBuilder} {
			if rnd.Intn(50) == 0 { // 2% chance of null
				builder.AppendNull()
				continue
			}
			builder.AppendValue(rnd.Intn(2) == 1)
		}
	}

	return leftBuilder.Build(), rightBuilder.Build()
}

func makeBenchInt64Arrays(alloc *memory.Allocator) (left, right *columnar.Number[int64]) {
	leftBuilder := columnar.NewNumberBuilder[int64](alloc)
	leftBuilder.Grow(8192)

	rightBuilder := columnar.NewNumberBuilder[int64](alloc)
	rightBuilder.Grow(8192)

	rnd := rand.New(rand.NewSource(0))
	for range 8192 {
		for _, builder := range []*columnar.NumberBuilder[int64]{leftBuilder, rightBuilder} {
			if rnd.Intn(50) == 0 { // 2% chance of null
				builder.AppendNull()
				continue
			}
			builder.AppendValue(int64(rnd.Intn(100)))
		}
	}

	return leftBuilder.Build(), rightBuilder.Build()
}

func makeBenchUint64Arrays(alloc *memory.Allocator) (left, right *columnar.Number[uint64]) {
	leftBuilder := columnar.NewNumberBuilder[uint64](alloc)
	leftBuilder.Grow(8192)

	rightBuilder := columnar.NewNumberBuilder[uint64](alloc)
	rightBuilder.Grow(8192)

	rnd := rand.New(rand.NewSource(0))
	for range 8192 {
		for _, builder := range []*columnar.NumberBuilder[uint64]{leftBuilder, rightBuilder} {
			if rnd.Intn(50) == 0 { // 2% chance of null
				builder.AppendNull()
				continue
			}
			builder.AppendValue(uint64(rnd.Intn(100)))
		}
	}

	return leftBuilder.Build(), rightBuilder.Build()
}

func makeBenchUTF8Arrays(alloc *memory.Allocator) (left, right *columnar.UTF8) {
	leftBuilder := columnar.NewUTF8Builder(alloc)
	leftBuilder.Grow(8192)

	rightBuilder := columnar.NewUTF8Builder(alloc)
	rightBuilder.Grow(8192)

	strings := []string{"apple", "banana", "cherry", "date", "elderberry"}

	rnd := rand.New(rand.NewSource(0))
	for range 8192 {
		for _, builder := range []*columnar.UTF8Builder{leftBuilder, rightBuilder} {
			if rnd.Intn(50) == 0 { // 2% chance of null
				builder.AppendNull()
				continue
			}
			builder.AppendValue([]byte(strings[rnd.Intn(len(strings))]))
		}
	}

	return leftBuilder.Build(), rightBuilder.Build()
}
