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

func TestNot(t *testing.T) {
	t.Run("invalid scalar kind", func(t *testing.T) {
		var alloc memory.Allocator

		res, err := compute.Not(&alloc, new(columnar.NullScalar), memory.Bitmap{})
		require.Error(t, err, "invalid function call should result in an error")
		require.Nil(t, res, "invalid function call should not result in a datum")
	})

	t.Run("invalid array kind", func(t *testing.T) {
		var alloc memory.Allocator

		res, err := compute.Not(&alloc, new(columnar.UTF8), memory.Bitmap{})
		require.Error(t, err, "invalid function call should result in an error")
		require.Nil(t, res, "invalid function call should not result in a datum")
	})

	t.Run("scalar", func(t *testing.T) {
		tt := []struct {
			name   string
			input  *columnar.BoolScalar
			expect *columnar.BoolScalar
		}{
			{
				name:   "true",
				input:  &columnar.BoolScalar{Value: true},
				expect: &columnar.BoolScalar{Value: false},
			},
			{
				name:   "false",
				input:  &columnar.BoolScalar{Value: false},
				expect: &columnar.BoolScalar{Value: true},
			},
			{
				name:   "null",
				input:  &columnar.BoolScalar{Null: true},
				expect: &columnar.BoolScalar{Null: true},
			},
		}

		for _, tc := range tt {
			t.Run(tc.name, func(t *testing.T) {
				var alloc memory.Allocator

				actual, err := compute.Not(&alloc, tc.input, memory.Bitmap{})
				require.NoError(t, err)
				columnartest.RequireDatumsEqual(t, tc.expect, actual)
			})
		}
	})

	t.Run("array", func(t *testing.T) {
		var alloc memory.Allocator

		var (
			input  = columnartest.Array(t, columnar.KindBool, &alloc, true, nil, false)
			expect = columnartest.Array(t, columnar.KindBool, &alloc, false, nil, true)
		)

		actual, err := compute.Not(&alloc, input, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("fully valid array", func(t *testing.T) {
		var alloc memory.Allocator

		var (
			input  = columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
			expect = columnartest.Array(t, columnar.KindBool, &alloc, false, true, false, true)
		)

		actual, err := compute.Not(&alloc, input, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})
}

func TestAnd(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
		expectError bool
	}{
		{
			name:        "fails on non-boolean types",
			left:        columnartest.Scalar(t, columnar.KindInt64, 0),
			right:       columnartest.Scalar(t, columnar.KindInt64, 0),
			expectError: true,
		},
		{
			name:        "fails on mismatch length arrays",
			left:        columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false),
			right:       columnartest.Array(t, columnar.KindBool, &alloc, true, false),
			expectError: true,
		},

		// (scalar, scalar) tests
		{
			name:   "valid-scalar AND null-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, true),
			right:  columnartest.Scalar(t, columnar.KindBool, false),
			expect: columnartest.Scalar(t, columnar.KindBool, false),
		},
		{
			name:   "valid-scalar AND null-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, true),
			right:  columnartest.Scalar(t, columnar.KindBool, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "null-scalar AND valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, false),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "null-scalar AND null-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// (scalar, array) tests
		{
			name:   "true-scalar AND array",
			left:   columnartest.Scalar(t, columnar.KindBool, true),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
		},
		{
			name:   "false-scalar AND array",
			left:   columnartest.Scalar(t, columnar.KindBool, false),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, false, false, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, false, nil),
		},
		{
			name:   "null-scalar AND array",
			left:   columnartest.Scalar(t, columnar.KindBool, nil),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// (array, scalar) tests
		{
			name:   "array AND true-scalar",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, true),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
		},
		{
			name:   "array AND false-scalar",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, false, false, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, false),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, false, nil),
		},
		{
			name:   "array AND null-scalar",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// (array, array) tests
		{
			name:   "array AND array",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false, nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, false, true, true, false, true, false, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, false, nil, nil, nil),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := compute.And(&alloc, tc.left, tc.right, memory.Bitmap{})
			if tc.expectError {
				require.Error(t, err, "invalid function call should result in an error")
				return
			}

			require.NoError(t, err, "valid function call should not result in an error")
			columnartest.RequireDatumsEqual(t, tc.expect, actual)
		})
	}
}

