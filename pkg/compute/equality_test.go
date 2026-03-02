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

func TestEquals_Errors(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
	}{
		{
			name:  "fails on mismatched types",
			left:  columnartest.Scalar(t, columnar.KindInt64, int64(0)),
			right: columnartest.Scalar(t, columnar.KindUint64, uint64(0)),
		},
		{
			name:  "fails on mismatch length arrays",
			left:  columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false),
			right: columnartest.Array(t, columnar.KindBool, &alloc, true, false),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compute.Equals(&alloc, tc.left, tc.right, memory.Bitmap{})
			require.Error(t, err, "invalid function call should result in an error")
		})
	}
}

func TestNotEquals_Errors(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
	}{
		{
			name:  "fails on mismatched types",
			left:  columnartest.Scalar(t, columnar.KindInt64, int64(0)),
			right: columnartest.Scalar(t, columnar.KindUint64, uint64(0)),
		},
		{
			name:  "fails on mismatch length arrays",
			left:  columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false),
			right: columnartest.Array(t, columnar.KindBool, &alloc, true, false),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compute.NotEquals(&alloc, tc.left, tc.right, memory.Bitmap{})
			require.Error(t, err, "invalid function call should result in an error")
		})
	}
}

func TestLessThan_Errors(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
	}{
		{
			name:  "fails on mismatched types",
			left:  columnartest.Scalar(t, columnar.KindInt64, int64(0)),
			right: columnartest.Scalar(t, columnar.KindUint64, uint64(0)),
		},
		{
			name:  "fails on mismatch length arrays",
			left:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2), int64(3)),
			right: columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2)),
		},
		{
			name:  "fails on bool type",
			left:  columnartest.Scalar(t, columnar.KindBool, true),
			right: columnartest.Scalar(t, columnar.KindBool, false),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compute.LessThan(&alloc, tc.left, tc.right, memory.Bitmap{})
			require.Error(t, err, "invalid function call should result in an error")
		})
	}
}

func TestLessOrEqual_Errors(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
	}{
		{
			name:  "fails on mismatched types",
			left:  columnartest.Scalar(t, columnar.KindInt64, int64(0)),
			right: columnartest.Scalar(t, columnar.KindUint64, uint64(0)),
		},
		{
			name:  "fails on mismatch length arrays",
			left:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2), int64(3)),
			right: columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2)),
		},
		{
			name:  "fails on bool type",
			left:  columnartest.Scalar(t, columnar.KindBool, true),
			right: columnartest.Scalar(t, columnar.KindBool, false),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compute.LessOrEqual(&alloc, tc.left, tc.right, memory.Bitmap{})
			require.Error(t, err, "invalid function call should result in an error")
		})
	}
}

func TestGreaterThan_Errors(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
	}{
		{
			name:  "fails on mismatched types",
			left:  columnartest.Scalar(t, columnar.KindInt64, int64(0)),
			right: columnartest.Scalar(t, columnar.KindUint64, uint64(0)),
		},
		{
			name:  "fails on mismatch length arrays",
			left:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2), int64(3)),
			right: columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2)),
		},
		{
			name:  "fails on bool type",
			left:  columnartest.Scalar(t, columnar.KindBool, true),
			right: columnartest.Scalar(t, columnar.KindBool, false),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compute.GreaterThan(&alloc, tc.left, tc.right, memory.Bitmap{})
			require.Error(t, err, "invalid function call should result in an error")
		})
	}
}

func TestGreaterOrEqual_Errors(t *testing.T) {
	var alloc memory.Allocator

	tt := []struct {
		name        string
		left, right columnar.Datum
		expect      columnar.Datum
	}{
		{
			name:  "fails on mismatched types",
			left:  columnartest.Scalar(t, columnar.KindInt64, int64(0)),
			right: columnartest.Scalar(t, columnar.KindUint64, uint64(0)),
		},
		{
			name:  "fails on mismatch length arrays",
			left:  columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2), int64(3)),
			right: columnartest.Array(t, columnar.KindInt64, &alloc, int64(1), int64(2)),
		},
		{
			name:  "fails on bool type",
			left:  columnartest.Scalar(t, columnar.KindBool, true),
			right: columnartest.Scalar(t, columnar.KindBool, false),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compute.GreaterOrEqual(&alloc, tc.left, tc.right, memory.Bitmap{})
			require.Error(t, err, "invalid function call should result in an error")
		})
	}
}

type equalityFunction func(alloc *memory.Allocator, left, right columnar.Datum, selection memory.Bitmap) (columnar.Datum, error)

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

				_, _ = s.fn(tempAlloc, s.left, s.right, memory.Bitmap{})
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
