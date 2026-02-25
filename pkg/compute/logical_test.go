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

func TestNot_Errors(t *testing.T) {
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
}

func TestAnd_Errors(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
	}{
		{
			name:  "fails on non-boolean types",
			left:  columnartest.Scalar(t, columnar.KindInt64, 0),
			right: columnartest.Scalar(t, columnar.KindInt64, 0),
		},
		{
			name:  "fails on mismatch length arrays",
			left:  columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false),
			right: columnartest.Array(t, columnar.KindBool, &alloc, true, false),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compute.And(&alloc, tc.left, tc.right, memory.Bitmap{})
			require.Error(t, err, "invalid function call should result in an error")
		})
	}
}

func TestOr_Errors(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
	}{
		{
			name:  "fails on non-boolean types",
			left:  columnartest.Scalar(t, columnar.KindInt64, 0),
			right: columnartest.Scalar(t, columnar.KindInt64, 0),
		},
		{
			name:  "fails on mismatch length arrays",
			left:  columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false),
			right: columnartest.Array(t, columnar.KindBool, &alloc, true, false),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compute.Or(&alloc, tc.left, tc.right, memory.Bitmap{})
			require.Error(t, err, "invalid function call should result in an error")
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