func TestOr(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
		expectError bool
	}{
		{
			name:        "fails on non-boolean types",
			left:        columnartest.Scalar(t, columnar.KindInt64, 0),
			right:       columnartest.Scalar(t, columnar.KindInt64, 0),
			expectError: true,
		},
		{
			name:        "fails on mismatch length arrays",
			left:        columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false),
			right:       columnartest.Array(t, columnar.KindBool, &alloc, true, false),
			expectError: true,
		},

		// (scalar, scalar) tests
		{
			name:   "valid-scalar OR null-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, true),
			right:  columnartest.Scalar(t, columnar.KindBool, false),
			expect: columnartest.Scalar(t, columnar.KindBool, true),
		},
		{
			name:   "valid-scalar OR null-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, true),
			right:  columnartest.Scalar(t, columnar.KindBool, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "null-scalar OR valid-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, false),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},
		{
			name:   "null-scalar OR null-scalar",
			left:   columnartest.Scalar(t, columnar.KindBool, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, nil),
			expect: columnartest.Scalar(t, columnar.KindBool, nil),
		},

		// (scalar, array) tests
		{
			name:   "true-scalar OR array",
			left:   columnartest.Scalar(t, columnar.KindBool, true),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, nil),
		},
		{
			name:   "false-scalar OR array",
			left:   columnartest.Scalar(t, columnar.KindBool, false),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil),
		},
		{
			name:   "null-scalar OR array",
			left:   columnartest.Scalar(t, columnar.KindBool, nil),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// (array, scalar) tests
		{
			name:   "array OR true-scalar",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, true),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, nil),
		},
		{
			name:   "array OR false-scalar",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, false, false, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, false),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, false, nil),
		},
		{
			name:   "array OR null-scalar",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil),
			right:  columnartest.Scalar(t, columnar.KindBool, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil),
		},

		// (array, array) tests
		{
			name:   "array OR array",
			left:   columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false, nil, nil, nil),
			right:  columnartest.Array(t, columnar.KindBool, &alloc, false, true, true, false, true, false, nil),
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, true, false, nil, nil, nil),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := compute.Or(&alloc, tc.left, tc.right, memory.Bitmap{})
			if tc.expectError {
				require.Error(t, err, "invalid function call should result in an error")
				return
			}

			require.NoError(t, err, "valid function call should not result in an error")
			columnartest.RequireDatumsEqual(t, tc.expect, actual)
		})
	}
}

func BenchmarkNot_Array(b *testing.B) {
	var alloc memory.Allocator

	builder := columnar.NewBoolBuilder(&alloc)
	builder.Grow(8192)

	rnd := rand.New(rand.NewSource(0))
	for range 8192 {
		valid := rnd.Intn(2)
		if valid != 1 {
			builder.AppendNull()
			continue
		}
		builder.AppendValue(rnd.Intn(2) == 1)
	}

	input := builder.Build()

	benchAlloc := memory.NewAllocator(&alloc)
	for b.Loop() {
		benchAlloc.Reclaim()

		_, _ = compute.Not(benchAlloc, input, memory.Bitmap{})
	}

	reportArrayBenchMetrics(b, input)
}

func BenchmarkAnd_Array(b *testing.B) {
	var alloc memory.Allocator

	leftBuilder := columnar.NewBoolBuilder(&alloc)
	leftBuilder.Grow(8192)

	rightBuilder := columnar.NewBoolBuilder(&alloc)
	rightBuilder.Grow(8192)

	rnd := rand.New(rand.NewSource(0))
	for range 8192 {
		for _, builder := range []*columnar.BoolBuilder{leftBuilder, rightBuilder} {
			valid := rnd.Intn(2)
			if valid != 1 {
				builder.AppendNull()
				continue
			}
			builder.AppendValue(rnd.Intn(2) == 1)
		}
	}

	var (
		left  = leftBuilder.Build()
		right = rightBuilder.Build()
	)

	benchAlloc := memory.NewAllocator(&alloc)
	for b.Loop() {
		benchAlloc.Reclaim()

		_, _ = compute.And(benchAlloc, left, right, memory.Bitmap{})
	}

	reportArrayBenchMetrics(b, left, right)
}

func BenchmarkOr_Array(b *testing.B) {
	var alloc memory.Allocator

	leftBuilder := columnar.NewBoolBuilder(&alloc)
	leftBuilder.Grow(8192)

	rightBuilder := columnar.NewBoolBuilder(&alloc)
	rightBuilder.Grow(8192)

	rnd := rand.New(rand.NewSource(0))
	for range 8192 {
		for _, builder := range []*columnar.BoolBuilder{leftBuilder, rightBuilder} {
			valid := rnd.Intn(2)
			if valid != 1 {
				builder.AppendNull()
				continue
			}
			builder.AppendValue(rnd.Intn(2) == 1)
		}
	}

	var (
		left  = leftBuilder.Build()
		right = rightBuilder.Build()
	)

	benchAlloc := memory.NewAllocator(&alloc)
	for b.Loop() {
		benchAlloc.Reclaim()

		_, _ = compute.Or(benchAlloc, left, right, memory.Bitmap{})
	}

	reportArrayBenchMetrics(b, left, right)
}

