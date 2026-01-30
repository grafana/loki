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

		res, err := compute.Not(&alloc, new(columnar.NullScalar))
		require.Error(t, err, "invalid function call should result in an error")
		require.Nil(t, res, "invalid function call should not result in a datum")
	})

	t.Run("invalid array kind", func(t *testing.T) {
		var alloc memory.Allocator

		res, err := compute.Not(&alloc, new(columnar.UTF8))
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

				actual, err := compute.Not(&alloc, tc.input)
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

		actual, err := compute.Not(&alloc, input)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, actual)
	})

	t.Run("fully valid array", func(t *testing.T) {
		var alloc memory.Allocator

		var (
			input  = columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)
			expect = columnartest.Array(t, columnar.KindBool, &alloc, false, true, false, true)
		)

		actual, err := compute.Not(&alloc, input)
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
			actual, err := compute.And(&alloc, tc.left, tc.right)
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
			actual, err := compute.Or(&alloc, tc.left, tc.right)
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

	tempAlloc := memory.NewAllocator(&alloc)
	for b.Loop() {
		tempAlloc.Reclaim()

		_, _ = compute.Not(tempAlloc, input)
	}

	b.SetBytes(int64(input.Size()))
	b.ReportMetric(float64(input.Len()*b.N)/b.Elapsed().Seconds(), "values/s")
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

	tempAlloc := memory.NewAllocator(&alloc)
	for b.Loop() {
		tempAlloc.Reclaim()

		_, _ = compute.And(tempAlloc, left, right)
	}

	totalValues := left.Len() + right.Len()
	b.SetBytes(int64(left.Size() + right.Size()))
	b.ReportMetric(float64(totalValues*b.N)/b.Elapsed().Seconds(), "values/s")
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

	tempAlloc := memory.NewAllocator(&alloc)
	for b.Loop() {
		tempAlloc.Reclaim()

		_, _ = compute.Or(tempAlloc, left, right)
	}

	totalValues := left.Len() + right.Len()
	b.SetBytes(int64(left.Size() + right.Size()))
	b.ReportMetric(float64(totalValues*b.N)/b.Elapsed().Seconds(), "values/s")
}