// TestNot_Selection tests the Not operation with various selection patterns
func TestNot_Selection(t *testing.T) {
	var alloc memory.Allocator

	t.Run("all rows selected", func(t *testing.T) {
		input := columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil, true)
		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil, false)

		actual, err := compute.Not(&alloc, input, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("partial selection - some rows selected", func(t *testing.T) {
		input := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
		selection := selectionMask(&alloc, true, false, true, false)
		// Expected: [false, null, false, null] - unselected rows become null
		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, nil, false, nil)

		actual, err := compute.Not(&alloc, input, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("full selection - all true", func(t *testing.T) {
		input := columnartest.Array(t, columnar.KindBool, &alloc, true, false, nil)
		selection := selectionMask(&alloc, true, true, true)
		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, true, nil)

		actual, err := compute.Not(&alloc, input, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("single element selection", func(t *testing.T) {
		input := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true)
		selection := selectionMask(&alloc, false, true, false)
		// Expected: [null, true, null]
		expect := columnartest.Array(t, columnar.KindBool, &alloc, nil, true, nil)

		actual, err := compute.Not(&alloc, input, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("selection with null values", func(t *testing.T) {
		input := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, false, nil)
		selection := selectionMask(&alloc, true, true, false, false)
		// Expected: [false, nil, nil, nil] - unselected rows and input nulls both result in null
		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, nil, nil, nil)

		actual, err := compute.Not(&alloc, input, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})
}

// TestAnd_Selection tests the And operation with various selection patterns
func TestAnd_Selection(t *testing.T) {
	var alloc memory.Allocator

	t.Run("all rows selected", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
		right := columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, false)
		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, false)

		actual, err := compute.And(&alloc, left, right, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("partial selection - some rows selected", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
		right := columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, false)
		selection := selectionMask(&alloc, true, false, true, false)
		// Expected: [true, null, false, null]
		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, false, nil)

		actual, err := compute.And(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("selection with all false bits - no rows selected", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
		right := columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, false)
		selection := selectionMask(&alloc, false, false, false, false)
		expect := columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil, nil)

		actual, err := compute.And(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("full selection - all true", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true)
		right := columnartest.Array(t, columnar.KindBool, &alloc, false, true, true)
		selection := selectionMask(&alloc, true, true, true)
		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, false, true)

		actual, err := compute.And(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("selection with scalar-array combination", func(t *testing.T) {
		left := columnartest.Scalar(t, columnar.KindBool, true)
		right := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
		selection := selectionMask(&alloc, true, false, true, false)
		// Expected: [true, null, true, null]
		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, true, nil)

		actual, err := compute.And(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("selection with array-scalar combination", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
		right := columnartest.Scalar(t, columnar.KindBool, true)
		selection := selectionMask(&alloc, false, true, false, true)
		// Expected: [null, false, null, false]
		expect := columnartest.Array(t, columnar.KindBool, &alloc, nil, false, nil, false)

		actual, err := compute.And(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("selection with null values in array", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, true, false)
		right := columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, nil)
		selection := selectionMask(&alloc, true, true, false, false)
		// Expected: [true, nil, nil, nil]
		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, nil, nil)

		actual, err := compute.And(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})
}

// TestOr_Selection tests the Or operation with various selection patterns
func TestOr_Selection(t *testing.T) {
	var alloc memory.Allocator

	t.Run("all rows selected", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
		right := columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, true)
		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, true)

		actual, err := compute.Or(&alloc, left, right, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("partial selection - some rows selected", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
		right := columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, true)
		selection := selectionMask(&alloc, true, false, true, false)
		// Expected: [true, null, true, null]
		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, true, nil)

		actual, err := compute.Or(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("selection with all false bits - no rows selected", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
		right := columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, true)
		selection := selectionMask(&alloc, false, false, false, false)
		expect := columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil, nil)

		actual, err := compute.Or(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("full selection - all true", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, false, false)
		right := columnartest.Array(t, columnar.KindBool, &alloc, false, true, false)
		selection := selectionMask(&alloc, true, true, true)
		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, true, false)

		actual, err := compute.Or(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("selection with scalar-array combination", func(t *testing.T) {
		left := columnartest.Scalar(t, columnar.KindBool, false)
		right := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
		selection := selectionMask(&alloc, true, false, true, false)
		// Expected: [true, null, true, null]
		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, true, nil)

		actual, err := compute.Or(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("selection with array-scalar combination", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
		right := columnartest.Scalar(t, columnar.KindBool, false)
		selection := selectionMask(&alloc, false, true, false, true)
		// Expected: [null, false, null, false]
		expect := columnartest.Array(t, columnar.KindBool, &alloc, nil, false, nil, false)

		actual, err := compute.Or(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("selection with null values in array", func(t *testing.T) {
		left := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, false, false)
		right := columnartest.Array(t, columnar.KindBool, &alloc, false, true, true, nil)
		selection := selectionMask(&alloc, true, true, false, false)
		// Expected: [true, nil, nil, nil]
		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, nil, nil)

		actual, err := compute.Or(&alloc, left, right, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})
}
